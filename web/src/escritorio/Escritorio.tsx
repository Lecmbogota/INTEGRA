import {
  useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState,
  type CSSProperties, type KeyboardEvent as KeyboardEventReact,
  type MouseEvent as MouseEventReact, type PointerEvent as PointerEventReact,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { CELDA, type AppId, type Disposicion, type WidgetInstancia } from './tipos'
import { useSistema } from './sistema'
import { usePreferencias, cssFondo } from './preferencias'
import { APPS } from './apps'
import { useDatos } from './datos'
import { useSesion } from './sesion'
import {
  WIDGETS, TAMANOS_RAPIDOS, GaleriaWidgets, tituloWidget, dimensionesLienzo, encajar, huecoMasCercano,
  primerHueco, ICONO, MARGEN, type ContextoWidget, type Rect, type TamanoRapido,
} from './Widgets'

// El escritorio: la capa que hay debajo de las ventanas. Pinta el fondo y un
// lienzo libre con una cuadrícula invisible de CELDA px donde viven los
// iconos y los widgets; cada uno se arrastra a donde se quiera y al soltar
// se pega a la celda más cercana. Las ventanas las pinta App por encima;
// aquí no se sabe nada de ellas salvo lo que ofrece `useSistema`.

// «hace 5 min»… vive en Widgets.tsx (lo usan varios widgets) y se
// reexporta desde aquí, que es de donde lo importan la barra y el inicio.
export { hace } from './Widgets'

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

// -------------------------------------------------------------- arrastre

// px de movimiento antes de considerar que se arrastra y no se pulsa.
const UMBRAL_ARRASTRE = 4

type Operacion = {
  x0: number
  y0: number
  activo: boolean
  alMover: (dx: number, dy: number) => void
  alSoltar: (dx: number, dy: number, arrastro: boolean) => void
}

// Arrastre genérico con pointer events (sin librerías). El elemento que
// recibe el pointerdown captura el puntero, así que sus propios
// onPointerMove/Up siguen llegando aunque el cursor salga de él. Devuelve los
// tres manejadores; `bajar` recibe qué hacer al mover y al soltar.
function useArrastre() {
  const op = useRef<Operacion | null>(null)
  const bajar = useCallback((e: PointerEventReact<HTMLElement>, alMover: Operacion['alMover'], alSoltar: Operacion['alSoltar']) => {
    if (e.button !== 0) return
    op.current = { x0: e.clientX, y0: e.clientY, activo: false, alMover, alSoltar }
    e.currentTarget.setPointerCapture(e.pointerId)
  }, [])
  const mover = useCallback((e: PointerEventReact<HTMLElement>) => {
    const a = op.current
    if (!a) return
    const dx = e.clientX - a.x0
    const dy = e.clientY - a.y0
    if (!a.activo) {
      if (Math.hypot(dx, dy) < UMBRAL_ARRASTRE) return
      a.activo = true
    }
    a.alMover(dx, dy)
  }, [])
  const soltar = useCallback((e: PointerEventReact<HTMLElement>) => {
    const a = op.current
    op.current = null
    if (!a) return
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* ya liberado */ }
    // Cancelado (el navegador se quedó con el gesto): se deja todo como estaba.
    if (e.type === 'pointercancel') { a.alSoltar(0, 0, false); return }
    a.alSoltar(e.clientX - a.x0, e.clientY - a.y0, a.activo)
  }, [])
  return { bajar, mover, soltar }
}

type Punteros = {
  onPointerMove: (e: PointerEventReact<HTMLElement>) => void
  onPointerUp: (e: PointerEventReact<HTMLElement>) => void
  onPointerCancel: (e: PointerEventReact<HTMLElement>) => void
}

// ----------------------------------------------------------------- iconos

function rectIcono(p: { x: number; y: number }): Rect { return { x: p.x, y: p.y, w: ICONO, h: ICONO } }

// Dónde va cada icono: los que tienen posición guardada, donde se dejaron
// (encajados en el área y, si ahora chocan con algo, al hueco más cercano);
// los demás, en el primer hueco libre recorriendo columnas de izquierda a
// derecha, en saltos de icono para que queden en columnas limpias.
function colocarIconos(
  apps: AppId[], guardadas: Disposicion['iconos'], widgets: Rect[], cols: number, filas: number,
): Map<AppId, { x: number; y: number }> {
  const res = new Map<AppId, { x: number; y: number }>()
  const ocupados: Rect[] = widgets.slice()
  const sinSitio: AppId[] = []
  for (const app of apps) {
    const p = guardadas[app]
    if (!p) { sinSitio.push(app); continue }
    const r = huecoMasCercano(rectIcono(p), ocupados, cols, filas)
    res.set(app, { x: r.x, y: r.y })
    ocupados.push(r)
  }
  for (const app of sinSitio) {
    const r = primerHueco(ICONO, ICONO, ocupados, cols, filas, ICONO)
      ?? primerHueco(ICONO, ICONO, ocupados, cols, filas)
      ?? { x: 0, y: 0, w: ICONO, h: ICONO }
    res.set(app, { x: r.x, y: r.y })
    ocupados.push(r)
  }
  return res
}

function IconoApp({ app, pos, seleccionado, desplazamiento, movil, onSeleccionar, onAbrir, onPointerDown, punteros }: {
  app: AppId
  pos: { x: number; y: number } | null
  seleccionado: boolean
  desplazamiento: { dx: number; dy: number } | null
  movil: boolean
  onSeleccionar: (app: AppId) => void
  onAbrir: (app: AppId) => void
  onPointerDown: (e: PointerEventReact<HTMLElement>, app: AppId) => void
  punteros: Punteros
}) {
  const def = APPS[app]
  const clases = ['icono-app']
  if (seleccionado) clases.push('seleccionado')
  if (desplazamiento) clases.push('arrastrando')
  const estilo: CSSProperties = {}
  if (pos) { estilo.left = pos.x * CELDA; estilo.top = pos.y * CELDA }
  if (desplazamiento) Object.assign(estilo, { '--dx': `${desplazamiento.dx}px`, '--dy': `${desplazamiento.dy}px` })
  return (
    <button
      type="button"
      className={clases.join(' ')}
      style={estilo}
      title={def.descripcion}
      aria-label={def.nombre}
      aria-pressed={seleccionado}
      onPointerDown={(e) => onPointerDown(e, app)}
      {...punteros}
      // En móvil un toque abre (se resuelve al soltar); en escritorio hace
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

type ModoRedim = 'e' | 's' | 'se'

// Marco de un widget: tarjeta de cristal con cabecera (título, «…»), cuerpo
// con scroll propio, asa de redimensionar y bordes derecho e inferior. El
// contenido lo pinta el tipo (Widgets.tsx). En móvil no se mueve ni se
// redimensiona: solo se quita o se ajusta.
function MarcoWidget({ inst, ctx, movil, desplazamiento, redimensionando, onMover, onRedimensionar, punteros, onTamano, onQuitar, onConfig }: {
  inst: WidgetInstancia
  ctx: ContextoWidget
  movil: boolean
  desplazamiento: { dx: number; dy: number } | null
  // Verdadero mientras se arrastra el asa: sin transiciones, que si no el
  // tamaño va a remolque del puntero.
  redimensionando: boolean
  onMover: (e: PointerEventReact<HTMLElement>, id: string) => void
  onRedimensionar: (e: PointerEventReact<HTMLElement>, id: string, modo: ModoRedim) => void
  punteros: Punteros
  onTamano: (id: string, t: TamanoRapido) => void
  onQuitar: (id: string) => void
  onConfig: (id: string, config: Record<string, unknown>) => void
}) {
  const def = WIDGETS[inst.tipo]
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null)
  const [ajustando, setAjustando] = useState(false)
  const botonMenu = useRef<HTMLButtonElement>(null)

  const abrirMenu = () => {
    const r = botonMenu.current?.getBoundingClientRect()
    if (r) setMenu({ x: r.left, y: r.bottom + 4 })
  }
  const contextual = (e: MouseEventReact<HTMLDivElement>) => {
    e.preventDefault()
    setMenu({ x: e.clientX, y: e.clientY })
  }
  const tamanoActual = TAMANOS_RAPIDOS.find(({ id }) => def.tamanos[id].w === inst.w && def.tamanos[id].h === inst.h)?.id

  const estilo: CSSProperties = movil ? {} : {
    left: inst.x * CELDA, top: inst.y * CELDA, width: inst.w * CELDA, height: inst.h * CELDA,
  }
  // El desplazamiento va en variables CSS para que la hoja de estilos pueda
  // combinarlo con la escala de «arrastrando» en un solo transform.
  if (desplazamiento) Object.assign(estilo, { '--dx': `${desplazamiento.dx}px`, '--dy': `${desplazamiento.dy}px` })

  const opciones: OpcionMenu[] = []
  if (!movil) {
    for (const t of TAMANOS_RAPIDOS) {
      opciones.push({ etiqueta: t.nombre, marcado: tamanoActual === t.id, accion: () => onTamano(inst.id, t.id) })
    }
    opciones.push({ separador: true })
  }
  if (def.ajustes) opciones.push({ etiqueta: 'Ajustes…', accion: () => setAjustando(true) })
  opciones.push({ etiqueta: 'Quitar', accion: () => onQuitar(inst.id) })

  return (
    <div
      className={`widget widget-tipo-${inst.tipo}${desplazamiento ? ' arrastrando' : ''}${redimensionando ? ' redimensionando' : ''}`}
      style={estilo}
      data-widget={inst.id}
      onContextMenu={contextual}
    >
      <div
        className="widget-cabecera"
        onPointerDown={(e) => {
          // El «…» no arrastra: abre el menú.
          if ((e.target as HTMLElement).closest('.widget-menu')) return
          if (!movil) onMover(e, inst.id)
        }}
        {...punteros}
      >
        <span className="widget-titulo">{tituloWidget(inst)}</span>
        <button ref={botonMenu} type="button" className="widget-menu" aria-label={`Opciones de ${def.nombre}`}
          aria-haspopup="menu" onClick={abrirMenu}>…</button>
      </div>
      <div className="widget-cuerpo">
        {ajustando && def.ajustes ? (
          <div className="widget-ajustes">
            {def.ajustes(inst, (config) => onConfig(inst.id, config), ctx)}
            <button type="button" className="widget-listo" onClick={() => setAjustando(false)}>Listo</button>
          </div>
        ) : def.render(inst, ctx)}
      </div>
      {!movil && (
        <>
          <div className="widget-borde widget-borde-e" onPointerDown={(e) => onRedimensionar(e, inst.id, 'e')} {...punteros} />
          <div className="widget-borde widget-borde-s" onPointerDown={(e) => onRedimensionar(e, inst.id, 's')} {...punteros} />
          <div className="widget-asa" aria-hidden onPointerDown={(e) => onRedimensionar(e, inst.id, 'se')} {...punteros} />
        </>
      )}
      {menu && <MenuContextual x={menu.x} y={menu.y} opciones={opciones} onCerrar={() => setMenu(null)} />}
    </div>
  )
}

// ------------------------------------------------------------- escritorio

export function Escritorio() {
  const sistema = useSistema()
  const { prefs, poner } = usePreferencias()
  const datos = useDatos()
  const { usuario } = useSesion()
  const esAdmin = usuario.role === 'admin'
  const disp = prefs.disposicion
  const movil = sistema.movil

  const iconos = useMemo(
    () => prefs.escritorio.filter((id) => APPS[id] && (!APPS[id].soloAdmin || esAdmin)),
    [prefs.escritorio, esAdmin],
  )

  // Celdas que caben en el área. Cambia con la ventana del navegador.
  const { cols, filas } = dimensionesLienzo(sistema.area)

  // Widgets tal como se pintan: lo guardado, encajado en el área actual (una
  // disposición hecha en 1920 px no se sale en una pantalla menor). Solo se
  // persiste cuando el usuario mueve algo.
  const widgets = useMemo(
    () => disp.widgets.map((w) => ({ ...w, ...encajar(w, cols, filas) })),
    [disp.widgets, cols, filas],
  )
  const posIconos = useMemo(
    () => colocarIconos(iconos, disp.iconos, widgets, cols, filas),
    [iconos, disp.iconos, widgets, cols, filas],
  )

  const ctx = useMemo<ContextoWidget>(
    () => ({ datos, abrir: sistema.abrir, movil, esAdmin }),
    [datos, sistema.abrir, movil, esAdmin],
  )

  const [seleccion, setSeleccion] = useState<AppId | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null)
  const [galeria, setGaleria] = useState(false)
  // Lo que se está arrastrando ahora mismo, en px desde su sitio.
  const [vista, setVista] = useState<{ tipo: 'icono' | 'widget'; id: string; dx: number; dy: number } | null>(null)
  // Tamaño en vivo mientras se redimensiona (ya pegado a celdas).
  const [tamanoVivo, setTamanoVivo] = useState<{ id: string; w: number; h: number } | null>(null)

  const { bajar, mover, soltar } = useArrastre()
  const punteros = useMemo<Punteros>(() => ({ onPointerMove: mover, onPointerUp: soltar, onPointerCancel: soltar }), [mover, soltar])

  const guardar = useCallback((d: Disposicion) => poner({ disposicion: d }), [poner])
  const abrir = useCallback((app: AppId) => { sistema.abrir(app) }, [sistema])

  // --- iconos: arrastrar a cualquier sitio; al soltar, a la celda más
  // cercana, y si está ocupada, al hueco libre más próximo.
  const bajarIcono = (e: PointerEventReact<HTMLElement>, app: AppId) => {
    setSeleccion(app)
    if (movil) {
      // En móvil no hay posiciones libres: el gesto solo sirve para abrir.
      bajar(e, () => {}, (_dx, _dy, arrastro) => { if (!arrastro) abrir(app) })
      return
    }
    bajar(e,
      (dx, dy) => setVista({ tipo: 'icono', id: app, dx, dy }),
      (dx, dy, arrastro) => {
        setVista(null)
        if (!arrastro) return
        const p = posIconos.get(app)
        if (!p) return
        const deseado = rectIcono({ x: Math.round(p.x + dx / CELDA), y: Math.round(p.y + dy / CELDA) })
        const ocupados: Rect[] = [
          ...widgets,
          ...iconos.filter((a) => a !== app).map((a) => rectIcono(posIconos.get(a) ?? { x: 0, y: 0 })),
        ]
        const r = huecoMasCercano(deseado, ocupados, cols, filas)
        guardar({ ...disp, iconos: { ...disp.iconos, [app]: { x: r.x, y: r.y } } })
      })
  }

  // --- widgets: mover por la cabecera, redimensionar por asa y bordes.
  const moverWidget = (e: PointerEventReact<HTMLElement>, id: string) => {
    bajar(e,
      (dx, dy) => setVista({ tipo: 'widget', id, dx, dy }),
      (dx, dy, arrastro) => {
        setVista(null)
        if (!arrastro) return
        const w = widgets.find((x) => x.id === id)
        if (!w) return
        const deseado = { x: Math.round(w.x + dx / CELDA), y: Math.round(w.y + dy / CELDA), w: w.w, h: w.h }
        // Los widgets se esquivan entre sí; los iconos se apartan solos si
        // no tienen posición fija (ver colocarIconos).
        const r = huecoMasCercano(deseado, widgets.filter((o) => o.id !== id), cols, filas)
        guardar({ ...disp, widgets: disp.widgets.map((o) => (o.id === id ? { ...o, x: r.x, y: r.y, w: r.w, h: r.h } : o)) })
      })
  }
  const redimensionarWidget = (e: PointerEventReact<HTMLElement>, id: string, modo: ModoRedim) => {
    const w = widgets.find((x) => x.id === id)
    if (!w) return
    const min = WIDGETS[w.tipo].minimo
    const calcular = (dx: number, dy: number) => ({
      id,
      w: modo === 's' ? w.w : Math.max(min.w, Math.min(cols - w.x, Math.round((w.w * CELDA + dx) / CELDA))),
      h: modo === 'e' ? w.h : Math.max(min.h, Math.min(filas - w.y, Math.round((w.h * CELDA + dy) / CELDA))),
    })
    bajar(e,
      (dx, dy) => setTamanoVivo(calcular(dx, dy)),
      (dx, dy, arrastro) => {
        setTamanoVivo(null)
        if (!arrastro) return
        const t = calcular(dx, dy)
        guardar({ ...disp, widgets: disp.widgets.map((o) => (o.id === id ? { ...o, x: w.x, y: w.y, w: t.w, h: t.h } : o)) })
      })
  }
  const tamanoRapido = (id: string, t: TamanoRapido) => {
    const w = widgets.find((x) => x.id === id)
    if (!w) return
    // El tamaño pedido, desplazado si no cabe desde donde está.
    const r = encajar({ x: w.x, y: w.y, ...WIDGETS[w.tipo].tamanos[t] }, cols, filas)
    guardar({ ...disp, widgets: disp.widgets.map((o) => (o.id === id ? { ...o, ...r } : o)) })
  }
  const quitarWidget = (id: string) => guardar({ ...disp, widgets: disp.widgets.filter((o) => o.id !== id) })
  const configurarWidget = (id: string, config: Record<string, unknown>) =>
    guardar({ ...disp, widgets: disp.widgets.map((o) => (o.id === id ? { ...o, config } : o)) })

  // --- menú contextual del fondo
  const contextual = (e: MouseEventReact<HTMLDivElement>) => {
    // Solo sobre el fondo, no sobre iconos ni widgets (tienen el suyo).
    const t = e.target as HTMLElement
    if (t.closest('.icono-app, .widget')) return
    e.preventDefault()
    setMenu({ x: e.clientX, y: e.clientY })
  }
  // Sin posiciones guardadas, todos se autocolocan en columnas por el orden
  // de prefs.escritorio: eso es «ordenar».
  const ordenar = () => guardar({ ...disp, iconos: {} })

  // Clic en el fondo: deselecciona. No hay API para quitar el foco a las
  // ventanas; con deseleccionar los iconos basta para que parezca lo mismo.
  const clicFondo = (e: MouseEventReact<HTMLDivElement>) => {
    const t = e.target as HTMLElement
    if (t.closest('.icono-app, .widget, .boton-anadir-widget')) return
    setSeleccion(null)
  }

  const pintarIcono = (app: AppId) => (
    <IconoApp
      key={app}
      app={app}
      pos={movil ? null : (posIconos.get(app) ?? null)}
      seleccionado={seleccion === app}
      desplazamiento={vista?.tipo === 'icono' && vista.id === app ? { dx: vista.dx, dy: vista.dy } : null}
      movil={movil}
      onSeleccionar={setSeleccion}
      onAbrir={abrir}
      onPointerDown={bajarIcono}
      punteros={punteros}
    />
  )
  const pintarWidget = (w: WidgetInstancia) => (
    <MarcoWidget
      key={w.id}
      inst={tamanoVivo?.id === w.id ? { ...w, w: tamanoVivo.w, h: tamanoVivo.h } : w}
      ctx={ctx}
      movil={movil}
      desplazamiento={vista?.tipo === 'widget' && vista.id === w.id ? { dx: vista.dx, dy: vista.dy } : null}
      redimensionando={tamanoVivo?.id === w.id}
      onMover={moverWidget}
      onRedimensionar={redimensionarWidget}
      punteros={punteros}
      onTamano={tamanoRapido}
      onQuitar={quitarWidget}
      onConfig={configurarWidget}
    />
  )

  return (
    <div
      className={`escritorio${movil ? ' movil' : ''}`}
      style={{ background: cssFondo(prefs.fondo) }}
      onContextMenu={contextual}
      onMouseDown={clicFondo}
    >
      {movil ? (
        // Móvil: sin posiciones libres. Iconos en rejilla por filas y los
        // widgets apilados debajo, con desplazamiento vertical.
        <>
          <div className="escritorio-iconos" aria-label="Iconos del escritorio">{iconos.map(pintarIcono)}</div>
          <div className="escritorio-widgets" aria-label="Widgets">{widgets.map(pintarWidget)}</div>
        </>
      ) : (
        <div className="lienzo" style={{ inset: MARGEN }}>
          {iconos.map(pintarIcono)}
          {widgets.map(pintarWidget)}
        </div>
      )}

      <GaleriaWidgets abierta={galeria} onCerrar={() => setGaleria(false)} />

      {menu && (
        <MenuContextual
          x={menu.x}
          y={menu.y}
          onCerrar={() => setMenu(null)}
          opciones={[
            { etiqueta: 'Ordenar iconos', accion: ordenar, deshabilitado: movil },
            { etiqueta: 'Añadir widget…', accion: () => setGaleria(true) },
            { separador: true },
            { etiqueta: 'Cambiar fondo…', accion: () => sistema.abrir('configuracion') },
            { etiqueta: 'Minimizar todas las ventanas', deshabilitado: sistema.ventanas.length === 0, accion: () => sistema.minimizarTodas() },
          ]}
        />
      )}
    </div>
  )
}
