// Command odoo-explorer inspecciona una instancia de Odoo y produce un informe
// con todo lo que Integra necesita saber antes de escribir el sincronizador:
// versión, compañías, campos personalizados, candidatos a "marca", variantes,
// tarifas, almacenes y un diagnóstico de calidad del catálogo.
//
// No escribe absolutamente nada en Odoo: solo usa authenticate, search_read,
// search_count y fields_get.
//
// Uso:
//
//	copiar .env.example a .env, rellenarlo y ejecutar:
//	go run ./cmd/odoo-explorer
//
// Las credenciales se leen del entorno o del fichero .env y nunca se escriben
// en el informe.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mdv/integra/internal/odoo"
)

func main() {
	envFile := flag.String("env", ".env", "fichero con las credenciales")
	out := flag.String("out", "odoo-report.json", "fichero de salida del informe")
	sampleSize := flag.Int("sample", 500, "número de productos a analizar en el diagnóstico de calidad")
	url := flag.String("url", "", "URL de Odoo (sobrescribe ODOO_URL)")
	soloVersion := flag.Bool("probe", false, "solo comprobar versión y alcance del servidor, sin autenticar")
	flag.Parse()

	loadDotEnv(*envFile)

	cfg := odoo.Config{
		URL:      os.Getenv("ODOO_URL"),
		Database: os.Getenv("ODOO_DB"),
		Username: os.Getenv("ODOO_USER"),
		APIKey:   os.Getenv("ODOO_API_KEY"),
	}
	if *url != "" {
		cfg.URL = *url
	}

	// -probe no necesita credenciales: `version` es una meta-llamada sin
	// autenticar. Sirve para confirmar que el host responde y qué versión
	// corre antes de molestarse en generar una API key.
	if *soloVersion {
		if strings.TrimSpace(cfg.URL) == "" {
			fatal("Indica la instancia con -url https://… o con ODOO_URL")
		}
		fmt.Printf("→ Sondeando %s\n\n", cfg.URL)
		ver, err := odoo.Version(cfg.URL)
		if err != nil {
			fatal("El servidor no respondió.\n\n%v\n\n%s", err, pistaConexion(err))
		}
		claves := make([]string, 0, len(ver))
		for k := range ver {
			claves = append(claves, k)
		}
		sort.Strings(claves)
		for _, k := range claves {
			fmt.Printf("  %-24s %v\n", k, ver[k])
		}

		// El nombre de la base rara vez coincide con el subdominio en las
		// instancias duplicadas para pruebas. Se intenta listar, aunque Odoo
		// Online suele tenerlo desactivado (list_db=False).
		fmt.Printf("\n→ Intentando listar bases de datos\n")
		if dbs, err := odoo.ListDatabases(cfg.URL); err != nil {
			fmt.Printf("  ✗ %v\n", err)
			fmt.Printf("%s\n", pistaBaseDeDatos(cfg.URL))
		} else {
			for _, d := range dbs {
				fmt.Printf("  • %s\n", d)
			}
		}

		fmt.Printf("\n✓ El endpoint XML-RPC responde. Siguiente paso: rellenar .env y ejecutar sin -probe.\n")
		return
	}

	if missing := faltantes(cfg); len(missing) > 0 {
		fatal("Faltan variables de entorno: %s\n\nCopia .env.example a .env y rellénalo.", strings.Join(missing, ", "))
	}

	rep := &Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Instance:    cfg.URL, // la URL sí, las credenciales nunca
		Database:    cfg.Database,
	}

	e := &explorer{rep: rep}

	fmt.Printf("→ Conectando con %s (base de datos: %s)\n\n", cfg.URL, cfg.Database)

	// Paso 1: versión sin autenticar. Confirma que el host responde y que la
	// API externa está expuesta, antes de gastar un intento de credenciales.
	ver, err := odoo.Version(cfg.URL)
	if err != nil {
		fatal("No se pudo leer la versión del servidor.\n\n%v\n\n%s", err, pistaConexion(err))
	}
	rep.Version = ver
	fmt.Printf("  Odoo %v (serie %v, protocolo %v)\n",
		ver["server_version"], ver["server_serie"], ver["protocol_version"])

	// Paso 2: autenticación.
	cli, err := odoo.Connect(cfg)
	if err != nil {
		fatal("No se pudo autenticar.\n\n%v\n\n%s", err, pistaConexion(err))
	}
	rep.UID = cli.UID()
	fmt.Printf("  Autenticado como uid=%d\n\n", cli.UID())

	e.cli = cli
	e.run()
	e.profundizar()
	e.viabilidad()
	e.precios()
	e.imagenes(120)

	// Informe en disco.
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fatal("No se pudo serializar el informe: %v", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fatal("No se pudo escribir %s: %v", *out, err)
	}

	e.sample(*sampleSize)
	e.resumen()

	data, _ = json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(*out, data, 0o644)

	fmt.Printf("\n✓ Informe completo escrito en %s\n", *out)
	if len(rep.Errors) > 0 {
		fmt.Printf("  (%d sondas fallaron; el detalle está en el informe)\n", len(rep.Errors))
	}
}

// ------------------------------------------------------------------- informe

type Report struct {
	GeneratedAt string                 `json:"generado_en"`
	Instance    string                 `json:"instancia"`
	Database    string                 `json:"base_de_datos"`
	UID         int64                  `json:"uid"`
	Version     map[string]interface{} `json:"version"`

	Usuario    odoo.Record   `json:"usuario,omitempty"`
	Companias  []odoo.Record `json:"companias,omitempty"`
	Monedas    []odoo.Record `json:"monedas,omitempty"`
	Almacenes  []odoo.Record `json:"almacenes,omitempty"`
	Tarifas    []odoo.Record `json:"tarifas,omitempty"`
	Categorias []odoo.Record `json:"categorias_producto,omitempty"`
	Etiquetas  []odoo.Record `json:"etiquetas_producto,omitempty"`
	Atributos  []odoo.Record `json:"atributos_variante,omitempty"`

	CamposPersonalizados []odoo.Record `json:"campos_personalizados,omitempty"`
	CandidatosMarca      []Candidato   `json:"candidatos_marca,omitempty"`
	CamposDisponibles    []string      `json:"campos_disponibles_product_product,omitempty"`

	Conteos      map[string]int   `json:"conteos"`
	Profundo     *Profundo        `json:"analisis_profundo,omitempty"`
	Viabilidad   *Viabilidad      `json:"viabilidad,omitempty"`
	Precios      *InformePrecios  `json:"precios,omitempty"`
	Imagenes     *InformeImagenes `json:"imagenes,omitempty"`
	Diagnostico  *Diagnostico     `json:"diagnostico_catalogo,omitempty"`
	MuestraDatos []odoo.Record    `json:"muestra_productos,omitempty"`

	Errors []string `json:"errores,omitempty"`
}

type Candidato struct {
	Campo       string `json:"campo"`
	Etiqueta    string `json:"etiqueta"`
	Tipo        string `json:"tipo"`
	Relacion    string `json:"relacion,omitempty"`
	Modelo      string `json:"modelo"`
	Motivo      string `json:"motivo"`
	ConValor    int    `json:"registros_con_valor"`
	TotalMirado int    `json:"registros_evaluados"`
}

type Diagnostico struct {
	Evaluados            int                 `json:"productos_evaluados"`
	SinSKU               int                 `json:"sin_referencia_interna"`
	SKUDuplicado         int                 `json:"referencia_duplicada"`
	SinPrecio            int                 `json:"precio_venta_cero"`
	PrecioPlaceholder    int                 `json:"precio_venta_igual_a_1"`
	PrecioBajoCosto      int                 `json:"precio_menor_que_costo"`
	TituloMayor60        int                 `json:"titulo_supera_60_caracteres"`
	TituloMayor200       int                 `json:"titulo_supera_200_caracteres"`
	SinDescripcion       int                 `json:"sin_descripcion_de_venta"`
	SinCategoria         int                 `json:"sin_categoria"`
	StockCeroPronosticoP int                 `json:"stock_cero_con_pronostico_positivo"`
	PrefijosSKU          []Prefijo           `json:"prefijos_sku"`
	RatioCostoPrecio     []Bucket            `json:"distribucion_costo_sobre_precio"`
	SospechaMoneda       string              `json:"sospecha_de_moneda,omitempty"`
	EjemplosProblema     map[string][]string `json:"ejemplos_por_problema,omitempty"`
}

type Prefijo struct {
	Prefijo string `json:"prefijo"`
	Conteo  int    `json:"conteo"`
}

type Bucket struct {
	Rango  string `json:"rango"`
	Conteo int    `json:"conteo"`
}

// ------------------------------------------------------------------ explorador

type explorer struct {
	cli      *odoo.Client
	rep      *Report
	empresas []int64
}

// probe ejecuta una sonda y registra el fallo sin abortar el resto.
func (e *explorer) probe(nombre string, fn func() error) {
	fmt.Printf("→ %s\n", nombre)
	if err := fn(); err != nil {
		msg := fmt.Sprintf("%s: %v", nombre, err)
		e.rep.Errors = append(e.rep.Errors, msg)
		fmt.Printf("  ✗ %v\n", err)
	}
}

// ctx construye el contexto multi-compañía para ver todas las empresas a la vez.
func (e *explorer) ctx() map[string]interface{} {
	if len(e.empresas) == 0 {
		return map[string]interface{}{}
	}
	ids := make([]interface{}, len(e.empresas))
	for i, id := range e.empresas {
		ids[i] = id
	}
	return map[string]interface{}{"allowed_company_ids": ids}
}

func (e *explorer) kw(extra map[string]interface{}) map[string]interface{} {
	kw := map[string]interface{}{"context": e.ctx()}
	for k, v := range extra {
		kw[k] = v
	}
	return kw
}

func (e *explorer) run() {
	e.rep.Conteos = map[string]int{}

	e.probe("Usuario autenticado", func() error {
		rows, err := e.cli.SearchRead("res.users",
			[]interface{}{[]interface{}{"id", "=", e.cli.UID()}},
			[]string{"name", "login", "company_id", "company_ids", "share"}, nil)
		if err != nil || len(rows) == 0 {
			return err
		}
		e.rep.Usuario = rows[0]
		fmt.Printf("  %s <%s>, compañía activa: %s\n",
			rows[0].Str("name"), rows[0].Str("login"), rows[0].RefName("company_id"))
		return nil
	})

	e.probe("Compañías", func() error {
		rows, err := e.cli.SearchRead("res.company", nil,
			[]string{"name", "currency_id", "parent_id", "country_id"},
			map[string]interface{}{"order": "id"})
		if err != nil {
			return err
		}
		e.rep.Companias = rows
		for _, r := range rows {
			e.empresas = append(e.empresas, r.ID())
			fmt.Printf("  [%d] %s — moneda %s\n", r.ID(), r.Str("name"), r.RefName("currency_id"))
		}
		if len(rows) == 1 {
			fmt.Printf("  → Una sola compañía: la marca NO puede salir de company_id\n")
		}
		return nil
	})

	e.probe("Monedas activas", func() error {
		rows, err := e.cli.SearchRead("res.currency", nil,
			[]string{"name", "symbol", "rate", "active"},
			map[string]interface{}{"context": map[string]interface{}{"active_test": false}, "order": "name"})
		if err != nil {
			return err
		}
		var activas []odoo.Record
		for _, r := range rows {
			if r.Bool("active") {
				activas = append(activas, r)
			}
		}
		e.rep.Monedas = activas
		for _, r := range activas {
			fmt.Printf("  %s (tasa %.6f)\n", r.Str("name"), r.Float("rate"))
		}
		return nil
	})

	e.probe("Campos de product.template y product.product", func() error {
		rows, err := e.cli.SearchRead("ir.model.fields",
			[]interface{}{[]interface{}{"model", "in", []interface{}{"product.template", "product.product"}}},
			[]string{"name", "field_description", "ttype", "relation", "model", "store", "state"},
			map[string]interface{}{"order": "model,name"})
		if err != nil {
			return err
		}

		disponibles := map[string]bool{}
		for _, r := range rows {
			if r.Str("model") == "product.product" || r.Str("model") == "product.template" {
				disponibles[r.Str("name")] = true
			}
			// state = "manual" son los campos creados por Studio o a mano.
			if r.Str("state") == "manual" || strings.HasPrefix(r.Str("name"), "x_") {
				e.rep.CamposPersonalizados = append(e.rep.CamposPersonalizados, r)
			}
		}
		for f := range disponibles {
			e.rep.CamposDisponibles = append(e.rep.CamposDisponibles, f)
		}
		sort.Strings(e.rep.CamposDisponibles)

		fmt.Printf("  %d campos en total, %d personalizados\n", len(rows), len(e.rep.CamposPersonalizados))
		for _, r := range e.rep.CamposPersonalizados {
			fmt.Printf("  ★ %s.%s (%s) — %q\n", r.Str("model"), r.Str("name"), r.Str("ttype"), r.Str("field_description"))
		}
		if len(e.rep.CamposPersonalizados) == 0 {
			fmt.Printf("  → No hay campos personalizados: la marca no está en un campo de Studio\n")
		}

		// Candidatos a "marca" por nombre o por etiqueta.
		for _, r := range rows {
			nombre := strings.ToLower(r.Str("name"))
			etiqueta := strings.ToLower(r.Str("field_description"))
			motivo := ""
			switch {
			case strings.Contains(nombre, "brand") || strings.Contains(nombre, "marca"):
				motivo = "el nombre técnico menciona marca/brand"
			case strings.Contains(etiqueta, "marca") || strings.Contains(etiqueta, "brand"):
				motivo = "la etiqueta visible menciona marca/brand"
			case r.Str("state") == "manual":
				motivo = "campo personalizado (Studio)"
			}
			if motivo == "" {
				continue
			}
			e.rep.CandidatosMarca = append(e.rep.CandidatosMarca, Candidato{
				Campo:    r.Str("name"),
				Etiqueta: r.Str("field_description"),
				Tipo:     r.Str("ttype"),
				Relacion: r.Str("relation"),
				Modelo:   r.Str("model"),
				Motivo:   motivo,
			})
		}
		return nil
	})

	e.probe("Conteos de catálogo", func() error {
		tipos := []struct {
			clave  string
			modelo string
			dom    []interface{}
		}{
			{"plantillas_total", "product.template", nil},
			{"plantillas_vendibles", "product.template", []interface{}{[]interface{}{"sale_ok", "=", true}}},
			{"variantes_total", "product.product", nil},
			{"variantes_vendibles", "product.product", []interface{}{[]interface{}{"sale_ok", "=", true}}},
			{"variantes_sin_referencia", "product.product", []interface{}{[]interface{}{"default_code", "=", false}}},
			{"variantes_con_codigo_barras", "product.product", []interface{}{[]interface{}{"barcode", "!=", false}}},
			{"variantes_con_imagen", "product.product", []interface{}{[]interface{}{"image_1920", "!=", false}}},
			{"plantillas_con_varias_variantes", "product.template", []interface{}{[]interface{}{"product_variant_count", ">", 1}}},
		}
		for _, t := range tipos {
			n, err := e.cli.SearchCount(t.modelo, t.dom)
			if err != nil {
				e.rep.Errors = append(e.rep.Errors, fmt.Sprintf("conteo %s: %v", t.clave, err))
				continue
			}
			e.rep.Conteos[t.clave] = n
			fmt.Printf("  %-34s %d\n", t.clave, n)
		}
		return nil
	})

	e.probe("Atributos de variante", func() error {
		rows, err := e.cli.SearchRead("product.attribute", nil,
			[]string{"name", "display_type", "create_variant", "number_related_products"},
			map[string]interface{}{"order": "name"})
		if err != nil {
			return err
		}
		e.rep.Atributos = rows
		for _, r := range rows {
			fmt.Printf("  %s (%s, create_variant=%s)\n", r.Str("name"), r.Str("display_type"), r.Str("create_variant"))
		}
		if len(rows) == 0 {
			fmt.Printf("  → Sin atributos definidos: hoy no hay variantes reales\n")
		}
		return nil
	})

	e.probe("Categorías de producto", func() error {
		rows, err := e.cli.SearchRead("product.category", nil,
			[]string{"complete_name", "parent_id", "product_count"},
			map[string]interface{}{"order": "complete_name", "limit": 300})
		if err != nil {
			return err
		}
		e.rep.Categorias = rows
		fmt.Printf("  %d categorías\n", len(rows))
		for i, r := range rows {
			if i >= 25 {
				fmt.Printf("  … y %d más (todas en el informe)\n", len(rows)-25)
				break
			}
			fmt.Printf("  %s\n", r.Str("complete_name"))
		}
		return nil
	})

	e.probe("Etiquetas de producto", func() error {
		rows, err := e.cli.SearchRead("product.tag", nil, []string{"name", "product_ids"},
			map[string]interface{}{"order": "name", "limit": 200})
		if err != nil {
			return err
		}
		e.rep.Etiquetas = rows
		if len(rows) == 0 {
			fmt.Printf("  → Sin etiquetas definidas\n")
		}
		for _, r := range rows {
			fmt.Printf("  %s (%d productos)\n", r.Str("name"), len(r.IDs("product_ids")))
		}
		return nil
	})

	e.probe("Tarifas (product.pricelist)", func() error {
		rows, err := e.cli.SearchRead("product.pricelist", nil,
			[]string{"name", "currency_id", "company_id", "active"},
			e.kw(map[string]interface{}{"order": "id"}))
		if err != nil {
			return err
		}
		e.rep.Tarifas = rows
		for _, r := range rows {
			n, err := e.cli.SearchCount("product.pricelist.item",
				[]interface{}{[]interface{}{"pricelist_id", "=", r.ID()}})
			if err != nil {
				n = -1
			}
			fmt.Printf("  [%d] %-40s moneda %-6s %d reglas\n",
				r.ID(), r.Str("name"), r.RefName("currency_id"), n)
		}
		if len(rows) == 0 {
			fmt.Printf("  → Sin tarifas: el PVP tendría que salir de list_price\n")
		}
		return nil
	})

	e.probe("Almacenes y ubicaciones", func() error {
		rows, err := e.cli.SearchRead("stock.warehouse", nil,
			[]string{"name", "code", "company_id", "lot_stock_id"},
			e.kw(map[string]interface{}{"order": "id"}))
		if err != nil {
			return err
		}
		e.rep.Almacenes = rows
		for _, r := range rows {
			fmt.Printf("  [%d] %s (%s) — compañía %s\n", r.ID(), r.Str("name"), r.Str("code"), r.RefName("company_id"))
		}
		return nil
	})
}

// campoDisponible evita pedir campos que no existen en esta versión de Odoo,
// que provocaría un error en todo el search_read.
func (e *explorer) campoDisponible(nombre string) bool {
	for _, f := range e.rep.CamposDisponibles {
		if f == nombre {
			return true
		}
	}
	return false
}

func (e *explorer) sample(n int) {
	e.probe(fmt.Sprintf("Diagnóstico de catálogo (muestra de %d variantes)", n), func() error {
		deseados := []string{
			"default_code", "barcode", "name", "list_price", "lst_price", "standard_price",
			"qty_available", "virtual_available", "free_qty", "incoming_qty",
			"categ_id", "product_tag_ids", "company_id", "currency_id",
			"type", "is_storable", "sale_ok", "purchase_ok", "active",
			"write_date", "product_tmpl_id", "uom_id", "weight",
			"description_sale", "product_template_variant_value_ids",
			// Campo de marca de la localización colombiana (l10n_co_edi).
			// No es un campo "manual", así que hay que pedirlo explícitamente.
			"l10n_co_edi_brand", "l10n_co_edi_customs_code",
		}
		campos := []string{}
		for _, f := range deseados {
			if e.campoDisponible(f) {
				campos = append(campos, f)
			}
		}
		// Los campos personalizados entran también: puede que ahí esté la marca.
		for _, c := range e.rep.CamposPersonalizados {
			if c.Str("model") == "product.product" && e.campoDisponible(c.Str("name")) {
				campos = append(campos, c.Str("name"))
			}
		}

		rows, err := e.cli.SearchRead("product.product", nil, campos,
			e.kw(map[string]interface{}{"limit": n, "order": "id"}))
		if err != nil {
			return err
		}

		d := analizar(rows)
		e.rep.Diagnostico = d

		// Se guardan solo 25 filas de muestra para no inflar el informe.
		lim := len(rows)
		if lim > 25 {
			lim = 25
		}
		e.rep.MuestraDatos = rows[:lim]

		// Cuántos registros tienen valor en cada candidato a marca.
		for i := range e.rep.CandidatosMarca {
			c := &e.rep.CandidatosMarca[i]
			if c.Modelo != "product.product" {
				continue
			}
			c.TotalMirado = len(rows)
			for _, r := range rows {
				if r.Has(c.Campo) {
					c.ConValor++
				}
			}
		}
		return nil
	})
}

func analizar(rows []odoo.Record) *Diagnostico {
	d := &Diagnostico{
		Evaluados:        len(rows),
		EjemplosProblema: map[string][]string{},
	}
	vistos := map[string]int{}
	prefijos := map[string]int{}
	var ratios []float64

	añadirEjemplo := func(clave, ejemplo string) {
		if len(d.EjemplosProblema[clave]) < 5 {
			d.EjemplosProblema[clave] = append(d.EjemplosProblema[clave], ejemplo)
		}
	}

	for _, r := range rows {
		nombre := r.Str("name")
		sku := r.Str("default_code")
		precio := r.Float("list_price")
		costo := r.Float("standard_price")

		if sku == "" {
			d.SinSKU++
			añadirEjemplo("sin_referencia_interna", nombre)
		} else {
			vistos[sku]++
			if vistos[sku] == 2 {
				d.SKUDuplicado++
				añadirEjemplo("referencia_duplicada", sku)
			}
			// Prefijo = primer segmento antes del guion, si lo hay.
			if i := strings.Index(sku, "-"); i > 0 && i <= 4 {
				prefijos[strings.ToUpper(sku[:i])]++
			} else {
				prefijos["(sin prefijo)"]++
			}
		}

		switch {
		case precio == 0:
			d.SinPrecio++
			añadirEjemplo("precio_venta_cero", etiqueta(sku, nombre))
		case precio == 1:
			d.PrecioPlaceholder++
			añadirEjemplo("precio_venta_igual_a_1", etiqueta(sku, nombre))
		}

		if precio > 0 && costo > 0 {
			ratios = append(ratios, costo/precio)
			if costo > precio {
				d.PrecioBajoCosto++
				añadirEjemplo("precio_menor_que_costo",
					fmt.Sprintf("%s — precio %.2f, costo %.2f", etiqueta(sku, nombre), precio, costo))
			}
		}

		if l := len([]rune(nombre)); l > 60 {
			d.TituloMayor60++
			if l > 200 {
				d.TituloMayor200++
				añadirEjemplo("titulo_supera_200_caracteres", fmt.Sprintf("%s (%d caracteres)", etiqueta(sku, nombre), l))
			}
		}
		if !r.Has("description_sale") {
			d.SinDescripcion++
		}
		if !r.Has("categ_id") {
			d.SinCategoria++
		}
		if r.Float("qty_available") == 0 && r.Float("virtual_available") > 0 {
			d.StockCeroPronosticoP++
			añadirEjemplo("stock_cero_con_pronostico_positivo",
				fmt.Sprintf("%s — a la mano 0, pronosticado %.0f", etiqueta(sku, nombre), r.Float("virtual_available")))
		}
	}

	for p, n := range prefijos {
		d.PrefijosSKU = append(d.PrefijosSKU, Prefijo{Prefijo: p, Conteo: n})
	}
	sort.Slice(d.PrefijosSKU, func(i, j int) bool { return d.PrefijosSKU[i].Conteo > d.PrefijosSKU[j].Conteo })
	if len(d.PrefijosSKU) > 30 {
		d.PrefijosSKU = d.PrefijosSKU[:30]
	}

	// Distribución del cociente costo/precio. Si la mayoría cae en el rango de
	// una tasa de cambio, es que precio y costo están en monedas distintas.
	buckets := []struct {
		rango string
		lo    float64
		hi    float64
	}{
		{"< 0.5 (margen sano)", 0, 0.5},
		{"0.5 – 1 (margen estrecho)", 0.5, 1},
		{"1 – 10 (precio por debajo del costo)", 1, 10},
		{"10 – 1.000", 10, 1000},
		{"1.000 – 10.000 (compatible con COP/USD)", 1000, 10000},
		{"> 10.000", 10000, 1e18},
	}
	conteos := make([]int, len(buckets))
	for _, r := range ratios {
		for i, b := range buckets {
			if r >= b.lo && r < b.hi {
				conteos[i]++
				break
			}
		}
	}
	for i, b := range buckets {
		d.RatioCostoPrecio = append(d.RatioCostoPrecio, Bucket{Rango: b.rango, Conteo: conteos[i]})
	}
	if len(ratios) > 0 {
		enRangoFX := conteos[4] + conteos[5]
		if float64(enRangoFX)/float64(len(ratios)) > 0.3 {
			d.SospechaMoneda = fmt.Sprintf(
				"%d de %d productos tienen un costo entre 1.000 y >10.000 veces su precio de venta. "+
					"Es coherente con costo en COP y precio de venta en USD sin convertir. "+
					"list_price NO es utilizable como PVP sin resolver esto.",
				enRangoFX, len(ratios))
		}
	}
	return d
}

func etiqueta(sku, nombre string) string {
	if sku == "" {
		return nombre
	}
	return sku + " · " + nombre
}

// ------------------------------------------------------------------- resumen

func (e *explorer) resumen() {
	d := e.rep.Diagnostico
	if d == nil {
		return
	}
	regla := strings.Repeat("─", 72)
	fmt.Printf("\n%s\n", regla)
	fmt.Printf("RESUMEN — %d variantes evaluadas\n", d.Evaluados)
	fmt.Printf("%s\n", regla)

	linea := func(etq string, n int) {
		if n == 0 {
			return
		}
		fmt.Printf("  %-42s %6d  (%4.1f%%)\n", etq, n, 100*float64(n)/float64(d.Evaluados))
	}
	linea("Sin referencia interna (SKU)", d.SinSKU)
	linea("Referencia duplicada", d.SKUDuplicado)
	linea("Precio de venta en 0", d.SinPrecio)
	linea("Precio de venta en 1 (placeholder)", d.PrecioPlaceholder)
	linea("Precio por debajo del costo", d.PrecioBajoCosto)
	linea("Título de más de 60 caracteres", d.TituloMayor60)
	linea("Título de más de 200 caracteres", d.TituloMayor200)
	linea("Sin descripción de venta", d.SinDescripcion)
	linea("Sin categoría", d.SinCategoria)
	linea("Stock 0 con pronóstico positivo", d.StockCeroPronosticoP)

	if len(d.PrefijosSKU) > 0 {
		fmt.Printf("\n  Prefijos de SKU más frecuentes:\n")
		for i, p := range d.PrefijosSKU {
			if i >= 12 {
				break
			}
			fmt.Printf("    %-14s %d\n", p.Prefijo, p.Conteo)
		}
	}

	if d.SospechaMoneda != "" {
		fmt.Printf("\n  ⚠ %s\n", d.SospechaMoneda)
	}

	if len(e.rep.CandidatosMarca) > 0 {
		fmt.Printf("\n  Candidatos a campo de marca:\n")
		for _, c := range e.rep.CandidatosMarca {
			cobertura := ""
			if c.TotalMirado > 0 {
				cobertura = fmt.Sprintf(" — con valor en %d/%d", c.ConValor, c.TotalMirado)
			}
			fmt.Printf("    %s.%s (%s)%s — %s\n", c.Modelo, c.Campo, c.Tipo, cobertura, c.Motivo)
		}
	} else {
		fmt.Printf("\n  ⚠ No se encontró ningún campo candidato a marca.\n")
		fmt.Printf("    La marca tendrá que deducirse de la categoría o del prefijo del SKU.\n")
	}
}

// --------------------------------------------------------------------- utilidades

func faltantes(c odoo.Config) []string {
	var out []string
	for _, p := range []struct {
		nombre string
		valor  string
	}{
		{"ODOO_URL", c.URL}, {"ODOO_DB", c.Database},
		{"ODOO_USER", c.Username}, {"ODOO_API_KEY", c.APIKey},
	} {
		if strings.TrimSpace(p.valor) == "" {
			out = append(out, p.nombre)
		}
	}
	return out
}

// loadDotEnv carga pares CLAVE=valor sin sobrescribir el entorno existente.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if _, existe := os.LookupEnv(k); !existe {
			_ = os.Setenv(k, v)
		}
	}
}

func pistaBaseDeDatos(baseURL string) string {
	return "  Cómo averiguar el nombre real de la base de datos:\n" +
		"    1. Entra a Odoo en el navegador\n" +
		"    2. Abre la consola del navegador (F12) y escribe:  odoo.info\n" +
		"       Devuelve algo como { server_version: \"18.0+e\", db: \"NOMBRE-REAL\" }\n" +
		"    Alternativa: Ajustes → activa el modo desarrollador; el nombre aparece\n" +
		"    al final de la página de ajustes.\n" +
		"    Aviso: en las bases duplicadas para pruebas el nombre casi nunca\n" +
		"    coincide con el subdominio."
}

func pistaConexion(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "API externa no está habilitada"),
		strings.Contains(strings.ToLower(msg), "not available"):
		return "Qué revisar:\n" +
			"  • La API externa de Odoo Online solo funciona en planes Custom.\n" +
			"    En One App Free y Standard está bloqueada y no hay forma de sortearlo.\n" +
			"  • Verifica el plan en https://www.odoo.com/my/subscription"
	case strings.Contains(msg, "credenciales"):
		return "Qué revisar:\n" +
			"  • ODOO_DB debe ser el nombre exacto de la base de datos, no el subdominio.\n" +
			"  • ODOO_USER es el correo con el que entras a Odoo.\n" +
			"  • En Odoo Online hay que fijar una contraseña local en Ajustes → Usuarios\n" +
			"    antes de poder generar y usar una API key.\n" +
			"  • Genera la key en Preferencias → Seguridad de la cuenta → Claves de API."
	default:
		return "Qué revisar:\n" +
			"  • Que ODOO_URL incluya https:// y no lleve barra final.\n" +
			"  • Que la instancia esté despierta (las de prueba se suspenden por inactividad)."
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "\n✗ "+format+"\n", args...)
	os.Exit(1)
}
