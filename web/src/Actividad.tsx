import { useCallback, useEffect, useRef, useState } from 'react'
import { api, fecha, num, type Actividad as Datos, type Evento, type TareaEnCurso } from './api'

// Nombres de los tipos de trabajo. Los internos —publicar_producto,
// confirmar_despacho— son claves del dominio y no cambian; aquí se traducen
// porque quien mira esta pantalla no tiene por qué conocerlas.
const TAREAS: Record<string, string> = {
  publicar_producto: 'Publicar fichas',
  actualizar_precio: 'Enviar precios',
  actualizar_stock: 'Enviar stock',
  pausar_publicacion: 'Retirar de la venta',
  reanudar_publicacion: 'Poner a la venta',
  conciliar_publicaciones: 'Contrastar con el canal',
  verificar_feed: 'Confirmar feeds de Falabella',
  ingerir_ordenes: 'Traer pedidos',
  orden_a_odoo: 'Montar pedidos en Odoo',
  confirmar_despacho: 'Avisar despachos al canal',
}

const CLASES: Record<string, string> = {
  persona: 'Alguien',
  trabajo: 'Falló',
  aviso: 'Aviso',
}

// Refresco mientras hay algo corriendo. Cinco segundos es suficiente para que
// parezca vivo sin convertir la pantalla abierta en una consulta por segundo
// contra la base.
const REFRESCO_MS = 5000

export function Actividad() {
  const [datos, setDatos] = useState<Datos | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [cargando, setCargando] = useState(true)
  const temporizador = useRef<number | null>(null)

  const cargar = useCallback(() => {
    api.actividad()
      .then((d) => { setDatos(d); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])

  useEffect(() => {
    cargar()
    return () => { if (temporizador.current) window.clearTimeout(temporizador.current) }
  }, [cargar])

  // Solo se repite mientras queda algo en marcha: con la cola vacía no hay
  // nada que mirar y seguir preguntando sería gasto sin motivo.
  useEffect(() => {
    if (!datos || datos.activas === 0) return
    temporizador.current = window.setTimeout(cargar, REFRESCO_MS)
    return () => { if (temporizador.current) window.clearTimeout(temporizador.current) }
  }, [datos, cargar])

  async function gobernar(accion: 'cancelar' | 'reintentar', t: TareaEnCurso) {
    const nombre = (TAREAS[t.tipo] ?? t.tipo).toLowerCase()
    if (accion === 'cancelar' && !window.confirm(
      `Se retirarán de la cola ${num(t.pendientes)} trabajos de «${nombre}» que aún no han empezado. Lo que ya está enviándose al canal no se detiene. ¿Continuar?`)) return
    setError(null)
    try {
      await api.gobernarCola(accion, t.tipo)
      cargar()
    } catch (e) {
      setError(`No se pudo ${accion}: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const cola = (datos?.cola ?? []).filter((t) => t.corriendo + t.pendientes + t.fallidos > 0)
  const hechas = (datos?.cola ?? []).filter((t) => t.corriendo + t.pendientes + t.fallidos === 0 && t.hechos > 0)

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Actividad</h1>
          <div className="sub">Qué está haciendo Integra ahora y qué se hizo antes</div>
        </div>
        <button onClick={cargar} disabled={cargando} data-guia="act-actualizar">Actualizar</button>
      </header>

      {error && <div className="aviso-caja">No se pudo leer la actividad: {error}</div>}

      <section className="panel" data-guia="actividad">
        <h2>En marcha</h2>
        <div className="cuerpo">
          {cargando && !datos && <div className="vacio">Cargando…</div>}

          {datos && datos.activas === 0 && cola.length === 0 && (
            <div className="vacio">
              Nada en cola. Todo lo que se pidió ya se envió.
            </div>
          )}

          {cola.map((t) => <FilaTarea key={t.tipo} t={t} onGobernar={gobernar} />)}

          {/* Lo que ya terminó hoy se resume en una línea: son cientos de
              trabajos idénticos y enumerarlos entierra lo que hay que leer. */}
          {hechas.length > 0 && (
            <div className="tenue mini-texto" style={{ marginTop: 10 }}>
              Hoy además: {hechas.map((t) => `${num(t.hechos)} ${(TAREAS[t.tipo] ?? t.tipo).toLowerCase()}`).join(' · ')}.
            </div>
          )}
        </div>
      </section>

      <div className="nota-previa" data-guia="act-nota-proceso">
        Los trabajos los procesa el <strong>worker</strong>. Si ves cosas encoladas que
        no avanzan, ese proceso está caído: arráncalo con <code>integra worker</code>.
        Integra lo vigila y abre un aviso cuando lleva más de tres minutos sin dar
        señales.
      </div>

      <section className="panel" data-guia="act-historia">
        <h2>Lo que ha pasado</h2>
        <div className="cuerpo">
          {datos && datos.historia.length === 0 && <div className="vacio">Todavía no hay nada anotado.</div>}
          {(datos?.historia ?? []).map((e, i) => <FilaEvento key={`${e.cuando}-${i}`} e={e} />)}
        </div>
      </section>

      <div className="nota-previa" data-guia="act-nota-historia">
        La línea de tiempo recoge lo que hizo una persona, lo que falló y los avisos que
        se abrieron. Los envíos que salieron bien <strong>no</strong> se listan uno a uno:
        una publicación de catálogo son cientos de líneas idénticas que enterrarían lo
        único que hay que leer.
      </div>
    </>
  )
}

function FilaTarea({ t, onGobernar }: {
  t: TareaEnCurso
  onGobernar: (accion: 'cancelar' | 'reintentar', t: TareaEnCurso) => void
}) {
  const vivos = t.corriendo + t.pendientes
  const total = vivos + t.hechos
  // El avance se mide contra lo hecho hoy más lo que queda: es una estimación
  // honesta, no una barra que llega al 100 % y se queda ahí.
  const pct = total > 0 ? Math.round((t.hechos / total) * 100) : 0

  return (
    <div className="fila-cuenta fila-apilable" data-guia="act-tarea">
      <div className="expande-recorta">
        <div className="fila">
          <strong className="expande">{TAREAS[t.tipo] ?? t.tipo}</strong>
          {t.corriendo > 0 && <span className="pastilla ok">{num(t.corriendo)} corriendo</span>}
          {t.pendientes > 0 && <span className="pastilla aviso">{num(t.pendientes)} en cola</span>}
          {t.fallidos > 0 && <span className="pastilla bloqueante">{num(t.fallidos)} fallidos</span>}
        </div>

        {vivos > 0 && (
          <div className="barra-avance" title={`${t.hechos} de ${total} hoy`}>
            <div className="barra-avance-relleno" style={{ width: `${pct}%` }} />
          </div>
        )}

        <div className="tenue mini-texto">
          {t.hechos > 0 && `${num(t.hechos)} completados hoy`}
          {t.desde && vivos > 0 && ` · lo más antiguo espera desde ${fecha(t.desde)}`}
        </div>

        {/* El último error se enseña aquí para no tener que ir a buscarlo:
            «5 fallidos» sin el motivo obliga a abrir la base de datos. */}
        {t.fallidos > 0 && t.ultimo_error && (
          <div className="mini-texto error">Último fallo: {t.ultimo_error}</div>
        )}
      </div>

      <div className="grupo-acciones" data-guia="act-acciones">
        {t.pendientes > 0 && (
          <button onClick={() => onGobernar('cancelar', t)}
            title="Retira de la cola lo que aún no ha empezado">
            Cancelar {num(t.pendientes)}
          </button>
        )}
        {t.fallidos > 0 && (
          <button onClick={() => onGobernar('reintentar', t)}
            title="Devuelve a la cola lo que agotó sus intentos">
            Reintentar {num(t.fallidos)}
          </button>
        )}
      </div>
    </div>
  )
}

function FilaEvento({ e }: { e: Evento }) {
  return (
    <div className="fila-evento" data-guia="act-evento">
      <span className={`pastilla ${e.malo ? 'bloqueante' : e.clase === 'persona' ? 'dudosa' : 'aviso'}`}>
        {CLASES[e.clase] ?? e.clase}
      </span>
      <div className="expande-recorta">
        <div>{TAREAS[e.titulo] ?? e.titulo}</div>
        {e.detalle && <div className="tenue mini-texto">{e.detalle}</div>}
      </div>
      <span className="tenue mini-texto no-encoge">
        {e.quien && `${e.quien} · `}{fecha(e.cuando)}
      </span>
    </div>
  )
}
