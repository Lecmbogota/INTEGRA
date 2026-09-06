package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// Integraciones administra las conexiones a Odoo desde la interfaz.
//
// Mismas reglas que en el CLI: la clave se comprueba ANTES de guardarla, se
// cifra antes de tocar disco y nunca vuelve a la interfaz. Solo una conexión
// está activa: Integra sincroniza contra un único Odoo a la vez.

func (s *Server) registrarIntegraciones(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integraciones", s.listarIntegraciones)
	mux.HandleFunc("POST /api/integraciones", s.crearIntegracion)
	mux.HandleFunc("PATCH /api/integraciones/{id}", s.editarIntegracion)
	mux.HandleFunc("POST /api/integraciones/{id}/probar", s.probarIntegracion)
	mux.HandleFunc("POST /api/integraciones/{id}/activar", s.activarIntegracion)
	mux.HandleFunc("DELETE /api/integraciones/{id}", s.borrarIntegracion)
}

func (s *Server) listarIntegraciones(w http.ResponseWriter, r *http.Request) {
	cs, err := s.st.ResumenConexiones(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if cs == nil {
		cs = []store.ConexionResumen{}
	}
	escribir(w, http.StatusOK, cs)
}

// crearIntegracion comprueba la credencial contra Odoo y solo si funciona la
// cifra y la guarda. La conexión nueva pasa a ser la activa.
func (s *Server) crearIntegracion(w http.ResponseWriter, r *http.Request) {
	var cuerpo struct {
		Nombre   string `json:"nombre"`
		URL      string `json:"url"`
		Database string `json:"database"`
		Usuario  string `json:"usuario"`
		APIKey   string `json:"api_key"`
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	cuerpo.URL = normalizarURL(cuerpo.URL)
	cuerpo.Database = strings.TrimSpace(cuerpo.Database)
	cuerpo.Usuario = strings.TrimSpace(cuerpo.Usuario)
	cuerpo.APIKey = strings.TrimSpace(cuerpo.APIKey)
	if cuerpo.URL == "" || cuerpo.Database == "" || cuerpo.Usuario == "" || cuerpo.APIKey == "" {
		escribir(w, http.StatusBadRequest,
			map[string]string{"error": "URL, base de datos, usuario y API key son obligatorios"})
		return
	}
	if cuerpo.Timezone == "" {
		cuerpo.Timezone = "America/Bogota"
	}
	if _, err := time.LoadLocation(cuerpo.Timezone); err != nil {
		escribir(w, http.StatusUnprocessableEntity,
			map[string]string{"error": fmt.Sprintf("zona horaria inválida: %s", cuerpo.Timezone)})
		return
	}
	if cuerpo.Nombre == "" {
		cuerpo.Nombre = nombrePorDefecto(cuerpo.URL, cuerpo.Database)
	}

	cli, err := odoo.Connect(odoo.Config{
		URL: cuerpo.URL, Database: cuerpo.Database,
		Username: cuerpo.Usuario, APIKey: cuerpo.APIKey,
	})
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "no se pudo conectar: " + err.Error()})
		return
	}

	cifrada, err := s.cif.CifrarTexto(cuerpo.APIKey)
	if err != nil {
		s.fallo(w, err)
		return
	}
	id, err := s.st.GuardarConexionOdoo(r.Context(), cuerpo.Nombre, cuerpo.URL,
		cuerpo.Database, cuerpo.Usuario, cifrada, cuerpo.Timezone)
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "create", "odoo_connections", strconv.FormatInt(id, 10), nil,
		map[string]any{"nombre": cuerpo.Nombre, "url": cuerpo.URL, "database": cuerpo.Database,
			"usuario": cuerpo.Usuario, "timezone": cuerpo.Timezone, "api_key": sinCredencial})
	escribir(w, http.StatusOK, map[string]any{
		"estado": "conectada y guardada como activa", "id": id, "uid": cli.UID(),
	})
}

// editarIntegracion cambia los datos de una conexión. La API key es opcional:
// vacía u omitida conserva la actual. Si cambia algo que afecta la
// autenticación, la combinación resultante se prueba antes de guardar.
func (s *Server) editarIntegracion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	var cuerpo struct {
		Nombre   *string `json:"nombre"`
		URL      *string `json:"url"`
		Database *string `json:"database"`
		Usuario  *string `json:"usuario"`
		APIKey   *string `json:"api_key"`
		Timezone *string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}

	actual, err := s.st.ConexionOdooPorID(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	nombre, base, db, usuario, tz := actual.Nombre, actual.BaseURL, actual.Database, actual.Username, actual.Timezone
	if cuerpo.Nombre != nil && strings.TrimSpace(*cuerpo.Nombre) != "" {
		nombre = strings.TrimSpace(*cuerpo.Nombre)
	}
	if cuerpo.URL != nil {
		base = normalizarURL(*cuerpo.URL)
	}
	if cuerpo.Database != nil {
		db = strings.TrimSpace(*cuerpo.Database)
	}
	if cuerpo.Usuario != nil {
		usuario = strings.TrimSpace(*cuerpo.Usuario)
	}
	if cuerpo.Timezone != nil && strings.TrimSpace(*cuerpo.Timezone) != "" {
		z := strings.TrimSpace(*cuerpo.Timezone)
		if _, err := time.LoadLocation(z); err != nil {
			escribir(w, http.StatusUnprocessableEntity,
				map[string]string{"error": fmt.Sprintf("zona horaria inválida: %s", z)})
			return
		}
		tz = z
	}
	claveNueva := ""
	if cuerpo.APIKey != nil {
		claveNueva = strings.TrimSpace(*cuerpo.APIKey)
	}

	// Cambió la URL, la base, el usuario o la clave: se prueba la combinación
	// resultante antes de guardar, igual que al crear.
	if cuerpo.URL != nil || cuerpo.Database != nil || cuerpo.Usuario != nil || claveNueva != "" {
		claveParaProbar := claveNueva
		if claveParaProbar == "" {
			claro, err := s.cif.DescifrarTexto(actual.APIKeyCifrada)
			if err != nil {
				s.fallo(w, err)
				return
			}
			claveParaProbar = claro
		}
		if _, err := odoo.Connect(odoo.Config{
			URL: base, Database: db, Username: usuario, APIKey: claveParaProbar,
		}); err != nil {
			escribir(w, http.StatusUnprocessableEntity,
				map[string]string{"error": "no se pudo conectar con los datos nuevos: " + err.Error()})
			return
		}
	}

	var claveCifrada []byte // nil: conservar la guardada
	if claveNueva != "" {
		claveCifrada, err = s.cif.CifrarTexto(claveNueva)
		if err != nil {
			s.fallo(w, err)
			return
		}
	}
	if err := s.st.ActualizarConexionOdoo(r.Context(), id, nombre, base, db, usuario, tz, claveCifrada); err != nil {
		s.fallo(w, err)
		return
	}
	despues := map[string]any{"nombre": nombre, "url": base, "database": db,
		"usuario": usuario, "timezone": tz}
	if claveNueva != "" {
		despues["api_key"] = sinCredencial
	}
	s.auditar(r, "update", "odoo_connections", strconv.FormatInt(id, 10),
		datosConexion(actual), despues)
	escribir(w, http.StatusOK, map[string]string{"estado": "guardada"})
}

// probarIntegracion autentica contra Odoo con la credencial guardada.
func (s *Server) probarIntegracion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	c, err := s.st.ConexionOdooPorID(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	claro, err := s.cif.DescifrarTexto(c.APIKeyCifrada)
	if err != nil {
		s.fallo(w, err)
		return
	}
	cli, err := odoo.Connect(odoo.Config{
		URL: c.BaseURL, Database: c.Database, Username: c.Username, APIKey: claro,
	})
	if err != nil {
		escribir(w, http.StatusOK, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]any{
		"ok": true, "mensaje": fmt.Sprintf("autenticado (uid=%d)", cli.UID()),
	})
}

// activarIntegracion convierte una conexión en la activa. Se prueba antes:
// activar una credencial rota dejaría el sync ciego sin que nadie se entere.
func (s *Server) activarIntegracion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	c, err := s.st.ConexionOdooPorID(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	claro, err := s.cif.DescifrarTexto(c.APIKeyCifrada)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if _, err := odoo.Connect(odoo.Config{
		URL: c.BaseURL, Database: c.Database, Username: c.Username, APIKey: claro,
	}); err != nil {
		escribir(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "no se activa: " + err.Error()})
		return
	}
	if err := s.st.ActivarConexionOdoo(r.Context(), id); err != nil {
		s.fallo(w, err)
		return
	}
	// Cambiar de Odoo cambia el catálogo entero de debajo de los canales: es
	// lo más gordo que se puede hacer desde la interfaz.
	s.auditar(r, "activate", "odoo_connections", strconv.FormatInt(id, 10), nil, datosConexion(c))
	escribir(w, http.StatusOK, map[string]string{"estado": "activada"})
}

// borrarIntegracion elimina una conexión inactiva y su catálogo. La activa no
// se puede borrar: primero hay que activar otra.
func (s *Server) borrarIntegracion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	antes, _ := s.st.ConexionOdooPorID(r.Context(), id)
	n, err := s.st.BorrarConexion(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	// Borrar una conexión se lleva por delante su catálogo: el número de
	// productos arrastrados forma parte de lo que hay que poder explicar.
	s.auditar(r, "delete", "odoo_connections", strconv.FormatInt(id, 10),
		datosConexion(antes), map[string]any{"productos_borrados": n})
	escribir(w, http.StatusOK, map[string]any{
		"estado": "borrada", "productos_borrados": n,
	})
}

// normalizarURL limpia la URL de la instancia: sin espacios alrededor ni
// barra final, que rompen la construcción del endpoint XML-RPC.
func normalizarURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}
	if _, err := url.Parse(u); err != nil {
		return u
	}
	return strings.TrimRight(u, "/")
}

func nombrePorDefecto(baseURL, db string) string {
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Host + " · " + db
	}
	return db
}

// datosConexion arma lo que de una conexión se puede guardar en la auditoría:
// todo menos la API key, que está cifrada en la base y ahí se queda.
func datosConexion(c *store.ConexionOdoo) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"nombre": c.Nombre, "url": c.BaseURL, "database": c.Database,
		"usuario": c.Username, "timezone": c.Timezone,
	}
}
