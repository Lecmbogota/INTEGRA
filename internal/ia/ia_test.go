package ia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Un servidor de mentira que habla como Ollama. Permite probar el protocolo
// entero —esquema incluido— sin depender de que haya un modelo descargado, que
// es lo que haría estas pruebas inútiles en integración continua.
func servidorFalso(t *testing.T, modelos []string, responder func(peticionOllama) (string, string)) *Ollama {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		var e etiquetasOllama
		for _, m := range modelos {
			e.Models = append(e.Models, struct {
				Name string `json:"name"`
			}{Name: m})
		}
		_ = json.NewEncoder(w).Encode(e)
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var p peticionOllama
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("petición ilegible: %v", err)
			return
		}
		contenido, errMsg := responder(p)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": contenido},
			"error":   errMsg,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	o := NuevoOllama()
	o.base = srv.URL
	o.modelo = "modelo-texto"
	o.modeloVision = "modelo-vision"
	return o
}

// Una descripción que pasa el mínimo, para los casos donde lo que se prueba
// es otra cosa.
const descripcionLarga = "El disco está diseñado para sistemas de almacenamiento en red que trabajan sin descanso. Su formato de 3.5 pulgadas y su velocidad de 7200 rpm lo hacen apto para equipos NAS de escritorio que leen y escriben de forma constante. Resulta apropiado para respaldos y archivos compartidos."

const fichaValida = `{
  "titulos": {
    "mercadolibre": "Disco Duro Toshiba N300 8TB NAS 7200rpm",
    "falabella": "Disco Duro Interno Toshiba N300 8TB 3.5 pulgadas NAS 7200rpm",
    "woocommerce": "Disco Duro Toshiba N300 8TB",
    "shopify": "Disco Duro Toshiba N300 8TB"
  },
  "descripcion": "El Toshiba N300 de 8TB está diseñado para sistemas de almacenamiento en red que trabajan sin descanso. Su formato de 3.5 pulgadas y su velocidad de 7200 rpm lo hacen apto para equipos NAS de escritorio que leen y escriben de forma constante. Resulta apropiado para respaldos, archivos compartidos y bibliotecas multimedia en casa u oficina.",
  "specs": [{"clave": "Capacidad", "valor": "8TB"}]
}`

func TestGenerarFichaLeeLaRespuestaYMandaElEsquema(t *testing.T) {
	var vistas peticionOllama
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(p peticionOllama) (string, string) {
			vistas = p
			return fichaValida, ""
		})

	f, err := o.GenerarFicha(context.Background(), "N300 8TB", "TOSHIBA", "Discos", "HDWG780XZSTA", "")
	if err != nil {
		t.Fatalf("generando: %v", err)
	}
	if f.Descripcion == "" || len(f.Titulos) != 4 || len(f.Specs) != 1 {
		t.Fatalf("ficha mal leída: %+v", f)
	}

	// El esquema es lo que hace fiable a un modelo pequeño; si dejara de
	// enviarse, la ficha seguiría saliendo bien en esta prueba pero fallaría
	// una de cada varias contra un modelo real.
	if len(vistas.Format) == 0 {
		t.Error("no se envió el esquema JSON en «format»")
	}
	if vistas.Stream {
		t.Error("se pidió streaming; la respuesta se lee de una vez")
	}
	if vistas.Model != "modelo-texto" {
		t.Errorf("modelo = %q, se esperaba el de texto", vistas.Model)
	}
}

func TestVerificarImagenMandaLaFotoAlModeloDeVision(t *testing.T) {
	var vistas peticionOllama
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(p peticionOllama) (string, string) {
			vistas = p
			return `{"resultado":"corresponde","nota":"es el disco"}`, ""
		})

	v, err := o.VerificarImagen(context.Background(), []byte("jpeg-de-mentira"), "N300", "TOSHIBA", "ABC")
	if err != nil {
		t.Fatalf("verificando: %v", err)
	}
	if v.Resultado != "corresponde" {
		t.Errorf("resultado = %q", v.Resultado)
	}
	if vistas.Model != "modelo-vision" {
		t.Errorf("modelo = %q; la visión tiene que usar el modelo multimodal", vistas.Model)
	}
	if len(vistas.Messages) != 1 || len(vistas.Messages[0].Images) != 1 {
		t.Fatalf("la imagen no viajó en el mensaje: %+v", vistas.Messages)
	}
}

// Los modelos locales envuelven el JSON en cercas o lo preceden de charla, y
// algunos de razonamiento anteponen un bloque <think>. Descartar esas
// respuestas sería tirar fichas perfectamente buenas.
func TestSeToleraLaCharlaAlrededorDelJSON(t *testing.T) {
	casos := map[string]string{
		"cercas markdown": "```json\n" + fichaValida + "\n```",
		"preámbulo":       "Aquí tienes la ficha:\n\n" + fichaValida,
		"bloque think":    "<think>El SKU parece un disco duro.</think>\n" + fichaValida,
		"cola":            fichaValida + "\n\nEspero que te sirva.",
	}
	for nombre, respuesta := range casos {
		t.Run(nombre, func(t *testing.T) {
			o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
				func(peticionOllama) (string, string) { return respuesta, "" })
			f, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", "")
			if err != nil {
				t.Fatalf("no se pudo leer: %v", err)
			}
			if f.Descripcion == "" {
				t.Error("descripción vacía")
			}
		})
	}
}

// Una ficha sin descripción es peor que un fallo: se guardaría, cerraría el
// aviso «sin descripción» y nadie volvería a mirar ese producto.
func TestFichaIncompletaSeRechaza(t *testing.T) {
	casos := map[string]string{
		"sin descripción": `{"titulos":{"mercadolibre":"x"},"descripcion":"","specs":[]}`,
		"sin títulos":     `{"titulos":{},"descripcion":"` + descripcionLarga + `","specs":[]}`,
		"no es json":      `lo siento, no puedo ayudarte con eso`,
	}
	for nombre, respuesta := range casos {
		t.Run(nombre, func(t *testing.T) {
			o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
				func(peticionOllama) (string, string) { return respuesta, "" })
			if f, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", ""); err == nil {
				t.Errorf("se aceptó una ficha inservible: %+v", f)
			}
		})
	}
}

// El título de MercadoLibre lo rechaza el propio canal si pasa de 60. Un
// modelo pequeño se pasa a menudo, y recortar por palabra entera salva la
// ficha en vez de descartarla.
func TestTituloLargoSeRecortaPorPalabra(t *testing.T) {
	largo := "Disco Duro Interno Toshiba N300 de 8TB para NAS 7200rpm con caché de 256MB"
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) {
			b, _ := json.Marshal(Ficha{
				Titulos:     map[string]string{"mercadolibre": largo},
				Descripcion: descripcionLarga,
			})
			return string(b), ""
		})

	f, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", "")
	if err != nil {
		t.Fatalf("generando: %v", err)
	}
	t2 := f.Titulos["mercadolibre"]
	if n := len([]rune(t2)); n > 60 {
		t.Errorf("título de %d caracteres; el límite de MercadoLibre son 60: %q", n, t2)
	}
	if strings.HasSuffix(t2, " ") || strings.HasSuffix(t2, "-") {
		t.Errorf("el recorte dejó basura al final: %q", t2)
	}
	if !strings.HasPrefix(largo, t2) {
		t.Errorf("el recorte cambió el texto: %q no es prefijo de %q", t2, largo)
	}
}

func TestVeredictoDesconocidoSeRechaza(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) {
			return `{"resultado":"si claro","nota":"x"}`, ""
		})
	if _, err := o.VerificarImagen(context.Background(), []byte("x"), "n", "m", "SKU"); err == nil {
		t.Error("se aceptó un veredicto que no es ninguno de los tres válidos")
	}
}

// Comprobar existe para dar el diagnóstico ANTES del lote; si el mensaje no
// dice qué hacer, no sirve de nada.
func TestComprobarDiceQueModeloFaltaYComoTraerlo(t *testing.T) {
	o := servidorFalso(t, []string{"otro-modelo:7b"},
		func(peticionOllama) (string, string) { return "", "" })

	err := o.Comprobar(context.Background())
	if err == nil {
		t.Fatal("no detectó que faltan los modelos configurados")
	}
	msg := err.Error()
	for _, quiero := range []string{"ollama pull modelo-texto", "ollama pull modelo-vision", "otro-modelo:7b"} {
		if !strings.Contains(msg, quiero) {
			t.Errorf("el mensaje no menciona %q:\n%s", quiero, msg)
		}
	}
}

func TestComprobarPasaConLosModelosPresentes(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto:latest", "modelo-vision"},
		func(peticionOllama) (string, string) { return "", "" })
	if err := o.Comprobar(context.Background()); err != nil {
		t.Errorf("debería pasar: %v", err)
	}
}

// Un servicio apagado es EL fallo más frecuente con un modelo local, y el
// mensaje tiene que decir cómo encenderlo.
func TestServicioApagadoDaInstrucciones(t *testing.T) {
	o := NuevoOllama()
	// Puerto cerrado a propósito.
	o.base = "http://127.0.0.1:1"

	err := o.Comprobar(context.Background())
	if err == nil {
		t.Fatal("debería fallar contra un puerto cerrado")
	}
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Errorf("el mensaje no dice cómo arrancarlo:\n%s", err)
	}
}

func TestModeloNoDescargadoSeExplicaConElPull(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) {
			return "", `model "modelo-texto" not found, try pulling it first`
		})
	_, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", "")
	if err == nil {
		t.Fatal("debería fallar")
	}
	if !strings.Contains(err.Error(), "ollama pull modelo-texto") {
		t.Errorf("el mensaje no trae el comando:\n%s", err)
	}
}

func TestNuevoRechazaProveedorDesconocido(t *testing.T) {
	t.Setenv("IA_PROVEEDOR", "gpt")
	if _, err := Nuevo(context.Background()); err == nil {
		t.Error("un proveedor inventado debería fallar")
	} else if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("el error no dice cuáles valen:\n%s", err)
	}
}

// Copiar el nombre del ERP —que empieza por el SKU— es EL fallo característico
// de un modelo pequeño. Al comprador el código no le dice nada y en
// MercadoLibre hunde la búsqueda, así que la ficha se rechaza y se reintenta.
func TestTituloQueEmpiezaPorElSKUSeRechaza(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) {
			b, _ := json.Marshal(Ficha{
				Titulos:     map[string]string{"mercadolibre": "HDWG780XZSTA 8TB N300 NAS"},
				Descripcion: descripcionLarga,
			})
			return string(b), ""
		})
	_, err := o.GenerarFicha(context.Background(), "n", "m", "c", "HDWG780XZSTA", "")
	if err == nil {
		t.Fatal("se aceptó un título que empieza por el SKU")
	}
	if !strings.Contains(err.Error(), "SKU") {
		t.Errorf("el error no explica el motivo:\n%s", err)
	}
}

// Dos frases no son una descripción: venden nada y además cierran el aviso
// «sin descripción», con lo que el producto queda peor que si no se hubiera
// tocado, porque ya nadie lo revisa.
func TestDescripcionDemasiadoCortaSeRechaza(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) {
			b, _ := json.Marshal(Ficha{
				Titulos:     map[string]string{"mercadolibre": "Disco Duro Toshiba N300 8TB"},
				Descripcion: "Un disco duro de Toshiba. Es de 8TB.",
			})
			return string(b), ""
		})
	if _, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", ""); err == nil {
		t.Error("se aceptó una descripción de dos frases")
	}
}

// Una respuesta vacía tiene que decir que está vacía, no «unexpected end of
// JSON input», que manda a buscar el fallo donde no está.
func TestRespuestaVaciaSeExplica(t *testing.T) {
	o := servidorFalso(t, []string{"modelo-texto", "modelo-vision"},
		func(peticionOllama) (string, string) { return "   ", "" })
	_, err := o.GenerarFicha(context.Background(), "n", "m", "c", "SKU", "")
	if err == nil {
		t.Fatal("debería fallar")
	}
	if !strings.Contains(err.Error(), "vacía") {
		t.Errorf("el mensaje no dice que llegó vacía:\n%s", err)
	}
}
