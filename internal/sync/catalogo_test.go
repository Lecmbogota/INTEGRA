package sync

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// Estas pruebas cubren un fallo que costaba ventas: un producto archivado o
// borrado en Odoo seguía publicado y vendiéndose en los cuatro canales. Se
// escondía en un detalle del protocolo —el dominio por defecto de Odoo esconde
// los archivados—, así que el servidor simulado reproduce esa semántica al pie
// de la letra: sin active_test en el contexto, los archivados no salen.
//
// Comprobado contra la instancia real (mdv_replica): product.product responde
// 545 registros con el dominio por defecto y 570 con active_test=false. Los 25
// de diferencia están archivados y varios conservan existencias en stock.quant.

// -------------------------------------------------------------- Odoo simulado

type prodFalso struct {
	id     int64
	sku    string
	nombre string
	activo bool
}

type odooFalso struct {
	*httptest.Server

	productos []prodFalso
	// sinCampoActive imita una instancia que no expone active en la lectura.
	sinCampoActive bool
	// sweepVacio imita una lectura rota: Odoo no reconoce ningún id.
	sweepVacio bool
	// lecturaVacia imita un sync incremental en el que nada cambió desde la
	// marca de agua: la lectura por write_date no devuelve ni una fila.
	lecturaVacia bool

	mu            sync.Mutex
	vioActiveTest bool
}

func nuevoOdoo(t *testing.T, productos ...prodFalso) *odooFalso {
	t.Helper()
	o := &odooFalso{productos: productos}

	mux := http.NewServeMux()
	mux.HandleFunc("/xmlrpc/2/common", func(w http.ResponseWriter, r *http.Request) {
		responder(w, int64(2)) // uid del usuario autenticado
	})
	mux.HandleFunc("/xmlrpc/2/object", func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		o.despachar(t, w, string(cuerpo))
	})

	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Close)
	return o
}

func (o *odooFalso) cliente(t *testing.T) *odoo.Client {
	t.Helper()
	cli, err := odoo.Connect(odoo.Config{
		URL: o.URL, Database: "mdv_replica", Username: "admin", APIKey: "admin",
	})
	if err != nil {
		t.Fatalf("conectando al Odoo simulado: %v", err)
	}
	return cli
}

// despachar enruta por modelo y método, que viajan como parámetros de
// execute_kw. Se busca la etiqueta completa para no confundir search con
// search_read.
func (o *odooFalso) despachar(t *testing.T, w http.ResponseWriter, cuerpo string) {
	pide := func(s string) bool { return strings.Contains(cuerpo, "<string>"+s+"</string>") }
	conActiveTest := strings.Contains(cuerpo, "active_test")

	switch {
	case pide("product.product") && pide("search_read"):
		o.mu.Lock()
		o.vioActiveTest = conActiveTest
		o.mu.Unlock()
		if o.lecturaVacia {
			responder(w, []interface{}{})
			return
		}
		responder(w, o.filasProducto(conActiveTest))

	case pide("product.product") && pide("search"):
		// El barrido de identificadores, sujeto a la misma regla: con el
		// dominio por defecto los archivados no salen. Se ignora el
		// "id in [...]" porque el catálogo de la prueba cabe en un lote.
		if o.sweepVacio {
			responder(w, []interface{}{})
			return
		}
		var ids []interface{}
		for _, p := range o.productos {
			if !p.activo && !conActiveTest {
				continue
			}
			ids = append(ids, p.id)
		}
		responder(w, ids)

	case pide("stock.warehouse"):
		responder(w, []interface{}{map[string]interface{}{
			"id": int64(1), "name": "Bodega Principal", "code": "WH",
			"lot_stock_id": []interface{}{int64(8), "WH/Stock"},
		}})

	case pide("stock.location"):
		responder(w, []interface{}{map[string]interface{}{
			"id": int64(8), "warehouse_id": []interface{}{int64(1), "Bodega Principal"},
		}})

	case pide("stock.quant"):
		var qs []interface{}
		for _, p := range o.productos {
			qs = append(qs, map[string]interface{}{
				"id":                p.id,
				"product_id":        []interface{}{p.id, p.nombre},
				"location_id":       []interface{}{int64(8), "WH/Stock"},
				"quantity":          5.0,
				"reserved_quantity": 0.0,
			})
		}
		responder(w, qs)

	default:
		t.Errorf("el Odoo simulado no esperaba esta llamada: %s", cuerpo)
		responder(w, []interface{}{})
	}
}

// filasProducto reproduce la regla que originaba el defecto: sin active_test
// en el contexto, Odoo no devuelve los archivados.
func (o *odooFalso) filasProducto(conActiveTest bool) []interface{} {
	var out []interface{}
	for _, p := range o.productos {
		if !p.activo && !conActiveTest {
			continue
		}
		r := map[string]interface{}{
			"id":              p.id,
			"default_code":    p.sku,
			"name":            p.nombre,
			"product_tmpl_id": []interface{}{p.id + 1000, p.nombre},
			"categ_id":        []interface{}{int64(7), "Todo / Colchones"},
			"write_date":      "2026-09-01 10:00:00",
		}
		if !o.sinCampoActive {
			r["active"] = p.activo
		}
		out = append(out, r)
	}
	return out
}

func responder(w http.ResponseWriter, v interface{}) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodResponse><params><param>`)
	escribirValor(&b, v)
	b.WriteString(`</param></params></methodResponse>`)
	w.Header().Set("Content-Type", "text/xml")
	_, _ = io.WriteString(w, b.String())
}

func escribirValor(b *strings.Builder, v interface{}) {
	b.WriteString("<value>")
	defer b.WriteString("</value>")

	switch t := v.(type) {
	case nil:
		b.WriteString("<nil/>")
	case bool:
		if t {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case int64:
		fmt.Fprintf(b, "<int>%d</int>", t)
	case float64:
		fmt.Fprintf(b, "<double>%g</double>", t)
	case string:
		b.WriteString("<string>")
		_ = xml.EscapeText(b, []byte(t))
		b.WriteString("</string>")
	case []interface{}:
		b.WriteString("<array><data>")
		for _, item := range t {
			escribirValor(b, item)
		}
		b.WriteString("</data></array>")
	case map[string]interface{}:
		b.WriteString("<struct>")
		for k, val := range t {
			b.WriteString("<member><name>" + k + "</name>")
			escribirValor(b, val)
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	default:
		panic(fmt.Sprintf("el codificador de la prueba no sabe escribir %T", v))
	}
}

// ------------------------------------------------------------ base simulada

// tiendaFalsa sustituye a PostgreSQL: solo anota qué le pidió el sync.
type tiendaFalsa struct {
	variantes   map[int64]int64
	identidades []store.Identidad
	stock       []store.FilaStock
	activas     []int64
	inactivas   []int64
	ajustes     int
	siguiente   int64
}

func nuevaTienda(variantes map[int64]int64) *tiendaFalsa {
	if variantes == nil {
		variantes = map[int64]int64{}
	}
	return &tiendaFalsa{variantes: variantes, siguiente: 1000}
}

func (t *tiendaFalsa) UpsertAlmacenes(_ context.Context, _ int64, as []store.Almacen) (map[int64]int64, error) {
	m := map[int64]int64{}
	for _, a := range as {
		m[a.OdooID] = a.OdooID
	}
	return m, nil
}

func (t *tiendaFalsa) UpsertIdentidad(_ context.Context, _ int64, ident store.Identidad) (int64, int64, error) {
	t.identidades = append(t.identidades, ident)
	if _, ya := t.variantes[ident.OdooProductID]; !ya {
		t.siguiente++
		t.variantes[ident.OdooProductID] = t.siguiente
	}
	return t.siguiente, t.variantes[ident.OdooProductID], nil
}

func (t *tiendaFalsa) VariantesPorOdooID(context.Context, int64) (map[int64]int64, error) {
	copia := make(map[int64]int64, len(t.variantes))
	for k, v := range t.variantes {
		copia[k] = v
	}
	return copia, nil
}

func (t *tiendaFalsa) ReemplazarStock(_ context.Context, _ int64, filas []store.FilaStock) error {
	t.stock = filas
	return nil
}

func (t *tiendaFalsa) RecalcularAtencion(context.Context) error { return nil }

func (t *tiendaFalsa) ActualizarWatermark(context.Context, int64, time.Time) error { return nil }

func (t *tiendaFalsa) AjustarActividad(_ context.Context, _ int64, activas, inactivas []int64) (int, int, error) {
	t.ajustes++
	t.activas = append(t.activas, activas...)
	t.inactivas = append(t.inactivas, inactivas...)
	return len(activas), len(inactivas), nil
}

func (t *tiendaFalsa) tieneIdentidad(odooID int64) bool {
	for _, i := range t.identidades {
		if i.OdooProductID == odooID {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ pruebas

// Un producto archivado en Odoo deja de ser mercancía. Antes del arreglo ni
// siquiera se leía —el dominio por defecto lo escondía—, así que en Integra
// seguía activo y los canales lo seguían vendiendo con stock que ya no toca.
func TestProductoArchivadoEnOdooSeDaDeBaja(t *testing.T) {
	odooSrv := nuevoOdoo(t,
		prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true},
		prodFalso{id: 102, sku: "COL-102", nombre: "Colchón Descatalogado", activo: false},
	)
	st := nuevaTienda(map[int64]int64{101: 1, 102: 2})

	res, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, time.Time{})
	if err != nil {
		t.Fatalf("sincronizando: %v", err)
	}

	if !slices.Contains(st.inactivas, 102) {
		t.Errorf("el producto archivado 102 no se dio de baja; inactivas = %v", st.inactivas)
	}
	if slices.Contains(st.inactivas, 101) {
		t.Errorf("se dio de baja el producto vivo 101; inactivas = %v", st.inactivas)
	}
	if !slices.Contains(st.activas, 101) {
		t.Errorf("el producto vivo 101 no se reafirmó como activo; activas = %v", st.activas)
	}
	if st.tieneIdentidad(102) {
		t.Error("un producto archivado no debe refrescar su identidad en Integra")
	}
	if res.Bajas != 1 {
		t.Errorf("Bajas = %d, se esperaba 1", res.Bajas)
	}
	// El sync sigue haciendo su trabajo de siempre.
	if !st.tieneIdentidad(101) {
		t.Error("no se guardó la identidad del producto vivo")
	}
	if len(st.stock) == 0 {
		t.Error("no se reemplazó el stock")
	}
}

// La lectura tiene que pedir explícitamente los archivados: es el único modo
// de enterarse de una baja, y es justo lo que faltaba.
func TestLaLecturaDeProductosPideLosArchivados(t *testing.T) {
	odooSrv := nuevoOdoo(t, prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true})
	st := nuevaTienda(map[int64]int64{101: 1})

	if _, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, time.Time{}); err != nil {
		t.Fatalf("sincronizando: %v", err)
	}

	odooSrv.mu.Lock()
	defer odooSrv.mu.Unlock()
	if !odooSrv.vioActiveTest {
		t.Error("product.product se leyó con el dominio por defecto: los archivados quedan invisibles")
	}
}

// Borrar de verdad un producto no cambia ningún write_date: el registro
// desaparece. Solo se detecta contrastando lo que Integra conoce.
func TestProductoBorradoEnOdooSeDaDeBaja(t *testing.T) {
	odooSrv := nuevoOdoo(t, prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true})
	st := nuevaTienda(map[int64]int64{101: 1, 999: 9})

	if _, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, time.Time{}); err != nil {
		t.Fatalf("sincronizando: %v", err)
	}

	if !slices.Contains(st.inactivas, 999) {
		t.Errorf("el producto borrado 999 no se dio de baja; inactivas = %v", st.inactivas)
	}
	if slices.Contains(st.inactivas, 101) {
		t.Errorf("se dio de baja un producto que sigue en Odoo; inactivas = %v", st.inactivas)
	}
}

// Un fallo de lectura no puede vaciar la tienda: si Odoo no reconoce ningún
// identificador, lo correcto es abortar, no dar de baja el catálogo entero.
func TestUnaLecturaRotaNoDaDeBajaElCatalogoEntero(t *testing.T) {
	odooSrv := nuevoOdoo(t, prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true})
	odooSrv.sweepVacio = true
	st := nuevaTienda(map[int64]int64{101: 1, 102: 2, 103: 3})

	_, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, time.Time{})
	if err == nil {
		t.Fatal("se esperaba un error en vez de dar de baja todo el catálogo")
	}
	if !strings.Contains(err.Error(), "catálogo entero") {
		t.Errorf("el error no explica lo que ocurre: %v", err)
	}
	if st.ajustes != 0 {
		t.Errorf("se tocó la actividad del catálogo pese al fallo de lectura (%d ajustes)", st.ajustes)
	}
}

// La baja no puede depender de haber pillado el cambio dentro de la ventana
// incremental: lo que se archivó antes de la última marca de agua no vuelve a
// aparecer nunca en la lectura por write_date.
func TestSeDetectaUnArchivadoAunqueLaLecturaIncrementalNoTraigaNada(t *testing.T) {
	odooSrv := nuevoOdoo(t,
		prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true},
		prodFalso{id: 102, sku: "COL-102", nombre: "Colchón Descatalogado", activo: false},
	)
	odooSrv.lecturaVacia = true
	st := nuevaTienda(map[int64]int64{101: 1, 102: 2})

	desde := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	res, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, desde)
	if err != nil {
		t.Fatalf("sincronizando: %v", err)
	}
	if res.Leidos != 0 {
		t.Fatalf("la prueba exige que la lectura incremental no traiga nada, trajo %d", res.Leidos)
	}
	if !slices.Contains(st.inactivas, 102) {
		t.Errorf("el archivado 102 pasó desapercibido; inactivas = %v", st.inactivas)
	}
	if slices.Contains(st.inactivas, 101) {
		t.Errorf("se dio de baja el producto vivo 101; inactivas = %v", st.inactivas)
	}
}

// Ante una instancia que no expone el campo active, la duda se resuelve a
// favor del producto: mejor seguir publicando que retirar todo por error.
func TestSinElCampoActiveNoSeDaDeBajaANadie(t *testing.T) {
	odooSrv := nuevoOdoo(t,
		prodFalso{id: 101, sku: "COL-101", nombre: "Colchón Alfa", activo: true},
		prodFalso{id: 103, sku: "COL-103", nombre: "Colchón Beta", activo: true},
	)
	odooSrv.sinCampoActive = true
	st := nuevaTienda(map[int64]int64{101: 1, 103: 3})

	res, err := nuevoCon(odooSrv.cliente(t), st, nil).Catalogo(context.Background(), 1, time.Time{})
	if err != nil {
		t.Fatalf("sincronizando: %v", err)
	}
	if len(st.inactivas) != 0 {
		t.Errorf("sin campo active no se puede dar de baja a nadie; inactivas = %v", st.inactivas)
	}
	if res.Bajas != 0 {
		t.Errorf("Bajas = %d, se esperaba 0", res.Bajas)
	}
}
