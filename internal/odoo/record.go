package odoo

import (
	"fmt"
	"time"
)

// Record es una fila devuelta por Odoo, sin tipar.
//
// Los accesores de abajo existen por una razón concreta: Odoo representa
// "campo vacío" con el booleano `false`, sea cual sea el tipo declarado del
// campo. Un `default_code` sin rellenar llega como `false`, no como `""`; un
// `categ_id` sin asignar llega como `false`, no como una tupla vacía.
// Cada accesor absorbe ese caso y devuelve el cero del tipo.
type Record map[string]interface{}

// Str devuelve un campo de texto. El `false` de Odoo se traduce a "".
func (r Record) Str(field string) string {
	switch v := r[field].(type) {
	case string:
		return v
	case bool: // false = vacío
		return ""
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// Has indica si el campo trae un valor real (ni ausente, ni `false`, ni "").
func (r Record) Has(field string) bool {
	v, present := r[field]
	if !present || v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

// Float devuelve un campo numérico. Odoo mezcla int y double según el valor.
func (r Record) Float(field string) float64 {
	switch v := r[field].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	default:
		return 0
	}
}

// Int devuelve un campo entero.
func (r Record) Int(field string) int64 {
	switch v := r[field].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// Bool devuelve un campo booleano.
func (r Record) Bool(field string) bool {
	b, _ := r[field].(bool)
	return b
}

// ID es el identificador del registro.
func (r Record) ID() int64 { return r.Int("id") }

// Ref devuelve un campo many2one, que Odoo serializa como [id, "nombre"]
// cuando tiene valor y como `false` cuando no lo tiene.
func (r Record) Ref(field string) (id int64, name string, ok bool) {
	arr, isArr := r[field].([]interface{})
	if !isArr || len(arr) < 2 {
		return 0, "", false
	}
	switch v := arr[0].(type) {
	case int64:
		id = v
	case float64:
		id = int64(v)
	default:
		return 0, "", false
	}
	name, _ = arr[1].(string)
	return id, name, true
}

// RefID devuelve solo el id de un many2one, o 0 si está vacío.
func (r Record) RefID(field string) int64 {
	id, _, _ := r.Ref(field)
	return id
}

// RefName devuelve solo el nombre de un many2one, o "" si está vacío.
func (r Record) RefName(field string) string {
	_, name, _ := r.Ref(field)
	return name
}

// IDs devuelve un campo one2many/many2many como lista de identificadores.
func (r Record) IDs(field string) []int64 {
	arr, ok := r[field].([]interface{})
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case int64:
			out = append(out, v)
		case float64:
			out = append(out, int64(v))
		}
	}
	return out
}

// Time interpreta un campo de fecha/hora. Odoo los envía como texto en UTC
// con el formato "2006-01-02 15:04:05".
func (r Record) Time(field string) (time.Time, bool) {
	if ts, ok := r[field].(time.Time); ok {
		return ts, true
	}
	s := r.Str(field)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}
