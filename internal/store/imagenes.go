package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ImagenGuardada es una fila del banco de imágenes.
type ImagenGuardada struct {
	ID           int64  `json:"id"`
	SHA256       string `json:"sha256"`
	Ruta         string `json:"-"`
	Formato      string `json:"formato"`
	Ancho        int    `json:"ancho"`
	Alto         int    `json:"alto"`
	Bytes        int    `json:"bytes"`
	Origen       string `json:"origen"`
	Posicion     int    `json:"posicion"`
	Principal    bool   `json:"principal"`
}

// RegistrarImagen da de alta una imagen y sus derivadas.
//
// Si el contenido ya estaba —mismo hash— se reutiliza la fila existente en vez
// de duplicarla: la misma foto de fabricante suele valer para varias variantes
// del mismo modelo.
func (s *Store) RegistrarImagen(ctx context.Context, sha, ruta, formato string,
	ancho, alto, bytes int, origen, origenRef string,
	derivadas []struct {
		Variante, Ruta, Formato string
		Ancho, Alto, Bytes      int
	}) (int64, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO imagenes (sha256, ruta, formato, ancho, alto, bytes, origen, origen_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (sha256) DO UPDATE SET ruta = EXCLUDED.ruta
		RETURNING id`,
		sha, ruta, formato, ancho, alto, bytes, origen, nulo(origenRef)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("registrando la imagen: %w", err)
	}

	for _, d := range derivadas {
		_, err = tx.Exec(ctx, `
			INSERT INTO imagen_derivadas (imagen_id, variante, ruta, formato, ancho, alto, bytes)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (imagen_id, variante) DO UPDATE
			SET ruta = EXCLUDED.ruta, ancho = EXCLUDED.ancho,
			    alto = EXCLUDED.alto, bytes = EXCLUDED.bytes`,
			id, d.Variante, d.Ruta, d.Formato, d.Ancho, d.Alto, d.Bytes)
		if err != nil {
			return 0, fmt.Errorf("registrando la derivada %s: %w", d.Variante, err)
		}
	}
	return id, tx.Commit(ctx)
}

// AsociarImagen enlaza una imagen a un producto, al final de la lista.
//
// La primera imagen de un producto queda como principal automáticamente: es lo
// que espera cualquiera al subir una sola foto.
func (s *Store) AsociarImagen(ctx context.Context, productoID, imagenID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var siguiente int
	var hayPrincipal bool
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(max(posicion) + 1, 0),
		       COALESCE(bool_or(principal), FALSE)
		FROM producto_imagenes WHERE product_id = $1`, productoID).Scan(&siguiente, &hayPrincipal)
	if err != nil {
		return fmt.Errorf("calculando la posición: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO producto_imagenes (product_id, imagen_id, posicion, principal)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (product_id, imagen_id) DO NOTHING`,
		productoID, imagenID, siguiente, !hayPrincipal)
	if err != nil {
		return fmt.Errorf("asociando la imagen al producto: %w", err)
	}
	return tx.Commit(ctx)
}

// ImagenesDeProducto devuelve las imágenes en su orden.
func (s *Store) ImagenesDeProducto(ctx context.Context, productoID int64) ([]ImagenGuardada, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT i.id, i.sha256, i.ruta, i.formato, i.ancho, i.alto, i.bytes, i.origen,
		       pi.posicion, pi.principal
		FROM producto_imagenes pi
		JOIN imagenes i ON i.id = pi.imagen_id
		WHERE pi.product_id = $1
		ORDER BY pi.principal DESC, pi.posicion`, productoID)
	if err != nil {
		return nil, fmt.Errorf("listando imágenes del producto: %w", err)
	}
	defer filas.Close()

	var out []ImagenGuardada
	for filas.Next() {
		var i ImagenGuardada
		if err := filas.Scan(&i.ID, &i.SHA256, &i.Ruta, &i.Formato, &i.Ancho, &i.Alto,
			&i.Bytes, &i.Origen, &i.Posicion, &i.Principal); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, filas.Err()
}

// RutaDeImagen resuelve la ruta en disco de una imagen o de una derivada suya.
func (s *Store) RutaDeImagen(ctx context.Context, sha, variante string) (string, error) {
	var ruta string
	var err error

	if variante == "" {
		err = s.pool.QueryRow(ctx, `SELECT ruta FROM imagenes WHERE sha256 = $1`, sha).Scan(&ruta)
	} else {
		err = s.pool.QueryRow(ctx, `
			SELECT d.ruta FROM imagen_derivadas d
			JOIN imagenes i ON i.id = d.imagen_id
			WHERE i.sha256 = $1 AND d.variante = $2`, sha, variante).Scan(&ruta)
	}
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("no existe la imagen %s (variante %q)", sha, variante)
	}
	return ruta, err
}

// DesasociarImagen quita una imagen de un producto.
//
// No borra el fichero: puede estar asociado a otros productos. La limpieza de
// huérfanas es una tarea aparte, para no perder por accidente una foto que
// otro producto sigue usando.
func (s *Store) DesasociarImagen(ctx context.Context, productoID, imagenID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var eraPrincipal bool
	err = tx.QueryRow(ctx, `
		DELETE FROM producto_imagenes
		WHERE product_id = $1 AND imagen_id = $2
		RETURNING principal`, productoID, imagenID).Scan(&eraPrincipal)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("el producto %d no tiene asociada la imagen %d", productoID, imagenID)
	}
	if err != nil {
		return err
	}

	// Si se quitó la principal, asciende la siguiente: un producto sin imagen
	// principal se publicaría sin foto de portada.
	if eraPrincipal {
		_, err = tx.Exec(ctx, `
			UPDATE producto_imagenes SET principal = TRUE
			WHERE (product_id, imagen_id) = (
			    SELECT product_id, imagen_id FROM producto_imagenes
			    WHERE product_id = $1 ORDER BY posicion LIMIT 1)`, productoID)
		if err != nil {
			return fmt.Errorf("reasignando la imagen principal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// MarcarPrincipal cambia cuál es la imagen de portada.
func (s *Store) MarcarPrincipal(ctx context.Context, productoID, imagenID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Primero se baja la actual: el índice único impide tener dos a la vez.
	if _, err := tx.Exec(ctx,
		`UPDATE producto_imagenes SET principal = FALSE WHERE product_id = $1 AND principal`,
		productoID); err != nil {
		return err
	}
	ct, err := tx.Exec(ctx,
		`UPDATE producto_imagenes SET principal = TRUE WHERE product_id = $1 AND imagen_id = $2`,
		productoID, imagenID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("el producto %d no tiene asociada la imagen %d", productoID, imagenID)
	}
	return tx.Commit(ctx)
}

// ProductoDeVariante resuelve el producto al que pertenece una variante.
func (s *Store) ProductoDeVariante(ctx context.Context, varianteID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT product_id FROM product_variants WHERE id = $1`, varianteID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("no existe la variante %d", varianteID)
	}
	return id, err
}

// DatosBusquedaImagen devuelve lo necesario para armar la consulta con la que
// buscar fotos del producto en internet.
func (s *Store) DatosBusquedaImagen(ctx context.Context, varianteID int64) (sku, nombre, marca string, shas []string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(v.sku,''), p.name, COALESCE(b.name,''),
		       COALESCE(ARRAY(
		           SELECT i.sha256 FROM producto_imagenes pi
		           JOIN imagenes i ON i.id = pi.imagen_id
		           WHERE pi.product_id = p.id), '{}')
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE v.id = $1`, varianteID).Scan(&sku, &nombre, &marca, &shas)
	if err == pgx.ErrNoRows {
		err = fmt.Errorf("no existe la variante %d", varianteID)
	}
	return
}

// ObjetivoBusqueda es un producto candidato a la búsqueda masiva de imágenes.
type ObjetivoBusqueda struct {
	VarianteID int64
	ProductoID int64
	SKU        string
	Nombre     string
	Marca      string
}

// ObjetivosBusquedaImagenes lista la mercancía publicable con SKU que aún
// necesita fotos. Sin SKU no se busca: la consulta sería demasiado ambigua
// para confiar en lo que devuelva.
//
// Con soloSinFoto, el universo son los productos sin ninguna imagen. Sin él,
// el criterio es más exigente: productos sin ninguna imagen APTA para los
// cuatro canales a la vez — al menos 600 px de lado (mínimo de Falabella),
// JPEG o PNG (WebP lo rechazan MercadoLibre y Falabella) y hasta 5 MB.
func (s *Store) ObjetivosBusquedaImagenes(ctx context.Context, soloSinFoto bool) ([]ObjetivoBusqueda, error) {
	criterio := `NOT EXISTS (
		SELECT 1 FROM producto_imagenes pi
		JOIN imagenes i ON i.id = pi.imagen_id
		WHERE pi.product_id = p.id
		  AND least(i.ancho, i.alto) >= 600
		  AND i.formato IN ('jpeg','png')
		  AND i.bytes <= 5*1024*1024)`
	if soloSinFoto {
		criterio = `NOT EXISTS (SELECT 1 FROM producto_imagenes pi WHERE pi.product_id = p.id)`
	}
	filas, err := s.pool.Query(ctx, `
		SELECT v.id, p.id, v.sku, p.name, COALESCE(b.name,'')
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE v.active AND p.active AND p.excluded_reason IS NULL
		  AND v.sku IS NOT NULL
		  AND `+criterio+`
		ORDER BY p.name`)
	if err != nil {
		return nil, fmt.Errorf("listando productos sin imagen: %w", err)
	}
	defer filas.Close()

	var out []ObjetivoBusqueda
	for filas.Next() {
		var o ObjetivoBusqueda
		if err := filas.Scan(&o.VarianteID, &o.ProductoID, &o.SKU, &o.Nombre, &o.Marca); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, filas.Err()
}

// ImagenReprocesable es una imagen del banco pendiente de transcodificar.
type ImagenReprocesable struct {
	ID        int64
	Ruta      string
	Origen    string
	OrigenRef string
}

// ImagenesWebP lista las imágenes guardadas en WebP, que MercadoLibre y
// Falabella rechazan y por tanto hay que transcodificar a JPEG.
func (s *Store) ImagenesWebP(ctx context.Context) ([]ImagenReprocesable, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT id, ruta, origen, COALESCE(origen_ref,'')
		FROM imagenes WHERE formato = 'webp' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listando imágenes WebP: %w", err)
	}
	defer filas.Close()

	var out []ImagenReprocesable
	for filas.Next() {
		var i ImagenReprocesable
		if err := filas.Scan(&i.ID, &i.Ruta, &i.Origen, &i.OrigenRef); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, filas.Err()
}

// SustituirImagen pasa las asociaciones de una imagen vieja a su versión
// nueva (conservando posición y portada, porque se actualizan en sitio) y
// borra la vieja. Devuelve las rutas de fichero que quedaron huérfanas para
// que el llamador las elimine del disco.
func (s *Store) SustituirImagen(ctx context.Context, viejaID, nuevaID int64) ([]string, error) {
	if viejaID == nuevaID {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Si algún producto ya tenía también la versión nueva, su fila vieja no se
	// puede renombrar (chocaría con la clave) y se elimina después.
	if _, err := tx.Exec(ctx, `
		UPDATE producto_imagenes pi SET imagen_id = $2
		WHERE pi.imagen_id = $1
		  AND NOT EXISTS (SELECT 1 FROM producto_imagenes x
		                  WHERE x.product_id = pi.product_id AND x.imagen_id = $2)`,
		viejaID, nuevaID); err != nil {
		return nil, fmt.Errorf("moviendo asociaciones: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM producto_imagenes WHERE imagen_id = $1`, viejaID); err != nil {
		return nil, err
	}

	var rutas []string
	filas, err := tx.Query(ctx, `
		SELECT ruta FROM imagen_derivadas WHERE imagen_id = $1
		UNION ALL
		SELECT ruta FROM imagenes WHERE id = $1`, viejaID)
	if err != nil {
		return nil, err
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

	if _, err := tx.Exec(ctx, `DELETE FROM imagen_derivadas WHERE imagen_id = $1`, viejaID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM imagenes WHERE id = $1`, viejaID); err != nil {
		return nil, err
	}
	return rutas, tx.Commit(ctx)
}

// ResumenImagenes cuenta el estado del banco.
type ResumenImagenes struct {
	Total            int `json:"total"`
	ProductosConFoto int `json:"productos_con_foto"`
	ProductosSinFoto int `json:"productos_sin_foto"`
	AptasML          int `json:"aptas_mercadolibre"`
}

func (s *Store) ResumenImagenes(ctx context.Context) (*ResumenImagenes, error) {
	var r ResumenImagenes
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM imagenes),
		  (SELECT count(DISTINCT product_id) FROM producto_imagenes),
		  (SELECT count(*) FROM products p
		     WHERE p.excluded_reason IS NULL AND p.active
		       AND NOT EXISTS (SELECT 1 FROM producto_imagenes pi WHERE pi.product_id = p.id)),
		  (SELECT count(*) FROM imagenes WHERE least(ancho, alto) >= 500
		     AND formato IN ('jpeg','png'))
	`).Scan(&r.Total, &r.ProductosConFoto, &r.ProductosSinFoto, &r.AptasML)
	if err != nil {
		return nil, fmt.Errorf("resumen de imágenes: %w", err)
	}
	return &r, nil
}
