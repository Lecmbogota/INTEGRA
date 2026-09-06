package pricing

import (
	"testing"
	"time"
)

func TestResolverPrecio_Override(t *testing.T) {
	input := VariantPricingInput{
		VariantID: 1,
		BasePrice: 100000,
		Cost:      70000,
	}
	channel := ChannelInfo{CommissionPct: 15, FixedCost: 2000}
	override := &PriceOverride{
		VariantID: 1,
		Price:     150000,
		Reason:    "Promoción exclusiva",
	}

	now := time.Now()
	res := ResolverPrecio(input, channel, nil, override, nil, now)

	if res.RegularPrice != 150000 {
		t.Fatalf("esperaba 150000, obtuve %.2f", res.RegularPrice)
	}
	if res.Source != SourceOverride {
		t.Fatalf("esperaba source override, obtuve %s", res.Source)
	}
	if res.SalePrice != nil {
		t.Fatalf("no esperaba sale_price, obtuve %v", *res.SalePrice)
	}
}

func TestResolverPrecio_ReglaEspecificidadYMargen(t *testing.T) {
	brandID := int64(10)
	categPath := "Computadores / Portátiles"
	input := VariantPricingInput{
		VariantID: 1,
		BrandID:   &brandID,
		CategPath: categPath,
		BasePrice: 1000000,
		Cost:      800000,
	}
	channel := ChannelInfo{CommissionPct: 10, FixedCost: 0}

	roundTo := float64(1000)
	minMargin := float64(30) // costo 800.000 * 1.30 = 1.040.000 mínimo

	reglas := []ChannelPriceRule{
		{
			ID:              1,
			AdjustmentType:  AdjustmentPercent,
			AdjustmentValue: 5,
			Priority:        100,
			Active:          true,
		},
		{
			ID:               2,
			BrandID:          &brandID,
			CategPathPrefix:  &categPath,
			AdjustmentType:   AdjustmentPercent,
			AdjustmentValue:  10,
			RoundTo:          &roundTo,
			MinMarginPercent: &minMargin,
			Priority:         10,
			Active:           true,
		},
	}

	now := time.Now()
	res := ResolverPrecio(input, channel, reglas, nil, nil, now)

	// Base 1.000.000 / 0.90 = 1.111.111,11
	// + 10% = 1.222.222,22
	// Redondeado a 1.000 = 1.223.000
	// Verifica que aplicó regla 2 (más específica)
	if res.RegularPrice < 1200000 {
		t.Fatalf("precio inesperado %.2f", res.RegularPrice)
	}
	if res.Source != SourcePricelist {
		t.Fatalf("source esperado pricelist, obtuve %s", res.Source)
	}
}

func TestResolverPrecio_OfertaVigente(t *testing.T) {
	input := VariantPricingInput{
		VariantID: 1,
		BasePrice: 500000,
		Cost:      300000,
	}
	channel := ChannelInfo{CommissionPct: 10, FixedCost: 0}

	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	start := now.Add(-1 * time.Hour)
	end := now.Add(24 * time.Hour)

	oferta := &Offer{
		ID:         99,
		VariantID:  1,
		OfferPrice: 480000,
		StartsAt:   start,
		EndsAt:     &end,
		Active:     true,
	}

	res := ResolverPrecio(input, channel, nil, nil, oferta, now)

	if res.SalePrice == nil || *res.SalePrice != 480000 {
		t.Fatalf("esperaba sale_price 480000, obtuve %v", res.SalePrice)
	}
	if res.Source != SourceOffer {
		t.Fatalf("esperaba source offer, obtuve %s", res.Source)
	}
}
