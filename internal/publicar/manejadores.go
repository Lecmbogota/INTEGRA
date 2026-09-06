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

// Servicio ejecuta los trabajos de publicación contra los canales.
type Servicio struct {
	st  *store.Store
	cif *crypto.Cifrador
	log *slog.Logger
	// baseURL es el origen público desde el que los canales descargan las
	// imágenes. Sin él, se publican productos sin fotos.
	baseURL string
}

func NuevoServicio(st *store.Store, cif *crypto.Cifrador, log *slog.Logger, baseURL string) *Servicio {
	return &Servicio{st: st, cif: cif, log: log, baseURL: baseURL}
}

// Registrar engancha los tres tipos de trabajo al worker.
func (s *Servicio) Registrar(w *jobs.Worker) {
	w.Registrar(TrabajoPublicar, s.publicar)
	w.Registrar(TrabajoPrecio, s.actualizarPrecio)
	w.Registrar(TrabajoStock, s.actualizarStock)
}

// adaptadorDe construye el adaptador de una cuenta con sus credenciales
// descifradas. Se hace por trabajo y no se cachea: así una credencial
// reemplazada surte efecto en el siguiente envío sin reiniciar el worker.
func (s *Servicio) adaptadorDe(ctx context.Context, cuentaID int64) (channel.Adapter, error) {
	return conectores.AdaptadorDeCuenta(ctx, s.st, s.cif, cuentaID)
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
	ad, err := s.adaptadorDe(ctx, p.CuentaID)
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

	res, err := ad.Publish(ctx, channel.PublishRequest{Product: prod})
	if err != nil {
		_ = s.st.AnotarErrorPublicacion(ctx, p.CuentaID, c.ProductoID, err.Error())
		return err
	}
	for _, aviso := range res.Warnings {
		s.log.Warn("publicación con aviso", "sku", c.SKU, "aviso", aviso)
	}

	return s.st.GuardarPublicacion(ctx, p.CuentaID, c.ProductoID, c.VarianteID,
		res.Ref.ListingID, "", res.Ref.VariantID,
		HashContenido(*c), HashPrecio(*c), HashStock(*c), c.PrecioCanal, c.Stock)
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
		Variants: []channel.Variant{{
			SKU: c.SKU, Barcode: c.Barcode,
			RegularPrice: c.PrecioCanal, Currency: c.Moneda, Quantity: c.Stock,
		}},
	}
}
