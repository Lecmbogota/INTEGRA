package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mdv/integra/internal/store"
)

// El pedido que agotaba sus cinco intentos no tenía camino de vuelta a Odoo
// que no pasara por editar la base a mano. Lo que se prueba aquí es lo que
// decide la capa HTTP del reintento: cuándo se encola el montaje, con qué
// cuenta viaja el trabajo y cuándo se rechaza sin encolar nada.

type reintentadorFalso struct {
	resultado store.Reintento
	err       error
	pedidos   []int64
}

func (r *reintentadorFalso) ReintentarOrden(_ context.Context, id int64) (store.Reintento, error) {
	r.pedidos = append(r.pedidos, id)
	return r.resultado, r.err
}

type encoladorFalso struct {
	cuentas, pedidos []int64
	err              error
}

func (e *encoladorFalso) encolar(_ context.Context, cuentaID, ordenID int64) error {
	e.cuentas = append(e.cuentas, cuentaID)
	e.pedidos = append(e.pedidos, ordenID)
	return e.err
}

func TestReintentarUnPedidoAgotadoLoEncolaConSuCuenta(t *testing.T) {
	st := &reintentadorFalso{resultado: store.Reintento{
		CuentaID: 3, Numero: "ML-1", Canal: "mercadolibre",
		IntentosPrevios: 5, ErrorPrevio: "Odoo no responde",
	}}
	enc := &encoladorFalso{}

	res, err := reintentarOrden(context.Background(), st, enc.encolar, 42)
	if err != nil {
		t.Fatalf("no debía fallar: %v", err)
	}
	if res.codigo != http.StatusAccepted {
		t.Errorf("código %d; un pedido reactivado se responde con 202", res.codigo)
	}
	if len(enc.pedidos) != 1 || enc.pedidos[0] != 42 || enc.cuentas[0] != 3 {
		t.Fatalf("el montaje tiene que encolarse para el pedido 42 con la cuenta 3; "+
			"se encoló pedidos=%v cuentas=%v", enc.pedidos, enc.cuentas)
	}
	if res.antes == nil || res.antes.IntentosPrevios != 5 {
		t.Error("la auditoría necesita saber qué se deshizo: los intentos previos no llegaron")
	}
}

// Devolver a la cola lo que ya está en Odoo crearía un segundo sale.order; lo
// que el canal canceló, uno de una venta que no existe. Ninguno se encola.
func TestLoQueYaEstaEnOdooOSeCanceloNoSeVuelveAEncolar(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
		codigo int
	}{
		{"ya en Odoo", store.ErrOrdenYaEnOdoo, http.StatusConflict},
		{"cancelado por el canal", store.ErrOrdenCancelada, http.StatusConflict},
		{"no existe", store.ErrOrdenNoExiste, http.StatusNotFound},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			enc := &encoladorFalso{}
			res, err := reintentarOrden(context.Background(),
				&reintentadorFalso{err: c.err}, enc.encolar, 42)
			if err != nil {
				t.Fatalf("es una respuesta para quien pulsa, no un fallo del servidor: %v", err)
			}
			if res.codigo != c.codigo {
				t.Errorf("código %d, se esperaba %d", res.codigo, c.codigo)
			}
			if len(enc.pedidos) != 0 {
				t.Error("se encoló el montaje de un pedido que no se puede reintentar")
			}
			if msg, _ := res.cuerpo["error"].(string); msg == "" {
				t.Error("la respuesta tiene que decir por qué no hay nada que reintentar")
			}
			if res.antes != nil {
				t.Error("no se reactivó nada: no hay nada que auditar")
			}
		})
	}
}

// Reintentar con un SKU que sigue sin existir solo gastaría intentos y
// volvería a fallar por lo mismo: se rechaza y se dice cuál falta.
func TestUnPedidoConSKUSinMapearNoSeEncolaYSeDiceCual(t *testing.T) {
	st := &reintentadorFalso{resultado: store.Reintento{
		Numero: "FB-9", SinMapear: []string{"SKU-NUEVO"},
	}}
	enc := &encoladorFalso{}

	res, err := reintentarOrden(context.Background(), st, enc.encolar, 7)
	if err != nil {
		t.Fatalf("no debía fallar: %v", err)
	}
	if res.codigo != http.StatusConflict {
		t.Errorf("código %d; se esperaba 409", res.codigo)
	}
	if len(enc.pedidos) != 0 {
		t.Error("no puede encolarse un montaje que va a fallar por el SKU")
	}
	if msg, _ := res.cuerpo["error"].(string); !strings.Contains(msg, "SKU-NUEVO") {
		t.Errorf("el mensaje tiene que nombrar el SKU que falta: %q", msg)
	}
	if res.antes != nil {
		t.Error("el pedido no se reactivó: no hay nada que auditar")
	}
}

// Un fallo de la base no puede acabar encolando un montaje de un pedido que
// sigue agotado: el trabajo se completaría sin hacer nada.
func TestUnFalloDeLaBaseNoEncolaNada(t *testing.T) {
	enc := &encoladorFalso{}
	_, err := reintentarOrden(context.Background(),
		&reintentadorFalso{err: errors.New("la base no responde")}, enc.encolar, 7)
	if err == nil {
		t.Fatal("el fallo de la base tenía que llegar al log")
	}
	if len(enc.pedidos) != 0 {
		t.Error("se encoló el montaje sin haber reiniciado el contador")
	}
}

// Si la cola no acepta el trabajo, el contador ya quedó a cero y el
// planificador lo recogerá; pero al operador se le dice la verdad.
func TestSiLaColaFallaSeDiceAunqueElContadorYaEsteACero(t *testing.T) {
	enc := &encoladorFalso{err: errors.New("la cola no responde")}
	_, err := reintentarOrden(context.Background(),
		&reintentadorFalso{resultado: store.Reintento{CuentaID: 1}}, enc.encolar, 7)
	if err == nil {
		t.Fatal("un encolado fallido no puede responder 202")
	}
}
