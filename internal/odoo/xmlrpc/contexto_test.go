package xmlrpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Las llamadas se construían sin contexto y con 180 segundos de plazo, por
// encima del lease de cinco minutos del worker cuando se encadenan varias.
// Consecuencias: apagar el worker no abortaba la llamada en curso, y una
// cadena lenta al crear un pedido podía superar el lease, hacer que otro
// worker reclamara el mismo trabajo y acabar con dos sale.order iguales, que
// Odoo no impide porque no exige client_order_ref única.

func TestCancelarElContextoAbortaLaLlamada(t *testing.T) {
	llego := make(chan struct{})
	// El manejador simula un Odoo lento, pero con salida propia: sin ella,
	// Close() del servidor esperaría para siempre a una petición que nadie
	// termina y la prueba se colgaría en vez de fallar.
	soltar := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(llego)
		select {
		case <-r.Context().Done():
		case <-soltar:
		case <-time.After(5 * time.Second):
		}
	}))
	defer func() { close(soltar); srv.Close() }()

	ctx, cancelar := context.WithCancel(context.Background())
	cli := New(srv.URL).ConContexto(ctx)

	fin := make(chan error, 1)
	go func() {
		_, err := cli.Call("version")
		fin <- err
	}()

	<-llego
	cancelar()

	select {
	case err := <-fin:
		if err == nil {
			t.Error("cancelar el contexto tiene que abortar la llamada")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("el error debe decir que se canceló, para no confundirlo con un fallo de Odoo: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("la llamada siguió viva tras cancelar: el apagado se quedaría esperando a Odoo")
	}
}

func TestSinContextoElClienteSigueFuncionando(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><methodResponse><params><param>
			<value><string>18.0</string></value></param></params></methodResponse>`))
	}))
	defer srv.Close()

	// Decenas de sitios llaman sin contexto: adoptar el nuevo camino no puede
	// romperlos.
	v, err := New(srv.URL).Call("version")
	if err != nil {
		t.Fatal(err)
	}
	if v != "18.0" {
		t.Errorf("respuesta = %v", v)
	}
}

func TestElPlazoPorLlamadaCabeDentroDelLease(t *testing.T) {
	// El lease de un trabajo son 5 minutos y crear un pedido encadena hasta
	// cinco llamadas. Un plazo por llamada que se acerque al lease hace
	// posible que dos workers procesen el mismo trabajo a la vez.
	const lease = 5 * time.Minute
	if timeoutLlamada >= lease/3 {
		t.Errorf("el plazo por llamada (%v) es demasiado grande frente al lease (%v)",
			timeoutLlamada, lease)
	}
	if got := New("http://x").HTTP.Timeout; got != timeoutLlamada {
		t.Errorf("el cliente se construyó con %v", got)
	}
}

func TestConContextoNoMutaElClienteOriginal(t *testing.T) {
	base := New("http://x")
	conCtx := base.ConContexto(context.Background())

	if base.ctx != nil {
		t.Error("ConContexto debe devolver una copia, no cambiar el cliente compartido")
	}
	if conCtx.ctx == nil {
		t.Error("la copia sí tiene que llevar el contexto")
	}
	if conCtx.Endpoint != base.Endpoint || conCtx.HTTP != base.HTTP {
		t.Error("la copia debe conservar endpoint y cliente HTTP")
	}
}
