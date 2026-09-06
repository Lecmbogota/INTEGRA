package odoo

import (
	"fmt"
)

// Este fichero da al cliente lo único que le faltaba para cerrar el ciclo:
// escribir. Hasta ahora Integra solo leía de Odoo, así que era imposible
// crear el pedido de venta de una orden que llegó de un canal.
//
// Toda escritura es explícita y acotada: no hay un método genérico que
// permita escribir cualquier modelo por accidente desde la interfaz.

// Create crea un registro y devuelve su identificador.
func (c *Client) Create(model string, valores map[string]interface{}) (int64, error) {
	raw, err := c.Execute(model, "create", []interface{}{valores}, nil)
	if err != nil {
		return 0, fmt.Errorf("creando %s: %w", model, err)
	}
	id, ok := aEntero(raw)
	if !ok {
		return 0, fmt.Errorf("creando %s: Odoo devolvió %T en vez de un identificador", model, raw)
	}
	return id, nil
}

// Write actualiza registros existentes.
func (c *Client) Write(model string, ids []int64, valores map[string]interface{}) error {
	return c.WriteCon(model, ids, valores, nil)
}

// WriteCon actualiza pasando un contexto de Odoo.
//
// El contexto no es un adorno: hay campos que solo son escribibles con la
// bandera adecuada. El caso que lo motivó es el ajuste de inventario —
// stock.quant solo acepta inventory_quantity_auto_apply con
// {"inventory_mode": true}, y sin él la escritura no falla pero tampoco surte
// efecto, que es el peor de los dos comportamientos posibles.
func (c *Client) WriteCon(model string, ids []int64, valores map[string]interface{}, contexto map[string]interface{}) error {
	if len(ids) == 0 {
		return nil
	}
	var kwargs map[string]interface{}
	if len(contexto) > 0 {
		kwargs = map[string]interface{}{"context": contexto}
	}
	_, err := c.Execute(model, "write", []interface{}{ids, valores}, kwargs)
	if err != nil {
		return fmt.Errorf("actualizando %s: %w", model, err)
	}
	return nil
}

// CreateCon crea un registro pasando un contexto de Odoo.
func (c *Client) CreateCon(model string, valores map[string]interface{}, contexto map[string]interface{}) (int64, error) {
	var kwargs map[string]interface{}
	if len(contexto) > 0 {
		kwargs = map[string]interface{}{"context": contexto}
	}
	raw, err := c.Execute(model, "create", []interface{}{valores}, kwargs)
	if err != nil {
		return 0, fmt.Errorf("creando %s: %w", model, err)
	}
	id, ok := aEntero(raw)
	if !ok {
		return 0, fmt.Errorf("creando %s: Odoo devolvió %T en vez de un identificador", model, raw)
	}
	return id, nil
}

// CallMethod invoca un método de negocio sobre registros concretos, como
// action_confirm en un pedido. Odoo bloquea por RPC los métodos que empiezan
// por guion bajo, así que solo sirve para la API pública del modelo.
func (c *Client) CallMethod(model, metodo string, ids []int64, extra ...interface{}) (interface{}, error) {
	args := append([]interface{}{ids}, extra...)
	raw, err := c.Execute(model, metodo, args, nil)
	if err != nil {
		return nil, fmt.Errorf("llamando %s.%s: %w", model, metodo, err)
	}
	return raw, nil
}

// BuscarUno devuelve el identificador del primer registro que cumple el
// dominio, o 0 si no hay ninguno. Es la pieza que hace idempotentes las
// creaciones: antes de crear, se comprueba si ya existe.
func (c *Client) BuscarUno(model string, dominio []interface{}) (int64, error) {
	filas, err := c.SearchRead(model, dominio, []string{"id"},
		map[string]interface{}{"limit": 1})
	if err != nil {
		return 0, err
	}
	if len(filas) == 0 {
		return 0, nil
	}
	return filas[0].ID(), nil
}

func aEntero(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
