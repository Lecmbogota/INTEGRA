package publicar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"

	// Los adaptadores se registran desde su init().
	_ "github.com/mdv/integra/internal/conectores/falabella"
	_ "github.com/mdv/integra/internal/conectores/mercadolibre"
	_ "github.com/mdv/integra/internal/conectores/shopify"
	_ "github.com/mdv/integra/internal/conectores/woocommerce"
)

// almacen es lo que el servicio necesita de la persistencia. Se declara como
// interfaz —y no como *store.Store— para poder ejercitar los manejadores con
// un doble: sin ella, comprobar qué hash se guarda tras cada envío exigiría
// una base de datos viva, y es justo ahí donde estaba el defecto más caro.
type almacen interface {
	UnCandidato(ctx context.Context, cuentaID, varianteID int64) (*store.CandidatoPublicacion, error)
	AtributosParaPublicar(ctx context.Context, productoID int64, canalCodigo string) (map[string]string, []string, error)
	RefDePublicacion(ctx context.Context, cuentaID, varianteID int64) (channel.ExternalRef, error)
	GuardarPublicacion(ctx context.Context, cuentaID, productoID, varianteID int64,
		externalID, externalURL, varianteExterna, contentHash, priceHash, stockHash string,
		precio float64, cantidad int) error
	// GuardarContenidoPublicado se usa al actualizar una publicación viva:
	// solo se envió la ficha, así que el precio y el stock publicados no se
	// tocan.
	GuardarContenidoPublicado(ctx context.Context, cuentaID, productoID, varianteID int64,
		externalID, externalURL, varianteExterna, contentHash string) error
	GuardarPrecioPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, precio float64) error
	GuardarStockPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, cantidad int) error
	AnotarErrorPublicacion(ctx context.Context, cuentaID, productoID int64, causa string) error
}

// Servicio ejecuta los trabajos de publicación contra los canales.
type Servicio struct {
	st  almacen
	log *slog.Logger
	// cola la aporta Registrar: actualizar una ficha ya publicada obliga a
	// pedir aparte el precio y el stock, porque Update no los lleva.
	cola encolador
	// adaptador resuelve el canal de una cuenta. Es un campo, y no una
	// llamada directa, para que las pruebas puedan inyectar un canal simulado.
	adaptador func(ctx context.Context, cuentaID int64) (channel.Adapter, error)
	// baseURL es el origen público desde el que los canales descargan las
	// imágenes. Sin él, se publican productos sin fotos.
	baseURL string
}

func NuevoServicio(st *store.Store, cif *crypto.Cifrador, log *slog.Logger, baseURL string) *Servicio {
	return &Servicio{
		st: st, log: log, baseURL: baseURL,
		// El adaptador se construye por trabajo y no se cachea: así una
		// credencial reemplazada surte efecto en el siguiente envío sin
		// reiniciar el worker.
		adaptador: func(ctx context.Context, cuentaID int64) (channel.Adapter, error) {
			return conectores.AdaptadorDeCuenta(ctx, st, cif, cuentaID)
		},
	}
}

// Registrar engancha los tres tipos de trabajo al worker.
func (s *Servicio) Registrar(w *jobs.Worker) {
	s.cola = w.Cola()
	w.Registrar(TrabajoPublicar, s.publicar)
	w.Registrar(TrabajoPrecio, s.actualizarPrecio)
	w.Registrar(TrabajoStock, s.actualizarStock)
}

func (s *Servicio) datos(ctx context.Context, t jobs.Trabajo) (*store.CandidatoPublicacion, channel.Adapter, PayloadPublicar, error) {
	var p PayloadPublicar
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return nil, nil, p, fmt.Errorf("payload ilegible: %w", err)
	}
	c, err := s.st.UnCandidato(ctx, p.CuentaID, p.VarianteID)
	if err != nil {
		return nil, nil, p, err
	}
	ad, err := s.adaptador(ctx, p.CuentaID)
	if err != nil {
		return nil, nil, p, err
	}
	return c, ad, p, nil
}

func (s *Servicio) publicar(ctx context.Context, t jobs.Trabajo) error {
	c, ad, p, err := s.datos(ctx, t)
	if err != nil {
		return err
	}
	if !c.Listo {
		return fmt.Errorf("la variante %d no está lista para publicar", p.VarianteID)
	}
	cap := ad.Capabilities()
	if cap.RequiresCategoryMapping && c.CategoriaCanal == "" {
		return fmt.Errorf("falta mapear la categoría para %s", c.SKU)
	}

	prod := s.producto(c, cap)
	// Los atributos obligatorios de la categoría solo los piden los
	// marketplaces; se cargan justo antes de enviar para que un valor
	// corregido hace un minuto ya viaje en esta publicación.
	if cap.RequiresCategoryMapping {
		attrs, faltan, err := s.st.AtributosParaPublicar(ctx, c.ProductoID, string(ad.Kind()))
		if err != nil {
			return err
		}
		if len(faltan) > 0 {
			causa := fmt.Sprintf("faltan atributos obligatorios de %s: %s",
				ad.Kind(), strings.Join(faltan, ", "))
			_ = s.st.AnotarErrorPublicacion(ctx, p.CuentaID, c.ProductoID, causa)
			return fmt.Errorf("%s", causa)
		}
		prod.Attributes = attrs
	}

	// Una publicación que ya existe no se vuelve a crear: se actualiza. Pasar
	// otra vez por Publish sería, en el mejor caso, adoptarla sin enviar nada
	// —el cambio de contenido no llegaría nunca— y en el peor duplicar el SKU
	// en el canal.
	if c.ExternalID != "" {
		return s.actualizarFicha(ctx, ad, c, p, prod, channel.ExternalRef{})
	}

	res, err := ad.Publish(ctx, channel.PublishRequest{Product: prod})
	if err != nil {
		_ = s.st.AnotarErrorPublicacion(ctx, p.CuentaID, c.ProductoID, err.Error())
		return err
	}
	for _, aviso := range res.Warnings {
		s.log.Warn("publicación con aviso", "sku", c.SKU, "aviso", aviso)
	}

	// Adoptar es reconocer el SKU que ya estaba en el canal sin escribir una
	// sola vez: ni la ficha, ni el precio, ni el stock de Integra han salido.
	// Darlos por publicados es lo que congelaba el producto para siempre.
	if res.Adopted {
		return s.actualizarFicha(ctx, ad, c, p, prod, res.Ref)
	}

	// Solo aquí hubo creación de verdad, y la creación lleva el precio y el
	// stock en el mismo cuerpo: los tres hashes describen lo que tiene el
	// canal.
	return s.st.GuardarPublicacion(ctx, p.CuentaID, c.ProductoID, c.VarianteID,
		res.Ref.ListingID, "", res.Ref.VariantID,
		HashContenido(*c), HashPrecio(*c), HashStock(*c), c.PrecioCanal, c.Stock)
}

// actualizarFicha manda el contenido a una publicación que ya existe, sea
// porque la teníamos registrada o porque el adaptador la adoptó por SKU.
//
// Update solo lleva la ficha —en los cuatro canales el precio y el stock
// tienen su propio endpoint, que es además el barato—, así que aquí quedan
// explícitamente sin publicar y se piden en sus propios trabajos.
func (s *Servicio) actualizarFicha(ctx context.Context, ad channel.Adapter,
	c *store.CandidatoPublicacion, p PayloadPublicar, prod channel.Product,
	ref channel.ExternalRef) error {

	if ref.ListingID == "" {
		// La adopción trae su propia referencia; si no, se usa la que se
		// guardó al publicar, que es la única que conoce el identificador de
		// la variante dentro del canal.
		r, err := s.st.RefDePublicacion(ctx, p.CuentaID, p.VarianteID)
		if err != nil {
			return err
		}
		ref = r
	}

	res, err := ad.Update(ctx, channel.UpdateRequest{Ref: ref, Product: prod})
	if err != nil {
		_ = s.st.AnotarErrorPublicacion(ctx, p.CuentaID, c.ProductoID, err.Error())
		return err
	}
	for _, aviso := range res.Warnings {
		s.log.Warn("actualización con aviso", "sku", c.SKU, "aviso", aviso)
	}
	if res.Ref.ListingID != "" {
		ref = res.Ref
	}

	// Solo se anota el contenido: Update no lleva precio ni stock en ninguno
	// de los cuatro canales. Los hashes de esos dos quedan intactos y sus
	// trabajos, encolados abajo, son los que los anotarán al enviarlos. Pasar
	// por GuardarPublicacion aquí ponía a cero el precio y el stock
	// publicados, que es mentira sobre lo que tiene el canal.
	if err := s.st.GuardarContenidoPublicado(ctx, p.CuentaID, c.ProductoID, c.VarianteID,
		ref.ListingID, "", ref.VariantID, HashContenido(*c)); err != nil {
		return err
	}
	return s.encolarPrecioYStock(ctx, p.CuentaID, p.VarianteID)
}

func (s *Servicio) encolarPrecioYStock(ctx context.Context, cuentaID, varianteID int64) error {
	if s.cola == nil {
		// Solo puede ocurrir si alguien ejecuta un manejador sin haber pasado
		// por Registrar: mejor fallar que dejar el precio sin enviar en
		// silencio, que es exactamente el defecto que se está corrigiendo.
		return fmt.Errorf("el servicio no tiene cola: la variante %d se quedaría sin precio ni stock", varianteID)
	}
	if err := encolar(ctx, s.cola, TrabajoPrecio, cuentaID, varianteID, 50); err != nil {
		return err
	}
	return encolar(ctx, s.cola, TrabajoStock, cuentaID, varianteID, 10)
}

func (s *Servicio) actualizarPrecio(ctx context.Context, t jobs.Trabajo) error {
	c, ad, p, err := s.datos(ctx, t)
	if err != nil {
		return err
	}
	ref, err := s.st.RefDePublicacion(ctx, p.CuentaID, p.VarianteID)
	if err != nil {
		return err
	}
	res, err := ad.UpdatePrice(ctx, []channel.PriceUpdate{{
		Ref: ref, RegularPrice: c.PrecioCanal, Currency: c.Moneda,
	}})
	if err != nil {
		return err
	}
	if len(res) > 0 && !res[0].OK {
		return res[0].Error
	}
	return s.st.GuardarPrecioPublicado(ctx, p.CuentaID, p.VarianteID, HashPrecio(*c), c.PrecioCanal)
}

func (s *Servicio) actualizarStock(ctx context.Context, t jobs.Trabajo) error {
	c, ad, p, err := s.datos(ctx, t)
	if err != nil {
		return err
	}
	ref, err := s.st.RefDePublicacion(ctx, p.CuentaID, p.VarianteID)
	if err != nil {
		return err
	}
	res, err := ad.UpdateStock(ctx, []channel.StockUpdate{{Ref: ref, Quantity: c.Stock}})
	if err != nil {
		return err
	}
	if len(res) > 0 && !res[0].OK {
		return res[0].Error
	}
	return s.st.GuardarStockPublicado(ctx, p.CuentaID, p.VarianteID, HashStock(*c), c.Stock)
}

// producto traduce el candidato a la vista normalizada del contrato de canal.
func (s *Servicio) producto(c *store.CandidatoPublicacion, cap channel.Capabilities) channel.Product {
	titulo := c.Titulo
	// El límite lo impone el canal: recortar aquí evita un rechazo seguro.
	if cap.MaxTitleLength > 0 {
		r := []rune(titulo)
		if len(r) > cap.MaxTitleLength {
			titulo = string(r[:cap.MaxTitleLength])
		}
	}
	var imgs []channel.Image
	for i, sha := range c.Imagenes {
		imgs = append(imgs, channel.Image{
			URL:      s.baseURL + "/imagenes/" + sha + "/cuadrada_1200",
			Position: i, Hash: sha,
		})
	}
	return channel.Product{
		SKU: c.SKU, Title: titulo, Description: c.Descripcion,
		Brand: c.Marca, CategoryID: c.CategoriaCanal, Images: imgs,
		Weight: c.Peso,
		// Sin las tres aristas, Falabella rechaza el alta: las pide por
		// separado y el peso no las sustituye.
		LengthCm: c.LargoCm, WidthCm: c.AnchoCm, HeightCm: c.AltoCm,
		Variants: []channel.Variant{{
			SKU: c.SKU, Barcode: c.Barcode,
			RegularPrice: c.PrecioCanal, Currency: c.Moneda, Quantity: c.Stock,
		}},
	}
}
