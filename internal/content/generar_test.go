package content

import (
	"strings"
	"testing"
)

// Todos los casos usan nombres reales del catálogo de MDV.

func TestLimpiar(t *testing.T) {
	casos := map[string]string{
		"Disco SanDisk WD Green SN350 NVMe - 500GB (copia)":       "Disco SanDisk WD Green SN350 NVMe - 500GB",
		"MUSHKIN MEMORIA RAM  DDR4 SODIMM PC4-25600 16GB  LAPTOP": "MUSHKIN MEMORIA RAM DDR4 SODIMM PC4-25600 16GB LAPTOP",
		" PR-250 Impresora Térmica de Recibos de 80 mm":           "PR-250 Impresora Térmica de Recibos de 80 mm",
		"MUSHKIN 16GB DDR5-5600 SODIMM  ":                         "MUSHKIN 16GB DDR5-5600 SODIMM",
		"Disco de  ":                                              "Disco de",
	}
	for entrada, esperado := range casos {
		if got := Limpiar(entrada); got != esperado {
			t.Errorf("Limpiar(%q)\n  = %q\n  se esperaba %q", entrada, got, esperado)
		}
	}
}

func TestNormalizarMayusculas(t *testing.T) {
	entrada := "KINGSTON MEMORIA RAM UDIMM DDR4-25600 16GB 3200MHZ DESKTOP LP"
	got := NormalizarMayusculas(entrada)

	// Las siglas técnicas se respetan.
	for _, sigla := range []string{"UDIMM", "DDR4-25600", "16GB", "3200MHZ", "LP"} {
		if !strings.Contains(got, sigla) {
			t.Errorf("se perdió la sigla %q en %q", sigla, got)
		}
	}
	// Las palabras normales dejan de gritar.
	if !strings.Contains(got, "Kingston") || !strings.Contains(got, "Memoria") {
		t.Errorf("las palabras normales deberían normalizarse: %q", got)
	}

	// Un texto ya bien escrito no se toca.
	bien := "Memoria Kingston 16GB DDR5-5600 UDIMM"
	if got := NormalizarMayusculas(bien); got != bien {
		t.Errorf("un texto correcto no debería cambiar: %q", got)
	}
}

func TestExtraerSpecs(t *testing.T) {
	specs := ExtraerSpecs("Memoria Kingston 16GB DDR5-5600 UDIMM",
		"Almacenamiento / Memorias / RAM / DDR5 / UDIMM")

	quiero := map[string]string{
		"Tipo":       "Memoria RAM",
		"Capacidad":  "16 GB",
		"Tecnología": "DDR5-5600",
		"Formato":    "UDIMM",
	}
	tengo := map[string]string{}
	for _, s := range specs {
		tengo[s.Clave] = s.Valor
	}
	for k, v := range quiero {
		if tengo[k] != v {
			t.Errorf("%s = %q, se esperaba %q (todas: %s)", k, tengo[k], v, Resumen(specs))
		}
	}
}

func TestExtraerSpecsVariados(t *testing.T) {
	casos := []struct {
		nombre string
		categ  string
		clave  string
		valor  string
	}{
		{"DISCO EXTERNO TOSHIBA CANVIO BASIC 2TB USB 3.0/2.0 2.5\" NEGRO MATE",
			"Almacenamiento / Disco / Mecanico / Externo / USB 3.0", "Capacidad", "2 TB"},
		{"DISCO EXTERNO TOSHIBA CANVIO BASIC 2TB USB 3.0/2.0 2.5\" NEGRO MATE",
			"Almacenamiento / Disco / Mecanico / Externo", "Tamaño", `2.5"`},
		{"SSD WESTERN DIGITAL 500GB WD BLUE SATA 3 / LEC 560 MB/s",
			"Almacenamiento / Disco / SSD / Interno / SATA 2.5\"", "Interfaz", "SATA"},
		{"Lexar NM610 PRO 500GB LNM610P500G-RNNNG M2 500 GB PCI Express 30 NVMe",
			"Almacenamiento / Disco / SSD / Interno / PCI M.2", "Interfaz", "NVMe"},
		{"22TB 3.5\" N300 PRO NAS 7200RPM",
			"Almacenamiento / Disco / Mecanico / Interno / NAS", "Revoluciones", "7200 RPM"},
		{"Tablet 11\" FHD+ Nxtpaper 4.0, Flip Case",
			"Cómputo / Tabletas", "Tamaño", `11"`},
		{"Fury Beast 16GB 5600MT/s DDR5 CL40 DIMM",
			"Almacenamiento / Memorias / RAM / DDR5", "Velocidad", "5600 MT/s"},
	}

	for _, c := range casos {
		t.Run(c.clave+"/"+c.valor, func(t *testing.T) {
			specs := ExtraerSpecs(c.nombre, c.categ)
			for _, s := range specs {
				if s.Clave == c.clave && s.Valor == c.valor {
					return
				}
			}
			t.Errorf("no se extrajo %s=%q de %q\n  se obtuvo: %s",
				c.clave, c.valor, c.nombre, Resumen(specs))
		})
	}
}

func TestTituloRespetaLimiteDeMercadoLibre(t *testing.T) {
	// El de 120 caracteres del catálogo real.
	f := Fuente{
		Nombre:    "KVR56U46BS8-32 Kingston 32GB PC5-44800 DDR5-5600MHz Non-ECC Unbuffered CL46 UDIMM",
		Marca:     "KINGSTON",
		CategPath: "Almacenamiento / Memorias / RAM / DDR5 / UDIMM",
		SKU:       "KVR56U46BS8-32",
	}
	b := Generar(f)

	ml := b.Titulos["mercadolibre"]
	if n := len([]rune(ml)); n > 60 {
		t.Fatalf("el título de MercadoLibre mide %d y el máximo es 60: %q", n, ml)
	}
	// No debe cortar a mitad de palabra.
	if strings.HasSuffix(ml, "-") || strings.HasSuffix(ml, " ") {
		t.Errorf("el título quedó cortado de forma fea: %q", ml)
	}
	// Los canales generosos conservan más información.
	if len(b.Titulos["shopify"]) <= len(ml) {
		t.Errorf("Shopify debería aprovechar su límite mayor:\n  ML=%q\n  Shopify=%q",
			ml, b.Titulos["shopify"])
	}
	t.Logf("ML (%d): %s", len([]rune(ml)), ml)
}

func TestTitulosParaTodosLosCanales(t *testing.T) {
	f := Fuente{
		Nombre:    "MUSHKIN MEMORIA RAM  DDR4 SODIMM PC4-25600 16GB 3200MHZ  LAPTOP",
		Marca:     "MUSHKIN",
		CategPath: "Almacenamiento / Memorias / RAM / DDR4 / SODIMM",
	}
	b := Generar(f)

	for canal, limite := range LimitesTitulo {
		tit, ok := b.Titulos[canal]
		if !ok || tit == "" {
			t.Fatalf("falta el título de %s", canal)
		}
		if n := len([]rune(tit)); n > limite {
			t.Errorf("%s: %d caracteres, máximo %d: %q", canal, n, limite, tit)
		}
	}
}

// El caso que da sentido a la familia por categoría: el nombre no dice qué es,
// pero la categoría sí.
func TestFamiliaCompletaNombresPobres(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    "Canvio Basic 2TB",
		Marca:     "TOSHIBA",
		CategPath: "Almacenamiento / Disco / Mecanico / Externo / USB 3.0",
	})
	if !strings.Contains(b.Titulos["mercadolibre"], "Disco Duro") {
		t.Errorf("el título debería decir qué es el producto: %q", b.Titulos["mercadolibre"])
	}
	if !strings.Contains(b.Titulos["mercadolibre"], "Toshiba") {
		t.Errorf("el título debería incluir la marca: %q", b.Titulos["mercadolibre"])
	}
}

// Y el caso opuesto: cuando no hay material, hay que decirlo.
func TestNombreInservibleDaConfianzaBaja(t *testing.T) {
	for _, nombre := range []string{"Disco de ", "Disco de Estado Solido ", "  "} {
		b := Generar(Fuente{Nombre: nombre, CategPath: "Almacenamiento / Disco / SSD / Interno"})
		if b.Confianza != ConfianzaBaja {
			t.Errorf("%q debería dar confianza baja, dio %q", nombre, b.Confianza)
		}
		if len(b.Avisos) == 0 {
			t.Errorf("%q debería generar un aviso explicando el problema", nombre)
		}
	}
}

func TestConfianzaAltaConDatosCompletos(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    "Memoria Kingston 16GB DDR5-5600 UDIMM",
		Marca:     "KINGSTON",
		CategPath: "Almacenamiento / Memorias / RAM / DDR5 / UDIMM",
		SKU:       "KVR56U46BS8-16",
	})
	if b.Confianza != ConfianzaAlta {
		t.Fatalf("confianza = %q, se esperaba alta. Avisos: %v", b.Confianza, b.Avisos)
	}
}

func TestDescripcionNoInventaNada(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    "Monitor 15\" Touch Screen Display",
		Marca:     "AON",
		CategPath: "POS / Cómputo / Monitor Táctil",
		SKU:       "AO-MO-1000",
		Peso:      2.4,
	})

	// Solo puede afirmar lo que viene del origen.
	if !strings.Contains(b.Descripcion, "AO-MO-1000") {
		t.Error("la descripción debería incluir la referencia")
	}
	if !strings.Contains(b.Descripcion, "2.40 kg") {
		t.Error("la descripción debería incluir el peso conocido")
	}
	// Nada de adjetivos comerciales inventados.
	for _, prohibido := range []string{"excelente", "mejor", "increíble", "garantiza", "líder"} {
		if strings.Contains(strings.ToLower(b.Descripcion), prohibido) {
			t.Errorf("la descripción no debe inventar afirmaciones comerciales (%q):\n%s",
				prohibido, b.Descripcion)
		}
	}
	t.Logf("descripción generada:\n%s", b.Descripcion)
}

// El nombre ya dice "Monitor"; anteponerle "Monitor Táctil" produciría
// "Monitor Táctil Monitor 15…", que se lee fatal en cualquier tienda.
func TestNoTartamudeaConLaFamilia(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    `Monitor 15" Touch Screen Display`,
		Marca:     "AON",
		CategPath: "POS / Cómputo / Monitor Táctil",
	})
	for canal, tit := range b.Titulos {
		if strings.Count(strings.ToLower(tit), "monitor") > 1 {
			t.Errorf("%s repite el sustantivo: %q", canal, tit)
		}
	}
	if strings.Count(strings.ToLower(b.Descripcion), "monitor") > 1 {
		t.Errorf("la descripción repite el sustantivo:\n%s", b.Descripcion)
	}
}

// El extractor normaliza "2TB" a "2 TB" para que se lea bien; hay que
// reconocer que son lo mismo o el título repite la capacidad.
func TestNoRepiteEspecificacionesYaPresentes(t *testing.T) {
	casos := []struct {
		nombre string
		categ  string
		marca  string
		repe   string
	}{
		{"Disco Estado Solido Externo SSD KINGSTON-2TB Negro",
			"Almacenamiento / Disco / SSD / Externo", "KINGSTON", "2 tb"},
		{"1TB SSD 2.5 SATA", "Almacenamiento / Disco / SSD / Interno", "SANDISK", "1 tb"},
		{"Kingston 32GB DDR5 SDRAM módulo de memoria",
			"Almacenamiento / Memorias / RAM / DDR5", "KINGSTON", "32 gb"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			b := Generar(Fuente{Nombre: c.nombre, Marca: c.marca, CategPath: c.categ})
			tit := strings.ToLower(b.Titulos["shopify"])
			sinEspacios := strings.ReplaceAll(tit, " ", "")
			buscado := strings.ReplaceAll(c.repe, " ", "")
			if strings.Count(sinEspacios, buscado) > 1 {
				t.Errorf("la capacidad aparece repetida: %q", b.Titulos["shopify"])
			}
		})
	}
}

// "Disco SSD Sandisk 1TB SSD 2.5 SATA" repite el tipo de producto.
func TestNoRepiteElTipoSiElNombreYaLoDice(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    "1TB SSD 2.5 SATA",
		Marca:     "SANDISK",
		CategPath: "Almacenamiento / Disco / SSD / Interno / SATA 2.5\"",
	})
	tit := strings.ToLower(b.Titulos["shopify"])
	if strings.Count(tit, "ssd") > 1 {
		t.Errorf("el tipo aparece repetido: %q", b.Titulos["shopify"])
	}
}

func TestNoDuplicaLaMarcaSiYaEstaEnElNombre(t *testing.T) {
	b := Generar(Fuente{
		Nombre:    "Memoria Kingston 16GB DDR5-5600 UDIMM",
		Marca:     "KINGSTON",
		CategPath: "Almacenamiento / Memorias / RAM / DDR5",
	})
	titulo := strings.ToLower(b.Titulos["shopify"])
	if strings.Count(titulo, "kingston") != 1 {
		t.Errorf("la marca aparece %d veces: %q", strings.Count(titulo, "kingston"), b.Titulos["shopify"])
	}
}

func TestRecortarPorPalabras(t *testing.T) {
	s := "Memoria RAM Kingston 32GB DDR5-5600 UDIMM Non-ECC Unbuffered CL46"
	got := recortarPorPalabras(s, 30)
	if len([]rune(got)) > 30 {
		t.Fatalf("mide %d: %q", len([]rune(got)), got)
	}
	if strings.HasSuffix(got, " ") || strings.HasSuffix(got, "-") {
		t.Errorf("terminación fea: %q", got)
	}
	// No debe partir una palabra por la mitad.
	if !strings.HasPrefix(s, got) {
		t.Errorf("el recorte debería ser un prefijo del original: %q", got)
	}
}
