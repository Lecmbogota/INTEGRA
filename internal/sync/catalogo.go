// Package sync lee de Odoo lo único que le pertenece —SKU, nombre y stock—
// y lo persiste. El resto del producto (precio, marca, descripción, categoría,
// exclusión) es propiedad de Integra: se edita en la interfaz y este paquete
// no lo toca jamás.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// tamañoPagina acota cada lectura para no pedir el catálogo entero de golpe.
const tamañoPagina = 200

type Resultado struct {
	Leidos    int
	Guardados int
	Almacenes int
	Duracion  time.Duration
}

type Sincronizador struct {
	cli *odoo.Client
	st  *store.Store
	log *slog.Logger
}

func Nuevo(cli *odoo.Client, st *store.Store, log *slog.Logger) *Sincronizador {
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

	for offset := 0; ; offset += tamañoPagina {
		filas, err := s.cli.SearchRead("product.product", dominio,
			[]string{"default_code", "name", "product_tmpl_id", "write_date"},
			map[string]interface{}{
				"limit": tamañoPagina, "offset": offset, "order": "id",
			})
		if err != nil {
			return nil, fmt.Errorf("leyendo productos (offset %d): %w", offset, err)
		}
		if len(filas) == 0 {
			break
		}
		res.Leidos += len(filas)

		for _, r := range filas {
			ident := store.Identidad{
				OdooTemplateID: r.RefID("product_tmpl_id"),
				OdooProductID:  r.ID(),
				Nombre:         r.Str("name"),
				SKU:            r.Str("default_code"),
			}
			if ts, ok := r.Time("write_date"); ok {
				ident.OdooWriteDate = &ts
				if ts.After(maxWrite) {
					maxWrite = ts
				}
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
