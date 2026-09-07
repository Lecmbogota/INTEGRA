import {
  useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState,
  type CSSProperties, type KeyboardEvent as KeyboardEventReact,
  type MouseEvent as MouseEventReact, type PointerEvent as PointerEventReact,
  type ReactNode, type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import type { AppId, Fondo } from './tipos'
import { useSistema } from './sistema'
import { usePreferencias, FONDOS } from './preferencias'
import { APPS, ORDEN_APPS } from './apps'
import { useDatos } from './datos'
import { useSesion } from './sesion'

// El escritorio: la capa que hay debajo de las ventanas. Pinta el fondo, la
// cuadrícula de iconos, los widgets y el menú contextual. Las ventanas las
// pinta App por encima; aquí no se sabe nada de ellas salvo lo que ofrece
// `useSistema` (minimizar todas, abrir).

// ------------------------------------------------- utilidades compartidas
// La barra de tareas y el menú de inicio reutilizan estas piezas para no
// escribir tres veces la misma lógica de «cerrar al pulsar fuera».

// Cierra un desplegable al pulsar fuera de `ref` o con Escape. `ignorar` es
// un selector de elementos que no cuentan como «fuera» (el botón que abre el
// desplegable, para que pulsarlo lo cierre en vez de cerrarlo y reabrirlo).
export function useCerrarFuera(
  ref: RefObject<HTMLElement | null>, activo: boolean, onCerrar: () => void, ignorar?: string,
) {
  // El callback va en un ref para que cambiar su identidad en cada render no
  // obligue a volver a registrar los listeners.
  const cb = useRef(onCerrar)
  cb.current = onCerrar
  useEffect(() => {
    if (!activo) return
    const abajo = (e: PointerEvent) => {
      const t = e.target as Element | null
      if (!t || ref.current?.contains(t)) return
      if (ignorar && t.closest(ignorar)) return
      cb.current()
    }
    const tecla = (e: KeyboardEvent) => { if (e.key === 'Escape') cb.current() }
    // En fase de captura: así se cierra aunque el elemento pulsado detenga la
    // propagación (un icono del escritorio, por ejemplo).
    document.addEventListener('pointerdown', abajo, true)
    document.addEventListener('keydown', tecla)
    return () => {
      document.removeEventListener('pointerdown', abajo, true)
      document.removeEventListener('keydown', tecla)
    }
  }, [activo, ref, ignorar])
}

export type OpcionMenu =
  | { separador: true }
  | { separador?: false; etiqueta: string; accion: () => void; deshabilitado?: boolean; marcado?: boolean }

// Menú contextual flotante. Va por portal al body porque `backdrop-filter`
// (el cristal de la barra) convierte a su elemento en contenedor de los
// descendientes `position: fixed`, y el menú saldría recortado o mal puesto.
export function MenuContextual({ x, y, opciones, onCerrar }: {
  x: number; y: number; opciones: OpcionMenu[]; onCerrar: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x, y })
  useCerrarFuera(ref, true, onCerrar)
  // Se recoloca tras medirse para no salirse de la pantalla.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setPos({
      x: Math.max(4, Math.min(x, window.innerWidth - r.width - 4)),
      y: Math.max(4, Math.min(y, window.innerHeight - r.height - 4)),
    })
  }, [x, y])
  useEffect(() => {
    // Foco al primer elemento para que las flechas funcionen desde el teclado.
    ref.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
  }, [])
  const teclas = (e: KeyboardEventReact<HTMLDivElement>) => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    e.preventDefault()
    const botones = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
    const n = botones.length
    if (!n) return
    const i = botones.indexOf(document.activeElement as HTMLButtonElement)
    botones[(i + (e.key === 'ArrowDown' ? 1 : n - 1) + n) % n].focus()
  }
  return createPortal(
    <div ref={ref} className="menu-contextual" role="menu" style={{ left: pos.x, top: pos.y }}
      onKeyDown={teclas} onContextMenu={(e) => e.preventDefault()}>
      {opciones.map((o, i) => o.separador
        ? <div key={i} className="menu-separador" role="separator" />
        : (
          <button key={i} type="button" role="menuitem" disabled={o.deshabilitado}
            className={o.marcado ? 'marcado' : undefined}
            onClick={() => { o.accion(); onCerrar() }}>
            {o.marcado !== undefined && <span className="menu-marca" aria-hidden>{o.marcado ? '✓' : ''}</span>}
            {o.etiqueta}
          </button>
        ))}
    </div>,
    document.body,
  )
}

// Nombre legible del rol (la API guarda 'admin' | 'operator' | 'viewer').
const ROLES: Record<string, string> = { admin: 'Administrador', operator: 'Operador', viewer: 'Solo lectura' }
export function nombreRol(rol: string): string { return ROLES[rol] ?? rol }

// «hace 5 min», «hace 2 h», «ayer»… para la última sincronización.
export function hace(iso: string | null | undefined): string {
  if (!iso) return 'nunca'
  const ms = Date.now() - new Date(iso).getTime()
  if (!Number.isFinite(ms)) return 'nunca'
  const min = Math.round(ms / 60000)
  if (min < 1) return 'ahora mismo'
  if (min < 60) return `hace ${min} min`
  const h = Math.round(min / 60)
  if (h < 24) return `hace ${h} h`
  const d = Math.round(h / 24)
  return d === 1 ? 'ayer' : `hace ${d} días`
}

// ----------------------------------------------------------------- fondo

function estiloFondo(f: Fondo): CSSProperties {
  switch (f.tipo) {
    case 'preset': {
      const p = FONDOS.find((x) => x.id === f.id) ?? FONDOS[0]
      return { background: p?.css }
    }
    case 'color':
      return { background: f.color }
    case 'degradado':
      return { background: `linear-gradient(135deg, ${f.desde}, ${f.hasta})` }
  }
}

// ----------------------------------------------------------------- iconos

const UMBRAL_ARRASTRE = 6 // px antes de considerar que se arrastra y no se pulsa

type Arrastre = { app: AppId; x0: number; y0: number; activo: boolean; destino: number | null }

function IconoApp({ app, indice, seleccionado, arrastrando, destino, movil, onSeleccionar, onAbrir, onPointerDown, onPointerMove, onPointerUp }: {
  app: AppId
  indice: number
  seleccionado: boolean
  arrastrando: { dx: number; dy: number } | null
  destino: boolean
  movil: boolean
  onSeleccionar: (app: AppId) => void
  onAbrir: (app: AppId) => void
  onPointerDown: (e: PointerEventReact<HTMLButtonElement>, app: AppId) => void
  onPointerMove: (e: PointerEventReact<HTMLButtonElement>) => void
  onPointerUp: (e: PointerEventReact<HTMLButtonElement>) => void
}) {
  const def = APPS[app]
  const clases = ['icono-app']
  if (seleccionado) clases.push('seleccionado')
  if (arrastrando) clases.push('arrastrando')
  if (destino) clases.push('destino')
  const estilo: CSSProperties | undefined = arrastrando
    ? { transform: `translate(${arrastrando.dx}px, ${arrastrando.dy}px)` }
    : undefined
  return (
    <button
      type="button"
      className={clases.join(' ')}
      style={estilo}
      data-indice={indice}
      title={def.descripcion}
      aria-label={def.nombre}
      aria-pressed={seleccionado}
      onPointerDown={(e) => onPointerDown(e, app)}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      // En móvil un toque abre (se resuelve en pointerup); en escritorio hace
      // falta doble clic o Enter.
      onDoubleClick={() => { if (!movil) onAbrir(app) }}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onAbrir(app) }
      }}
      onFocus={() => onSeleccionar(app)}
    >
      <span className="icono-app-img" style={{ background: def.color }} aria-hidden>{def.icono}</span>
      <span className="icono-app-nombre">{def.nombre}</span>
    </button>
  )
}

// ---------------------------------------------------------------- widgets

function Widget({ titulo, onAbrir, children, className }: {
  titulo: string; onAbrir: () => void; children: ReactNode; className?: string
}) {
  // Es un `div` con rol de botón y no un `<button>` porque dentro va contenido
  // de bloque (listas, cifras), que HTML no permite dentro de un botón.
  return (
    <div className={`widget ${className ?? ''}`} role="button" tabIndex={0} onClick={onAbrir}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onAbrir() } }}>
      <div className="widget-titulo">{titulo}</div>
      {children}
    </div>
  )
}

function WidgetAtencion() {
  const datos = useDatos()
  const sistema = useSistema()
  const lista = datos.atencion.slice(0, 5)
  const total = datos.resumen?.en_atencion ?? datos.atencion.reduce((s, a) => s + a.cantidad, 0)
  return (
    <Widget titulo="Atención" className="widget-atencion" onAbrir={() => sistema.abrir('catalogo')}>
      <div className="widget-cifra">
        <strong>{total}</strong>
        <span>{total === 1 ? 'producto pide revisión' : 'productos piden revisión'}</span>
      </div>
      {lista.length > 0 ? (
        <ul className="widget-lista">
          {lista.map((a) => (
            <li key={a.motivo} data-severidad={a.severidad}>
              <span className="widget-motivo">{a.motivo}</span>
              <span className="widget-numero">{a.cantidad}</span>
            </li>
          ))}
        </ul>
      ) : <div className="widget-vacio">Nada pendiente</div>}
    </Widget>
  )
}

function WidgetPedidos() {
  const datos = useDatos()
  const sistema = useSistema()
  const p = datos.pedidos
  return (
    <Widget titulo="Pedidos" className="widget-pedidos" onAbrir={() => sistema.abrir('pedidos')}>
      {p ? (
        <div className="widget-tres">
          <div><strong>{p.recibidos}</strong><span>recibidos</span></div>
          <div className={p.fallidos > 0 ? 'mal' : undefined}><strong>{p.fallidos}</strong><span>fallidos</span></div>
          {/* «montados» = ya creados en Odoo (en_odoo en la API). */}
          <div className="bien"><strong>{p.en_odoo}</strong><span>montados</span></div>
        </div>
      ) : <div className="widget-vacio">Sin datos todavía</div>}
    </Widget>
  )
}

function WidgetActividad() {
  const datos = useDatos()
  const sistema = useSistema()
  const n = datos.tareasActivas
  return (
    <Widget titulo="Actividad" className="widget-actividad" onAbrir={() => sistema.abrir('actividad')}>
      <div className="widget-cifra">
        <strong className={n > 0 ? 'en-marcha' : undefined}>{n}</strong>
        <span>{n === 1 ? 'tarea en marcha' : 'tareas en marcha'}</span>
      </div>
      <div className="widget-pie">
        Última sincronización: <b>{hace(datos.resumen?.ultima_sincronizacion)}</b>
      </div>
    </Widget>
  )
}

// ------------------------------------------------------------- escritorio

export function Escritorio() {
  const sistema = useSistema()
  const { prefs, poner } = usePreferencias()
  const { usuario } = useSesion()
  const esAdmin = usuario.role === 'admin'

  const iconos = useMemo(
    () => prefs.escritorio.filter((id) => APPS[id] && (!APPS[id].soloAdmin || esAdmin)),
    [prefs.escritorio, esAdmin],
  )

  const [seleccion, setSeleccion] = useState<AppId | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null)
  const arrastre = useRef<Arrastre | null>(null)
  const [vista, setVista] = useState<{ app: AppId; dx: number; dy: number; destino: number | null } | null>(null)

  const abrir = useCallback((app: AppId) => { sistema.abrir(app) }, [sistema])

  // --- arrastre de iconos con pointer events (sin librerías)
  const bajar = (e: PointerEventReact<HTMLButtonElement>, app: AppId) => {
    if (e.button !== 0) return
    setSeleccion(app)
    arrastre.current = { app, x0: e.clientX, y0: e.clientY, activo: false, destino: null }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  const mover = (e: PointerEventReact<HTMLButtonElement>) => {
    const a = arrastre.current
    if (!a) return
    const dx = e.clientX - a.x0
    const dy = e.clientY - a.y0
    if (!a.activo) {
      if (Math.hypot(dx, dy) < UMBRAL_ARRASTRE) return
      a.activo = true
    }
    // El icono arrastrado tiene `pointer-events: none` (clase .arrastrando),
    // así que elementFromPoint devuelve el icono que hay debajo del puntero.
    const bajo = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('.icono-app')
    const destino = bajo?.dataset.indice != null ? Number(bajo.dataset.indice) : null
    a.destino = destino
    setVista({ app: a.app, dx, dy, destino })
  }
  const soltar = (e: PointerEventReact<HTMLButtonElement>) => {
    const a = arrastre.current
    arrastre.current = null
    setVista(null)
    if (!a) return
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* ya liberado */ }
    if (e.type === 'pointercancel') return
    if (a.activo) {
      const desde = iconos.indexOf(a.app)
      if (a.destino != null && a.destino !== desde && desde >= 0) {
        // Se reordena sobre la lista completa de prefs (no sobre la filtrada
        // por rol) para no perder los iconos que este usuario no ve.
        const orden = prefs.escritorio.slice()
        const objetivo = iconos[a.destino]
        orden.splice(orden.indexOf(a.app), 1)
        const j = orden.indexOf(objetivo)
        orden.splice(a.destino > desde ? j + 1 : j, 0, a.app)
        poner({ escritorio: orden })
      }
      return
    }
    // Sin arrastre: fue un clic. En móvil no hay doble clic: un toque abre.
    if (sistema.movil) abrir(a.app)
  }

  // --- menú contextual del fondo
  const contextual = (e: MouseEventReact<HTMLDivElement>) => {
    // Solo sobre el fondo, no sobre iconos ni widgets.
    const t = e.target as HTMLElement
    if (t.closest('.icono-app, .widget')) return
    e.preventDefault()
    setMenu({ x: e.clientX, y: e.clientY })
  }
  const ordenar = () => {
    // Orden canónico de las apps, conservando solo las que están en el escritorio.
    const presentes = new Set(prefs.escritorio)
    poner({ escritorio: ORDEN_APPS.filter((id) => presentes.has(id)) })
  }
  const alternarWidget = (k: keyof typeof prefs.widgets) => {
    poner({ widgets: { ...prefs.widgets, [k]: !prefs.widgets[k] } })
  }

  // Clic en el fondo: deselecciona. No hay API para quitar el foco a las
  // ventanas; con deseleccionar los iconos basta para que parezca lo mismo.
  const clicFondo = (e: MouseEventReact<HTMLDivElement>) => {
    const t = e.target as HTMLElement
    if (t.closest('.icono-app, .widget')) return
    setSeleccion(null)
  }

  const hayWidgets = prefs.widgets.atencion || prefs.widgets.pedidos || prefs.widgets.actividad

  return (
    <div
      className={`escritorio${sistema.movil ? ' movil' : ''}`}
      style={estiloFondo(prefs.fondo)}
      onContextMenu={contextual}
      onMouseDown={clicFondo}
    >
      <div className="escritorio-iconos" aria-label="Iconos del escritorio">
        {iconos.map((app, i) => (
          <IconoApp
            key={app}
            app={app}
            indice={i}
            seleccionado={seleccion === app}
            arrastrando={vista?.app === app ? { dx: vista.dx, dy: vista.dy } : null}
            destino={vista != null && vista.destino === i && vista.app !== app}
            movil={sistema.movil}
            onSeleccionar={setSeleccion}
            onAbrir={abrir}
            onPointerDown={bajar}
            onPointerMove={mover}
            onPointerUp={soltar}
          />
        ))}
      </div>

      {hayWidgets && (
        <aside className="widgets" aria-label="Widgets">
          {prefs.widgets.atencion && <WidgetAtencion />}
          {prefs.widgets.pedidos && <WidgetPedidos />}
          {prefs.widgets.actividad && <WidgetActividad />}
        </aside>
      )}

      {menu && (
        <MenuContextual
          x={menu.x}
          y={menu.y}
          onCerrar={() => setMenu(null)}
          opciones={[
            { etiqueta: 'Ordenar iconos', accion: ordenar },
            { separador: true },
            { etiqueta: 'Widget de atención', marcado: prefs.widgets.atencion, accion: () => alternarWidget('atencion') },
            { etiqueta: 'Widget de pedidos', marcado: prefs.widgets.pedidos, accion: () => alternarWidget('pedidos') },
            { etiqueta: 'Widget de actividad', marcado: prefs.widgets.actividad, accion: () => alternarWidget('actividad') },
            { separador: true },
            { etiqueta: 'Cambiar fondo…', accion: () => sistema.abrir('configuracion') },
            { etiqueta: 'Minimizar todas las ventanas', deshabilitado: sistema.ventanas.length === 0, accion: () => sistema.minimizarTodas() },
          ]}
        />
      )}
    </div>
  )
}
