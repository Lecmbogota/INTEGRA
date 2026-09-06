// Package odoo es el conector con la API externa de Odoo (XML-RPC).
//
// Referencia: https://www.odoo.com/documentation/18.0/developer/reference/external_api.html
//
// Nota importante sobre planes: la API externa de Odoo Online solo está
// disponible en planes Custom. En One App Free y Standard el servidor responde
// con un error de acceso. ErrPlanSinAPI detecta ese caso y lo explica.
package odoo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/mdv/integra/internal/odoo/xmlrpc"
)

// ErrPlanSinAPI indica que la instancia no tiene habilitada la API externa.
var ErrPlanSinAPI = errors.New("la API externa no está habilitada en esta instancia (requiere plan Custom en Odoo Online)")

// ErrCredenciales indica que la autenticación fue rechazada.
var ErrCredenciales = errors.New("credenciales rechazadas: revisa base de datos, usuario y API key")

// Config son los datos de conexión a una instancia.
type Config struct {
	URL      string // https://mi-empresa.odoo.com
	Database string
	Username string
	APIKey   string // API key, o contraseña
}

// Client es una sesión autenticada contra una instancia de Odoo.
type Client struct {
	cfg    Config
	common *xmlrpc.Client
	object *xmlrpc.Client
	uid    int64
}

// ConContexto devuelve una copia del cliente cuyas llamadas se cortan cuando
// el contexto se cancela.
//
// Sin esto, apagar el worker no abortaba la llamada en curso a Odoo: el
// proceso se quedaba esperando la respuesta, y una cadena lenta podía superar
// el lease del trabajo y acabar con dos workers creando el mismo pedido.
func (c *Client) ConContexto(ctx context.Context) *Client {
	copia := *c
	copia.common = c.common.ConContexto(ctx)
	copia.object = c.object.ConContexto(ctx)
	return &copia
}

// Connect autentica contra la instancia y devuelve un cliente listo para usar.
func Connect(cfg Config) (*Client, error) {
	base := strings.TrimRight(cfg.URL, "/")
	if base == "" {
		return nil, errors.New("la URL de Odoo está vacía")
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("URL de Odoo inválida: %w", err)
	}

	c := &Client{
		cfg:    cfg,
		common: xmlrpc.New(base + "/xmlrpc/2/common"),
		object: xmlrpc.New(base + "/xmlrpc/2/object"),
	}

	raw, err := c.common.Call("authenticate", cfg.Database, cfg.Username, cfg.APIKey, map[string]interface{}{})
	if err != nil {
		return nil, clasificar(err)
	}

	// Odoo devuelve `false` (no un error) cuando las credenciales no valen.
	uid, ok := raw.(int64)
	if !ok || uid == 0 {
		return nil, ErrCredenciales
	}
	c.uid = uid
	return c, nil
}

// UID es el identificador del usuario autenticado.
func (c *Client) UID() int64 { return c.uid }

// Version devuelve la información de versión del servidor. No requiere autenticación,
// por lo que sirve para comprobar conectividad antes de tener credenciales válidas.
func Version(baseURL string) (map[string]interface{}, error) {
	raw, err := xmlrpc.New(strings.TrimRight(baseURL, "/") + "/xmlrpc/2/common").Call("version")
	if err != nil {
		return nil, clasificar(err)
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("respuesta de versión inesperada: %T", raw)
	}
	return m, nil
}

// ListDatabases intenta enumerar las bases de datos de la instancia.
//
// La mayoría de despliegues, y Odoo Online siempre, arrancan con list_db=False
// por seguridad, así que lo normal es que esto falle. Se intenta igualmente
// porque cuando funciona ahorra el paso manual de buscar el nombre.
func ListDatabases(baseURL string) ([]string, error) {
	raw, err := xmlrpc.New(strings.TrimRight(baseURL, "/") + "/xmlrpc/2/db").Call("list")
	if err != nil {
		return nil, fmt.Errorf("el servidor no permite listar bases de datos (list_db desactivado): %w", err)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("respuesta inesperada al listar bases: %T", raw)
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("el servidor devolvió una lista vacía de bases de datos")
	}
	return out, nil
}

// Execute invoca execute_kw sobre un modelo.
//
//	args   → argumentos posicionales (por ejemplo, el dominio de búsqueda)
//	kwargs → argumentos con nombre (fields, limit, offset, order, context)
func (c *Client) Execute(model, method string, args []interface{}, kwargs map[string]interface{}) (interface{}, error) {
	if args == nil {
		args = []interface{}{}
	}
	if kwargs == nil {
		kwargs = map[string]interface{}{}
	}
	raw, err := c.object.Call("execute_kw",
		c.cfg.Database, c.uid, c.cfg.APIKey,
		model, method, args, kwargs,
	)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", model, method, clasificar(err))
	}
	return raw, nil
}

// SearchRead ejecuta search_read y normaliza el resultado a filas.
func (c *Client) SearchRead(model string, domain []interface{}, fields []string, kwargs map[string]interface{}) ([]Record, error) {
	if domain == nil {
		domain = []interface{}{}
	}
	kw := map[string]interface{}{}
	for k, v := range kwargs {
		kw[k] = v
	}
	if len(fields) > 0 {
		kw["fields"] = toIfaceSlice(fields)
	}

	raw, err := c.Execute(model, "search_read", []interface{}{domain}, kw)
	if err != nil {
		return nil, err
	}
	rows, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s.search_read devolvió %T y se esperaba una lista", model, raw)
	}
	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, Record(m))
		}
	}
	return out, nil
}

// SearchCount cuenta registros que cumplen un dominio.
func (c *Client) SearchCount(model string, domain []interface{}) (int, error) {
	if domain == nil {
		domain = []interface{}{}
	}
	raw, err := c.Execute(model, "search_count", []interface{}{domain}, nil)
	if err != nil {
		return 0, err
	}
	n, ok := raw.(int64)
	if !ok {
		return 0, fmt.Errorf("%s.search_count devolvió %T", model, raw)
	}
	return int(n), nil
}

// ReadGroup agrupa y agrega, evitando traerse miles de filas para contar.
func (c *Client) ReadGroup(model string, domain []interface{}, fields, groupby []string, kwargs map[string]interface{}) ([]Record, error) {
	if domain == nil {
		domain = []interface{}{}
	}
	kw := map[string]interface{}{
		"fields":  toIfaceSlice(fields),
		"groupby": toIfaceSlice(groupby),
	}
	for k, v := range kwargs {
		kw[k] = v
	}
	raw, err := c.Execute(model, "read_group", []interface{}{domain}, kw)
	if err != nil {
		return nil, err
	}
	rows, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s.read_group devolvió %T", model, raw)
	}
	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, Record(m))
		}
	}
	return out, nil
}

// FieldsGet devuelve la definición de los campos de un modelo.
func (c *Client) FieldsGet(model string, attributes []string) (map[string]interface{}, error) {
	raw, err := c.Execute(model, "fields_get",
		[]interface{}{[]interface{}{}},
		map[string]interface{}{"attributes": toIfaceSlice(attributes)},
	)
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s.fields_get devolvió %T", model, raw)
	}
	return m, nil
}

func toIfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// clasificar traduce errores crudos del servidor a errores accionables.
func clasificar(err error) error {
	var f *xmlrpc.Fault
	if errors.As(err, &f) {
		msg := strings.ToLower(f.Message)
		switch {
		case strings.Contains(msg, "not available") && strings.Contains(msg, "api"),
			strings.Contains(msg, "external api"),
			strings.Contains(msg, "subscription"):
			return fmt.Errorf("%w (respuesta del servidor: %s)", ErrPlanSinAPI, f.Message)
		case esErrorDeCredenciales(f.Message):
			return fmt.Errorf("%w (respuesta del servidor: %s)", ErrCredenciales, f.Message)
		}
	}
	return err
}

// esErrorDeCredenciales distingue un rechazo de autenticación real de un
// traceback cualquiera que casualmente contenga la palabra "invalid".
//
// Odoo devuelve los errores de servidor como tracebacks completos de Python.
// Buscar subcadenas sueltas en ellos produce diagnósticos falsos: un
// AttributeError sobre un campo inexistente acababa reportándose como
// "credenciales rechazadas", mandando a quien depura por el camino equivocado.
func esErrorDeCredenciales(mensaje string) bool {
	// Los rechazos de autenticación de Odoo son mensajes cortos, no tracebacks.
	if strings.Contains(mensaje, "Traceback (most recent call last)") {
		return false
	}
	msg := strings.ToLower(mensaje)
	for _, s := range []string{
		"access denied",
		"invalid credentials",
		"authentication failed",
		"wrong login",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
