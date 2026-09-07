import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode,
} from 'react'
import { api } from '../api'
import type { Notificacion, Notificaciones } from './tipos'
import { useSesion } from './sesion'
import { useSistema } from './sistema'

// Centro de notificaciones del escritorio. El proveedor sondea la API cada
// 20 s y convierte en notificaciones lo que ha cambiado desde la última vez:
// avisos abiertos nuevos, pedidos nuevos, pedidos fallidos y la cola que se
// vacía. Lo ya visto se guarda por usuario para no repetir al recargar.
// No se usa `Notification` del navegador: ni permisos ni sorpresas.

const INTERVALO_MS = 20_000
const MAXIMO = 100
const EMERGENTE_MS = 6_000
const MAX_EMERGENTES = 3

type Guardado = {
  vistos: string[]        // ids de origen ya notificados (aviso:<id>)
  lista: Notificacion[]
  pedidos?: { total: number; fallidos: number }
  activas?: number
}

function claveDe(usuarioId: number) { return `integra.escritorio.notif.${usuarioId}` }

function cargar(usuarioId: number): Guardado | null {
  try {
    const crudo = localStorage.getItem(claveDe(usuarioId))
    if (!crudo) return null
    const g = JSON.parse(crudo) as Partial<Guardado>
    return {
      vistos: Array.isArray(g.vistos) ? g.vistos : [],
      lista: Array.isArray(g.lista) ? g.lista : [],
      pedidos: g.pedidos,
      activas: g.activas,
    }
  } catch {
    return null
  }
}

function nuevoId() {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

// Hora relativa en castellano: «ahora», «hace 5 min», «hace 2 h», «ayer»…
export function horaRelativa(iso: string, ahora = Date.now()): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((ahora - t) / 1000))
  if (s < 45) return 'ahora'
  const m = Math.round(s / 60)
  if (m < 60) return `hace ${m} min`
  const h = Math.round(m / 60)
  if (h < 24) return `hace ${h} h`
  const d = Math.round(h / 24)
  if (d === 1) return 'ayer'
  if (d < 7) return `hace ${d} días`
  return new Date(iso).toLocaleDateString('es', { day: 'numeric', month: 'short' })
}

function esHoy(iso: string): boolean {
  const d = new Date(iso), h = new Date()
  return d.getFullYear() === h.getFullYear() && d.getMonth() === h.getMonth() && d.getDate() === h.getDate()
}

// Contexto público (el contrato) y otro interno con las emergentes, que solo
// necesita el componente <Emergentes/>.
const Ctx = createContext<Notificaciones | null>(null)
type Emergente = { n: Notificacion; hasta: number }
// El contrato solo sabe marcar todas como leídas; marcar una sola (al pulsarla)
// es cosa de estos componentes y va por el contexto interno.
type Interno = { emergentes: Emergente[]; cerrar: (id: string) => void; marcarUna: (id: string) => void }
const CtxInterno = createContext<Interno | null>(null)

export function ProveedorNotificaciones({ children }: { children: ReactNode }) {
  const { usuario } = useSesion()
  // Se lee localStorage una sola vez (useState perezoso), no en cada render.
  const [inicial] = useState(() => cargar(usuario.id))
  const [lista, setLista] = useState<Notificacion[]>(() => inicial?.lista ?? [])
  const [emergentes, setEmergentes] = useState<Emergente[]>([])

  // Memoria del sondeo. Vive en refs porque no pinta nada: cambia cada 20 s y
  // no queremos re-renderizar todo el escritorio por ello.
  const vistos = useRef<Set<string>>(new Set(inicial?.vistos ?? []))
  const pedidosPrev = useRef<{ total: number; fallidos: number } | null>(inicial?.pedidos ?? null)
  const activasPrev = useRef<number | null>(inicial?.activas ?? null)
  // Verdadero hasta que se guarda algo por primera vez para este usuario: en
  // ese primer sondeo se aprende el estado actual sin avisar, para no soltar
  // de golpe todos los avisos que ya llevaban días abiertos.
  const estrenando = useRef<boolean>(inicial === null)
  const listaRef = useRef(lista)
  listaRef.current = lista

  const guardar = useCallback((l: Notificacion[]) => {
    const g: Guardado = {
      vistos: Array.from(vistos.current).slice(-MAXIMO * 3),
      lista: l.slice(0, MAXIMO),
      pedidos: pedidosPrev.current ?? undefined,
      activas: activasPrev.current ?? undefined,
    }
    try { localStorage.setItem(claveDe(usuario.id), JSON.stringify(g)) } catch { /* sin espacio: se olvida al recargar */ }
  }, [usuario.id])

  const cerrarEmergente = useCallback((id: string) => {
    setEmergentes(es => es.filter(e => e.n.id !== id))
  }, [])

  const notificar = useCallback<Notificaciones['notificar']>((n, emergente = true) => {
    const completa: Notificacion = { ...n, id: nuevoId(), cuando: new Date().toISOString(), leida: false }
    setLista(l => {
      const nueva = [completa, ...l].slice(0, MAXIMO)
      guardar(nueva)
      return nueva
    })
    if (emergente) {
      // Se apilan como mucho tres: la más antigua cede el sitio.
      setEmergentes(es => [...es, { n: completa, hasta: Date.now() + EMERGENTE_MS }].slice(-MAX_EMERGENTES))
    }
  }, [guardar])

  // Caducidad de las emergentes: un solo temporizador que se reprograma para
  // la más próxima a vencer.
  useEffect(() => {
    if (!emergentes.length) return
    const proxima = Math.min(...emergentes.map(e => e.hasta))
    const t = window.setTimeout(() => {
      const ahora = Date.now()
      setEmergentes(es => es.filter(e => e.hasta > ahora))
    }, Math.max(0, proxima - Date.now()))
    return () => window.clearTimeout(t)
  }, [emergentes])

  // Sondeo. Cada fuente se compara con lo recordado y solo lo nuevo se
  // convierte en notificación.
  useEffect(() => {
    let vivo = true
    const sondear = async () => {
      const [alertas, pedidos, actividad] = await Promise.allSettled([
        api.alertas(), api.resumenOrdenes(), api.actividad(1),
      ])
      if (!vivo) return
      const primera = estrenando.current
      estrenando.current = false

      // Avisos abiertos: los que tienen un id que no hemos visto.
      if (alertas.status === 'fulfilled') {
        for (const a of alertas.value) {
          const clave = `aviso:${a.id}`
          if (vistos.current.has(clave)) continue
          vistos.current.add(clave)
          if (primera) continue
          notificar({
            tipo: 'aviso',
            severidad: a.severidad,
            titulo: a.canal ? `Aviso en ${a.canal}` : 'Aviso',
            texto: a.mensaje,
            app: 'automatizacion',
          })
        }
      }

      // Pedidos: el total crece con cada pedido que entra, y los fallidos con
      // cada uno que no llega a Odoo. Se usa el total y no `recibidos` porque
      // ese contador baja cuando los pedidos avanzan de estado.
      if (pedidos.status === 'fulfilled') {
        const r = pedidos.value
        const prev = pedidosPrev.current
        if (prev && !primera) {
          const nuevos = r.total - prev.total
          if (nuevos > 0) {
            notificar({
              tipo: 'pedido', severidad: 'info', app: 'pedidos',
              titulo: nuevos === 1 ? '1 pedido nuevo' : `${nuevos} pedidos nuevos`,
              texto: r.recibidos > 0 ? `${r.recibidos} pendientes de pasar a Odoo` : undefined,
            })
          }
          const fallos = r.fallidos - prev.fallidos
          if (fallos > 0) {
            notificar({
              tipo: 'pedido', severidad: 'error', app: 'pedidos',
              titulo: fallos === 1 ? 'Un pedido no llegó a Odoo' : `${fallos} pedidos no llegaron a Odoo`,
              texto: `${r.fallidos} fallidos en total`,
            })
          }
        }
        pedidosPrev.current = { total: r.total, fallidos: r.fallidos }
      }

      // Cola de trabajo: de «algo en marcha» a «nada» es que terminó.
      if (actividad.status === 'fulfilled') {
        const activas = actividad.value.activas
        const prev = activasPrev.current
        if (prev !== null && prev > 0 && activas === 0 && !primera) {
          notificar({
            tipo: 'tarea', severidad: 'info', app: 'actividad',
            titulo: 'Terminó lo que había en cola',
            texto: 'No queda ningún trabajo en marcha.',
          })
        }
        activasPrev.current = activas
      }

      guardar(listaRef.current)
    }
    sondear()
    const t = window.setInterval(sondear, INTERVALO_MS)
    return () => { vivo = false; window.clearInterval(t) }
  }, [notificar, guardar])

  const marcarLeidas = useCallback(() => {
    setLista(l => {
      if (l.every(n => n.leida)) return l
      const nueva = l.map(n => n.leida ? n : { ...n, leida: true })
      guardar(nueva)
      return nueva
    })
  }, [guardar])

  const quitar = useCallback((id: string) => {
    setLista(l => { const nueva = l.filter(n => n.id !== id); guardar(nueva); return nueva })
    cerrarEmergente(id)
  }, [guardar, cerrarEmergente])

  const vaciar = useCallback(() => {
    setLista(() => { guardar([]); return [] })
    setEmergentes([])
  }, [guardar])

  const marcarUna = useCallback((id: string) => {
    setLista(l => {
      const i = l.findIndex(n => n.id === id && !n.leida)
      if (i < 0) return l
      const nueva = l.map(n => n.id === id ? { ...n, leida: true } : n)
      guardar(nueva)
      return nueva
    })
  }, [guardar])

  const valor = useMemo<Notificaciones>(() => ({
    lista,
    noLeidas: lista.filter(n => !n.leida).length,
    marcarLeidas, quitar, vaciar, notificar,
  }), [lista, marcarLeidas, quitar, vaciar, notificar])

  const interno = useMemo<Interno>(() => ({ emergentes, cerrar: cerrarEmergente, marcarUna }), [emergentes, cerrarEmergente, marcarUna])

  return (
    <Ctx.Provider value={valor}>
      <CtxInterno.Provider value={interno}>{children}</CtxInterno.Provider>
    </Ctx.Provider>
  )
}

export function useNotificaciones(): Notificaciones {
  const c = useContext(Ctx)
  if (!c) throw new Error('useNotificaciones fuera de ProveedorNotificaciones')
  return c
}

// ------------------------------------------------------------- iconos

function Icono({ tipo }: { tipo: Notificacion['tipo'] }) {
  const comun = { width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
  switch (tipo) {
    case 'aviso':
      return <svg {...comun}><path d="M12 3 2 20h20L12 3z" /><path d="M12 10v4M12 17.5v.5" /></svg>
    case 'pedido':
      return <svg {...comun}><path d="M6 6h15l-1.5 8h-12z" /><path d="M6 6 5 3H2" /><circle cx="9" cy="19" r="1.5" /><circle cx="17" cy="19" r="1.5" /></svg>
    case 'tarea':
      return <svg {...comun}><circle cx="12" cy="12" r="9" /><path d="m8.5 12.5 2.5 2.5 4.5-5" /></svg>
    default:
      return <svg {...comun}><circle cx="12" cy="12" r="9" /><path d="M12 8v.5M12 11v5" /></svg>
  }
}

function useInterno(): Interno {
  const c = useContext(CtxInterno)
  if (!c) throw new Error('Notificaciones fuera de ProveedorNotificaciones')
  return c
}

// Al pulsar una notificación: se marca leída y se abre su app. Compartido por
// el centro y las emergentes.
function useAbrirNotificacion() {
  const sistema = useSistema()
  const { marcarUna } = useInterno()
  return useCallback((n: Notificacion) => {
    marcarUna(n.id)
    if (n.app) sistema.abrir(n.app)
  }, [sistema, marcarUna])
}

// ------------------------------------------------- centro de notificaciones

export function CentroNotificaciones({ abierto, onCerrar }: { abierto: boolean; onCerrar: () => void }) {
  const { lista, noLeidas, marcarLeidas, quitar, vaciar } = useNotificaciones()
  const abrir = useAbrirNotificacion()
  const [, tic] = useState(0)

  // Las horas relativas envejecen: se repinta cada minuto mientras está abierto.
  useEffect(() => {
    if (!abierto) return
    const t = window.setInterval(() => tic(x => x + 1), 60_000)
    return () => window.clearInterval(t)
  }, [abierto])

  // Escape cierra el panel, como cualquier menú del escritorio.
  useEffect(() => {
    if (!abierto) return
    const alTeclear = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', alTeclear)
    return () => window.removeEventListener('keydown', alTeclear)
  }, [abierto, onCerrar])

  const hoy = lista.filter(n => esHoy(n.cuando))
  const antes = lista.filter(n => !esHoy(n.cuando))

  const pulsar = (n: Notificacion) => { abrir(n); onCerrar() }
  // Al cerrar el panel todo lo que se ha tenido delante queda leído: es lo
  // que se espera de un centro de notificaciones y evita insignias eternas.
  const cerrar = () => { marcarLeidas(); onCerrar() }

  const grupo = (titulo: string, ns: Notificacion[]) => ns.length === 0 ? null : (
    <section className="notif-grupo" key={titulo}>
      <h3>{titulo}</h3>
      {ns.map(n => (
        <article
          key={n.id}
          className={`notif sev-${n.severidad}${n.leida ? '' : ' no-leida'}`}
          onClick={() => pulsar(n)}
          role="button"
          tabIndex={0}
          onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pulsar(n) } }}
        >
          <span className="notif-icono"><Icono tipo={n.tipo} /></span>
          <div className="notif-cuerpo">
            <div className="notif-titulo">{n.titulo}</div>
            {n.texto && <div className="notif-texto">{n.texto}</div>}
            <div className="notif-hora">{horaRelativa(n.cuando)}</div>
          </div>
          <button
            type="button" className="notif-quitar" aria-label="Quitar"
            onClick={e => { e.stopPropagation(); quitar(n.id) }}
          >✕</button>
        </article>
      ))}
    </section>
  )

  return (
    <>
      {abierto && <div className="centro-notif-velo" onClick={cerrar} />}
      <aside className={`centro-notif${abierto ? ' abierto' : ''}`} aria-hidden={!abierto} aria-label="Notificaciones">
        <header className="centro-notif-cabecera">
          <h2>Notificaciones{noLeidas > 0 && <span className="centro-notif-cuenta">{noLeidas}</span>}</h2>
          <div className="centro-notif-acciones">
            <button type="button" className="enlace" onClick={marcarLeidas} disabled={noLeidas === 0}>Marcar leídas</button>
            <button type="button" className="enlace" onClick={vaciar} disabled={lista.length === 0}>Vaciar</button>
            <button type="button" className="centro-notif-cerrar" aria-label="Cerrar" onClick={cerrar}>✕</button>
          </div>
        </header>
        <div className="centro-notif-lista">
          {lista.length === 0 && (
            <p className="centro-notif-vacio">Nada nuevo. Aquí aparecerán avisos, pedidos y trabajos terminados.</p>
          )}
          {grupo('Hoy', hoy)}
          {grupo('Antes', antes)}
        </div>
      </aside>
    </>
  )
}

// ------------------------------------------------------------ emergentes

export function Emergentes() {
  const { emergentes, cerrar } = useInterno()
  const abrir = useAbrirNotificacion()
  if (emergentes.length === 0) return null
  return (
    <div className="emergentes" aria-live="polite">
      {emergentes.map(({ n }) => (
        <div
          key={n.id}
          className={`emergente sev-${n.severidad}`}
          role="button"
          tabIndex={0}
          onClick={() => { cerrar(n.id); abrir(n) }}
          onKeyDown={e => { if (e.key === 'Enter') { cerrar(n.id); abrir(n) } }}
        >
          <span className="notif-icono"><Icono tipo={n.tipo} /></span>
          <div className="notif-cuerpo">
            <div className="notif-titulo">{n.titulo}</div>
            {n.texto && <div className="notif-texto">{n.texto}</div>}
          </div>
          <button
            type="button" className="notif-quitar" aria-label="Cerrar"
            onClick={e => { e.stopPropagation(); cerrar(n.id) }}
          >✕</button>
        </div>
      ))}
    </div>
  )
}
