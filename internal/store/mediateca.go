package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Mediateca: el banco de imágenes visto entero, no producto a producto.
//
// Las fotos son el único activo de Integra que ocupa disco de verdad y que
// ningún canal perdona: una ficha sin foto no vende, y una foto pequeña la
// rechaza MercadoLibre en el alta. Mirarlas de una en una desde cada producto
// no permite ver lo que importa —cuántos productos no pueden publicarse por
// falta de imagen, cuánto disco pesa lo que ya no usa nadie— porque son
// preguntas sobre el conjunto.

// LadoMinimoCanales es el lado más pequeño que acepta el canal más exigente.
// Por debajo de esto la foto no sirve en ninguna parte.
const LadoMinimoCanales = 600

// ImagenBanco es una imagen del banco con los productos que la usan.
type ImagenBanco struct {
	ID        int64              `json:"id"`
	SHA256    string             `json:"sha256"`
	Ancho     int                `json:"ancho"`
	Alto      int                `json:"alto"`
	Bytes     int64              `json:"bytes"`
	Formato   string             `json:"formato"`
	Productos []ProductoDeImagen `json:"productos"`
	// De dónde salió: subida a mano, descargada de una búsqueda en internet
	// (con la fuente), o del banco del fabricante. Sin esto, una foto de una
	// moto entre las huérfanas no dice cómo llegó ahí.
	Origen    string    `json:"origen"`
	OrigenRef string    `json:"origen_ref"`
	Creada    time.Time `json:"creada"`
}

type ProductoDeImagen struct {
	VarianteID int64  `json:"variante_id"`
	SKU        string `json:"sku"`
	Nombre     string `json:"nombre"`
	Principal  bool   `json:"principal"`
}

// PaginaImagenes es una página del banco más los tres números que mueven
// decisiones, calculados sobre TODO el banco y no sobre la página.
type PaginaImagenes struct {
	Total   int           `json:"total"`
	Limite  int           `json:"limite"`
	Offset  int           `json:"offset"`
	Items   []ImagenBanco `json:"items"`
	SinFoto int           `json:"productos_sin_foto"`
	// Pequenas: por debajo del mínimo del canal más exigente.
	Pequenas       int   `json:"pequenas"`
	BytesHuerfanos int64 `json:"bytes_huerfanos"`
}

// Banco lista el banco de imágenes con su filtro.
//
// Los filtros son los que responden a una pregunta que alguien se hace de
// verdad: «¿qué puedo publicar?», «¿qué está demasiado pequeño?», «¿qué ocupa
// disco sin usarse?», «¿qué subí dos veces?». Un listado sin ellos son mil
// miniaturas iguales.
// ordenesBanco son los órdenes que responden a una pregunta: qué llegó
// último, qué pesa más (para vaciar disco) y qué es más pequeño (lo que
// primero rechaza un canal).
var ordenesBanco = map[string]string{
	"recientes": "i.created_at DESC, i.id DESC",
	"pesadas":   "i.bytes DESC, i.id DESC",
	"pequenas":  "LEAST(i.ancho, i.alto) ASC, i.id DESC",
	"grandes":   "LEAST(i.ancho, i.alto) DESC, i.id DESC",
}

func (s *Store) Banco(ctx context.Context, filtro, busca, orden string, limite, offset int) (*PaginaImagenes, error) {
	ordenSQL, ok := ordenesBanco[orden]
	if !ok {
		ordenSQL = ordenesBanco["recientes"]
	}
	if limite <= 0 || limite > 200 {
		limite = 60
	}
	if offset < 0 {
		offset = 0
	}

	var cond []string
	args := []any{limite, offset}
	sig := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	usada := `EXISTS (SELECT 1 FROM producto_imagenes pi WHERE pi.imagen_id = i.id)`
	switch filtro {
	case "aptas":
		cond = append(cond, fmt.Sprintf("LEAST(i.ancho, i.alto) >= %d", LadoMinimoCanales))
	case "pequenas":
		cond = append(cond, fmt.Sprintf("LEAST(i.ancho, i.alto) < %d", LadoMinimoCanales))
	case "huerfanas":
		cond = append(cond, "NOT "+usada)
	case "duplicadas":
		// Mismo tamaño y mismas dimensiones con hash distinto: no son el mismo
		// fichero —el hash es único— pero casi siempre son la misma foto
		// recomprimida, y ocupan disco dos veces.
		cond = append(cond, `EXISTS (SELECT 1 FROM imagenes o
			WHERE o.id <> i.id AND o.bytes = i.bytes AND o.ancho = i.ancho AND o.alto = i.alto)`)
	}
	if b := strings.TrimSpace(busca); b != "" {
		// Se busca por lo que el operador tiene a mano: el SKU o el nombre del
		// producto. Nadie recuerda un sha256.
		cond = append(cond, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM producto_imagenes pi
			JOIN product_variants v ON v.product_id = pi.product_id
			JOIN products p ON p.id = pi.product_id
			WHERE pi.imagen_id = i.id
			  AND (v.sku ILIKE %s OR p.name ILIKE %s))`, sig("%"+b+"%"), sig("%"+b+"%")))
	}

	donde := ""
	if len(cond) > 0 {
		donde = "WHERE " + strings.Join(cond, " AND ")
	}

	var pg PaginaImagenes
	pg.Limite, pg.Offset = limite, offset

	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM imagenes i `+donde, args[2:]...).Scan(&pg.Total); err != nil {
		return nil, fmt.Errorf("contando el banco de imágenes: %w", err)
	}

	filas, err := s.pool.Query(ctx, `
		SELECT i.id, i.sha256, i.ancho, i.alto, i.bytes, i.formato,
		       i.origen, COALESCE(i.origen_ref, ''), i.created_at
		FROM imagenes i `+donde+`
		ORDER BY `+ordenSQL+`
		LIMIT $1 OFFSET $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("listando el banco de imágenes: %w", err)
	}
	defer filas.Close()

	pg.Items = []ImagenBanco{}
	ids := []int64{}
	for filas.Next() {
		var im ImagenBanco
		if err := filas.Scan(&im.ID, &im.SHA256, &im.Ancho, &im.Alto, &im.Bytes,
			&im.Formato, &im.Origen, &im.OrigenRef, &im.Creada); err != nil {
			return nil, err
		}
		im.Productos = []ProductoDeImagen{}
		pg.Items = append(pg.Items, im)
		ids = append(ids, im.ID)
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}

	// Los productos de las imágenes de esta página, en una sola consulta: una
	// por imagen serían sesenta viajes para pintar una cuadrícula.
	if len(ids) > 0 {
		vinculos, err := s.pool.Query(ctx, `
			SELECT pi.imagen_id, v.id, COALESCE(v.sku,''), p.name, pi.principal
			FROM producto_imagenes pi
			JOIN products p ON p.id = pi.product_id
			JOIN product_variants v ON v.product_id = p.id
			WHERE pi.imagen_id = ANY($1)
			ORDER BY pi.principal DESC, v.id`, ids)
		if err != nil {
			return nil, fmt.Errorf("leyendo los productos de las imágenes: %w", err)
		}
		defer vinculos.Close()

		porImagen := map[int64][]ProductoDeImagen{}
		for vinculos.Next() {
			var imagenID int64
			var pr ProductoDeImagen
			if err := vinculos.Scan(&imagenID, &pr.VarianteID, &pr.SKU, &pr.Nombre, &pr.Principal); err != nil {
				return nil, err
			}
			porImagen[imagenID] = append(porImagen[imagenID], pr)
		}
		if err := vinculos.Err(); err != nil {
			return nil, err
		}
		for i := range pg.Items {
			if ps, ok := porImagen[pg.Items[i].ID]; ok {
				pg.Items[i].Productos = ps
			}
		}
	}

	// Los tres números del encabezado, sobre el banco entero.
	if err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM products p
		   WHERE p.active AND p.excluded_reason IS NULL
		     AND NOT EXISTS (SELECT 1 FROM producto_imagenes pi WHERE pi.product_id = p.id)),
		  (SELECT count(*) FROM imagenes WHERE LEAST(ancho, alto) < $1),
		  (SELECT COALESCE(sum(bytes), 0) FROM imagenes i
		   WHERE NOT EXISTS (SELECT 1 FROM producto_imagenes pi WHERE pi.imagen_id = i.id))`,
		LadoMinimoCanales).Scan(&pg.SinFoto, &pg.Pequenas, &pg.BytesHuerfanos); err != nil {
		return nil, fmt.Errorf("resumiendo el banco de imágenes: %w", err)
	}
	return &pg, nil
}

// BorrarDelBanco elimina una imagen que no usa ningún producto y devuelve
// las rutas de sus ficheros —el original y las derivadas— para que quien
// llama los quite del disco: la base los olvida en cascada, pero el disco no.
//
// Se niega a borrar una que sí se use: la ficha del canal apunta a esa URL, y
// dejarla sin fichero convierte una publicación viva en una con la foto rota,
// que es peor que el disco ocupado. Para quitarla de un producto está la
// pantalla del producto.
func (s *Store) BorrarDelBanco(ctx context.Context, id int64) ([]string, error) {
	var enUso int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM producto_imagenes WHERE imagen_id = $1`, id).Scan(&enUso); err != nil {
		return nil, fmt.Errorf("comprobando el uso de la imagen %d: %w", id, err)
	}
	if enUso > 0 {
		return nil, fmt.Errorf("la imagen %d la usan %d productos: quítala de ellos antes de borrarla", id, enUso)
	}

	var rutas []string
	filas, err := s.pool.Query(ctx, `SELECT ruta FROM imagen_derivadas WHERE imagen_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("leyendo las derivadas de la imagen %d: %w", id, err)
	}
	for filas.Next() {
		var r string
		if err := filas.Scan(&r); err != nil {
			filas.Close()
			return nil, err
		}
		rutas = append(rutas, r)
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		return nil, err
	}

	var original string
	if err := s.pool.QueryRow(ctx,
		`DELETE FROM imagenes WHERE id = $1 RETURNING ruta`, id).Scan(&original); err != nil {
		return nil, fmt.Errorf("borrando la imagen %d: %w", id, err)
	}
	return append(rutas, original), nil
}
