// Package xmlrpc implementa un cliente XML-RPC mínimo, orientado al uso
// concreto que hace Odoo de este protocolo.
//
// Existen librerías XML-RPC de terceros para Go, pero todas decodifican hacia
// structs tipados y eso choca de frente con una peculiaridad de Odoo: los
// campos vacíos no llegan como cadena vacía ni como nil, llegan como el
// booleano `false`. Un `description_sale` sin rellenar es `<boolean>0</boolean>`,
// no `<string></string>`. Decodificar eso hacia un `string` revienta.
//
// Por eso este decodificador devuelve siempre `interface{}` y deja la
// interpretación de la unión `false | valor` a la capa de arriba (ver el
// paquete odoo, tipos Str/Float/Int/Ref). Es la única forma de tener un
// conector que no se caiga con un campo vacío.
package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fault es un error devuelto por el servidor dentro de <fault>.
type Fault struct {
	Code    int
	Message string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("xmlrpc fault %d: %s", f.Code, f.Message)
}

// Client habla XML-RPC contra un endpoint concreto.
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// New construye un cliente con un timeout razonable para lecturas masivas.
func New(endpoint string) *Client {
	return &Client{
		Endpoint: endpoint,
		HTTP:     &http.Client{Timeout: 180 * time.Second},
	}
}

// Call invoca un método remoto y devuelve el valor decodificado.
func (c *Client) Call(method string, params ...interface{}) (interface{}, error) {
	body, err := encodeRequest(method, params)
	if err != nil {
		return nil, fmt.Errorf("codificando petición %s: %w", method, err)
	}

	req, err := http.NewRequest(http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("User-Agent", "integra-odoo-client/1.0")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llamando a %s: %w", c.Endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("leyendo respuesta: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "…"
		}
		return nil, fmt.Errorf("HTTP %d desde %s: %s", resp.StatusCode, c.Endpoint, snippet)
	}

	return decodeResponse(raw)
}

// ---------------------------------------------------------------- codificación

func encodeRequest(method string, params []interface{}) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	if err := xml.EscapeText(&buf, []byte(method)); err != nil {
		return nil, err
	}
	buf.WriteString(`</methodName><params>`)
	for _, p := range params {
		buf.WriteString(`<param>`)
		if err := encodeValue(&buf, p); err != nil {
			return nil, err
		}
		buf.WriteString(`</param>`)
	}
	buf.WriteString(`</params></methodCall>`)
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, v interface{}) error {
	buf.WriteString("<value>")
	defer buf.WriteString("</value>")

	switch t := v.(type) {
	case nil:
		buf.WriteString("<nil/>")
	case bool:
		if t {
			buf.WriteString("<boolean>1</boolean>")
		} else {
			buf.WriteString("<boolean>0</boolean>")
		}
	case int:
		fmt.Fprintf(buf, "<int>%d</int>", t)
	case int64:
		fmt.Fprintf(buf, "<int>%d</int>", t)
	case float64:
		fmt.Fprintf(buf, "<double>%s</double>", strconv.FormatFloat(t, 'f', -1, 64))
	case string:
		buf.WriteString("<string>")
		if err := xml.EscapeText(buf, []byte(t)); err != nil {
			return err
		}
		buf.WriteString("</string>")
	case []byte:
		fmt.Fprintf(buf, "<base64>%s</base64>", base64.StdEncoding.EncodeToString(t))
	case time.Time:
		fmt.Fprintf(buf, "<dateTime.iso8601>%s</dateTime.iso8601>", t.Format("20060102T15:04:05"))
	case []interface{}:
		buf.WriteString("<array><data>")
		for _, item := range t {
			if err := encodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteString("</data></array>")
	// Los identificadores de Odoo se manejan como int64 en todo el proyecto,
	// así que las listas de ids llegan aquí como []int64 y []int. Sin estos
	// dos casos, TODA escritura falla: write y los métodos de negocio reciben
	// siempre una lista de ids como primer argumento.
	case []int64:
		buf.WriteString("<array><data>")
		for _, n := range t {
			fmt.Fprintf(buf, "<value><int>%d</int></value>", n)
		}
		buf.WriteString("</data></array>")
	case []int:
		buf.WriteString("<array><data>")
		for _, n := range t {
			fmt.Fprintf(buf, "<value><int>%d</int></value>", n)
		}
		buf.WriteString("</data></array>")
	case []string:
		buf.WriteString("<array><data>")
		for _, s := range t {
			buf.WriteString("<value><string>")
			if err := xml.EscapeText(buf, []byte(s)); err != nil {
				return err
			}
			buf.WriteString("</string></value>")
		}
		buf.WriteString("</data></array>")
	case map[string]string:
		buf.WriteString("<struct>")
		for k, val := range t {
			buf.WriteString("<member><name>")
			if err := xml.EscapeText(buf, []byte(k)); err != nil {
				return err
			}
			buf.WriteString("</name><value><string>")
			if err := xml.EscapeText(buf, []byte(val)); err != nil {
				return err
			}
			buf.WriteString("</string></value></member>")
		}
		buf.WriteString("</struct>")
	case map[string]interface{}:
		buf.WriteString("<struct>")
		for k, val := range t {
			buf.WriteString("<member><name>")
			if err := xml.EscapeText(buf, []byte(k)); err != nil {
				return err
			}
			buf.WriteString("</name>")
			if err := encodeValue(buf, val); err != nil {
				return err
			}
			buf.WriteString("</member>")
		}
		buf.WriteString("</struct>")
	default:
		return fmt.Errorf("tipo no soportado por el codificador XML-RPC: %T", v)
	}
	return nil
}

// ---------------------------------------------------------------- decodificación

func decodeResponse(raw []byte) (interface{}, error) {
	d := &decoder{x: xml.NewDecoder(bytes.NewReader(raw))}

	for {
		tok, err := d.x.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("respuesta XML-RPC sin <params> ni <fault>")
		}
		if err != nil {
			return nil, fmt.Errorf("XML mal formado: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch start.Name.Local {
		case "fault":
			v, err := d.firstValue()
			if err != nil {
				return nil, err
			}
			return nil, asFault(v)
		case "params":
			v, err := d.firstValue()
			if err != nil {
				return nil, err
			}
			return v, nil
		}
	}
}

func asFault(v interface{}) error {
	m, ok := v.(map[string]interface{})
	if !ok {
		return &Fault{Message: fmt.Sprint(v)}
	}
	f := &Fault{}
	if c, ok := m["faultCode"].(int64); ok {
		f.Code = int(c)
	}
	switch s := m["faultString"].(type) {
	case string:
		f.Message = strings.TrimSpace(s)
	default:
		f.Message = fmt.Sprint(m["faultString"])
	}
	return f
}

type decoder struct{ x *xml.Decoder }

// firstValue avanza hasta el primer <value> y lo decodifica.
func (d *decoder) firstValue() (interface{}, error) {
	for {
		tok, err := d.x.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "value" {
			return d.value()
		}
	}
}

// value decodifica el contenido de un <value> cuyo StartElement ya se consumió.
//
// Según la especificación, un <value> sin elemento de tipo es una cadena.
// Odoo se apoya en esto, así que no es un caso teórico.
func (d *decoder) value() (interface{}, error) {
	var text []byte
	for {
		tok, err := d.x.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text = append(text, t...)
		case xml.StartElement:
			v, err := d.typed(t)
			if err != nil {
				return nil, err
			}
			if err := d.closeElement("value"); err != nil {
				return nil, err
			}
			return v, nil
		case xml.EndElement:
			if t.Name.Local == "value" {
				return string(text), nil
			}
			return nil, fmt.Errorf("</%s> inesperado dentro de <value>", t.Name.Local)
		}
	}
}

// typed decodifica un elemento de tipo, consumiendo su etiqueta de cierre.
func (d *decoder) typed(start xml.StartElement) (interface{}, error) {
	switch start.Name.Local {
	case "string":
		s, err := d.text(start.Name.Local)
		return s, err

	case "int", "i4", "i8":
		s, err := d.text(start.Name.Local)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("entero XML-RPC inválido %q: %w", s, err)
		}
		return n, nil

	case "double":
		s, err := d.text(start.Name.Local)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("double XML-RPC inválido %q: %w", s, err)
		}
		return f, nil

	case "boolean":
		s, err := d.text(start.Name.Local)
		if err != nil {
			return nil, err
		}
		return strings.TrimSpace(s) == "1", nil

	case "dateTime.iso8601":
		s, err := d.text(start.Name.Local)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		for _, layout := range []string{"20060102T15:04:05", "2006-01-02T15:04:05", time.RFC3339} {
			if ts, err := time.Parse(layout, s); err == nil {
				return ts, nil
			}
		}
		return s, nil // se devuelve crudo antes que perder el dato

	case "base64":
		s, err := d.text(start.Name.Local)
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("base64 inválido: %w", err)
		}
		return b, nil

	case "nil":
		return nil, d.closeElement("nil")

	case "array":
		return d.array()

	case "struct":
		return d.structure()

	default:
		// Tipo desconocido: se descarta el subárbol en vez de abortar.
		return nil, d.x.Skip()
	}
}

func (d *decoder) array() (interface{}, error) {
	out := []interface{}{}
	for {
		tok, err := d.x.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "data":
				// se continúa: los <value> cuelgan de aquí
			case "value":
				v, err := d.value()
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			default:
				if err := d.x.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "array" {
				return out, nil
			}
		}
	}
}

func (d *decoder) structure() (interface{}, error) {
	out := map[string]interface{}{}
	var key string
	for {
		tok, err := d.x.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "member":
				key = ""
			case "name":
				s, err := d.text("name")
				if err != nil {
					return nil, err
				}
				key = strings.TrimSpace(s)
			case "value":
				v, err := d.value()
				if err != nil {
					return nil, err
				}
				out[key] = v
			default:
				if err := d.x.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "struct" {
				return out, nil
			}
		}
	}
}

// text acumula el contenido textual hasta el cierre de name.
func (d *decoder) text(name string) (string, error) {
	var sb strings.Builder
	for {
		tok, err := d.x.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			if t.Name.Local == name {
				return sb.String(), nil
			}
		case xml.StartElement:
			if err := d.x.Skip(); err != nil {
				return "", err
			}
		}
	}
}

// closeElement consume tokens hasta cerrar name.
func (d *decoder) closeElement(name string) error {
	for {
		tok, err := d.x.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == name {
				return nil
			}
		case xml.StartElement:
			if err := d.x.Skip(); err != nil {
				return err
			}
		}
	}
}
