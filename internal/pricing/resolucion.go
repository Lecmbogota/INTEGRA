package pricing

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Tipos de ajuste de reglas de canal.
const (
	AdjustmentPercent = "percent"
	AdjustmentFixed   = "fixed"
)

// Fuentes de precio efectivo.
const (
	SourceOverride  = "override"
	SourceOffer     = "offer"
	SourcePricelist = "pricelist"
	SourceListPrice = "list_price"
)

// ChannelPriceRule representa una regla de ajuste sobre el precio base para un canal.
type ChannelPriceRule struct {
	ID               int64
	ChannelAccountID int64
	BrandID          *int64
	CategPathPrefix  *string
	AdjustmentType   string
	AdjustmentValue  float64
	RoundTo          *float64
	MinMarginPercent *float64
	Priority         int
	Active           bool
}

// PriceOverride es un precio manual fijado para una variante en un canal.
type PriceOverride struct {
	VariantID        int64
	ChannelAccountID int64
	Price            float64
	Reason           string
}

// Offer representa una oferta temporal con vigencia.
type Offer struct {
	ID               int64
	VariantID        int64
	ChannelAccountID int64
	OfferPrice       float64
	StartsAt         time.Time
	EndsAt           *time.Time
	AppliedAt        *time.Time
	RevertedAt       *time.Time
	Active           bool
}

// EffectivePrice es el precio resuelto final para publicar o auditar.
type EffectivePrice struct {
	VariantID        int64
	ChannelAccountID int64
	RegularPrice     float64
	SalePrice        *float64
	Currency         string
	Source           string
	Explanation      string
}

// ChannelInfo contiene comisiones y costos fijos de un canal.
type ChannelInfo struct {
	CommissionPct float64
	FixedCost     float64
	Currency      string
}

// VariantPricingInput agrupa los datos del producto necesarios para resolver el precio.
type VariantPricingInput struct {
	VariantID  int64
	ProductID  int64
	BrandID    *int64
	CategPath  string
	BasePrice  float64
	Cost       float64
}

// ResolverPrecio computa el precio efectivo regular y de oferta aplicando:
// 1. Override manual (si existe, gana sobre todo).
// 2. Base + Comisión del canal.
// 3. Regla de canal más específica (marca + categoría) por prioridad.
// 4. Margen mínimo sobre costo.
// 5. Oferta activa en la fecha dada (si existe y está en ventana).
func ResolverPrecio(
	input VariantPricingInput,
	channel ChannelInfo,
	rules []ChannelPriceRule,
	override *PriceOverride,
	offer *Offer,
	now time.Time,
) EffectivePrice {
	moneda := channel.Currency
	if moneda == "" {
		moneda = "COP"
	}

	res := EffectivePrice{
		VariantID: input.VariantID,
		Currency:  moneda,
	}

	// 1. Override manual
	if override != nil && override.Price > 0 {
		res.RegularPrice = override.Price
		res.Source = SourceOverride
		res.Explanation = fmt.Sprintf("Override manual: %.2f (%s)", override.Price, override.Reason)
		// Si además hay oferta activa menor al override, se puede aplicar como sale_price
		if esOfertaVigente(offer, now) && offer.OfferPrice < override.Price {
			op := offer.OfferPrice
			res.SalePrice = &op
			res.Explanation += fmt.Sprintf(" | Oferta activa: %.2f", op)
		}
		return res
	}

	// 2. Base inicial compensando comisión de canal
	precio := compensarComision(input.BasePrice, channel.CommissionPct, channel.FixedCost)
	expl := fmt.Sprintf("Base %.2f + canal (%.1f%% + %.2f) = %.2f",
		input.BasePrice, channel.CommissionPct, channel.FixedCost, precio)
	source := SourcePricelist
	if input.BasePrice <= 0 {
		source = SourceListPrice
	}

	// 3. Buscar regla más aplicable
	mejorRegla := seleccionarMejorRegla(rules, input.BrandID, input.CategPath)
	if mejorRegla != nil {
		precio, expl = aplicarRegla(*mejorRegla, precio, expl)
	} else {
		// Redondeo por defecto a centenas si no hay regla específica
		precio = math.Ceil(precio/100) * 100
	}

	// 4. Margen mínimo sobre costo
	if input.Cost > 0 {
		var minMargen float64
		if mejorRegla != nil && mejorRegla.MinMarginPercent != nil {
			minMargen = *mejorRegla.MinMarginPercent
		}
		if minMargen > 0 {
			precioMinimo := input.Cost * (1 + minMargen/100)
			if precio < precioMinimo {
				precio = math.Ceil(precioMinimo/100) * 100
				expl += fmt.Sprintf(", ajustado al margen mín %.1f%% (costo %.2f) = %.2f", minMargen, input.Cost, precio)
			}
		}
	}

	res.RegularPrice = precio
	res.Source = source
	res.Explanation = expl

	// 5. Oferta vigente
	if esOfertaVigente(offer, now) {
		op := offer.OfferPrice
		if op < res.RegularPrice {
			res.SalePrice = &op
			res.Source = SourceOffer
			res.Explanation += fmt.Sprintf(" | Oferta activa: %.2f", op)
		}
	}

	return res
}

func esOfertaVigente(o *Offer, now time.Time) bool {
	if o == nil || !o.Active || o.OfferPrice <= 0 {
		return false
	}
	if now.Before(o.StartsAt) {
		return false
	}
	if o.EndsAt != nil && now.After(*o.EndsAt) {
		return false
	}
	return true
}

func compensarComision(base, comisionPct, costoFijo float64) float64 {
	if base <= 0 {
		return base
	}
	if comisionPct >= 100 {
		comisionPct = 99.9
	}
	return (base + costoFijo) / (1 - comisionPct/100)
}

func seleccionarMejorRegla(rules []ChannelPriceRule, brandID *int64, categPath string) *ChannelPriceRule {
	var mejor *ChannelPriceRule
	mejorScore := -1

	for i := range rules {
		r := &rules[i]
		if !r.Active {
			continue
		}

		score := 0
		// Coincidencia de marca
		if r.BrandID != nil {
			if brandID == nil || *r.BrandID != *brandID {
				continue
			}
			score += 100
		}

		// Coincidencia de prefijo de categoría
		if r.CategPathPrefix != nil && *r.CategPathPrefix != "" {
			prefijo := *r.CategPathPrefix
			if !strings.HasPrefix(categPath, prefijo) {
				continue
			}
			score += 10 + len(prefijo)
		}

		// Si no tiene marca ni categoría, es regla global de la cuenta (score = 0)
		// A igualdad de score de especificidad, desempata la prioridad (menor número = mayor prioridad)
		if score > mejorScore || (score == mejorScore && mejor != nil && r.Priority < mejor.Priority) {
			mejor = r
			mejorScore = score
		}
	}

	return mejor
}

func aplicarRegla(r ChannelPriceRule, precio float64, expl string) (float64, string) {
	switch r.AdjustmentType {
	case AdjustmentPercent:
		// adjustment_value: ej. 5.0 (sumar 5%) o -10.0 (descuento 10%)
		precio = precio * (1 + r.AdjustmentValue/100)
		expl += fmt.Sprintf(" + regla %d (%.2f%%) = %.2f", r.ID, r.AdjustmentValue, precio)
	case AdjustmentFixed:
		precio = precio + r.AdjustmentValue
		expl += fmt.Sprintf(" + regla %d (+%.2f) = %.2f", r.ID, r.AdjustmentValue, precio)
	}

	if r.RoundTo != nil && *r.RoundTo > 0 {
		paso := *r.RoundTo
		precio = math.Ceil(precio/paso) * paso
		expl += fmt.Sprintf(", redondeado a %.0f = %.2f", paso, precio)
	} else {
		precio = math.Ceil(precio/100) * 100
	}

	return precio, expl
}
