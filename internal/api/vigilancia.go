package api

// Vigilancia de los procesos que no pueden avisar de su propia muerte.
//
// Todo lo que Integra vigila lo vigila el planificador, y el planificador corre
// dentro del worker. Cuando el que se para es el worker, no queda nadie ahí
// dentro para contarlo: durante días no se sincroniza Odoo, no sale un precio y
// no entra un solo pedido de los cuatro canales —se sigue vendiendo con el
// stock congelado del último sync— mientras el panel se ve en verde. Por eso
// esta vigilancia vive en la API, que es el otro proceso, y despacha ella misma
// el correo en vez de dejárselo al que está muerto.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mdv/integra/internal/store"
)

// AlertaProcesoMudo es el tipo de alerta de un proceso que dejó de latir.
const AlertaProcesoMudo = "proceso_sin_latido"

// intervaloVigilancia es cada cuánto la API mira los latidos. Un minuto sobra:
// la tolerancia de cada latido se mide en varios periodos suyos.
const intervaloVigilancia = time.Minute

// consecuencias traduce el nombre técnico del proceso a lo que deja de ocurrir
// cuando se para. El correo lo lee quien decide, no quien despliega.
var consecuencias = map[string]string{
	store.ComponenteWorker: "no se publican precios ni stock y no entra ningún pedido de los canales",
	store.ComponenteCopias: "no se está copiando ni la base ni el banco de fotos",
}

// vigilado es lo que la vigilancia necesita de la persistencia. Se declara como
// interfaz para poder probar el aviso sin base de datos.
type vigilado interface {
	LatidosAtrasados(ctx context.Context) ([]store.Latido, error)
	CrearAlerta(ctx context.Context, tipo, severidad string, cuentaID *int64, mensaje string, detalle any) error
}

// ConVigilanciaDeProcesos engancha la salida de avisos de esta vigilancia.
//
// Va aparte del constructor, que ya tiene siete parámetros, y porque sin ella
// la vigilancia sigue sirviendo: levanta la alerta en el panel y responde en
// /estado, solo que sin correo.
func (s *Server) ConVigilanciaDeProcesos(notificar func(context.Context) error) *Server {
	s.notificar = notificar
	return s
}

// vigilarEnBucle corre mientras viva el servidor.
func (s *Server) vigilarEnBucle(ctx context.Context) {
	t := time.NewTicker(intervaloVigilancia)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := vigilarProcesos(ctx, s.st, s.log, s.notificar); err != nil {
				s.log.Error("vigilando los latidos", "error", err)
			}
		}
	}
}

// vigilarProcesos levanta la alerta de cada proceso que dejó de latir y saca
// los avisos que haya pendientes.
func vigilarProcesos(ctx context.Context, st vigilado, log *slog.Logger, notificar func(context.Context) error) error {
	atrasados, err := st.LatidosAtrasados(ctx)
	if err != nil {
		return err
	}
	if len(atrasados) == 0 {
		return nil
	}

	for _, l := range atrasados {
		// El mensaje no dice cuánto lleva muerto a propósito: las alertas se
		// deduplican por texto mientras nadie las reconozca, y un mensaje que
		// cambia cada minuto abriría una alerta por minuto hasta llenar la
		// bandeja y el correo.
		mensaje := fmt.Sprintf("el proceso %q lleva más de %s sin dar señales de vida",
			l.Componente, plazo(l.Ventana()))
		if c := consecuencias[l.Componente]; c != "" {
			mensaje += ": " + c
		}
		log.Error("proceso sin latido", "componente", l.Componente, "ultimo", l.Visto)
		if err := st.CrearAlerta(ctx, AlertaProcesoMudo, "critical", nil, mensaje,
			map[string]any{"componente": l.Componente, "ultimo_latido": l.Visto}); err != nil {
			return err
		}
	}

	// El despacho se hace aquí, y no en el planificador como el resto de los
	// avisos, porque el planificador es justo lo que puede estar muerto. Solo
	// se llama cuando hay algo atrasado: mientras todo late, el que despacha
	// sigue siendo el planificador y no hay dos procesos mandando correo.
	if notificar != nil {
		return notificar(ctx)
	}
	return nil
}

// plazo pone en palabras la ventana tolerada. Va en el asunto de un correo que
// lee una persona, donde "72h0m0s" no dice nada.
func plazo(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d días", int(d.Hours())/24)
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d horas", int(d.Hours()))
	default:
		return fmt.Sprintf("%d minutos", int(d.Minutes()))
	}
}

// estado dice si Integra está haciendo su trabajo, no solo si la API responde.
//
// /healthz sigue siendo la sonda del orquestador y contesta ok mientras el
// proceso esté en pie. Esta ruta es para el servicio de uptime: devuelve 503
// en cuanto un proceso deja de latir, y eso hace sonar un teléfono desde fuera
// de la máquina, sin depender de que el proceso muerto mande un correo.
func (s *Server) estado(w http.ResponseWriter, r *http.Request) {
	latidos, err := s.st.Latidos(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	codigo, estado := http.StatusOK, "ok"
	for _, l := range latidos {
		if l.Atrasado {
			codigo, estado = http.StatusServiceUnavailable, "degradado"
			break
		}
	}
	if latidos == nil {
		latidos = []store.Latido{}
	}
	escribir(w, codigo, map[string]any{"estado": estado, "procesos": latidos})
}
