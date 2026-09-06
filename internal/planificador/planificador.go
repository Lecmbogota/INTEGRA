// Package planificador es lo que hace que Integra funcione sola.
//
// Sin él, la plataforma es un conjunto de botones que alguien tiene que
// pulsar: sincronizar, publicar, traer pedidos. El planificador dispara esas
// tres cosas por horario y vigila que sigan sanas, que es la diferencia entre
// una herramienta y un sistema.
package planificador

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/ordenes"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
)

// Tipos de alerta que levanta la vigilancia.
const (
	AlertaTokenVencido     = "token_vencido"
	AlertaPedidoSinMapear  = "pedido_sin_mapear"
	AlertaPedidoFallido    = "pedido_fallido"
	AlertaPublicacionErr   = "publicacion_con_error"
	AlertaTrabajosFallidos = "trabajos_fallidos"
	AlertaSinStock         = "sin_stock_publicado"
)

type Planificador struct {
	st   *store.Store
	cola *jobs.Cola
	log  *slog.Logger
	tick time.Duration
	// sincronizar dispara la lectura de Odoo. Se inyecta para no depender
	// aquí del motor de sincronización.
	sincronizar func(context.Context) (string, error)
	// notificar saca las alertas hacia donde alguien las vea. Nulo = no hay
	// salida configurada y las alertas se quedan solo en el panel.
	notificar func(context.Context) error
}

// ConNotificaciones engancha la salida de alertas.
//
// Va aparte del constructor para que el planificador no dependa del paquete
// de notificaciones: aquí solo se sabe que hay algo que despachar avisos.
func (p *Planificador) ConNotificaciones(f func(context.Context) error) *Planificador {
	p.notificar = f
	return p
}

func Nuevo(st *store.Store, cola *jobs.Cola, log *slog.Logger, tick time.Duration,
	sincronizar func(context.Context) (string, error)) *Planificador {
	if tick < time.Second {
		tick = time.Minute
	}
	return &Planificador{st: st, cola: cola, log: log, tick: tick, sincronizar: sincronizar}
}

// Ejecutar corre hasta que se cancele el contexto.
func (p *Planificador) Ejecutar(ctx context.Context) error {
	p.log.Info("planificador en marcha", "tick", p.tick.String())
	t := time.NewTicker(p.tick)
	defer t.Stop()

	// Una pasada al arrancar: si el proceso estuvo caído durante una tarea
	// programada, se ejecuta al volver en vez de esperar al día siguiente.
	p.pasada(ctx)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("planificador detenido")
			return nil
		case <-t.C:
			p.pasada(ctx)
		}
	}
}

func (p *Planificador) pasada(ctx context.Context) {
	if err := p.dispararVencidos(ctx); err != nil {
		p.log.Error("disparando horarios", "error", err)
	}
	if err := p.moverPromociones(ctx); err != nil {
		p.log.Error("aplicando promociones", "error", err)
	}
	if err := p.Vigilar(ctx); err != nil {
		p.log.Error("vigilancia", "error", err)
	}
	// Después de vigilar, para que los avisos que acaba de levantar salgan en
	// esta misma pasada y no en la siguiente.
	if p.notificar != nil {
		if err := p.notificar(ctx); err != nil {
			p.log.Error("enviando notificaciones", "error", err)
		}
	}
}

// dispararVencidos ejecuta las tareas cuyo momento llegó.
//
// La marca se avanza ANTES de encolar: si algo falla al encolar, la tarea no
// se repite en bucle en cada tick, que sería mucho peor que perder una corrida.
func (p *Planificador) dispararVencidos(ctx context.Context) error {
	vencidos, err := p.st.HorariosVencidos(ctx)
	if err != nil {
		return err
	}
	for _, h := range vencidos {
		if err := p.st.MarcarHorarioEjecutado(ctx, h); err != nil {
			return err
		}
		p.log.Info("horario disparado", "nombre", h.Nombre, "alcance", h.Alcance)

		if err := p.ejecutar(ctx, h); err != nil {
			p.log.Error("ejecutando horario", "nombre", h.Nombre, "error", err)
			_ = p.st.CrearAlerta(ctx, "horario_fallido", "error", h.CuentaID,
				fmt.Sprintf("el horario %q falló: %v", h.Nombre, err), nil)
		}
	}
	return nil
}

// moverPromociones empuja al canal las promociones que acaban de empezar y
// deshace las que acaban de terminar.
//
// El recálculo va antes de planificar porque la publicación lee los precios
// efectivos ya guardados: al revés, el envío saldría con el precio anterior y
// la rebaja llegaría una pasada tarde.
func (p *Planificador) moverPromociones(ctx context.Context) error {
	cambios, err := p.st.PromocionesPendientes(ctx)
	if err != nil {
		return err
	}
	for _, c := range cambios {
		if _, err := p.st.RecalcularPreciosCuenta(ctx, c.CuentaID); err != nil {
			return fmt.Errorf("recalculando precios de la cuenta %d: %w", c.CuentaID, err)
		}
		if _, err := publicar.Planificar(ctx, p.st, p.cola, c.CuentaID); err != nil {
			return fmt.Errorf("planificando envío de la cuenta %d: %w", c.CuentaID, err)
		}
		if err := p.st.MarcarPromocionesProcesadas(ctx, c.Aplicar, c.Revertir); err != nil {
			return err
		}
		p.log.Info("promociones movidas", "cuenta", c.CuentaID,
			"aplicadas", len(c.Aplicar), "revertidas", len(c.Revertir))
	}
	return nil
}

func (p *Planificador) ejecutar(ctx context.Context, h store.Horario) error {
	switch h.Alcance {
	case "full":
		// Sincronizar con Odoo y, a continuación, planificar los envíos de
		// todas las cuentas: el catálogo nuevo no sirve si no sale al canal.
		if p.sincronizar != nil {
			if _, err := p.sincronizar(ctx); err != nil {
				return err
			}
		}
		return p.planificarTodas(ctx, h)
	case "price", "stock":
		// El motor decide por hash qué cambió, así que ambos alcances usan el
		// mismo camino: planificar y dejar que el diff filtre.
		return p.planificarTodas(ctx, h)
	default:
		return fmt.Errorf("alcance desconocido: %s", h.Alcance)
	}
}

func (p *Planificador) planificarTodas(ctx context.Context, h store.Horario) error {
	cuentas, err := p.st.ListarCuentas(ctx)
	if err != nil {
		return err
	}
	for _, c := range cuentas {
		// Si el horario apunta a una cuenta concreta, solo esa.
		if h.CuentaID != nil && *h.CuentaID != c.ID {
			continue
		}
		if _, err := publicar.Planificar(ctx, p.st, p.cola, c.ID); err != nil {
			return err
		}
		// Los pedidos se traen en la misma pasada: es lo más urgente y
		// aprovecha que ya se está hablando con el canal.
		if err := ordenes.EncolarIngesta(ctx, p.cola, c.ID); err != nil {
			return err
		}
	}
	// Antes de encolar montajes, se rescata lo que falló por un SKU que
	// todavía no estaba sincronizado: si el catálogo ya lo tiene, la línea se
	// empareja y el pedido vuelve a la cola en vez de quedarse perdido.
	if lin, ped, err := p.st.ReemparejarLineasHuerfanas(ctx); err != nil {
		return err
	} else if lin > 0 || ped > 0 {
		p.log.Info("pedidos rescatados al aparecer su SKU en el catálogo",
			"lineas_emparejadas", lin, "pedidos_reactivados", ped)
	}

	// Red de seguridad para el montaje en Odoo: la ingesta encola cada pedido
	// nuevo, pero un pedido guardado antes de que existiera ese encolado, o
	// uno cuyo trabajo se perdió, se quedaría en 'received' para siempre. Esta
	// pasada los recupera; la clave única del trabajo evita duplicarlos.
	pendientes, err := p.st.OrdenesPendientesOdoo(ctx, 200)
	if err != nil {
		return err
	}
	for _, o := range pendientes {
		if err := ordenes.EncolarMontaje(ctx, p.cola, o.CuentaID, o.ID); err != nil {
			return err
		}
	}
	return nil
}

// Vigilar levanta alertas de lo que se rompió sin que nadie mire.
//
// Todas se deduplican mientras sigan sin reconocerse, así que un problema
// persistente no llena la bandeja.
func (p *Planificador) Vigilar(ctx context.Context) error {
	// Pedidos que no se pudieron montar en Odoo.
	res, err := p.st.ResumenOrdenes(ctx)
	if err != nil {
		return err
	}
	if res.Fallidos > 0 {
		_ = p.st.CrearAlerta(ctx, AlertaPedidoFallido, "error", nil,
			fmt.Sprintf("%d pedidos no se pudieron crear en Odoo", res.Fallidos), nil)
	}
	if res.SinMapear > 0 {
		_ = p.st.CrearAlerta(ctx, AlertaPedidoSinMapear, "warning", nil,
			fmt.Sprintf("%d líneas de pedido traen un SKU que no existe en el catálogo", res.SinMapear), nil)
	}

	// Cuentas cuya última prueba de conexión falló: casi siempre es el token.
	cuentas, err := p.st.ListarCuentas(ctx)
	if err != nil {
		return err
	}
	for _, c := range cuentas {
		if c.ProbadaOK != nil && !*c.ProbadaOK {
			id := c.ID
			_ = p.st.CrearAlerta(ctx, AlertaTokenVencido, "critical", &id,
				fmt.Sprintf("la conexión con %s no funciona: %s", c.CanalNombre, c.ProbadaMsg), nil)
		}
	}

	// Publicaciones que el canal rechazó.
	pubs, err := p.st.ResumenPublicaciones(ctx)
	if err != nil {
		return err
	}
	for _, pu := range pubs {
		if pu.ConError > 0 {
			id := pu.CuentaID
			_ = p.st.CrearAlerta(ctx, AlertaPublicacionErr, "error", &id,
				fmt.Sprintf("%d publicaciones con error en %s", pu.ConError, pu.Canal), nil)
		}
	}

	// Trabajos agotados: se quedaron sin reintentos y nadie los verá si no
	// se avisa.
	if n, err := p.st.TrabajosFallidos(ctx); err == nil && n > 0 {
		_ = p.st.CrearAlerta(ctx, AlertaTrabajosFallidos, "error", nil,
			fmt.Sprintf("%d trabajos agotaron sus reintentos", n), nil)
	}

	// Productos publicados que se quedaron sin stock: siguen a la venta y
	// generan una venta que no se puede despachar.
	if n, err := p.st.PublicadosSinStock(ctx); err == nil && n > 0 {
		_ = p.st.CrearAlerta(ctx, AlertaSinStock, "warning", nil,
			fmt.Sprintf("%d productos publicados se quedaron sin existencias", n), nil)
	}
	return nil
}
