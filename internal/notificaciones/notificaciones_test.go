package notificaciones

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/store"
)

type almacenFalso struct {
	destinos    []store.DestinoNotificacion
	pendientes  []store.AlertaPendiente
	minPedida   string
	notificadas []int64
	ultimoErr   error
	anotados    int
}

func (a *almacenFalso) DestinosActivos(context.Context) ([]store.DestinoNotificacion, error) {
	return a.destinos, nil
}

func (a *almacenFalso) AlertasSinNotificar(_ context.Context, min string, _ int) ([]store.AlertaPendiente, error) {
	a.minPedida = min
	return a.pendientes, nil
}

func (a *almacenFalso) MarcarAlertasNotificadas(_ context.Context, ids []int64) error {
	a.notificadas = append(a.notificadas, ids...)
	return nil
}

func (a *almacenFalso) AnotarEnvioNotificacion(_ context.Context, _ int64, err error) error {
	a.anotados++
	a.ultimoErr = err
	return nil
}

func montar(t *testing.T, al *almacenFalso, env Enviador) *Servicio {
	t.Helper()
	clave, err := crypto.GenerarClave()
	if err != nil {
		t.Fatal(err)
	}
	cif, err := crypto.DesdeBase64(clave)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(ConfigSMTP{
		Host: "smtp.example", Puerto: 587, Usuario: "u", Password: "p",
		Remitente: "integra@mdv.com", Destinatarios: []string{"ops@mdv.com"},
	})
	enc, err := cif.CifrarTexto(string(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for i := range al.destinos {
		al.destinos[i].ConfigEnc = enc
	}
	return nuevoCon(al, cif, nil, env)
}

func TestSeMandaUnSoloCorreoConTodasLasAlertas(t *testing.T) {
	al := &almacenFalso{
		destinos: []store.DestinoNotificacion{{ID: 1, Tipo: "email", Nombre: "ops", MinSeveridad: "error"}},
		pendientes: []store.AlertaPendiente{
			{ID: 10, Severidad: "error", Mensaje: "3 pedidos no se pudieron crear en Odoo"},
			{ID: 11, Severidad: "critical", Mensaje: "el token de MercadoLibre caducó"},
		},
	}
	var envios int
	var asunto, cuerpo string
	s := montar(t, al, func(_ ConfigSMTP, a, c string) error {
		envios++
		asunto, cuerpo = a, c
		return nil
	})

	if err := s.Despachar(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Un correo por alerta llenaría la bandeja en un incidente y entrenaría a
	// la gente a ignorarlos.
	if envios != 1 {
		t.Errorf("se hicieron %d envíos; las alertas van agrupadas en uno", envios)
	}
	if !strings.Contains(asunto, "2 avisos") || !strings.Contains(asunto, "critical") {
		t.Errorf("el asunto debe resumir cuántas y la peor severidad: %q", asunto)
	}
	for _, esperado := range []string{"pedidos no se pudieron crear", "token de MercadoLibre"} {
		if !strings.Contains(cuerpo, esperado) {
			t.Errorf("falta %q en el cuerpo", esperado)
		}
	}
	if len(al.notificadas) != 2 {
		t.Errorf("se sellaron %d alertas, quería 2", len(al.notificadas))
	}
}

func TestSiElCorreoFallaLasAlertasNoSeSellan(t *testing.T) {
	al := &almacenFalso{
		destinos:   []store.DestinoNotificacion{{ID: 1, Tipo: "email", Nombre: "ops", MinSeveridad: "error"}},
		pendientes: []store.AlertaPendiente{{ID: 10, Severidad: "error", Mensaje: "algo pasó"}},
	}
	s := montar(t, al, func(ConfigSMTP, string, string) error {
		return errors.New("conexión rechazada")
	})

	if err := s.Despachar(context.Background()); err != nil {
		t.Fatalf("un destino roto no puede tumbar la pasada del planificador: %v", err)
	}
	// Sellarlas antes del envío convierte un fallo de correo en alertas que
	// nadie ve nunca.
	if len(al.notificadas) != 0 {
		t.Error("no se pueden dar por notificadas alertas que no salieron")
	}
	if al.ultimoErr == nil {
		t.Error("el fallo debe quedar anotado en el destino, o falla en silencio para siempre")
	}
}

func TestSinDestinosNoEsUnError(t *testing.T) {
	al := &almacenFalso{}
	s := montar(t, al, func(ConfigSMTP, string, string) error {
		t.Fatal("no se debía enviar nada")
		return nil
	})
	if err := s.Despachar(context.Background()); err != nil {
		t.Errorf("no configurar destinos es lo normal al principio: %v", err)
	}
}

func TestSeRespetaLaSeveridadMinimaDelDestino(t *testing.T) {
	al := &almacenFalso{
		destinos:   []store.DestinoNotificacion{{ID: 1, Tipo: "email", MinSeveridad: "critical"}},
		pendientes: []store.AlertaPendiente{{ID: 1, Severidad: "critical", Mensaje: "x"}},
	}
	s := montar(t, al, func(ConfigSMTP, string, string) error { return nil })
	if err := s.Despachar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if al.minPedida != "critical" {
		t.Errorf("se pidió mínimo %q; mandar avisos informativos entrena a ignorarlos", al.minPedida)
	}
}

func TestLosDestinosNoImplementadosSeSaltan(t *testing.T) {
	al := &almacenFalso{
		destinos:   []store.DestinoNotificacion{{ID: 1, Tipo: "telegram", MinSeveridad: "error"}},
		pendientes: []store.AlertaPendiente{{ID: 1, Severidad: "error", Mensaje: "x"}},
	}
	s := montar(t, al, func(ConfigSMTP, string, string) error {
		t.Fatal("telegram no está implementado: no puede fingir que salió")
		return nil
	})
	if err := s.Despachar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(al.notificadas) != 0 {
		t.Error("no se puede sellar como notificada una alerta que no se mandó a ningún sitio")
	}
}

func TestUnaConfiguracionIncompletaNoTumbaElResto(t *testing.T) {
	al := &almacenFalso{
		destinos: []store.DestinoNotificacion{
			{ID: 1, Tipo: "email", Nombre: "roto", MinSeveridad: "error"},
			{ID: 2, Tipo: "email", Nombre: "bueno", MinSeveridad: "error"},
		},
		pendientes: []store.AlertaPendiente{{ID: 1, Severidad: "error", Mensaje: "x"}},
	}
	s := montar(t, al, func(ConfigSMTP, string, string) error { return nil })
	// El primero queda con configuración ilegible.
	al.destinos[0].ConfigEnc = []byte("no es json cifrado")

	if err := s.Despachar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(al.notificadas) == 0 {
		t.Error("el destino sano tenía que recibir aunque el otro esté roto")
	}
}

func TestElMensajeEscapaElPuntoInicialDeLinea(t *testing.T) {
	// Un punto solo al principio de línea termina el mensaje en SMTP: sin
	// escaparlo, el correo llega cortado por la mitad.
	msg := construirMensaje(ConfigSMTP{
		Remitente: "a@b.com", Destinatarios: []string{"c@d.com"},
	}, "Asunto", "primera línea\n.punto peligroso\núltima")

	if !strings.Contains(string(msg), "\n..punto peligroso") {
		t.Errorf("el punto inicial no se duplicó:\n%s", msg)
	}
}

func TestElAsuntoConAcentosSeCodifica(t *testing.T) {
	msg := string(construirMensaje(ConfigSMTP{
		Remitente: "a@b.com", Destinatarios: []string{"c@d.com"},
	}, "Publicación con í y ñ", "cuerpo"))

	linea := ""
	for _, l := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(l, "Subject:") {
			linea = l
		}
	}
	if strings.Contains(linea, "Publicación") {
		t.Errorf("el asunto va sin codificar y llegará roto: %q", linea)
	}
	if !strings.Contains(linea, "=?utf-8?") {
		t.Errorf("falta la codificación del asunto: %q", linea)
	}
}
