// Package pricing replica el motor de tarifas de Odoo.
//
// Hace falta replicarlo porque Odoo bloquea por RPC toda llamada a métodos que
// empiezan por guion bajo, y el cálculo vive en _get_products_price y
// _compute_price_rule. No hay endpoint público que devuelva "el precio de este
// producto según esta tarifa".
//
// Leer list_price no es alternativa: en el catálogo de MDV el 58% de los
// productos lo tiene en 1,00 y donde tiene valor está en USD mientras el coste
// está en COP. El precio real solo existe como resultado de aplicar las reglas
// de product.pricelist.item sobre standard_price.
//
// Se sigue el orden de resolución de Odoo: de la regla más específica a la más
// general, con las de variante ganando a las de plantilla, estas a las de
// categoría y estas a la global.
package pricing

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// AppliedOn indica el ámbito de una regla. Los literales son los de Odoo y el
// orden alfabético coincide con el de especificidad, que es de lo que se
// aprovecha la ordenación.
const (
	OnVariant  = "0_product_variant"
	OnProduct  = "1_product"
	OnCategory = "2_product_category"
	OnGlobal   = "3_global"
)

// Base indica sobre qué se calcula.
const (
	BaseListPrice     = "list_price"
	BaseStandardPrice = "standard_price"
	BasePricelist     = "pricelist"
)

// ComputeMode indica cómo se calcula.
const (
	ComputeFixed      = "fixed"
	ComputePercentage = "percentage"
	ComputeFormula    = "formula"
)

// Rule es una regla de product.pricelist.item.
type Rule struct {
	ID           int64
	PricelistID  int64
	AppliedOn    string
	ComputePrice string
	Base         string

	CategoryID      int64
	TemplateID      int64
	VariantID       int64
	BasePricelistID int64

	FixedPrice     float64
	PercentPrice   float64
	PriceDiscount  float64
	PriceSurcharge float64
	PriceRound     float64
	PriceMinMargin float64
	PriceMaxMargin float64
	MinQuantity    float64

	DateStart *time.Time
	DateEnd   *time.Time
}

// Product es lo que hace falta saber del producto para tarificarlo.
type Product struct {
	VariantID  int64
	TemplateID int64
	CategoryID int64
	// CategoryAncestors incluye la propia categoría y todas sus ascendientes:
	// una regla sobre "Almacenamiento" aplica a "Almacenamiento / Disco / SSD".
	CategoryAncestors []int64
	ListPrice         float64
	Cost              float64
}

// Pricelist es una tarifa con sus reglas.
type Pricelist struct {
	ID       int64
	Name     string
	Currency string
	Rules    []Rule
}

// Result es el precio calculado y de dónde salió.
type Result struct {
	Price float64
	// RuleID es la regla que decidió el precio; cero si no aplicó ninguna.
	RuleID int64
	// Applied indica si alguna regla llegó a aplicarse. Cuando es falso, el
	// precio devuelto es el list_price sin tocar, que en MDV no es fiable.
	Applied bool
	// Explanation describe el cálculo, para poder mostrarlo en la interfaz y
	// justificar por qué un producto sale a un precio y no a otro.
	Explanation string
}

// Engine calcula precios contra un conjunto de tarifas.
type Engine struct {
	// porID permite resolver las reglas con base "pricelist", que delegan el
	// cálculo en otra tarifa.
	porID map[int64]*Pricelist
}

// NewEngine construye el motor. Las reglas se ordenan una sola vez.
func NewEngine(listas []Pricelist) *Engine {
	e := &Engine{porID: make(map[int64]*Pricelist, len(listas))}
	for i := range listas {
		pl := listas[i]
		ordenarReglas(pl.Rules)
		e.porID[pl.ID] = &pl
	}
	return e
}

// ordenarReglas replica el ORDER BY de Odoo: primero lo más específico, y a
// igual especificidad, la regla con mayor cantidad mínima.
func ordenarReglas(rs []Rule) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].AppliedOn != rs[j].AppliedOn {
			return rs[i].AppliedOn < rs[j].AppliedOn
		}
		if rs[i].MinQuantity != rs[j].MinQuantity {
			return rs[i].MinQuantity > rs[j].MinQuantity
		}
		return rs[i].ID > rs[j].ID
	})
}

// Price calcula el precio de un producto según una tarifa.
func (e *Engine) Price(pricelistID int64, p Product, cantidad float64, cuando time.Time) (Result, error) {
	return e.price(pricelistID, p, cantidad, cuando, 0)
}

// profundidad corta las cadenas de tarifas que se referencian entre sí. Odoo
// permite construir ciclos y, sin este corte, el cálculo no terminaría.
const maxProfundidad = 5

func (e *Engine) price(pricelistID int64, p Product, cantidad float64, cuando time.Time, prof int) (Result, error) {
	if prof > maxProfundidad {
		return Result{}, fmt.Errorf("cadena de tarifas demasiado profunda desde la tarifa %d: posible ciclo", pricelistID)
	}
	pl, ok := e.porID[pricelistID]
	if !ok {
		return Result{}, fmt.Errorf("tarifa %d desconocida", pricelistID)
	}

	for i := range pl.Rules {
		r := &pl.Rules[i]
		if !aplica(r, p, cantidad, cuando) {
			continue
		}
		precio, expl, err := e.calcular(r, p, cantidad, cuando, prof)
		if err != nil {
			return Result{}, err
		}
		return Result{Price: precio, RuleID: r.ID, Applied: true, Explanation: expl}, nil
	}

	// Sin regla aplicable, Odoo devuelve el list_price. En MDV eso significa
	// que el producto no tiene precio fiable: quien llama debe tratar
	// Applied=false como "no publicable".
	return Result{Price: p.ListPrice, Applied: false,
		Explanation: "ninguna regla de la tarifa aplica; se devuelve list_price sin tocar"}, nil
}

// aplica decide si una regla es candidata para un producto.
func aplica(r *Rule, p Product, cantidad float64, cuando time.Time) bool {
	if cantidad < r.MinQuantity {
		return false
	}
	if r.DateStart != nil && cuando.Before(*r.DateStart) {
		return false
	}
	if r.DateEnd != nil && cuando.After(*r.DateEnd) {
		return false
	}

	switch r.AppliedOn {
	case OnVariant:
		return r.VariantID != 0 && r.VariantID == p.VariantID
	case OnProduct:
		return r.TemplateID != 0 && r.TemplateID == p.TemplateID
	case OnCategory:
		if r.CategoryID == 0 {
			return false
		}
		// La regla aplica a la categoría y a todas sus descendientes.
		for _, id := range p.CategoryAncestors {
			if id == r.CategoryID {
				return true
			}
		}
		return false
	case OnGlobal:
		return true
	default:
		return false
	}
}

func (e *Engine) calcular(r *Rule, p Product, cantidad float64, cuando time.Time, prof int) (float64, string, error) {
	switch r.ComputePrice {
	case ComputeFixed:
		return r.FixedPrice, fmt.Sprintf("precio fijo %.2f", r.FixedPrice), nil

	case ComputePercentage:
		base, expl, err := e.base(r, p, cantidad, cuando, prof)
		if err != nil {
			return 0, "", err
		}
		precio := base * (1 - r.PercentPrice/100)
		return precio, fmt.Sprintf("%s × (1 − %.2f%%) = %.2f", expl, r.PercentPrice, precio), nil

	case ComputeFormula:
		base, expl, err := e.base(r, p, cantidad, cuando, prof)
		if err != nil {
			return 0, "", err
		}

		// Un descuento negativo es un margen: -25 significa base × 1,25.
		// Es como MDV tiene configurada la tarifa Predeterminado.
		precio := base*(1-r.PriceDiscount/100) + r.PriceSurcharge
		detalle := fmt.Sprintf("%s × (1 − %.2f%%)", expl, r.PriceDiscount)
		if r.PriceSurcharge != 0 {
			detalle += fmt.Sprintf(" + %.2f", r.PriceSurcharge)
		}

		if r.PriceRound > 0 {
			precio = redondearA(precio, r.PriceRound)
			detalle += fmt.Sprintf(", redondeado a %.2f", r.PriceRound)
		}
		// Los márgenes se miden sobre la base, no sobre el precio resultante.
		if r.PriceMinMargin != 0 && precio < base+r.PriceMinMargin {
			precio = base + r.PriceMinMargin
			detalle += fmt.Sprintf(", elevado al margen mínimo %.2f", r.PriceMinMargin)
		}
		if r.PriceMaxMargin != 0 && precio > base+r.PriceMaxMargin {
			precio = base + r.PriceMaxMargin
			detalle += fmt.Sprintf(", limitado al margen máximo %.2f", r.PriceMaxMargin)
		}

		return precio, fmt.Sprintf("%s = %.2f", detalle, precio), nil

	default:
		return 0, "", fmt.Errorf("modo de cálculo desconocido %q en la regla %d", r.ComputePrice, r.ID)
	}
}

func (e *Engine) base(r *Rule, p Product, cantidad float64, cuando time.Time, prof int) (float64, string, error) {
	switch r.Base {
	case BaseStandardPrice:
		return p.Cost, fmt.Sprintf("coste %.2f", p.Cost), nil
	case BasePricelist:
		if r.BasePricelistID == 0 {
			return 0, "", fmt.Errorf("la regla %d usa base 'pricelist' sin indicar cuál", r.ID)
		}
		res, err := e.price(r.BasePricelistID, p, cantidad, cuando, prof+1)
		if err != nil {
			return 0, "", err
		}
		return res.Price, fmt.Sprintf("tarifa %d → %.2f", r.BasePricelistID, res.Price), nil
	case BaseListPrice, "":
		return p.ListPrice, fmt.Sprintf("precio de lista %.2f", p.ListPrice), nil
	default:
		return 0, "", fmt.Errorf("base desconocida %q en la regla %d", r.Base, r.ID)
	}
}

// redondearA lleva el precio al múltiplo más cercano de paso.
func redondearA(v, paso float64) float64 {
	if paso <= 0 {
		return v
	}
	return math.Round(v/paso) * paso
}
