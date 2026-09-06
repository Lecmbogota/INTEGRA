package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/notificaciones"
	"github.com/mdv/integra/internal/store"
)

// Los destinos de aviso son lo único que separa «Integra avisa» de «Integra
// apunta»: sin ninguno configurado, cada alerta que levanta el planificador se
// guarda en una tabla que nadie lee. Estas pruebas cubren las tres formas de
// romperlo sin darse cuenta.

// servidorDeAvisos monta el servidor con un cifrador de verdad —la
// configuración SMTP se guarda cifrada y sin clave no hay nada que probar— y
// con una sesión de administrador ya firmada.
func servidorDeAvisos(t *testing.T) (*Server, string, *[]notificaciones.ConfigSMTP) {
	t.Helper()
	s, st, ctx := servidorDePrueba(t)

	clave, err := crypto.GenerarClave()
	if err != nil {
		t.Fatal(err)
	}
	bruta, err := base64.StdEncoding.DecodeString(clave)
	if err != nil {
		t.Fatal(err)
	}
	cif, err := crypto.Nuevo(bruta)
	if err != nil {
		t.Fatal(err)
	}
	s.cif = cif

	_, token := cobayaConSesion(t, s, st, ctx, auth.RolAdmin)

	// El envío se intercepta para no necesitar un servidor SMTP y para poder
	// mirar con qué configuración se habría enviado de verdad.
	var enviados []notificaciones.ConfigSMTP
	s.enviarCorreo = func(cfg notificaciones.ConfigSMTP, asunto, cuerpo string) error {
		enviados = append(enviados, cfg)
		return nil
	}
	return s, token, &enviados
}

// comoAdmin firma la petición con la sesión de administrador del servidor.
func comoAdmin(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func guardarDestino(t *testing.T, s *Server, token, cuerpo string) int64 {
	t.Helper()
	w := httptest.NewRecorder()
	r := comoAdmin(httptest.NewRequest(http.MethodPut, "/api/avisos/destinos", strings.NewReader(cuerpo)), token)
	s.guardarDestinoAviso(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("guardar respondió %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.st.BorrarDestino(context.Background(), res.ID) })
	return res.ID
}

func TestLaContrasenaSMTPNoVuelveNuncaAlNavegador(t *testing.T) {
	s, token, _ := servidorDeAvisos(t)
	guardarDestino(t, s, token, `{"nombre":"Operaciones","min_severidad":"error","activo":true,
		"config":{"host":"smtp.ejemplo.com","puerto":587,"usuario":"avisos@mdv.test",
		          "password":"secreta-de-verdad","remitente":"avisos@mdv.test",
		          "destinatarios":["luis@mdv.test"]}}`)

	w := httptest.NewRecorder()
	s.listarDestinosAviso(w, comoAdmin(httptest.NewRequest(http.MethodGet, "/api/avisos/destinos", nil), token))
	if w.Code != http.StatusOK {
		t.Fatalf("listar respondió %d", w.Code)
	}

	cuerpo := w.Body.String()
	// La contraseña, el servidor y el usuario no tienen por qué salir; la que
	// importa de verdad es la contraseña, que da acceso a enviar correo en
	// nombre de la empresa y acabaría en el historial del navegador.
	if strings.Contains(cuerpo, "secreta-de-verdad") {
		t.Error("la contraseña SMTP salió en la respuesta de la API")
	}
	// A quién le llega sí se enseña: es la pregunta que se hace cualquiera al
	// abrir la pantalla.
	if !strings.Contains(cuerpo, "luis@mdv.test") {
		t.Error("no se ven los destinatarios: la pantalla no puede decir a quién avisa")
	}
}

func TestEditarUnDestinoSinTocarLaContrasenaNoLaBorra(t *testing.T) {
	s, token, enviados := servidorDeAvisos(t)
	id := guardarDestino(t, s, token, `{"nombre":"Operaciones","min_severidad":"error","activo":true,
		"config":{"host":"smtp.ejemplo.com","puerto":587,"usuario":"avisos@mdv.test",
		          "password":"secreta-de-verdad","remitente":"avisos@mdv.test",
		          "destinatarios":["luis@mdv.test"]}}`)

	// La pantalla nunca recibe la contraseña, así que al cambiar solo la
	// severidad la manda vacía. Si eso la borrara, el destino dejaría de
	// enviar sin que nadie hubiera tocado el correo, y el silencio se
	// descubriría el día que hiciera falta un aviso.
	cuerpo, err := json.Marshal(map[string]any{
		"id": id, "nombre": "Operaciones", "min_severidad": "warning", "activo": true,
		"config": map[string]any{
			"host": "smtp.ejemplo.com", "puerto": 587, "usuario": "avisos@mdv.test",
			"password": "", "remitente": "avisos@mdv.test",
			"destinatarios": []string{"luis@mdv.test"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.guardarDestinoAviso(w, comoAdmin(httptest.NewRequest(http.MethodPut, "/api/avisos/destinos", strings.NewReader(string(cuerpo))), token))
	if w.Code != http.StatusOK {
		t.Fatalf("editar respondió %d: %s", w.Code, w.Body.String())
	}

	// El correo de prueba es lo que enseña con qué credenciales se enviaría.
	w = httptest.NewRecorder()
	r := comoAdmin(httptest.NewRequest(http.MethodPost, "/api/avisos/destinos/x/probar", nil), token)
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	s.probarDestinoAviso(w, r)

	if len(*enviados) != 1 {
		t.Fatalf("se esperaba un envío de prueba, hubo %d", len(*enviados))
	}
	if (*enviados)[0].Password != "secreta-de-verdad" {
		t.Errorf("editar la severidad borró la contraseña: quedó %q", (*enviados)[0].Password)
	}
}

func TestUnDestinoSinDestinatariosNoSeGuarda(t *testing.T) {
	s, token, _ := servidorDeAvisos(t)

	// Un destino sin nadie a quien avisar se guardaba tan campante y dejaba la
	// pantalla con una fila verde que no manda nada a ninguna parte, que es
	// peor que no tener ninguna.
	w := httptest.NewRecorder()
	s.guardarDestinoAviso(w, comoAdmin(httptest.NewRequest(http.MethodPut, "/api/avisos/destinos",
		strings.NewReader(`{"nombre":"Vacío","min_severidad":"error","activo":true,
			"config":{"host":"smtp.ejemplo.com","remitente":"avisos@mdv.test","destinatarios":[]}}`)), token))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("se esperaba 400 y respondió %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "destinatario") {
		t.Errorf("el error no dice qué falta: %q", w.Body.String())
	}
}

func TestLaComaDeMasNoDejaUnDestinatarioVacio(t *testing.T) {
	s, token, enviados := servidorDeAvisos(t)
	// La pantalla los pide separados por comas y sobra una con facilidad. Un
	// destinatario vacío hace que el servidor SMTP rechace el correo entero.
	id := guardarDestino(t, s, token, `{"nombre":"Con comas","min_severidad":"error","activo":true,
		"config":{"host":"smtp.ejemplo.com","remitente":"avisos@mdv.test",
		          "destinatarios":["luis@mdv.test, ","  ","ana@mdv.test"]}}`)

	w := httptest.NewRecorder()
	r := comoAdmin(httptest.NewRequest(http.MethodPost, "/api/avisos/destinos/x/probar", nil), token)
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	s.probarDestinoAviso(w, r)

	if len(*enviados) != 1 {
		t.Fatalf("se esperaba un envío, hubo %d", len(*enviados))
	}
	quiero := []string{"luis@mdv.test", "ana@mdv.test"}
	if len((*enviados)[0].Destinatarios) != len(quiero) {
		t.Fatalf("destinatarios mal limpiados: %q", (*enviados)[0].Destinatarios)
	}
	for i, d := range quiero {
		if (*enviados)[0].Destinatarios[i] != d {
			t.Errorf("destinatario %d: %q, se esperaba %q", i, (*enviados)[0].Destinatarios[i], d)
		}
	}
}

func TestElCorreoDePruebaQueFallaSeAnotaYNoRompeLaPeticion(t *testing.T) {
	s, token, _ := servidorDeAvisos(t)
	id := guardarDestino(t, s, token, `{"nombre":"Roto","min_severidad":"error","activo":true,
		"config":{"host":"smtp.ejemplo.com","remitente":"avisos@mdv.test",
		          "destinatarios":["luis@mdv.test"]}}`)

	s.enviarCorreo = func(notificaciones.ConfigSMTP, string, string) error {
		return errFalso{}
	}

	w := httptest.NewRecorder()
	r := comoAdmin(httptest.NewRequest(http.MethodPost, "/api/avisos/destinos/x/probar", nil), token)
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	s.probarDestinoAviso(w, r)

	// Responde 200 con ok:false, no un 500: el fallo es del servidor de correo
	// del cliente, y la pantalla tiene que poder enseñar el motivo en vez de
	// un error genérico.
	if w.Code != http.StatusOK {
		t.Fatalf("se esperaba 200 y respondió %d", w.Code)
	}
	var res struct {
		OK      bool   `json:"ok"`
		Mensaje string `json:"mensaje"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Error("un envío fallido se reportó como correcto")
	}
	if res.Mensaje == "" {
		t.Error("no se dice por qué falló: sin el motivo no se puede arreglar")
	}

	// Y queda anotado, para que la pantalla lo enseñe sin volver a probar.
	lista, err := s.st.ListarDestinos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var visto *store.DestinoListado
	for i := range lista {
		if lista[i].ID == id {
			visto = &lista[i]
		}
	}
	if visto == nil {
		t.Fatal("el destino desapareció de la lista")
	}
	if visto.UltimoError == "" {
		t.Error("el fallo no quedó anotado: la pantalla no puede enseñar que está roto")
	}
}

type errFalso struct{}

func (errFalso) Error() string { return "no se pudo conectar con smtp.ejemplo.com" }
