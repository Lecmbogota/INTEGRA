// Package publicar decide qué mandar a cada canal y encola el trabajo.
//
// El principio es no reenviar nada que no haya cambiado: se calculan tres
// hashes por variante (contenido, precio, stock) y se comparan con lo último
// publicado. Cambiar una unidad de stock no puede obligar a reenviar título,
// descripción e imágenes — es el endpoint más caro de todos los canales.
package publicar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"
)

// Tipos de trabajo que atiende el worker.
const (
	TrabajoPublicar = "publicar_producto"
	TrabajoPrecio   = "actualizar_precio"
	TrabajoStock    = "actualizar_stock"
)

// PayloadPublicar identifica qué publicar y dónde.
type PayloadPublicar struct {
	CuentaID   int64 `json:"cuenta_id"`
	VarianteID int64 `json:"variante_id"`
}

// Plan es el resultado de comparar catálogo contra lo publicado.
type Plan struct {
	Publicar   int `json:"publicar"`
	Precio     int `json:"precio"`
	Stock      int `json:"stock"`
	SinCambios int `json:"sin_cambios"`
	NoListos   int `json:"no_listos"`
	// BajoCosto: variantes cuyo precio no cubre el coste y a las que se les
	// retuvo el envío de precio.
	BajoCosto int `json:"bajo_costo"`
}

// catalogo y encolador son lo que Planificar necesita del store y de la cola.
// Son interfaces para poder planificar contra dobles en las pruebas;
// *store.Store y *jobs.Cola las cumplen sin tocar a quien las llama.
type catalogo interface {
	CandidatosPublicacion(ctx context.Context, cuentaID int64) ([]store.CandidatoPublicacion, error)
}

type encolador interface {
	Encolar(ctx context.Context, kind string, payload any, op jobs.Opciones) (int64, error)
}

// Planificar recorre el catálogo publicable de una cuenta, calcula los hashes
// y encola solo lo que cambió. Devuelve el resumen de lo encolado.
//
// Es idempotente: la clave única del trabajo impide que dos planificaciones
// seguidas encolen dos veces el mismo envío.
func Planificar(ctx context.Context, st catalogo, cola encolador, cuentaID int64) (*Plan, error) {
	candidatos, err := st.CandidatosPublicacion(ctx, cuentaID)
	if err != nil {
		return nil, err
	}

	p := &Plan{}
	for _, c := range candidatos {
		if !c.Listo {
			p.NoListos++
			continue
		}

		// Un precio que no cubre el coste no sale. En un alta se retiene el
		// producto entero, porque el alta lleva el precio dentro; sobre una
		// publicación que ya existe se retiene solo el envío de precio, y así
		// el canal sigue mostrando el precio bueno que ya tenía en vez de que
		// lo pise el malo. El stock se manda igual: dejar de sincronizar
		// existencias por un problema de precio haría vender lo que no hay.
		if c.BloqueadoPorCosto {
			p.BajoCosto++
			if c.ExternalID == "" {
				continue
			}
		}

		hContenido := HashContenido(c)
		hPrecio := HashPrecio(c)
		hStock := HashStock(c)

		switch {
		// Sin publicación previa: la creación lleva precio y stock dentro del
		// mismo envío, así que basta con encolarla.
		case c.ExternalID == "":
			if err := encolar(ctx, cola, TrabajoPublicar, cuentaID, c.VarianteID, 100); err != nil {
				return nil, err
			}
			p.Publicar++
		default:
			cambio := false
			// Sobre una publicación que ya existe, el contenido se manda con
			// Update, que no lleva precio ni stock. Por eso las tres ramas se
			// evalúan a la vez: si fueran excluyentes, editar la descripción
			// y subir el precio en la misma tanda dejaría el precio sin
			// enviar y con su hash dado por bueno.
			if c.ContentHash != hContenido {
				if err := encolar(ctx, cola, TrabajoPublicar, cuentaID, c.VarianteID, 100); err != nil {
					return nil, err
				}
				p.Publicar++
				cambio = true
			}
			if c.PriceHash != hPrecio {
				// Retenido no es lo mismo que sin cambios: el precio cambió,
				// simplemente no se manda.
				if !c.BloqueadoPorCosto {
					if err := encolar(ctx, cola, TrabajoPrecio, cuentaID, c.VarianteID, 50); err != nil {
						return nil, err
					}
					p.Precio++
				}
				cambio = true
			}
			// El stock es lo más urgente: vender lo que no hay cuesta más caro
			// que cualquier otro desajuste.
			if c.StockHash != hStock {
				if err := encolar(ctx, cola, TrabajoStock, cuentaID, c.VarianteID, 10); err != nil {
					return nil, err
				}
				p.Stock++
				cambio = true
			}
			if !cambio {
				p.SinCambios++
			}
		}
	}
	return p, nil
}

func encolar(ctx context.Context, cola encolador, kind string, cuentaID, varianteID int64, prioridad int) error {
	_, err := cola.Encolar(ctx, kind, PayloadPublicar{CuentaID: cuentaID, VarianteID: varianteID},
		jobs.Opciones{
			UniqueKey: fmt.Sprintf("%s:%d:%d", kind, cuentaID, varianteID),
			Priority:  prioridad,
			CuentaID:  cuentaID,
		})
	return err
}

// EncolarStock pide el envío de stock de una variante a una cuenta sin pasar
// por Planificar. Lo usa la ingesta de pedidos: una venta baja el stock en la
// base al instante, pero el diff por hash solo corre desde el horario, así que
// los demás canales seguían ofreciendo la unidad vendida hasta el día
// siguiente. Lleva la misma clave única y la misma prioridad que el trabajo
// que encola Planificar: una planificación que llegue después no lo duplica,
// y el stock sigue por delante de todo lo demás.
func EncolarStock(ctx context.Context, cola encolador, cuentaID, varianteID int64) error {
	return encolar(ctx, cola, TrabajoStock, cuentaID, varianteID, 10)
}

// HashContenido cubre todo lo que obliga a reenviar la ficha completa.
func HashContenido(c store.CandidatoPublicacion) string {
	var b strings.Builder
	b.WriteString(c.SKU)
	b.WriteByte('|')
	b.WriteString(c.Titulo)
	b.WriteByte('|')
	b.WriteString(c.Descripcion)
	b.WriteByte('|')
	b.WriteString(c.Marca)
	b.WriteByte('|')
	b.WriteString(c.CategoriaCanal)
	b.WriteByte('|')
	b.WriteString(c.Barcode)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(c.Peso, 'f', 4, 64))
	for _, img := range c.Imagenes {
		b.WriteByte('|')
		b.WriteString(img)
	}
	return hashDe(b.String())
}

// HashPrecio incluye el precio ya ajustado por la comisión del canal: un
// cambio de comisión debe reenviar precios aunque el catálogo no se mueva.
func HashPrecio(c store.CandidatoPublicacion) string {
	return hashDe(strconv.FormatFloat(c.PrecioCanal, 'f', 4, 64) + "|" + c.Moneda)
}

func HashStock(c store.CandidatoPublicacion) string {
	return hashDe(strconv.Itoa(c.Stock))
}

func hashDe(s string) string {
	suma := sha256.Sum256([]byte(s))
	return hex.EncodeToString(suma[:8]) // 16 hex bastan para detectar cambios
}
