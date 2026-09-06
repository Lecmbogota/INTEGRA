package main

import (
	"fmt"

	"github.com/mdv/integra/internal/odoo"
)

// Viabilidad responde dos preguntas que deciden la arquitectura:
// si Odoo permite calcular el precio de tarifa por API, y cuántos productos
// son publicables hoy sin tocar nada.
type Viabilidad struct {
	PrecioPorTarifa    string        `json:"calculo_de_precio_por_tarifa"`
	MuestraPrecios     []odoo.Record `json:"muestra_precio_por_tarifa,omitempty"`
	Embudo             []Etapa       `json:"embudo_de_publicabilidad,omitempty"`
	PublicablesHoy     int           `json:"publicables_hoy"`
	MarcasNormalizadas []Prefijo     `json:"marcas_normalizadas,omitempty"`
}

type Etapa struct {
	Criterio string `json:"criterio"`
	Quedan   int    `json:"productos_que_quedan"`
}

func (e *explorer) viabilidad() {
	v := &Viabilidad{}
	e.rep.Viabilidad = v

	e.probe("¿Se puede pedir el precio de tarifa por API?", func() error {
		// Odoo bloquea por RPC los métodos que empiezan por "_", así que
		// _get_products_price no es invocable. Se prueba la vía del contexto.
		rows, err := e.cli.SearchRead("product.product",
			[]interface{}{[]interface{}{"list_price", ">", 1}},
			[]string{"default_code", "name", "list_price", "standard_price"},
			map[string]interface{}{
				"limit":   5,
				"order":   "id",
				"context": map[string]interface{}{"pricelist": 5},
			})
		if err != nil {
			v.PrecioPorTarifa = "el contexto pricelist no es utilizable: " + err.Error()
			return err
		}

		// Se intenta leer el campo "price", que en versiones antiguas respondía
		// al contexto pricelist.
		conPrecio, err2 := e.cli.SearchRead("product.product",
			[]interface{}{[]interface{}{"list_price", ">", 1}},
			[]string{"default_code", "price", "list_price", "standard_price"},
			map[string]interface{}{
				"limit":   5,
				"order":   "id",
				"context": map[string]interface{}{"pricelist": 5},
			})
		if err2 != nil {
			v.PrecioPorTarifa = "NO existe un campo 'price' sensible al contexto en Odoo 18: " +
				"Integra tendrá que replicar la lógica de tarifas leyendo product.pricelist.item"
			fmt.Printf("  ✗ campo 'price' no legible: %v\n", err2)
			fmt.Printf("  → Integra replicará el motor de tarifas en Go\n")
			v.MuestraPrecios = rows
			return nil
		}

		v.PrecioPorTarifa = "el campo 'price' responde al contexto pricelist"
		v.MuestraPrecios = conPrecio
		for _, r := range conPrecio {
			fmt.Printf("  %-18s lista=%12.2f  costo=%14.2f  tarifa=%12.2f\n",
				r.Str("default_code"), r.Float("list_price"), r.Float("standard_price"), r.Float("price"))
		}
		return nil
	})

	e.probe("Embudo de publicabilidad", func() error {
		// Cada etapa añade un requisito que TODOS los canales exigen.
		etapas := []struct {
			nombre string
			domain []interface{}
		}{
			{"todas las variantes", []interface{}{}},
			{"+ vendible (sale_ok)", []interface{}{
				[]interface{}{"sale_ok", "=", true}}},
			{"+ almacenable (is_storable)", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true}}},
			{"+ con referencia interna", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true},
				[]interface{}{"default_code", "!=", false}}},
			{"+ con imagen", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true},
				[]interface{}{"default_code", "!=", false},
				[]interface{}{"image_1920", "!=", false}}},
			{"+ con stock positivo", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true},
				[]interface{}{"default_code", "!=", false},
				[]interface{}{"image_1920", "!=", false},
				[]interface{}{"qty_available", ">", 0}}},
			{"+ con marca identificada", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true},
				[]interface{}{"default_code", "!=", false},
				[]interface{}{"image_1920", "!=", false},
				[]interface{}{"qty_available", ">", 0},
				[]interface{}{"l10n_co_edi_brand", "!=", false}}},
			{"+ con descripción de venta", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true},
				[]interface{}{"default_code", "!=", false},
				[]interface{}{"image_1920", "!=", false},
				[]interface{}{"qty_available", ">", 0},
				[]interface{}{"l10n_co_edi_brand", "!=", false},
				[]interface{}{"description_sale", "!=", false}}},
		}
		for _, et := range etapas {
			n, err := e.cli.SearchCount("product.product", et.domain)
			if err != nil {
				return err
			}
			v.Embudo = append(v.Embudo, Etapa{Criterio: et.nombre, Quedan: n})
			v.PublicablesHoy = n
			fmt.Printf("  %-32s %4d\n", et.nombre, n)
		}
		return nil
	})
}
