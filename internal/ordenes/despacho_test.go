package ordenes

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/store"
)

// Confirmar el despacho es el otro extremo del ciclo. Sin esto, MDV despacha y
// el marketplace nunca se entera: MercadoLibre y Falabella miden el tiempo
// hasta el envío y, pasado el plazo, cancelan, reembolsan al comprador y bajan
// la reputación del vendedor.

// canalQueAnota recuerda con qué se le confirmó el despacho.
type canalQueAnota struct {
	channel.Adapter
	avisos []channel.Fulfillment
	refs   []channel.ExternalRef
	err    error
}

func (c *canalQueAnota) AckOrder(_ context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	c.refs = append(c.refs, ref)
	c.avisos = append(c.avisos, f)
	return c.err
}

func servicioDeDespacho(t *testing.T, st *tiendaFalsa, od *odooFalso, canal *canalQueAnota) *Servicio {
	t.Helper()
	s := nuevoCon(st, slog.New(slog.DiscardHandler), od.abrir(t))
	s.adaptador = func(context.Context, int64) (channel.Adapter, error) { return canal, nil }
	return s
}

func pedidoMontado() *store.OrdenPorDespachar {
	return &store.OrdenPorDespachar{
		ID: 11, CuentaID: 250, Canal: "woocommerce",
		ExternalID: "735", Numero: "WC-735", OdooPedidoID: 900,
	}
}

func TestSinAlbaranValidadoNoSeAvisaAlCanal(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	// El albarán existe pero está en preparación: la mercancía sigue en
	// bodega. Avisar aquí manda al comprador un aviso de envío que no existe,
	// y en MercadoLibre eso abre una reclamación.
	od.albaranEstado = "assigned"

	canal := &canalQueAnota{}
	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatalf("un pedido aún en bodega no es un error: %v", err)
	}
	if len(canal.avisos) != 0 {
		t.Error("se avisó al canal de un despacho que no ha ocurrido")
	}
	if st.despachoHecho {
		t.Error("se marcó como despachado sin que saliera de bodega")
	}
}

func TestElAlbaranValidadoConfirmaElDespachoAlCanal(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"

	canal := &canalQueAnota{}
	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatal(err)
	}

	if len(canal.avisos) != 1 {
		t.Fatalf("se esperaba un aviso al canal, hubo %d", len(canal.avisos))
	}
	if canal.refs[0].ListingID != "735" {
		t.Errorf("se avisó del pedido equivocado: %q", canal.refs[0].ListingID)
	}
	if canal.avisos[0].ShippedAt.IsZero() {
		t.Error("no se mandó la fecha de despacho: el canal la usa para medir el plazo")
	}
	if !st.salidaMarcada {
		t.Error("no se anotó la salida de bodega")
	}
	if !st.despachoHecho {
		t.Error("no se selló el despacho: el trabajo volvería a avisar al canal")
	}
}

func TestSinGuiaSeConfirmaIgual(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"
	// El módulo de transporte de Odoo no está instalado en MDV, así que no hay
	// guía que leer. En Mercado Envíos y en Falabella la logística la pone el
	// canal y tampoco hay ninguna que mandar: exigirla bloquearía justo los
	// pedidos con menos margen de tiempo.
	od.traeGuia = false

	canal := &canalQueAnota{}
	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatalf("la falta de guía no puede impedir la confirmación: %v", err)
	}
	if len(canal.avisos) != 1 {
		t.Fatalf("no se avisó al canal por no haber guía")
	}
	if canal.avisos[0].TrackingNumber != "" {
		t.Errorf("se inventó una guía: %q", canal.avisos[0].TrackingNumber)
	}
}

func TestLaGuiaEscritaAManoGanaALaDeOdoo(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	p := pedidoMontado()
	p.Guia, p.Transportadora = "ENV-999", "Servientrega"
	st.porDespachar = p

	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"
	od.traeGuia = true
	od.guiaOdoo, od.transpOdoo = "VIEJA-111", "Otra"

	canal := &canalQueAnota{}
	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatal(err)
	}
	// Si alguien se molestó en teclearla es porque la de Odoo faltaba o estaba
	// mal; pisarla con la de Odoo desharía la corrección.
	if canal.avisos[0].TrackingNumber != "ENV-999" {
		t.Errorf("se mandó la guía de Odoo en vez de la escrita: %q", canal.avisos[0].TrackingNumber)
	}
	if canal.avisos[0].Carrier != "Servientrega" {
		t.Errorf("transportadora equivocada: %q", canal.avisos[0].Carrier)
	}
}

func TestLaGuiaDeOdooSeUsaCuandoNadieEscribioNinguna(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"
	// Si algún día se instala el módulo delivery, la guía tiene que empezar a
	// llegar sola sin tocar el código.
	od.traeGuia = true
	od.guiaOdoo, od.transpOdoo = "ODOO-555", "Coordinadora"

	canal := &canalQueAnota{}
	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatal(err)
	}
	if canal.avisos[0].TrackingNumber != "ODOO-555" {
		t.Errorf("no se leyó la guía de Odoo: %q", canal.avisos[0].TrackingNumber)
	}
	if canal.avisos[0].Carrier != "Coordinadora" {
		t.Errorf("no se leyó la transportadora de Odoo: %q", canal.avisos[0].Carrier)
	}
}

func TestUnCanalQueDiceQueNoProcedeCierraElPedidoEnVezDeReintentarlo(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"

	// El canal gestiona el envío por su cuenta (Mercado Envíos), o el pedido
	// ya estaba despachado. No hay nada que arreglar, así que reintentarlo
	// cinco veces solo llena la pantalla de errores que nadie puede resolver.
	canal := &canalQueAnota{err: &channel.Error{
		Kind: channel.MercadoLibre, StatusCode: http.StatusConflict,
		Code: "fulfillment", Message: "el envío lo gestiona MercadoLibre",
	}}

	if err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11); err != nil {
		t.Fatalf("un «no procede» no es un fallo: %v", err)
	}
	if !st.despachoHecho {
		t.Error("no se cerró el pedido: el trabajo lo reintentaría para siempre")
	}
	if st.falloDespacho != "" {
		t.Errorf("se anotó como fallo algo que el canal dio por resuelto: %q", st.falloDespacho)
	}
}

func TestUnFalloDeRedSeAnotaYSeReintenta(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	st.porDespachar = pedidoMontado()
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"

	canal := &canalQueAnota{err: &channel.Error{
		Kind: channel.WooCommerce, StatusCode: http.StatusBadGateway,
		Message: "la tienda no responde",
	}}

	err := servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11)
	if err == nil {
		t.Fatal("un 502 tiene que devolver error para que el trabajo se reintente")
	}
	if st.despachoHecho {
		t.Error("se selló como despachado sin que el canal se enterara")
	}
	if st.falloDespacho == "" {
		t.Error("el motivo no quedó anotado: la pantalla no puede enseñar por qué falla")
	}
}

func TestElTercerFalloLevantaUnAviso(t *testing.T) {
	st, od := nuevaTienda(), nuevoOdoo(t)
	p := pedidoMontado()
	// Ya van dos intentos: este es el tercero. Los dos primeros suelen ser un
	// corte de red y avisar de todos entrena a ignorar el correo.
	p.Intentos = 2
	st.porDespachar = p
	od.albaranEstado = "done"
	od.albaranFecha = "2026-09-06 14:30:00"

	canal := &canalQueAnota{err: &channel.Error{
		Kind: channel.WooCommerce, StatusCode: http.StatusBadGateway, Message: "sigue sin responder",
	}}
	_ = servicioDeDespacho(t, st, od, canal).ConfirmarDespacho(context.Background(), 11)

	var visto bool
	for _, a := range st.alertas {
		if a.tipo == AlertaDespachoFallido {
			visto = true
		}
	}
	if !visto {
		t.Error("al tercer fallo nadie se entera de que el canal sigue esperando")
	}
}
