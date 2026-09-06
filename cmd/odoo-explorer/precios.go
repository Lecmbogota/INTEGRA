package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/pricing"
)

// InformePrecios compara el list_price de Odoo con el precio que resulta de
// aplicar las tarifas, que es el que Integra publicaría.
type InformePrecios struct {
	Tarifa          string         `json:"tarifa"`
	Evaluados       int            `json:"productos_evaluados"`
	ConPrecio       int            `json:"con_precio_calculado"`
	SinRegla        int            `json:"sin_regla_aplicable"`
	SinCoste        int            `json:"sin_coste"`
	BajoCoste       int            `json:"calculado_por_debajo_del_coste"`
	DesviacionGrave int            `json:"list_price_desviado_mas_de_10x"`
	Muestra         []LineaPrecio  `json:"muestra"`
	PorTarifa       map[string]int `json:"cobertura_por_tarifa,omitempty"`
}

type LineaPrecio struct {
	SKU         string  `json:"sku"`
	Nombre      string  `json:"nombre"`
	Categoria   string  `json:"categoria"`
	ListPrice   float64 `json:"list_price_odoo"`
	Coste       float64 `json:"coste"`
	Calculado   float64 `json:"precio_calculado"`
	Aplicada    bool    `json:"regla_aplicada"`
	Explicacion string  `json:"explicacion"`
}

func (e *explorer) precios() {
	inf := &InformePrecios{PorTarifa: map[string]int{}}
	e.rep.Precios = inf

	var motor *pricing.Engine
	var tarifaPrincipal int64
	// ancestros[categoriaID] = la categoría y todas sus ascendientes.
	ancestros := map[int64][]int64{}

	e.probe("Cargando categorías para resolver herencia", func() error {
		rows, err := e.cli.SearchRead("product.category", nil,
			[]string{"complete_name", "parent_path"},
			map[string]interface{}{"limit": 2000})
		if err != nil {
			return err
		}
		for _, r := range rows {
			// parent_path viene como "1/5/12/": la cadena completa de padres.
			var cadena []int64
			for _, s := range strings.Split(strings.Trim(r.Str("parent_path"), "/"), "/") {
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					cadena = append(cadena, n)
				}
			}
			if len(cadena) == 0 {
				cadena = []int64{r.ID()}
			}
			ancestros[r.ID()] = cadena
		}
		fmt.Printf("  %d categorías con su cadena de ascendientes\n", len(ancestros))
		return nil
	})

	e.probe("Construyendo el motor de tarifas", func() error {
		listas, err := e.cli.SearchRead("product.pricelist", nil,
			[]string{"name", "currency_id"}, e.kw(map[string]interface{}{"order": "id"}))
		if err != nil {
			return err
		}
		items, err := e.cli.SearchRead("product.pricelist.item", nil,
			[]string{"pricelist_id", "applied_on", "compute_price", "base",
				"categ_id", "product_tmpl_id", "product_id", "base_pricelist_id",
				"fixed_price", "percent_price", "price_discount", "price_surcharge",
				"price_round", "price_min_margin", "price_max_margin", "min_quantity",
				"date_start", "date_end"},
			e.kw(map[string]interface{}{"limit": 5000}))
		if err != nil {
			return err
		}

		porLista := map[int64][]pricing.Rule{}
		for _, it := range items {
			r := pricing.Rule{
				ID:              it.ID(),
				PricelistID:     it.RefID("pricelist_id"),
				AppliedOn:       it.Str("applied_on"),
				ComputePrice:    it.Str("compute_price"),
				Base:            it.Str("base"),
				CategoryID:      it.RefID("categ_id"),
				TemplateID:      it.RefID("product_tmpl_id"),
				VariantID:       it.RefID("product_id"),
				BasePricelistID: it.RefID("base_pricelist_id"),
				FixedPrice:      it.Float("fixed_price"),
				PercentPrice:    it.Float("percent_price"),
				PriceDiscount:   it.Float("price_discount"),
				PriceSurcharge:  it.Float("price_surcharge"),
				PriceRound:      it.Float("price_round"),
				PriceMinMargin:  it.Float("price_min_margin"),
				PriceMaxMargin:  it.Float("price_max_margin"),
				MinQuantity:     it.Float("min_quantity"),
			}
			if ts, ok := it.Time("date_start"); ok {
				r.DateStart = &ts
			}
			if ts, ok := it.Time("date_end"); ok {
				r.DateEnd = &ts
			}
			porLista[r.PricelistID] = append(porLista[r.PricelistID], r)
		}

		var pls []pricing.Pricelist
		for _, l := range listas {
			pls = append(pls, pricing.Pricelist{
				ID: l.ID(), Name: l.Str("name"),
				Currency: l.RefName("currency_id"),
				Rules:    porLista[l.ID()],
			})
			inf.PorTarifa[l.Str("name")] = len(porLista[l.ID()])
			// Se tarifica con la primera lista en COP que tenga reglas: en MDV
			// es "Predeterminado", la que usa la fuerza de ventas.
			if tarifaPrincipal == 0 && l.RefName("currency_id") == "COP" && len(porLista[l.ID()]) > 0 {
				tarifaPrincipal = l.ID()
				inf.Tarifa = l.Str("name")
			}
		}
		motor = pricing.NewEngine(pls)
		fmt.Printf("  %d tarifas, %d reglas. Se tarifica con %q\n", len(pls), len(items), inf.Tarifa)
		return nil
	})

	if motor == nil || tarifaPrincipal == 0 {
		return
	}

	e.probe("Calculando el precio real de todo el catálogo", func() error {
		rows, err := e.cli.SearchRead("product.product",
			[]interface{}{[]interface{}{"sale_ok", "=", true}},
			[]string{"default_code", "name", "categ_id", "product_tmpl_id",
				"list_price", "standard_price"},
			e.kw(map[string]interface{}{"limit": 2000, "order": "id"}))
		if err != nil {
			return err
		}
		ahora := time.Now()

		for _, r := range rows {
			catID := r.RefID("categ_id")
			p := pricing.Product{
				VariantID:         r.ID(),
				TemplateID:        r.RefID("product_tmpl_id"),
				CategoryID:        catID,
				CategoryAncestors: ancestros[catID],
				ListPrice:         r.Float("list_price"),
				Cost:              r.Float("standard_price"),
			}
			if len(p.CategoryAncestors) == 0 && catID != 0 {
				p.CategoryAncestors = []int64{catID}
			}

			res, err := motor.Price(tarifaPrincipal, p, 1, ahora)
			if err != nil {
				return err
			}

			inf.Evaluados++
			switch {
			case p.Cost == 0:
				inf.SinCoste++
			case !res.Applied:
				inf.SinRegla++
			default:
				inf.ConPrecio++
				if res.Price < p.Cost {
					inf.BajoCoste++
				}
				if p.ListPrice > 0 && (res.Price/p.ListPrice > 10 || p.ListPrice/res.Price > 10) {
					inf.DesviacionGrave++
				}
			}

			if res.Applied && p.Cost > 0 && len(inf.Muestra) < 20 {
				inf.Muestra = append(inf.Muestra, LineaPrecio{
					SKU: r.Str("default_code"), Nombre: r.Str("name"),
					Categoria: r.RefName("categ_id"),
					ListPrice: p.ListPrice, Coste: p.Cost,
					Calculado: res.Price, Aplicada: res.Applied,
					Explicacion: res.Explanation,
				})
			}
		}

		fmt.Printf("\n  %-14s %-34s %12s %14s %14s\n", "SKU", "Categoría", "list_price", "coste", "PRECIO REAL")
		fmt.Printf("  %s\n", strings.Repeat("─", 92))
		for _, l := range inf.Muestra[:min(12, len(inf.Muestra))] {
			cat := l.Categoria
			if len([]rune(cat)) > 33 {
				cat = string([]rune(cat)[:32]) + "…"
			}
			sku := l.SKU
			if sku == "" {
				sku = "(sin SKU)"
			}
			fmt.Printf("  %-14s %-34s %12.2f %14.2f %14.2f\n",
				truncar(sku, 14), cat, l.ListPrice, l.Coste, l.Calculado)
		}
		return nil
	})

	fmt.Printf("\n  %s\n", strings.Repeat("─", 60))
	fmt.Printf("  Tarifa aplicada:                    %s\n", inf.Tarifa)
	fmt.Printf("  Productos vendibles evaluados:      %d\n", inf.Evaluados)
	fmt.Printf("  Con precio calculable:              %d\n", inf.ConPrecio)
	fmt.Printf("  Sin regla de tarifa que aplique:    %d\n", inf.SinRegla)
	fmt.Printf("  Sin coste (no se puede tarificar):  %d\n", inf.SinCoste)
	fmt.Printf("  Calculado por debajo del coste:     %d\n", inf.BajoCoste)
	fmt.Printf("  list_price desviado más de 10×:     %d\n", inf.DesviacionGrave)

	claves := make([]string, 0, len(inf.PorTarifa))
	for k := range inf.PorTarifa {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	fmt.Printf("\n  Reglas por tarifa:\n")
	for _, k := range claves {
		fmt.Printf("    %-32s %d\n", k, inf.PorTarifa[k])
	}
}

func truncar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

var _ = odoo.Record{}
