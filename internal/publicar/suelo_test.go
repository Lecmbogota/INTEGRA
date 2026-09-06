package publicar

import (
	"context"
	"testing"

	"github.com/mdv/integra/internal/store"
)

// Vender por debajo de coste no se descubre hasta cuadrar el mes, con la
// mercancía ya despachada. Estas pruebas fijan qué hace el motor cuando el
// precio que saldría no cubre el coste.

// Sobre una publicación que ya existe se retiene solo el precio: el canal se
// queda con el precio bueno que ya tenía en vez de que lo pise el malo. El
// stock sí sale, porque dejar de sincronizar existencias por un problema de
// precio haría vender lo que no hay.
func TestUnPrecioBajoCosteNoSaleAlCanalPeroElStockSi(t *testing.T) {
	c := candidatoListo()
	c.ExternalID = "77"

	viejo := c
	c.ContentHash = HashContenido(viejo)
	c.PriceHash = HashPrecio(viejo)
	c.StockHash = HashStock(viejo)

	// El coste sube en Odoo por encima del precio: el recálculo lo apunta en
	// la cola de atención y el candidato llega marcado.
	c.PrecioCanal = 40000
	c.BloqueadoPorCosto = true
	c.Stock = 2

	cola := &colaFalsa{}
	plan, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cola.tiene(TrabajoPrecio) {
		t.Fatalf("se mandó al canal un precio que no cubre el coste: %v", cola.encolados)
	}
	if !cola.tiene(TrabajoStock) {
		t.Fatalf("el stock tiene que seguir sincronizándose: %v", cola.encolados)
	}
	if plan.BajoCosto != 1 || plan.Precio != 0 {
		t.Fatalf("el resumen no dice lo que pasó: %+v", plan)
	}
}

// El alta lleva el precio dentro del mismo envío, así que no hay forma de
// publicar el producto sin publicar el precio: se retiene entero.
func TestUnProductoNuevoConPrecioBajoCosteNoSePublica(t *testing.T) {
	c := candidatoListo() // sin ExternalID: nunca se publicó
	c.PrecioCanal = 40000
	c.BloqueadoPorCosto = true

	cola := &colaFalsa{}
	plan, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cola.encolados) != 0 {
		t.Fatalf("no se puede dar de alta a pérdida: %v", cola.encolados)
	}
	if plan.BajoCosto != 1 || plan.Publicar != 0 {
		t.Fatalf("el resumen no dice lo que pasó: %+v", plan)
	}
}
