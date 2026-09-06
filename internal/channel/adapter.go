// Package channel define el contrato que cumple todo canal de venta.
//
// El núcleo de sincronización nunca importa un paquete de canal concreto: solo
// conoce Adapter y Capabilities. Cuando el comportamiento difiere entre
// canales, el núcleo consulta capacidades en vez de preguntar "¿eres
// MercadoLibre?". Añadir un quinto canal es escribir un Adapter y registrarlo.
package channel

import (
	"context"
	"errors"
	"time"
)

// Kind identifica un tipo de canal.
type Kind string

const (
	MercadoLibre Kind = "mercadolibre"
	Falabella    Kind = "falabella"
	WooCommerce  Kind = "woocommerce"
	Shopify      Kind = "shopify"
)

// Adapter es la interfaz que implementa cada canal.
//
// Todos los métodos deben respetar la cancelación del contexto y devolver
// errores que se puedan clasificar con EsReintentable.
type Adapter interface {
	Kind() Kind
	Capabilities() Capabilities

	// --- catálogo ---

	// Publish crea una publicación nueva y devuelve el identificador del canal.
	// Debe ser idempotente: si el SKU ya existe en el canal, se adopta la
	// publicación existente en vez de crear un duplicado.
	Publish(ctx context.Context, req PublishRequest) (PublishResult, error)

	// Update modifica una publicación existente. Solo recibe lo que cambió.
	Update(ctx context.Context, req UpdateRequest) (UpdateResult, error)

	// UpdateStock y UpdatePrice existen aparte de Update porque son los
	// endpoints baratos: el stock cambia a diario y el contenido casi nunca.
	UpdateStock(ctx context.Context, ups []StockUpdate) ([]OpResult, error)
	UpdatePrice(ctx context.Context, ups []PriceUpdate) ([]OpResult, error)

	Pause(ctx context.Context, ref ExternalRef) error
	Resume(ctx context.Context, ref ExternalRef) error
	FetchStatus(ctx context.Context, refs []ExternalRef) ([]ListingStatus, error)

	// --- adopción de catálogo existente ---

	// ListRemote enumera lo que ya está publicado en la cuenta, para poder
	// emparejarlo por SKU con el catálogo de Odoo sin republicar nada.
	ListRemote(ctx context.Context, cur Cursor) (RemotePage, error)

	// --- órdenes ---

	FetchOrders(ctx context.Context, desde time.Time, cur Cursor) (OrderPage, error)
	AckOrder(ctx context.Context, ref ExternalRef, f Fulfillment) error
}

// Capabilities describe qué sabe hacer un canal.
//
// Es lo que evita los condicionales por canal dentro del núcleo. Ejemplo: si
// ScheduledOffers es falso, el planificador programa dos trabajos —aplicar el
// precio de oferta y revertirlo— en vez de mandar las fechas al canal.
type Capabilities struct {
	// NativeCompareAtPrice: el canal tiene un campo de precio tachado.
	// Shopify sí (compareAtPrice); MercadoLibre no.
	NativeCompareAtPrice bool

	// ScheduledOffers: el canal acepta fecha de inicio y fin de promoción.
	// WooCommerce y Falabella sí; Shopify y MercadoLibre no.
	ScheduledOffers bool

	BulkPriceUpdate bool
	BulkStockUpdate bool
	MaxBatchSize    int

	// AsyncFeeds: las escrituras devuelven un identificador de proceso y hay
	// que consultar el resultado después. Es el modelo de Falabella.
	AsyncFeeds bool

	// Variants: cómo modela el canal las variantes.
	Variants VariantSupport

	// RequiresOAuthRefresh: los tokens caducan y hay que renovarlos.
	// MercadoLibre y Shopify sí; WooCommerce y Falabella usan claves fijas.
	RequiresOAuthRefresh bool

	// MaxTitleLength es el límite de caracteres del título. MercadoLibre
	// impone 60, y el 14,7% del catálogo de MDV lo supera.
	MaxTitleLength int

	// RequiresDescription: el canal rechaza publicaciones sin descripción.
	RequiresDescription bool

	// RequiresCategoryMapping: hay que mapear la categoría de Odoo a la del
	// canal antes de poder publicar.
	RequiresCategoryMapping bool
}

// VariantSupport describe el modelo de variantes de un canal.
type VariantSupport int

const (
	// SinVariantes: cada variante es una publicación independiente.
	SinVariantes VariantSupport = iota
	// VariantesEnPublicacion: una publicación agrupa varias variantes.
	VariantesEnPublicacion
)

// ExternalRef apunta a algo dentro del canal.
type ExternalRef struct {
	ListingID string
	VariantID string
	SKU       string
}

// Cursor pagina resultados. Cada canal lo interpreta a su manera; el núcleo
// solo lo devuelve tal cual en la siguiente llamada.
type Cursor struct {
	Token string
	Page  int
	Size  int
}

// Product es la vista normalizada que recibe un adaptador.
type Product struct {
	SKU         string
	Title       string
	Description string
	Brand       string
	CategoryID  string
	Attributes  map[string]string
	Images      []Image
	Variants    []Variant
	Weight      float64
	Volume      float64

	// Medidas del paquete en centímetros, tal como las guarda Integra por
	// variante (product_variants.largo_cm / ancho_cm / alto_cm, migración
	// 017). Weight y Volume no bastan para los canales que piden las tres
	// aristas por separado: Falabella marca PackageHeight, PackageWidth y
	// PackageLength como obligatorios dentro de <ProductData> de ProductCreate
	// y ProductUpdate, en enteros de centímetros
	// (https://developers.falabella.com/v600.0.0/reference/productcreate), y
	// sin ellas rechaza el alta entera.
	LengthCm float64
	WidthCm  float64
	HeightCm float64
}

// Variant es una variante concreta con su precio y su stock.
type Variant struct {
	SKU          string
	Barcode      string
	Attributes   map[string]string
	RegularPrice float64
	SalePrice    float64 // cero si no hay oferta
	SaleStartsAt *time.Time
	SaleEndsAt   *time.Time
	Currency     string
	Quantity     int
	ExternalRef  ExternalRef
}

// Image es una imagen de producto.
type Image struct {
	URL      string
	Position int
	Hash     string
}

type PublishRequest struct {
	Product Product
	DryRun  bool
}

type PublishResult struct {
	Ref ExternalRef
	// Adopted es cierto cuando el producto ya existía en el canal y se adoptó
	// en vez de crearse. Con catálogo vivo en Falabella y MercadoLibre, este
	// es el camino habitual en la primera sincronización.
	Adopted     bool
	VariantRefs map[string]ExternalRef // SKU → referencia
	Warnings    []string
}

type UpdateRequest struct {
	Ref     ExternalRef
	Product Product
	// Fields limita lo que se envía. Vacío = todo.
	Fields []string
	DryRun bool
}

type UpdateResult struct {
	Ref      ExternalRef
	Warnings []string
}

type StockUpdate struct {
	Ref      ExternalRef
	Quantity int
}

type PriceUpdate struct {
	Ref          ExternalRef
	RegularPrice float64
	SalePrice    float64
	StartsAt     *time.Time
	EndsAt       *time.Time
	Currency     string
}

// OpResult es el resultado de una operación dentro de un lote. Un fallo en un
// elemento no invalida el resto.
type OpResult struct {
	Ref   ExternalRef
	OK    bool
	Error error
	// FeedID lo rellenan los canales asíncronos, para consultar el resultado.
	FeedID string
}

type ListingStatus struct {
	Ref       ExternalRef
	Status    string
	Price     float64
	Quantity  int
	Permalink string
}

type RemotePage struct {
	Items []RemoteListing
	Next  Cursor
	Done  bool
}

// RemoteListing es una publicación que ya existe en el canal.
type RemoteListing struct {
	Ref      ExternalRef
	Title    string
	Status   string
	Price    float64
	Quantity int
	Variants []RemoteListing
}

type OrderPage struct {
	Orders []Order
	Next   Cursor
	Done   bool
}

// Order es un pedido tal como lo entrega el canal, ya normalizado.
type Order struct {
	ExternalID string
	Number     string
	Status     string
	OrderedAt  time.Time
	// UpdatedAt es la última modificación en el canal (cambio de estado,
	// pago, envío). Cero si el canal no la da; el núcleo entonces usa
	// OrderedAt para la marca de agua.
	UpdatedAt time.Time
	Currency  string
	Total     float64
	Shipping  float64
	Tax       float64
	Buyer     Buyer
	Lines     []OrderLine
	// Raw conserva la carga original para poder reprocesar sin volver a pedirla.
	Raw []byte
}

type Buyer struct {
	Name     string
	Document string
	Email    string
	Phone    string
	Address  Address
}

type Address struct {
	Line1      string
	Line2      string
	City       string
	State      string
	PostalCode string
	Country    string
}

type OrderLine struct {
	ExternalID string
	SKU        string
	VariantRef string
	Title      string
	Quantity   float64
	UnitPrice  float64
	TotalPrice float64
}

// Fulfillment confirma al canal que un pedido se despachó.
type Fulfillment struct {
	TrackingNumber string
	Carrier        string
	ShippedAt      time.Time
}

// --------------------------------------------------------------- errores

// Error clasifica un fallo del canal para que el núcleo decida si reintentar.
type Error struct {
	Kind       Kind
	StatusCode int
	Code       string
	Message    string
	// RetryAfter lo rellena el canal en respuestas 429.
	RetryAfter time.Duration
	Err        error
}

func (e *Error) Error() string {
	if e.Code != "" {
		return string(e.Kind) + ": " + e.Code + ": " + e.Message
	}
	return string(e.Kind) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// ErrNoEncontrado indica que la publicación ya no existe en el canal.
var ErrNoEncontrado = errors.New("la publicación no existe en el canal")

// EsReintentable decide si merece la pena volver a intentarlo.
//
// La regla es la del enunciado: reintentar 429 y 5xx, nunca un 4xx de
// validación. Reintentar un "título demasiado largo" no lo arregla y quema
// cupo de la API que otras publicaciones sí necesitan.
func EsReintentable(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		// Un error que no viene del canal suele ser de red: se reintenta.
		return !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded)
	}
	switch {
	case e.StatusCode == 429:
		return true
	case e.StatusCode >= 500:
		return true
	case e.StatusCode == 408:
		return true
	default:
		return false
	}
}
