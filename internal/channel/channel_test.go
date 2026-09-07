package channel

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// adaptadorFalso es el mínimo que hace falta para probar el registro.
type adaptadorFalso struct {
	kind Kind
	caps Capabilities
}

func (a *adaptadorFalso) Kind() Kind                 { return a.kind }
func (a *adaptadorFalso) Capabilities() Capabilities { return a.caps }

func (a *adaptadorFalso) Publish(context.Context, PublishRequest) (PublishResult, error) {
	return PublishResult{}, nil
}
func (a *adaptadorFalso) Update(context.Context, UpdateRequest) (UpdateResult, error) {
	return UpdateResult{}, nil
}
func (a *adaptadorFalso) UpdateStock(context.Context, []StockUpdate) ([]OpResult, error) {
	return nil, nil
}
func (a *adaptadorFalso) UpdatePrice(context.Context, []PriceUpdate) ([]OpResult, error) {
	return nil, nil
}
func (a *adaptadorFalso) Pause(context.Context, ExternalRef) error  { return nil }
func (a *adaptadorFalso) Resume(context.Context, ExternalRef) error { return nil }
func (a *adaptadorFalso) FetchStatus(context.Context, []ExternalRef) ([]ListingStatus, error) {
	return nil, nil
}
func (a *adaptadorFalso) ListRemote(context.Context, Cursor) (RemotePage, error) {
	return RemotePage{Done: true}, nil
}
func (a *adaptadorFalso) FetchOrders(context.Context, time.Time, Cursor) (OrderPage, error) {
	return OrderPage{Done: true}, nil
}
func (a *adaptadorFalso) AckOrder(context.Context, ExternalRef, Fulfillment) error { return nil }

// Comprobación en tiempo de compilación de que el falso cumple la interfaz.
var _ Adapter = (*adaptadorFalso)(nil)

func TestRegistroYConstruccion(t *testing.T) {
	resetParaTest()
	t.Cleanup(resetParaTest)

	Register(WooCommerce, func(cfg Config) (Adapter, error) {
		return &adaptadorFalso{kind: WooCommerce}, nil
	})

	if !EstaRegistrado(WooCommerce) {
		t.Fatal("woocommerce debería estar registrado")
	}
	if EstaRegistrado(Shopify) {
		t.Fatal("shopify no debería estar registrado")
	}

	a, err := New(WooCommerce, Config{AccountID: 1, AccountName: "AON Woo"})
	if err != nil {
		t.Fatalf("construyendo: %v", err)
	}
	if a.Kind() != WooCommerce {
		t.Fatalf("kind = %q", a.Kind())
	}
}

func TestCanalDesconocido(t *testing.T) {
	resetParaTest()
	t.Cleanup(resetParaTest)

	Register(Shopify, func(Config) (Adapter, error) { return &adaptadorFalso{kind: Shopify}, nil })

	_, err := New(MercadoLibre, Config{})
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	// El mensaje debe decir qué canales sí hay: es el error que verá quien
	// olvide importar el paquete del canal.
	if !contiene(err.Error(), "shopify") {
		t.Fatalf("el error debería listar los canales registrados: %v", err)
	}
}

func TestRegistroDuplicadoEntraEnPanico(t *testing.T) {
	resetParaTest()
	t.Cleanup(resetParaTest)

	Register(Falabella, func(Config) (Adapter, error) { return &adaptadorFalso{}, nil })

	defer func() {
		if recover() == nil {
			t.Fatal("registrar dos veces el mismo canal debería provocar pánico")
		}
	}()
	Register(Falabella, func(Config) (Adapter, error) { return &adaptadorFalso{}, nil })
}

func TestRegisteredEsEstable(t *testing.T) {
	resetParaTest()
	t.Cleanup(resetParaTest)

	Register(Shopify, func(Config) (Adapter, error) { return &adaptadorFalso{}, nil })
	Register(WooCommerce, func(Config) (Adapter, error) { return &adaptadorFalso{}, nil })
	Register(Falabella, func(Config) (Adapter, error) { return &adaptadorFalso{}, nil })

	primera := fmt.Sprint(Registered())
	for i := 0; i < 20; i++ {
		if fmt.Sprint(Registered()) != primera {
			t.Fatal("Registered() debe devolver siempre el mismo orden")
		}
	}
}

func TestErrorDeFactorySePropaga(t *testing.T) {
	resetParaTest()
	t.Cleanup(resetParaTest)

	fallo := errors.New("falta el token de acceso")
	Register(MercadoLibre, func(Config) (Adapter, error) { return nil, fallo })

	_, err := New(MercadoLibre, Config{AccountName: "RingConn ML"})
	if !errors.Is(err, fallo) {
		t.Fatalf("el error de la factory debería propagarse, se obtuvo %v", err)
	}
	// Y debe decir de qué cuenta se trata: con 16 cuentas, un error sin
	// contexto no sirve para nada.
	if !contiene(err.Error(), "RingConn ML") {
		t.Fatalf("el error debería nombrar la cuenta: %v", err)
	}
}

func TestEsReintentable(t *testing.T) {
	casos := []struct {
		nombre   string
		err      error
		esperado bool
	}{
		{"429 por exceso de peticiones", &Error{StatusCode: 429}, true},
		{"500 del servidor", &Error{StatusCode: 500}, true},
		{"503 no disponible", &Error{StatusCode: 503}, true},
		{"408 tiempo agotado", &Error{StatusCode: 408}, true},
		{"400 validación", &Error{StatusCode: 400}, false},
		{"401 no autorizado", &Error{StatusCode: 401}, false},
		{"403 prohibido", &Error{StatusCode: 403}, false},
		{"404 no encontrado", &Error{StatusCode: 404}, false},
		{"422 título demasiado largo", &Error{StatusCode: 422, Code: "title_too_long"}, false},
		{"error de red", errors.New("connection reset by peer"), true},
		{"contexto cancelado", context.Canceled, false},
		{"plazo agotado", context.DeadlineExceeded, false},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := EsReintentable(c.err); got != c.esperado {
				t.Errorf("EsReintentable(%v) = %v, se esperaba %v", c.err, got, c.esperado)
			}
		})
	}
}

func TestErrorEnvuelve(t *testing.T) {
	raiz := errors.New("EOF")
	err := &Error{Kind: Falabella, StatusCode: 500, Message: "feed rechazado", Err: raiz}

	if !errors.Is(err, raiz) {
		t.Error("Error debería envolver la causa")
	}
	if !contiene(err.Error(), "falabella") {
		t.Errorf("el mensaje debería nombrar el canal: %s", err)
	}
}

func contiene(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (a *adaptadorFalso) Delete(context.Context, ExternalRef) error { return nil }
