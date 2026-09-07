package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Proveedor local sobre Ollama.
//
// Se habla su API nativa y no la compatible con OpenAI porque la nativa acepta
// un esquema JSON en `format`, y eso es lo que hace usable a un modelo
// pequeño: en vez de rogarle que responda solo JSON —que un 3B incumple a
// menudo— el servidor restringe la generación y el JSON sale bien formado
// siempre.

// Modelos por defecto, elegidos MIDIENDO sobre el catálogo real de MDV y no
// por tamaño. Se probaron tres con los mismos cuatro productos:
//
//	qwen2.5:3b   1 de 4 fichas válidas,  4 s cada una. Copia el nombre del ERP
//	             en los cuatro canales e inventa specs.
//	gemma3:4b    4 de 4, 44 s. Pero leyó «SEG GEN US» como «usado» y publicaba
//	             un producto nuevo como de segunda mano, y tradujo «Rainbow 6
//	             Siege» —un videojuego— por «diseño arcoíris».
//	qwen2.5:7b   4 de 4, 54 s. El único que respeta los nombres propios y no
//	             se inventa cifras.
//
// El 7B no cabe en 4 GB de VRAM y se parte 58/42 entre CPU y GPU, así que el
// catálogo entero son unas siete horas. Es un lote que se corre una vez y de
// noche; publicar cuatrocientas fichas malas sale mucho más caro.
const (
	modeloTextoPorDefecto = "qwen2.5:7b"
	servidorPorDefecto    = "http://localhost:11434"
)

type Ollama struct {
	base   string
	modelo string
	cli    *http.Client
}

func NuevoOllama() *Ollama {
	return &Ollama{
		base:   entorno("OLLAMA_HOST", servidorPorDefecto),
		modelo: entorno("IA_MODELO", modeloTextoPorDefecto),
		// Sin tiempo límite corto: en una portátil con la GPU justa, la
		// primera petición carga el modelo en memoria y puede tardar un
		// minuto largo. Cortarla a los 30 s haría fallar precisamente el
		// arranque, que es cuando todo va más lento.
		cli: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (o *Ollama) Descripcion() string {
	return fmt.Sprintf("modelo local %s en %s", o.modelo, o.base)
}

// --------------------------------------------------------------- protocolo

type mensajeOllama struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type peticionOllama struct {
	Model    string          `json:"model"`
	Messages []mensajeOllama `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type respuestaOllama struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

// esquemaFicha restringe la generación a la forma exacta que Integra guarda.
var esquemaFicha = json.RawMessage(`{
  "type": "object",
  "properties": {
    "titulos": {
      "type": "object",
      "properties": {
        "mercadolibre": {"type": "string"},
        "falabella":    {"type": "string"},
        "woocommerce":  {"type": "string"},
        "shopify":      {"type": "string"}
      },
      "required": ["mercadolibre", "falabella", "woocommerce", "shopify"]
    },
    "descripcion": {"type": "string"},
    "specs": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "clave": {"type": "string"},
          "valor": {"type": "string"}
        },
        "required": ["clave", "valor"]
      }
    }
  },
  "required": ["titulos", "descripcion", "specs"]
}`)

func (o *Ollama) chat(ctx context.Context, p peticionOllama) (string, error) {
	cuerpo, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(o.base, "/")+"/api/chat", bytes.NewReader(cuerpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.cli.Do(req)
	if err != nil {
		return "", o.errorDeRed(err)
	}
	defer resp.Body.Close()

	var r respuestaOllama
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("respuesta ilegible de Ollama (HTTP %d): %w", resp.StatusCode, err)
	}
	if r.Error != "" {
		// El error más frecuente con diferencia es un modelo sin descargar, y
		// merece una instrucción y no un volcado.
		if strings.Contains(strings.ToLower(r.Error), "not found") {
			return "", fmt.Errorf("el modelo %q no está descargado; ejecuta:\n\n    ollama pull %s\n",
				p.Model, p.Model)
		}
		return "", fmt.Errorf("Ollama: %s", r.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama devolvió HTTP %d", resp.StatusCode)
	}
	return r.Message.Content, nil
}

func (o *Ollama) errorDeRed(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) || strings.Contains(err.Error(), "connection refused") {
		return fmt.Errorf(`no hay nadie escuchando en %s.

Arranca el servicio con «ollama serve», o instálalo desde https://ollama.com/download
si todavía no está. Si lo tienes en otra máquina, apunta OLLAMA_HOST a ella`, o.base)
	}
	return fmt.Errorf("hablando con Ollama en %s: %w", o.base, err)
}

// --------------------------------------------------------------- capacidades

func (o *Ollama) GenerarFicha(ctx context.Context, nombre, marca, categoria, sku, specsPrevias string) (*Ficha, error) {
	texto, err := o.chat(ctx, peticionOllama{
		Model:  o.modelo,
		Stream: false,
		Format: esquemaFicha,
		Messages: []mensajeOllama{
			{Role: "user", Content: promptFicha(nombre, marca, categoria, sku, specsPrevias)},
		},
		Options: map[string]any{
			// Temperatura baja: esto es copia de producto, no literatura, y
			// cuanto más creativo se pone un modelo pequeño más se inventa
			// capacidades y garantías que no están en los datos.
			"temperature": 0.3,
			"num_predict": 1024,
			// La ventana se fija a mano porque la de Ollama por defecto son
			// 4096 y este prompt lleva un ejemplo completo: entre lo que ocupa
			// el prompt y lo que hay que generar se desbordaba, y el JSON
			// llegaba cortado por la mitad. El síntoma era «unexpected end of
			// JSON input», que no apunta a la causa en absoluto.
			"num_ctx": 8192,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generando ficha de %q: %w", sku, err)
	}

	// Un modelo pequeño devuelve de vez en cuando la cadena vacía. Sin este
	// caso aparte, el error era «unexpected end of JSON input», que no dice
	// nada de lo que pasó y manda a buscar el fallo donde no está.
	if strings.TrimSpace(texto) == "" {
		return nil, fmt.Errorf("el modelo devolvió una respuesta vacía para %q", sku)
	}

	var f Ficha
	if err := json.Unmarshal([]byte(extraerJSON(texto)), &f); err != nil {
		return nil, fmt.Errorf("respuesta ilegible para %q: %w", sku, err)
	}
	if err := validarFicha(&f, sku); err != nil {
		return nil, err
	}
	return &f, nil
}

// --------------------------------------------------------------- comprobación

type etiquetasOllama struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Comprobar verifica lo que puede fallar, en el orden en que falla.
func (o *Ollama) Comprobar(ctx context.Context) error {
	ctx, cancelar := context.WithTimeout(ctx, 15*time.Second)
	defer cancelar()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(o.base, "/")+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return o.errorDeRed(err)
	}
	defer resp.Body.Close()

	var e etiquetasOllama
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return fmt.Errorf("%s responde pero no parece Ollama: %w", o.base, err)
	}

	instalados := make([]string, 0, len(e.Models))
	for _, m := range e.Models {
		instalados = append(instalados, m.Name)
	}
	sort.Strings(instalados)

	falta := []string{}
	if !tieneModelo(instalados, o.modelo) {
		falta = append(falta, o.modelo)
	}
	if len(falta) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "falta el modelo por descargar. Ejecuta:\n")
		for _, m := range falta {
			fmt.Fprintf(&b, "\n    ollama pull %s", m)
		}
		if len(instalados) > 0 {
			fmt.Fprintf(&b, "\n\nInstalados ahora mismo: %s", strings.Join(instalados, ", "))
			fmt.Fprintf(&b, "\nSi prefieres usar uno de esos, ponlo en IA_MODELO.")
		} else {
			fmt.Fprintf(&b, "\n\nNo hay ningún modelo instalado todavía.")
		}
		return errors.New(b.String())
	}
	return nil
}

// tieneModelo compara tolerando la etiqueta: Ollama lista «qwen2.5:3b» y quien
// configura escribe a veces «qwen2.5» a secas.
func tieneModelo(instalados []string, quiero string) bool {
	for _, m := range instalados {
		if m == quiero || strings.TrimSuffix(m, ":latest") == quiero {
			return true
		}
		if !strings.Contains(quiero, ":") && strings.HasPrefix(m, quiero+":") {
			return true
		}
	}
	return false
}

func entorno(clave, porDefecto string) string {
	if v := strings.TrimSpace(os.Getenv(clave)); v != "" {
		return v
	}
	return porDefecto
}
