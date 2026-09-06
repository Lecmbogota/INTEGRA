package mercadolibre

import "testing"

// Todos los casos salieron de ejecutar el predictor sobre el catálogo real.
func TestCoherenciaSeparaAciertosDeDisparates(t *testing.T) {
	casos := []struct {
		nombre   string
		sugerida string
		rutaOdoo string
		acierto  bool
	}{
		// Aciertos claros: comparten vocabulario con la rama de origen.
		{"memorias RAM", "Memorias RAM", "Almacenamiento / Memorias / RAM / DDR5 / UDIMM", true},
		{"discos", "Discos Duros y SSDs", "Almacenamiento / Disco / Mecanico / Externo / USB 3.0", true},
		{"impresoras térmicas", "Impresoras Térmicas",
			"POS / Impresión Térmica / Recibos / Ancho de Impresión: 80mm", true},
		{"lectores de códigos", "Lectores de Códigos de Barras",
			"POS / Lector de Códigos / Alámbrico / Tipo de Código: 1D/2D", true},
		{"monitores", "Monitores", "Cómputo / Monitor de Escritorio", true},
		{"tablets", "Tablets", "Cómputo / Tabletas / Procesador: MediaTek", true},
		{"mini pcs", "Mini PCs", "POS / Cómputo / Mini CPU / Procesador: Intel", true},

		// Disparates reales: el predictor se enganchó a una palabra suelta.
		{"cajones → dados", "Dados", "POS / Cajones Monederos / Billetes: 3 / Monedas: 4", false},
		{"AIO → sistemas operativos", "Sistemas Operativos",
			"POS / Cómputo / AIO / Procesador: Intel / OS: Windows", false},
		{"corporativo → skins", "Skins",
			"Cómputo / Corporativo / Procesador: Intel / OS: Windows", false},
		{"lector → etiquetas", "Etiquetas",
			"POS / Lector de Códigos / Inalámbrico / Tipo de Código: 1D/2D", false},
		{"batería → terminales POS", "Terminales POS para tarjetas",
			"POS / Soportes y Accesorios / Accesorios / Batería Recargable", false},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			coh := Coherencia(c.sugerida, c.rutaOdoo, "")
			conf := ConfianzaCon(Sugerencia{Categoria: c.sugerida}, 0, c.rutaOdoo, "")

			if c.acierto && RequiereRevision(conf) {
				t.Errorf("%q sobre %q debería pasar sin revisión (coherencia=%.2f, confianza=%.2f)",
					c.sugerida, c.rutaOdoo, coh, conf)
			}
			if !c.acierto && !RequiereRevision(conf) {
				t.Errorf("%q sobre %q debería marcarse para revisión (coherencia=%.2f, confianza=%.2f)",
					c.sugerida, c.rutaOdoo, coh, conf)
			}
		})
	}
}

func TestCoherenciaPorRaiz(t *testing.T) {
	// "Impresoras" e "Impresión" comparten raíz aunque no sean iguales.
	if Coherencia("Impresoras Térmicas", "POS / Impresión Térmica / Recibos", "") == 0 {
		t.Error("impresoras/impresión deberían reconocerse como afines")
	}
	// "Lectores" y "Lector".
	if Coherencia("Lectores de Memorias", "POS / Lectores de Memoria / Lector de Tarjetas", "") == 0 {
		t.Error("lectores/lector deberían reconocerse como afines")
	}
	// Pero raíces cortas no deben producir falsos positivos.
	if raizComun("dado", "dato") {
		t.Error("palabras de menos de 5 letras no deberían compararse por raíz")
	}
}

func TestCoherenciaUsaLaFamilia(t *testing.T) {
	// Cuando la ruta de Odoo no ayuda, la familia detectada por el generador
	// de contenido puede salvar la coherencia.
	sinFamilia := Coherencia("Pad Mouses", "Gaming / Mouse Pads / Grande", "")
	conFamilia := Coherencia("Pad Mouses", "Gaming / Mouse Pads / Grande", "Mouse Pad")
	if conFamilia < sinFamilia {
		t.Error("añadir la familia no debería empeorar la coherencia")
	}
}

func TestCoherenciaCasosLimite(t *testing.T) {
	if Coherencia("", "algo", "") != 0 {
		t.Error("una sugerencia vacía no tiene coherencia")
	}
	if c := Coherencia("Memorias RAM", "", ""); c != 0 {
		t.Errorf("sin ruta de origen la coherencia debería ser 0, fue %.2f", c)
	}
	// Las palabras vacías no deben contar como coincidencia.
	if c := Coherencia("Accesorios de Otros", "POS / Accesorios / Otros", ""); c != 0 {
		t.Errorf("solo palabras vacías no debería dar coherencia, fue %.2f", c)
	}
}

func TestConfianzaConPenalizaGradualmente(t *testing.T) {
	s := Sugerencia{Categoria: "Memorias RAM"}
	buena := ConfianzaCon(s, 0, "Almacenamiento / Memorias / RAM", "")
	mala := ConfianzaCon(Sugerencia{Categoria: "Dados"}, 0, "POS / Cajones Monederos", "")

	if buena <= mala {
		t.Errorf("una sugerencia coherente debería puntuar más: %.2f vs %.2f", buena, mala)
	}
	if mala >= 0.5 {
		t.Errorf("una sugerencia incoherente debería quedar por debajo de 0,5: %.2f", mala)
	}
}
