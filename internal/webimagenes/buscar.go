// Package webimagenes busca fotos de producto en internet a partir del SKU.
//
// Usa la búsqueda de imágenes de DuckDuckGo, que no exige clave de API. Es un
// endpoint no documentado (el mismo que consume su propia web), así que puede
// romperse sin aviso: los errores se devuelven claros para que la interfaz
// pueda decir "reintenta" en vez de fallar en silencio. Si algún día hace
// falta algo con contrato, el reemplazo natural es Google Programmable Search
// con clave de API.
package webimagenes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Candidata es una imagen encontrada, aún sin descargar.
type Candidata struct {
	URL      string // la imagen a tamaño completo
	Titulo   string
	Fuente   string // página donde aparece
	Ancho    int
	Alto     int
	Redirect string // host de la fuente, para citar el origen
}

// agenteUsuario imita un navegador corriente: el endpoint rechaza clientes
// sin User-Agent.
const agenteUsuario = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

type Buscador struct {
	cli *http.Client
}

func NuevoBuscador() *Buscador {
	return &Buscador{cli: &http.Client{Timeout: 20 * time.Second}}
}

var reVQD = regexp.MustCompile(`vqd="?([\d-]+)"?`)

// Buscar devuelve candidatas para la consulta, mayores que minLado píxeles.
func (b *Buscador) Buscar(ctx context.Context, consulta string, minLado, max int) ([]Candidata, error) {
	consulta = strings.TrimSpace(consulta)
	if consulta == "" {
		return nil, fmt.Errorf("la consulta está vacía")
	}

	// Primer paso: la página de resultados entrega el token vqd que el
	// endpoint JSON exige para responder.
	vqd, err := b.obtenerVQD(ctx, consulta)
	if err != nil {
		return nil, err
	}

	q := url.Values{
		"l":   {"es-co"},
		"o":   {"json"},
		"q":   {consulta},
		"vqd": {vqd},
		"f":   {",,,"},
		"p":   {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://duckduckgo.com/i.js?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agenteUsuario)
	req.Header.Set("Referer", "https://duckduckgo.com/")
	req.Header.Set("Accept", "application/json")

	resp, err := b.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando la búsqueda de imágenes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("la búsqueda de imágenes respondió HTTP %d (el endpoint no es oficial; reintenta)", resp.StatusCode)
	}

	var cuerpo struct {
		Results []struct {
			Title  string `json:"title"`
			Image  string `json:"image"`
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Source string `json:"source"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cuerpo); err != nil {
		return nil, fmt.Errorf("respuesta de búsqueda ilegible: %w", err)
	}

	// Dos pasadas conservando el orden de relevancia del buscador: primero las
	// que alcanzan los 1200 px del zoom (el óptimo común de los marketplaces),
	// después las que solo llegan al mínimo. Así, si hay material bueno, es lo
	// que se descarga.
	var out []Candidata
	for _, soloOptimas := range []bool{true, false} {
		for _, r := range cuerpo.Results {
			menor := r.Width
			if r.Height < menor {
				menor = r.Height
			}
			if menor < minLado || (menor >= 1200) != soloOptimas {
				continue
			}
			out = append(out, Candidata{
				URL: r.Image, Titulo: r.Title, Fuente: r.URL,
				Ancho: r.Width, Alto: r.Height, Redirect: hostDe(r.URL),
			})
			if max > 0 && len(out) >= max {
				return out, nil
			}
		}
	}
	return out, nil
}

func (b *Buscador) obtenerVQD(ctx context.Context, consulta string) (string, error) {
	q := url.Values{"q": {consulta}, "iax": {"images"}, "ia": {"images"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://duckduckgo.com/?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", agenteUsuario)

	resp, err := b.cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("abriendo la búsqueda: %w", err)
	}
	defer resp.Body.Close()

	cuerpo, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	m := reVQD.FindSubmatch(cuerpo)
	if m == nil {
		return "", fmt.Errorf("no se encontró el token de búsqueda (¿cambió DuckDuckGo?)")
	}
	return string(m[1]), nil
}

// Descargar trae el fichero de una candidata, acotado a maxBytes.
func (b *Buscador) Descargar(ctx context.Context, imagenURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imagenURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agenteUsuario)
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")

	resp, err := b.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	datos, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(datos)) > maxBytes {
		return nil, fmt.Errorf("supera el límite de %d bytes", maxBytes)
	}
	return datos, nil
}

func hostDe(pagina string) string {
	u, err := url.Parse(pagina)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
