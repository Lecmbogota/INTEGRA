package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
)

// InformeImagenes dice si el catálogo tiene fotos de verdad.
//
// Existe porque el conteo ingenuo engaña: en Odoo, product.product.image_1920
// es un campo relacionado que cae en la imagen de la plantilla, y filtrar por
// != False devolvió los 632 productos. Contar bytes y hashes distintos es la
// única forma de saber cuántas imágenes reales hay.
type InformeImagenes struct {
	Evaluados       int              `json:"productos_evaluados"`
	ConImagen       int              `json:"con_imagen"`
	SinImagen       int              `json:"sin_imagen"`
	DistintasReales int              `json:"imagenes_distintas"`
	Repetidas       []ImagenRepetida `json:"imagenes_repetidas,omitempty"`
	Resoluciones    []Prefijo        `json:"resoluciones,omitempty"`
	BytesMedios     int              `json:"bytes_medios"`
	// MenoresDe500 cuenta las que no llegan al mínimo que pide MercadoLibre.
	MenoresDe500 int `json:"menores_de_500px"`
}

type ImagenRepetida struct {
	Hash       string `json:"hash"`
	Productos  int    `json:"productos"`
	Bytes      int    `json:"bytes"`
	Resolucion string `json:"resolucion"`
}

func (e *explorer) imagenes(muestra int) {
	inf := &InformeImagenes{}
	e.rep.Imagenes = inf

	e.probe(fmt.Sprintf("Imágenes reales (muestra de %d productos)", muestra), func() error {
		filas, err := e.cli.SearchRead("product.product",
			[]interface{}{[]interface{}{"sale_ok", "=", true}},
			[]string{"default_code", "image_1920"},
			e.kw(map[string]interface{}{"limit": muestra, "order": "id"}))
		if err != nil {
			return err
		}

		porHash := map[string]*ImagenRepetida{}
		resoluciones := map[string]int{}
		var totalBytes int

		for _, r := range filas {
			inf.Evaluados++

			// El campo llega como base64; vacío se traduce a "" por Str().
			b64 := r.Str("image_1920")
			if b64 == "" {
				inf.SinImagen++
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil || len(raw) == 0 {
				inf.SinImagen++
				continue
			}
			inf.ConImagen++
			totalBytes += len(raw)

			suma := sha256.Sum256(raw)
			h := hex.EncodeToString(suma[:])[:12]
			res := resolucionDe(raw)
			resoluciones[res]++

			if x := porHash[h]; x != nil {
				x.Productos++
			} else {
				porHash[h] = &ImagenRepetida{Hash: h, Productos: 1, Bytes: len(raw), Resolucion: res}
			}
		}

		inf.DistintasReales = len(porHash)
		if inf.ConImagen > 0 {
			inf.BytesMedios = totalBytes / inf.ConImagen
		}

		for _, v := range porHash {
			// Una imagen compartida por varios productos es un marcador de
			// posición o una foto genérica: no sirve para publicar.
			if v.Productos > 1 {
				inf.Repetidas = append(inf.Repetidas, *v)
			}
		}
		sort.Slice(inf.Repetidas, func(i, j int) bool {
			return inf.Repetidas[i].Productos > inf.Repetidas[j].Productos
		})

		for res, n := range resoluciones {
			inf.Resoluciones = append(inf.Resoluciones, Prefijo{Prefijo: res, Conteo: n})
			if w, h := dimensiones(res); w > 0 && (w < 500 || h < 500) {
				inf.MenoresDe500 += n
			}
		}
		sort.Slice(inf.Resoluciones, func(i, j int) bool {
			return inf.Resoluciones[i].Conteo > inf.Resoluciones[j].Conteo
		})

		fmt.Printf("  evaluados            %d\n", inf.Evaluados)
		fmt.Printf("  con imagen           %d\n", inf.ConImagen)
		fmt.Printf("  sin imagen           %d\n", inf.SinImagen)
		fmt.Printf("  imágenes DISTINTAS   %d\n", inf.DistintasReales)
		fmt.Printf("  tamaño medio         %d bytes\n", inf.BytesMedios)
		fmt.Printf("  por debajo de 500px  %d\n", inf.MenoresDe500)

		if len(inf.Repetidas) > 0 {
			fmt.Printf("\n  Imágenes compartidas por varios productos:\n")
			for i, r := range inf.Repetidas {
				if i >= 5 {
					break
				}
				fmt.Printf("    %s  %d productos  %d bytes  %s\n",
					r.Hash, r.Productos, r.Bytes, r.Resolucion)
			}
		}
		if len(inf.Resoluciones) > 0 {
			fmt.Printf("\n  Resoluciones:\n")
			for i, r := range inf.Resoluciones {
				if i >= 8 {
					break
				}
				fmt.Printf("    %-14s %d\n", r.Prefijo, r.Conteo)
			}
		}
		return nil
	})
}

// resolucionDe lee las dimensiones de la cabecera del fichero, sin decodificar
// la imagen entera.
func resolucionDe(b []byte) string {
	// PNG: la cabecera IHDR trae ancho y alto en los bytes 16-23.
	if len(b) > 24 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		w := int(b[16])<<24 | int(b[17])<<16 | int(b[18])<<8 | int(b[19])
		h := int(b[20])<<24 | int(b[21])<<16 | int(b[22])<<8 | int(b[23])
		return fmt.Sprintf("%dx%d PNG", w, h)
	}
	// JPEG: hay que recorrer los marcadores hasta el SOF.
	if len(b) > 4 && b[0] == 0xFF && b[1] == 0xD8 {
		i := 2
		for i+9 < len(b) {
			if b[i] != 0xFF {
				i++
				continue
			}
			marca := b[i+1]
			// SOF0..SOF3 y SOF5..SOF7 llevan las dimensiones.
			if (marca >= 0xC0 && marca <= 0xC3) || (marca >= 0xC5 && marca <= 0xC7) {
				h := int(b[i+5])<<8 | int(b[i+6])
				w := int(b[i+7])<<8 | int(b[i+8])
				return fmt.Sprintf("%dx%d JPEG", w, h)
			}
			longitud := int(b[i+2])<<8 | int(b[i+3])
			if longitud <= 0 {
				break
			}
			i += 2 + longitud
		}
		return "JPEG"
	}
	// WebP: Odoo 17+ convierte las imágenes a este formato al subirlas.
	if len(b) > 30 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		switch string(b[12:16]) {
		case "VP8X":
			w := (int(b[24]) | int(b[25])<<8 | int(b[26])<<16) + 1
			h := (int(b[27]) | int(b[28])<<8 | int(b[29])<<16) + 1
			return fmt.Sprintf("%dx%d WebP", w, h)
		case "VP8 ":
			w := (int(b[26]) | int(b[27])<<8) & 0x3FFF
			h := (int(b[28]) | int(b[29])<<8) & 0x3FFF
			return fmt.Sprintf("%dx%d WebP", w, h)
		case "VP8L":
			bits := int(b[21]) | int(b[22])<<8 | int(b[23])<<16 | int(b[24])<<24
			w := (bits & 0x3FFF) + 1
			h := ((bits >> 14) & 0x3FFF) + 1
			return fmt.Sprintf("%dx%d WebP", w, h)
		}
		return "WebP"
	}
	if len(b) > 3 && string(b[0:3]) == "GIF" {
		return "GIF"
	}
	// Sin identificar: se devuelven los primeros bytes para poder averiguarlo.
	n := 8
	if len(b) < n {
		n = len(b)
	}
	return "desconocida " + hex.EncodeToString(b[:n])
}

func dimensiones(res string) (int, int) {
	var w, h int
	if _, err := fmt.Sscanf(res, "%dx%d", &w, &h); err != nil {
		return 0, 0
	}
	return w, h
}
