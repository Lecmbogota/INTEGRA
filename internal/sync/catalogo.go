// Package sync lee de Odoo lo único que le pertenece —SKU, nombre y stock—
// y lo persiste. El resto del producto (precio, marca, descripción, categoría,
// exclusión) es propiedad de Integra: se edita en la interfaz y este paquete
// no lo toca jamás.
//
// La baja de un producto sí es identidad, no dato comercial: si en Odoo se
// archiva o se borra, deja de ser mercancía y no puede seguir vendiéndose en
// los canales. Por eso el sync también fija products.active y
// product_variants.active, que nadie más escribe.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// tamañoPagina acota cada lectura para no pedir el catálogo entero de golpe.
const tamañoPagina = 200

// tamañoLote acota cuántos identificadores se preguntan de una vez al
// comprobar qué productos siguen existiendo en Odoo.
const tamañoLote = 500

type Resultado struct {
	Leidos    int
	Guardados int
	Almacenes int
	// Bajas y Altas cuentan las variantes que dejaron de ser mercancía en
	// Odoo (archivadas o borradas) y las que volvieron a serlo.
	Bajas    int
	Altas    int
	Duracion time.Duration
}

// almacen es lo que el sincronizador necesita de la capa de persistencia.
//
// Se declara del lado que la consume para poder probar con un doble en
// memoria: sin esto, comprobar que un producto archivado en Odoo se marca
// inactivo exigiría un PostgreSQL con catálogo cargado.
type almacen interface {
	UpsertAlmacenes(ctx context.Context, conexionID int64, as []store.Almacen) (map[int64]int64, error)
	UpsertIdentidad(ctx context.Context, conexionID int64, ident store.Identidad) (int64, int64, error)
	VariantesPorOdooID(ctx context.Context, conexionID int64) (map[int64]int64, error)
	ReemplazarStock(ctx context.Context, filas []store.FilaStock) error
	RecalcularAtencion(ctx context.Context) error
	ActualizarWatermark(ctx context.Context, conexionID int64, hasta time.Time) error
	AjustarActividad(ctx context.Context, conexionID int64, activas, inactivas []int64) (altas, bajas int, err error)
}

type Sincronizador struct {
	cli *odoo.Client
	st  almacen
	log *slog.Logger
}

func Nuevo(cli *odoo.Client, st *store.Store, log *slog.Logger) *Sincronizador {
	return nuevoCon(cli, tienda{st}, log)
}

// nuevoCon inyecta la persistencia. Lo usan las pruebas, que sustituyen la
// base por un doble en memoria y Odoo por un servidor XML-RPC simulado.
func nuevoCon(cli *odoo.Client, st almacen, log *slog.Logger) *Sincronizador {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sincronizador{cli: cli, st: st, log: log}
}

// Catalogo trae la identidad (SKU y nombre) y el stock por almacén.
//
// Si desde es distinto de cero, solo se leen los productos modificados después
// de esa fecha: es la lectura incremental que evita traerse el catálogo
// completo cada noche. El stock se relee siempre entero, porque cambia con
// cada venta y no tiene write_date útil.
func (s *Sincronizador) Catalogo(ctx context.Context, conexionID int64, desde time.Time) (*Resultado, error) {
	inicio := time.Now()
	res := &Resultado{}

	almacenes, mapaAlmacenes, err := s.sincronizarAlmacenes(ctx, conexionID)
	if err != nil {
		return nil, err
	}
	res.Almacenes = len(almacenes)

	stock, err := s.leerStock(ctx, almacenes)
	if err != nil {
		return nil, err
	}

	var maxWrite time.Time

	dominio := []interface{}{}
	if !desde.IsZero() {
		dominio = append(dominio, []interface{}{"write_date", ">", desde.UTC().Format("2006-01-02 15:04:05")})
	}

	// Con el dominio por defecto Odoo esconde los archivados, así que un
	// producto retirado del catálogo se limitaba a dejar de aparecer en la
	// lectura: nadie se enteraba, en Integra seguía activo y los cuatro
	// canales lo seguían vendiendo con un stock que ya no existe. Con
	// active_test=false llegan con active=false y la baja se registra en la
	// misma pasada que la lee, sabiendo de qué SKU se trata.
	contexto := map[string]interface{}{"active_test": false}

	// Se acumulan los dos conjuntos en vez de escribir producto a producto:
	// la actividad se ajusta en una sola transacción al final, cuando ya
	// existen las filas que acaba de crear el upsert.
	var activos, inactivos []int64

	for offset := 0; ; offset += tamañoPagina {
		filas, err := s.cli.SearchRead("product.product", dominio,
			[]string{"default_code", "name", "product_tmpl_id", "categ_id", "write_date", "active"},
			map[string]interface{}{
				"limit": tamañoPagina, "offset": offset, "order": "id",
				"context": contexto,
			})
		if err != nil {
			return nil, fmt.Errorf("leyendo productos (offset %d): %w", offset, err)
		}
		if len(filas) == 0 {
			break
		}
		res.Leidos += len(filas)

		for _, r := range filas {
			// El write_date de un archivado también mueve la marca de agua:
			// archivar modifica el registro, y no anotarlo haría que la
			// lectura incremental volviera a traérselo cada noche.
			ts, tieneFecha := r.Time("write_date")
			if tieneFecha && ts.After(maxWrite) {
				maxWrite = ts
			}

			if !esMercancia(r) {
				inactivos = append(inactivos, r.ID())
				// No se refresca la identidad de lo que ya no es mercancía:
				// un producto archivado no vuelve a entrar en el catálogo de
				// Integra por haber sido leído.
				continue
			}
			activos = append(activos, r.ID())

			ident := store.Identidad{
				OdooTemplateID: r.RefID("product_tmpl_id"),
				OdooProductID:  r.ID(),
				Nombre:         r.Str("name"),
				SKU:            r.Str("default_code"),
				// La categoría es identificador, no dato comercial: es lo que
				// category_mappings traduce a la categoría de cada canal.
				CategPath:   r.RefName("categ_id"),
				OdooCategID: r.RefID("categ_id"),
			}
			if tieneFecha {
				ident.OdooWriteDate = &ts
			}

			if _, _, err := s.st.UpsertIdentidad(ctx, conexionID, ident); err != nil {
				return nil, err
			}
			res.Guardados++
		}

		if len(filas) < tamañoPagina {
			break
		}
	}

	// El stock se reemplaza entero y para todo el catálogo, no solo para los
	// productos que salieron en la lectura incremental: vender una unidad
	// cambia stock.quant pero no el write_date del producto, así que atarlo a
	// la identidad dejaría existencias congeladas.
	variantes, err := s.st.VariantesPorOdooID(ctx, conexionID)
	if err != nil {
		return nil, err
	}

	// La lectura por write_date no basta para las bajas: borrar un producto no
	// cambia ninguna fecha —el registro se desvanece— y lo que se archivó
	// antes de la última marca de agua no volverá a aparecer nunca. Por eso
	// cada sync contrasta además lo que Integra ya tiene guardado.
	vivosOdoo, caidosOdoo, err := s.estadoEnOdoo(variantes)
	if err != nil {
		return nil, err
	}
	activos = append(activos, vivosOdoo...)
	inactivos = append(inactivos, caidosOdoo...)

	// Un producto llega por los dos caminos, y pueden contradecirse si se
	// archiva entre una llamada y otra. Manda la baja: dejar de vender de más
	// es el error barato; el caro es vender lo que ya no existe.
	activos, inactivos = depurar(activos, inactivos)

	res.Altas, res.Bajas, err = s.st.AjustarActividad(ctx, conexionID, activos, inactivos)
	if err != nil {
		return nil, err
	}
	if res.Bajas > 0 {
		// Dejan de publicarse y de recibir precio y stock, pero la ficha que
		// ya está creada en el canal sigue viva: retirarla exige un trabajo de
		// pausa que hoy no existe, así que al menos queda dicho.
		s.log.Warn("productos dados de baja en Odoo; sus publicaciones siguen abiertas en los canales y hay que pausarlas",
			"variantes", res.Bajas, "productos_odoo", inactivos)
	}

	var filasStock []store.FilaStock
	for k, q := range stock {
		varID, ok := variantes[k.productoOdooID]
		if !ok {
			continue // existe en Odoo pero aún no en Integra: llegará en el próximo sync completo
		}
		dbWID, ok := mapaAlmacenes[k.almacenOdooID]
		if !ok {
			continue
		}
		filasStock = append(filasStock, store.FilaStock{
			VarianteID: varID, AlmacenID: dbWID,
			OnHand: q.onHand, Forecast: q.forecast, Free: q.free,
		})
	}
	if err := s.st.ReemplazarStock(ctx, filasStock); err != nil {
		return nil, err
	}

	// La cola de atención mezcla datos de Odoo con datos de Integra, así que
	// se recalcula entera al final en vez de producto a producto.
	if err := s.st.RecalcularAtencion(ctx); err != nil {
		return nil, err
	}

	if !maxWrite.IsZero() {
		if err := s.st.ActualizarWatermark(ctx, conexionID, maxWrite); err != nil {
			return nil, err
		}
	}
	res.Duracion = time.Since(inicio)
	return res, nil
}

// ------------------------------------------------------------- auxiliares

// esMercancia interpreta el campo active de Odoo.
//
// La ausencia del campo se trata como "sigue siendo mercancía": una instancia
// que no lo exponga no puede provocar la baja de todo el catálogo. Solo un
// false explícito da de baja.
func esMercancia(r odoo.Record) bool {
	v, presente := r["active"]
	if !presente {
		return true
	}
	b, esBool := v.(bool)
	if !esBool {
		return true
	}
	return b
}

// estadoEnOdoo contrasta el catálogo que Integra ya tiene guardado con lo que
// Odoo dice ahora mismo, y reparte los identificadores en los que siguen
// siendo mercancía y los que no.
//
// Se pregunta por los identificadores conocidos, no se recorre el catálogo
// remoto entero, y son dos búsquedas por lote que solo devuelven ids: una con
// active_test=false, que dice cuáles siguen existiendo, y otra con el dominio
// por defecto, que dice cuáles siguen activos. La diferencia entre ambas es
// exactamente el conjunto de archivados.
//
// Va aparte de la lectura por write_date a propósito: así la baja no depende
// de haber pillado el cambio en la ventana incremental. Borrar no cambia
// ninguna fecha, y lo archivado hace meses no vuelve a aparecer jamás.
func (s *Sincronizador) estadoEnOdoo(variantes map[int64]int64) (vivos, caidos []int64, err error) {
	if len(variantes) == 0 {
		return nil, nil, nil
	}
	conocidos := make([]int64, 0, len(variantes))
	for id := range variantes {
		conocidos = append(conocidos, id)
	}
	slices.Sort(conocidos) // orden estable: hace reproducibles el registro y las pruebas

	existen := map[int64]bool{}
	activos := map[int64]bool{}
	for i := 0; i < len(conocidos); i += tamañoLote {
		lote := conocidos[i:min(i+tamañoLote, len(conocidos))]
		if err := s.buscarIDs(lote, false, existen); err != nil {
			return nil, nil, err
		}
		if err := s.buscarIDs(lote, true, activos); err != nil {
			return nil, nil, err
		}
	}

	for _, id := range conocidos {
		if existen[id] && activos[id] {
			vivos = append(vivos, id)
		} else {
			caidos = append(caidos, id)
		}
	}

	// Que Odoo no reconozca ni un solo producto de los que Integra tiene
	// apunta a una lectura rota —permisos, base equivocada— antes que a un
	// catálogo borrado entero. Dar de baja todo por un fallo de lectura sería
	// peor que no dar de baja nada: dejaría a la empresa sin una sola
	// publicación viva en los cuatro canales.
	if len(vivos) == 0 {
		return nil, nil, fmt.Errorf("Odoo no da por vivo ninguno de los %d productos de esta conexión: se aborta antes de dar de baja el catálogo entero", len(conocidos))
	}
	return vivos, caidos, nil
}

// buscarIDs pregunta cuáles del lote cumplen el dominio y los anota en dentro.
// Con soloActivos se usa el dominio por defecto de Odoo, que esconde los
// archivados; sin él se piden todos con active_test=false.
func (s *Sincronizador) buscarIDs(lote []int64, soloActivos bool, dentro map[int64]bool) error {
	kwargs := map[string]interface{}{}
	if !soloActivos {
		kwargs["context"] = map[string]interface{}{"active_test": false}
	}
	raw, err := s.cli.Execute("product.product", "search",
		[]interface{}{[]interface{}{[]interface{}{"id", "in", lote}}}, kwargs)
	if err != nil {
		return fmt.Errorf("comprobando qué productos siguen en Odoo: %w", err)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return fmt.Errorf("product.product.search devolvió %T y se esperaba una lista", raw)
	}
	for _, v := range arr {
		if id, ok := v.(int64); ok {
			dentro[id] = true
		}
	}
	return nil
}

// depurar deja las dos listas sin repetidos y sin solaparse. Ante la
// contradicción gana la baja, que es el lado prudente.
func depurar(activos, inactivos []int64) ([]int64, []int64) {
	slices.Sort(inactivos)
	inactivos = slices.Compact(inactivos)
	slices.Sort(activos)
	activos = slices.Compact(activos)
	activos = slices.DeleteFunc(activos, func(id int64) bool {
		_, dadoDeBaja := slices.BinarySearch(inactivos, id)
		return dadoDeBaja
	})
	return activos, inactivos
}

func (s *Sincronizador) sincronizarAlmacenes(ctx context.Context, conexionID int64) ([]store.Almacen, map[int64]int64, error) {
	filas, err := s.cli.SearchRead("stock.warehouse", nil,
		[]string{"name", "code", "lot_stock_id"}, map[string]interface{}{"order": "id"})
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo almacenes: %w", err)
	}
	var as []store.Almacen
	for _, r := range filas {
		as = append(as, store.Almacen{
			OdooID: r.ID(), Codigo: r.Str("code"), Nombre: r.Str("name"),
			LotStockID: r.RefID("lot_stock_id"),
		})
	}
	mapa, err := s.st.UpsertAlmacenes(ctx, conexionID, as)
	return as, mapa, err
}

type claveStock struct {
	productoOdooID int64
	almacenOdooID  int64
}

type cantidades struct{ onHand, forecast, free float64 }

// leerStock desglosa las existencias por almacén leyendo stock.quant.
//
// El camino aparentemente obvio —pedir qty_available con {"warehouse": id} en
// el contexto— NO funciona: Odoo devuelve el total global para todos los
// almacenes, así que las siete bodegas de MDV salían con las mismas 12.062
// unidades. Publicar eso significaría ofrecer en MercadoLibre el stock que
// está consignado en las tiendas de Falabella.
//
// stock.quant sí guarda la cantidad por ubicación, y stock.location expone a
// qué almacén pertenece cada una. Es más trabajo pero es el dato correcto.
//
// Nota: qty_forecast queda a cero. El pronóstico por almacén exige recorrer
// stock.move pendientes; se publica sobre existencias reales, que es lo
// prudente mientras eso no exista.
func (s *Sincronizador) leerStock(ctx context.Context, almacenes []store.Almacen) (map[claveStock]cantidades, error) {
	// Ubicación interna → almacén de Odoo.
	locs, err := s.cli.SearchRead("stock.location",
		[]interface{}{[]interface{}{"usage", "=", "internal"}},
		[]string{"warehouse_id"},
		map[string]interface{}{"limit": 5000, "context": map[string]interface{}{"active_test": false}})
	if err != nil {
		return nil, fmt.Errorf("leyendo ubicaciones: %w", err)
	}
	almacenDeUbicacion := make(map[int64]int64, len(locs))
	for _, l := range locs {
		if w := l.RefID("warehouse_id"); w != 0 {
			almacenDeUbicacion[l.ID()] = w
		}
	}
	if len(almacenDeUbicacion) == 0 {
		return nil, fmt.Errorf("ninguna ubicación interna tiene almacén asociado: no se puede desglosar el stock")
	}

	out := map[claveStock]cantidades{}
	for offset := 0; ; offset += 1000 {
		quants, err := s.cli.SearchRead("stock.quant",
			[]interface{}{[]interface{}{"location_id.usage", "=", "internal"}},
			[]string{"product_id", "location_id", "quantity", "reserved_quantity"},
			map[string]interface{}{"limit": 1000, "offset": offset, "order": "id"})
		if err != nil {
			return nil, fmt.Errorf("leyendo existencias (offset %d): %w", offset, err)
		}
		if len(quants) == 0 {
			break
		}
		for _, q := range quants {
			almID, ok := almacenDeUbicacion[q.RefID("location_id")]
			if !ok {
				continue
			}
			k := claveStock{q.RefID("product_id"), almID}
			c := out[k]
			cantidad := q.Float("quantity")
			c.onHand += cantidad
			// Lo reservado ya está comprometido con otro pedido: no se publica.
			c.free += cantidad - q.Float("reserved_quantity")
			out[k] = c
		}
		if len(quants) < 1000 {
			break
		}
	}
	return out, nil
}

// ------------------------------------------------------------- persistencia

// tienda añade a *store.Store la única operación que la capa de persistencia
// todavía no ofrece. Vive aquí, y no en internal/store junto a
// UpsertIdentidad, porque la baja es un asunto exclusivo del sync: nadie más
// en Integra escribe products.active ni product_variants.active.
type tienda struct{ *store.Store }

// AjustarActividad fija product_variants.active según lo que Odoo dice de cada
// product.product y recalcula products.active a partir de sus variantes.
//
// Se hace en una sola transacción y comparando contra el valor actual para que
// el conteo devuelto sea el de filas que realmente cambiaron: un sync completo
// pasa por aquí con el catálogo entero en la lista de activas y no debe
// reportar seiscientas altas cada noche.
func (t tienda) AjustarActividad(ctx context.Context, conexionID int64, activas, inactivas []int64) (altas, bajas int, err error) {
	if len(activas) == 0 && len(inactivas) == 0 {
		return 0, 0, nil
	}
	tx, err := t.Pool().Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	marcar := func(ids []int64, activa bool) (int, error) {
		if len(ids) == 0 {
			return 0, nil
		}
		tag, err := tx.Exec(ctx, `
			UPDATE product_variants v SET active = $3, updated_at = now()
			FROM products p
			WHERE p.id = v.product_id AND p.odoo_connection_id = $1
			  AND v.odoo_product_id = ANY($2) AND v.active <> $3`,
			conexionID, ids, activa)
		if err != nil {
			return 0, err
		}
		return int(tag.RowsAffected()), nil
	}

	if bajas, err = marcar(inactivas, false); err != nil {
		return 0, 0, fmt.Errorf("dando de baja variantes: %w", err)
	}
	if altas, err = marcar(activas, true); err != nil {
		return 0, 0, fmt.Errorf("reactivando variantes: %w", err)
	}

	// products es la plantilla: sigue siendo mercancía mientras le quede una
	// variante viva. Se recalcula en bloque, y no solo para lo tocado, para
	// que una baja interrumpida a medias se corrija sola en el sync siguiente.
	if _, err := tx.Exec(ctx, `
		UPDATE products p SET active = c.viva, updated_at = now()
		FROM (
		    SELECT p2.id,
		           EXISTS (SELECT 1 FROM product_variants v
		                   WHERE v.product_id = p2.id AND v.active) AS viva
		    FROM products p2 WHERE p2.odoo_connection_id = $1
		) c
		WHERE c.id = p.id AND p.active <> c.viva`, conexionID); err != nil {
		return 0, 0, fmt.Errorf("recalculando la actividad de los productos: %w", err)
	}

	return altas, bajas, tx.Commit(ctx)
}
