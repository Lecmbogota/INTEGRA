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
	TrabajoPublicar  = "publicar_producto"
	TrabajoPrecio    = "actualizar_precio"
	TrabajoStock     = "actualizar_stock"
	TrabajoPausar    = "pausar_publicacion"
	TrabajoReanudar  = "reanudar_publicacion"
	TrabajoConciliar = "conciliar_publicaciones"
)

// PayloadPublicar identifica qué publicar y dónde.
type PayloadPublicar struct {
	CuentaID   int64 `json:"cuenta_id"`
	VarianteID int64 `json:"variante_id"`
}

// PayloadCuenta es el de los trabajos que barren una cuenta entera. La
// conciliación no tiene una variante que nombrar: elige ella misma cuáles
// contrasta en cada pasada.
type PayloadCuenta struct {
	CuentaID int64 `json:"cuenta_id"`
}

// Plan es el resultado de comparar catálogo contra lo publicado.
type Plan struct {
	Publicar   int `json:"publicar"`
	Precio     int `json:"precio"`
	Stock      int `json:"stock"`
	SinCambios int `json:"sin_cambios"`
	NoListos   int `json:"no_listos"`
	// Pausar cuenta lo que sigue abierto en el canal sin producto detrás;
	// Reanudar, lo que vuelve a abrirse porque el producto regresó al
	// catálogo.
	Pausar   int `json:"pausar"`
	Reanudar int `json:"reanudar"`
}

// catalogo y encolador son lo que Planificar necesita del store y de la cola.
// Son interfaces para poder planificar contra dobles en las pruebas;
// *store.Store y *jobs.Cola las cumplen sin tocar a quien las llama.
type catalogo interface {
	CandidatosPublicacion(ctx context.Context, cuentaID int64) ([]store.CandidatoPublicacion, error)
	PublicacionesHuerfanas(ctx context.Context, cuentaID int64) ([]store.PublicacionViva, error)
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

		hContenido := HashContenido(c)
		hPrecio := HashPrecio(c)
		hStock := HashStock(c)

		// Una publicación que Integra pausó al salir el producto del catálogo
		// tiene que volver a abrirse cuando el producto vuelve, y no lo haría
		// sola: los hashes siguen coincidiendo —la ficha del canal no cambió
		// mientras estaba pausada— así que ninguna de las tres ramas de abajo
		// se dispararía y la ficha se quedaría cerrada para siempre. La que
		// pausó el canal no se toca: reabrir una baja por infracción es lo que
		// convierte un aviso en una sanción.
		if c.EstadoPublicacion == "paused" && c.PausaMotivo == store.PausaCatalogo {
			if err := encolar(ctx, cola, TrabajoReanudar, cuentaID, c.VarianteID, 50); err != nil {
				return nil, err
			}
			p.Reanudar++
		}

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
				if err := encolar(ctx, cola, TrabajoPrecio, cuentaID, c.VarianteID, 50); err != nil {
					return nil, err
				}
				p.Precio++
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

	// Lo que se quedó fuera del catálogo no aparece en el bucle de arriba: al
	// archivarlo en Odoo o al borrarle el SKU deja de ser candidato y
	// desaparece de la planificación, pero su ficha sigue viva en el canal
	// vendiendo con la última cantidad conocida. Retirarla es el trabajo de
	// pausa; lo caro es que alguien compre un producto que la empresa ya no
	// tiene y haya que cancelarle la venta.
	huerfanas, err := st.PublicacionesHuerfanas(ctx, cuentaID)
	if err != nil {
		return nil, err
	}
	for _, h := range huerfanas {
		if err := encolar(ctx, cola, TrabajoPausar, cuentaID, h.VarianteID, 20); err != nil {
			return nil, err
		}
		p.Pausar++
	}
	return p, nil
}

// EncolarConciliacion pide contrastar contra el canal lo que Integra da por
// publicado en una cuenta.
//
// Va aparte de Planificar porque no compara catálogo contra hashes sino
// Integra contra el canal, y es lo único que descubre una publicación que el
// marketplace dio de baja por su cuenta. Se pide en cada pasada del
// planificador y el trabajo se reparte él mismo por tandas, así que pedirlo de
// más no cuesta peticiones.
func EncolarConciliacion(ctx context.Context, cola encolador, cuentaID int64) error {
	_, err := cola.Encolar(ctx, TrabajoConciliar, PayloadCuenta{CuentaID: cuentaID},
		jobs.Opciones{
			UniqueKey: fmt.Sprintf("%s:%d", TrabajoConciliar, cuentaID),
			Priority:  200, // detrás de todo lo que sí escribe en el canal
			CuentaID:  cuentaID,
		})
	return err
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
