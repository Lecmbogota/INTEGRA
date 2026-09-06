package main

import (
	"fmt"
	"sort"

	"github.com/mdv/integra/internal/odoo"
)

// Profundo recoge lo que la primera pasada solo insinúa: qué marcas existen de
// verdad, si los atributos generan variantes, cómo se estructuran las tarifas y
// dónde está el stock.
type Profundo struct {
	ValoresAtributo  map[string][]string `json:"valores_por_atributo,omitempty"`
	UsoAtributos     []UsoAtributo       `json:"uso_real_de_atributos,omitempty"`
	ReglasTarifa     []odoo.Record       `json:"reglas_de_tarifa,omitempty"`
	StockPorAlmacen  []StockAlmacen      `json:"stock_por_almacen,omitempty"`
	CoberturaCampos  map[string]int      `json:"cobertura_de_campos_clave,omitempty"`
	PlantillasVarias int                 `json:"plantillas_con_mas_de_una_variante"`
	CategoriasTop    []Prefijo           `json:"categorias_con_mas_productos,omitempty"`
	Marcas           []Prefijo           `json:"marcas_en_l10n_co_edi_brand,omitempty"`
}

type UsoAtributo struct {
	Atributo   string `json:"atributo"`
	Plantillas int    `json:"plantillas_que_lo_usan"`
}

type StockAlmacen struct {
	Almacen   string  `json:"almacen"`
	Ubicacion string  `json:"ubicacion"`
	Cantidad  float64 `json:"cantidad_total"`
	Lineas    int     `json:"lineas_de_stock"`
}

func (e *explorer) profundizar() {
	p := &Profundo{
		ValoresAtributo: map[string][]string{},
		CoberturaCampos: map[string]int{},
	}
	e.rep.Profundo = p

	e.probe("Valores de cada atributo (aquí deberían estar las marcas)", func() error {
		rows, err := e.cli.SearchRead("product.attribute.value", nil,
			[]string{"name", "attribute_id"},
			map[string]interface{}{"order": "attribute_id,sequence,name", "limit": 2000})
		if err != nil {
			return err
		}
		for _, r := range rows {
			attr := r.RefName("attribute_id")
			p.ValoresAtributo[attr] = append(p.ValoresAtributo[attr], r.Str("name"))
		}
		claves := make([]string, 0, len(p.ValoresAtributo))
		for k := range p.ValoresAtributo {
			claves = append(claves, k)
		}
		sort.Strings(claves)
		for _, k := range claves {
			v := p.ValoresAtributo[k]
			fmt.Printf("  %-14s (%d valores): %s\n", k, len(v), unir(v, 14))
		}
		return nil
	})

	e.probe("Marcas reales en l10n_co_edi_brand", func() error {
		rows, err := e.cli.ReadGroup("product.product",
			[]interface{}{[]interface{}{"l10n_co_edi_brand", "!=", false}},
			[]string{"l10n_co_edi_brand"}, []string{"l10n_co_edi_brand"}, e.kw(nil))
		if err != nil {
			return err
		}
		for _, r := range rows {
			p.Marcas = append(p.Marcas, Prefijo{
				Prefijo: r.Str("l10n_co_edi_brand"),
				Conteo:  int(r.Int("l10n_co_edi_brand_count")),
			})
		}
		sort.Slice(p.Marcas, func(i, j int) bool { return p.Marcas[i].Conteo > p.Marcas[j].Conteo })
		fmt.Printf("  %d valores distintos:\n", len(p.Marcas))
		for i, m := range p.Marcas {
			if i >= 40 {
				fmt.Printf("  … %d más\n", len(p.Marcas)-40)
				break
			}
			fmt.Printf("    %-38s %d productos\n", m.Prefijo, m.Conteo)
		}
		return nil
	})

	e.probe("Uso real de los atributos en plantillas", func() error {
		// Si "brand" fuese la marca, debería aparecer en cientos de plantillas.
		rows, err := e.cli.ReadGroup("product.template.attribute.line", nil,
			[]string{"attribute_id"}, []string{"attribute_id"}, nil)
		if err != nil {
			return err
		}
		for _, r := range rows {
			p.UsoAtributos = append(p.UsoAtributos, UsoAtributo{
				Atributo:   r.RefName("attribute_id"),
				Plantillas: int(r.Int("attribute_id_count")),
			})
		}
		sort.Slice(p.UsoAtributos, func(i, j int) bool {
			return p.UsoAtributos[i].Plantillas > p.UsoAtributos[j].Plantillas
		})
		if len(p.UsoAtributos) == 0 {
			fmt.Printf("  → Ninguna plantilla usa atributos: los 8 atributos están definidos pero sin aplicar\n")
		}
		for _, u := range p.UsoAtributos {
			fmt.Printf("  %-14s %d plantillas\n", u.Atributo, u.Plantillas)
		}
		return nil
	})

	e.probe("Plantillas con más de una variante", func() error {
		// product_variant_count no es almacenado y no se puede filtrar; se cuenta
		// agrupando las variantes por plantilla.
		rows, err := e.cli.ReadGroup("product.product", nil,
			[]string{"product_tmpl_id"}, []string{"product_tmpl_id"},
			map[string]interface{}{"context": map[string]interface{}{"active_test": false}})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Int("product_tmpl_id_count") > 1 {
				p.PlantillasVarias++
			}
		}
		fmt.Printf("  %d de %d plantillas tienen más de una variante\n", p.PlantillasVarias, len(rows))
		if p.PlantillasVarias == 0 {
			fmt.Printf("  → El catálogo es 1 plantilla = 1 variante. Hoy NO hay variantes reales.\n")
		}
		return nil
	})

	e.probe("Reglas de las tarifas", func() error {
		rows, err := e.cli.SearchRead("product.pricelist.item", nil,
			[]string{"pricelist_id", "applied_on", "compute_price", "percent_price",
				"fixed_price", "base", "categ_id", "product_tmpl_id", "date_start", "date_end",
				"price_discount", "price_surcharge", "price_round", "price_min_margin",
				"price_max_margin", "base_pricelist_id", "min_quantity"},
			e.kw(map[string]interface{}{"order": "pricelist_id,id", "limit": 300}))
		if err != nil {
			return err
		}
		p.ReglasTarifa = rows
		porTarifa := map[string]int{}
		for _, r := range rows {
			porTarifa[r.RefName("pricelist_id")]++
		}
		for _, r := range rows[:min(10, len(rows))] {
			fmt.Printf("  [%s] aplica_a=%-18s calculo=%-8s base=%-14s dto=%.2f%% recargo=%.2f fijo=%.2f %s\n",
				r.RefName("pricelist_id"), r.Str("applied_on"), r.Str("compute_price"),
				r.Str("base"), r.Float("price_discount"), r.Float("price_surcharge"),
				r.Float("fixed_price"), r.RefName("categ_id"))
		}
		if len(rows) > 10 {
			fmt.Printf("  … %d reglas más en el informe\n", len(rows)-10)
		}
		return nil
	})

	e.probe("Stock por almacén", func() error {
		for _, w := range e.rep.Almacenes {
			locID := w.RefID("lot_stock_id")
			if locID == 0 {
				continue
			}
			rows, err := e.cli.ReadGroup("stock.quant",
				[]interface{}{[]interface{}{"location_id", "child_of", locID}},
				[]string{"quantity"}, []string{"location_id"}, e.kw(nil))
			if err != nil {
				return err
			}
			var total float64
			var lineas int
			for _, r := range rows {
				total += r.Float("quantity")
				lineas += int(r.Int("location_id_count"))
			}
			p.StockPorAlmacen = append(p.StockPorAlmacen, StockAlmacen{
				Almacen:   w.Str("name"),
				Ubicacion: w.RefName("lot_stock_id"),
				Cantidad:  total,
				Lineas:    lineas,
			})
			fmt.Printf("  %-26s %10.0f unidades en %d líneas\n", w.Str("name"), total, lineas)
		}
		return nil
	})

	e.probe("Cobertura de campos clave en TODO el catálogo", func() error {
		checks := []struct {
			clave  string
			domain []interface{}
		}{
			{"con_l10n_co_edi_brand", []interface{}{[]interface{}{"l10n_co_edi_brand", "!=", false}}},
			{"con_referencia_interna", []interface{}{[]interface{}{"default_code", "!=", false}}},
			{"con_descripcion_venta", []interface{}{[]interface{}{"description_sale", "!=", false}}},
			{"precio_mayor_que_uno", []interface{}{[]interface{}{"list_price", ">", 1}}},
			{"vendibles_y_almacenables", []interface{}{
				[]interface{}{"sale_ok", "=", true},
				[]interface{}{"is_storable", "=", true}}},
			{"con_stock_positivo", []interface{}{[]interface{}{"qty_available", ">", 0}}},
		}
		for _, c := range checks {
			n, err := e.cli.SearchCount("product.product", c.domain)
			if err != nil {
				e.rep.Errors = append(e.rep.Errors, fmt.Sprintf("cobertura %s: %v", c.clave, err))
				continue
			}
			p.CoberturaCampos[c.clave] = n
			fmt.Printf("  %-28s %d\n", c.clave, n)
		}
		return nil
	})

	e.probe("Categorías con más productos", func() error {
		rows, err := e.cli.ReadGroup("product.product",
			[]interface{}{[]interface{}{"sale_ok", "=", true}},
			[]string{"categ_id"}, []string{"categ_id"}, e.kw(nil))
		if err != nil {
			return err
		}
		for _, r := range rows {
			p.CategoriasTop = append(p.CategoriasTop, Prefijo{
				Prefijo: r.RefName("categ_id"),
				Conteo:  int(r.Int("categ_id_count")),
			})
		}
		sort.Slice(p.CategoriasTop, func(i, j int) bool {
			return p.CategoriasTop[i].Conteo > p.CategoriasTop[j].Conteo
		})
		for i, c := range p.CategoriasTop {
			if i >= 15 {
				fmt.Printf("  … %d categorías más\n", len(p.CategoriasTop)-15)
				break
			}
			fmt.Printf("  %-52s %d\n", c.Prefijo, c.Conteo)
		}
		return nil
	})
}

func unir(v []string, max int) string {
	if len(v) <= max {
		return join(v)
	}
	return join(v[:max]) + fmt.Sprintf(" … (+%d)", len(v)-max)
}

func join(v []string) string {
	out := ""
	for i, s := range v {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
