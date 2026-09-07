import {
  Fragment, useEffect, useRef, useState, type KeyboardEvent as KeyboardEventReact, type MouseEvent as MouseEventReact,
  type PointerEvent as PointerEventReact, type ReactNode,
} from 'react'
import type { Pestana, Sistema, Ventana as VentanaTipo } from './tipos'
import { APPS } from './apps'
import { MenuContextual, type OpcionMenu } from './Escritorio'
import { puedeAdelante, puedeAtras, useSistema } from './sistema'
import './ventanas.css'

// Marco de una ventana: barra de título con pestañas, barra de dirección con
// migas, mover, redimensionar, botones. No sabe nada de la app que contiene;
// solo pide cambios al gestor (useSistema) y pinta la geometría que este le
// da. La geometría de la ventana (x, y, w, h) es siempre la real, también
// maximizada o ajustada: el gestor la recalcula.
//
// Como el explorador de Windows 11: cada ventana tiene pestañas (cada una
// con su historial) y, bajo la barra de título, una barra de dirección con
// ← → ↑ y las migas del historial de la pestaña activa. Con una sola
// pestaña la barra de título enseña solo icono y título, sin aspecto de
// pestaña, para no cargar el marco.

// Píxeles que hay que mover el puntero antes de considerar que se arrastra:
// evita que un clic en la barra de título desplace la ventana un píxel.
const UMBRAL_ARRASTRE = 3
// Distancia al borde del área a la que se ofrece maximizar / ajustar.
const MARGEN_BORDE = 6
// Duración de las animaciones de salida; la misma que en ventanas.css.
const DURACION_SALIDA = 120
// Ancho a partir del cual las pestañas van dentro de la barra de título (a la
// izquierda de los controles) en vez de en una fila propia debajo.
const ANCHO_PESTANAS_INTEGRADAS = 720
// Mantener pulsado ← este tiempo despliega el historial entero como menú.
const PULSACION_LARGA = 500

type Lado = 'n' | 's' | 'e' | 'o' | 'ne' | 'nw' | 'se' | 'sw'
const LADOS: Lado[] = ['n', 's', 'e', 'o', 'ne', 'nw', 'se', 'sw']
type Previa = 'maximizada' | 'izquierda' | 'derecha' | null
type Salida = 'cerrando' | 'minimizando' | null
type Menu = { x: number; y: number; opciones: OpcionMenu[] } | null

type Arrastre = {
  // Puntero al empezar, esquina de la ventana al empezar y origen del área
  // en coordenadas de cliente (para saber cuándo el puntero toca un borde).
  px: number; py: number; x: number; y: number; ox: number; oy: number; movido: boolean
}
type Redimension = { lado: Lado; px: number; py: number; x: number; y: number; w: number; h: number }

function reducirMovimiento(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// Qué recorrido de la guía explica cada app. Casi siempre coincide con el
// id; los diálogos con clave distinta se listan, y lo que no tiene recorrido
// propio (configuración, la propia ayuda) abre el general.
function claveGuiaDe(app: string): string {
  const especiales: Record<string, string> = {
    'selector-mediateca': 'selector', 'asignar-foto': 'asignar', configuracion: 'general', ayuda: 'general',
  }
  return especiales[app] ?? app
}

// Página que enseña una pestaña ahora mismo.
function paginaDe(t: Pestana) {
  return t.historial[t.indice]
}

const ICONO_MAS = (
  <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M6 2v8M2 6h8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
)
const ICONO_CERRAR = (
  <svg width="10" height="10" viewBox="0 0 12 12" aria-hidden="true"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
)

export function Ventana({ ventana, children }: { ventana: VentanaTipo; children: ReactNode }) {
  const sistema = useSistema()
  const def = APPS[ventana.app]
  const { area, movil } = sistema
  const activa = sistema.activa === ventana.id
  const { id, estado, pestanas, pestanaActiva } = ventana

  const marcoRef = useRef<HTMLDivElement>(null)
  const arrastreRef = useRef<Arrastre | null>(null)
  const redimensionRef = useRef<Redimension | null>(null)
  const previaRef = useRef<Previa>(null)
  const [previa, setPrevia] = useState<Previa>(null)
  const [saliendo, setSaliendo] = useState<Salida>(null)
  const temporizadorRef = useRef<number | null>(null)
  const [menu, setMenu] = useState<Menu>(null)

  useEffect(() => () => {
    if (temporizadorRef.current !== null) window.clearTimeout(temporizadorRef.current)
  }, [])

  // Al pasar a ser la activa sin que el foco del documento esté ya dentro
  // (Alt+Tab, abrir desde la barra), el marco recibe el foco para que el
  // teclado siga a la ventana. Si la app acaba de enfocar un campo, se respeta.
  useEffect(() => {
    const marco = marcoRef.current
    if (!activa || !marco || estado === 'minimizada') return
    if (marco.contains(document.activeElement)) return
    marco.focus({ preventScroll: true })
  }, [activa, estado])

  // Cerrar y minimizar salen con una animación corta y luego avisan al
  // gestor; con movimiento reducido, de inmediato.
  const salir = (modo: Exclude<Salida, null>) => {
    if (saliendo) return
    const ejecutar = () => {
      temporizadorRef.current = null
      setSaliendo(null)
      if (modo === 'cerrando') sistema.cerrar(id)
      else sistema.minimizar(id)
    }
    if (reducirMovimiento()) {
      ejecutar()
      return
    }
    setSaliendo(modo)
    temporizadorRef.current = window.setTimeout(ejecutar, DURACION_SALIDA)
  }

  const alternarMaximizada = () => {
    if (estado === 'maximizada') sistema.restaurar(id)
    else sistema.maximizar(id)
  }

  // Cerrar una pestaña: si es la última, es cerrar la ventana, con su
  // animación de salida.
  const cerrarPestana = (pestanaId: string) => {
    if (pestanas.length <= 1) salir('cerrando')
    else sistema.cerrarPestana(id, pestanaId)
  }

  // Atajos con el foco dentro de la ventana (los eventos de las páginas
  // suben hasta el marco):
  //   Alt+← / Alt+→ / Alt+↑   historial de la pestaña (y se le quita al
  //                           navegador, que con Alt+← se iría de Integra)
  //   Ctrl+T                  nueva pestaña (misma app base)
  //   Ctrl+W                  cerrar pestaña (la última cierra la ventana)
  //   Ctrl+Tab / Ctrl+Shift+Tab   pestaña siguiente / anterior
  //   Ctrl+1…8, Ctrl+9        pestaña N, última
  // Ctrl+T y Ctrl+W los captura el propio navegador en muchas
  // configuraciones (abre o cierra SU pestaña antes de que lleguen aquí);
  // se registran con preventDefault igualmente para los navegadores y modos
  // (PWA instalada, quiosco) en los que sí llegan. Se usan códigos físicos
  // (KeyT…) para no depender de la distribución del teclado.
  const teclas = (e: KeyboardEventReact<HTMLDivElement>) => {
    if (e.metaKey) return
    if (e.altKey && !e.ctrlKey && !e.shiftKey) {
      if (e.key === 'ArrowLeft' && puedeAtras(ventana)) sistema.atras(id)
      else if (e.key === 'ArrowRight' && puedeAdelante(ventana)) sistema.adelante(id)
      else if (e.key === 'ArrowUp' && ventana.indice > 0) sistema.irAPagina(id, 0)
      else return
      e.preventDefault()
      return
    }
    if (!e.ctrlKey || e.altKey) return
    if (e.key === 'Tab') {
      const i = pestanas.findIndex(t => t.id === pestanaActiva)
      const paso = e.shiftKey ? -1 : 1
      sistema.activarPestana(id, pestanas[(i + paso + pestanas.length) % pestanas.length].id)
      e.preventDefault()
      return
    }
    if (e.shiftKey) return
    if (e.code === 'KeyT') sistema.nuevaPestana(id)
    else if (e.code === 'KeyW') cerrarPestana(pestanaActiva)
    else if (/^Digit[1-9]$/.test(e.code)) {
      const n = Number(e.code.slice(5))
      const t = n === 9 ? pestanas[pestanas.length - 1] : pestanas[n - 1]
      if (!t) return
      sistema.activarPestana(id, t.id)
    } else return
    e.preventDefault()
  }

  const fijarPrevia = (p: Previa) => {
    if (previaRef.current === p) return
    previaRef.current = p
    setPrevia(p)
  }

  // ---- mover arrastrando la barra de título

  const empezarArrastre = (e: PointerEventReact<HTMLDivElement>) => {
    if (e.button !== 0 || movil || saliendo) return
    // Los botones y las pestañas tienen su propio arrastre / clic; el hueco
    // libre de la barra (también entre pestañas y controles) mueve la ventana.
    if ((e.target as HTMLElement).closest('button, [role="tab"]')) return
    const marco = marcoRef.current
    if (!marco) return
    const rect = marco.getBoundingClientRect()
    const relX = e.clientX - rect.left
    // Origen del área en coordenadas de cliente: la esquina del marco menos
    // su posición dentro del área.
    const ox = rect.left - ventana.x
    const oy = rect.top - ventana.y

    let { x, y } = ventana
    if (estado !== 'normal') {
      // Arrastrar una ventana maximizada o ajustada la restaura a su tamaño
      // anterior, colocada de modo que el puntero quede en la misma
      // proporción de la barra de título que antes.
      const ant = ventana.anterior ?? { x, y, w: def.tamano.w, h: def.tamano.h }
      x = Math.round(ventana.x + relX - relX * (ant.w / ventana.w))
      y = ventana.y
      sistema.restaurar(id)
      sistema.mover(id, x, y)
    }
    arrastreRef.current = { px: e.clientX, py: e.clientY, x, y, ox, oy, movido: false }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const moverArrastre = (e: PointerEventReact<HTMLDivElement>) => {
    const a = arrastreRef.current
    if (!a) return
    const dx = e.clientX - a.px
    const dy = e.clientY - a.py
    if (!a.movido) {
      if (Math.abs(dx) < UMBRAL_ARRASTRE && Math.abs(dy) < UMBRAL_ARRASTRE) return
      a.movido = true
    }
    sistema.mover(id, a.x + dx, a.y + dy)

    // Cerca de un borde del área se enseña dónde caerá la ventana al soltar.
    const px = e.clientX - a.ox
    const py = e.clientY - a.oy
    let p: Previa = null
    if (py <= MARGEN_BORDE) p = 'maximizada'
    else if (px <= MARGEN_BORDE) p = 'izquierda'
    else if (px >= area.w - MARGEN_BORDE) p = 'derecha'
    fijarPrevia(p)
  }

  const terminarArrastre = (e: PointerEventReact<HTMLDivElement>, aplicar: boolean) => {
    const a = arrastreRef.current
    if (!a) return
    arrastreRef.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      // Ya liberada (pointercancel): no importa.
    }
    const p = previaRef.current
    fijarPrevia(null)
    if (!aplicar || !p) return
    if (p === 'maximizada') sistema.maximizar(id)
    else sistema.ajustar(id, p)
  }

  const dobleClicTitulo = (e: MouseEventReact<HTMLDivElement>) => {
    if (movil || (e.target as HTMLElement).closest('button, [role="tab"]')) return
    alternarMaximizada()
  }

  // ---- redimensionar por bordes y esquinas

  const empezarRedimension = (lado: Lado) => (e: PointerEventReact<HTMLDivElement>) => {
    if (e.button !== 0 || estado !== 'normal' || movil || saliendo) return
    e.stopPropagation()
    sistema.enfocar(id)
    redimensionRef.current = { lado, px: e.clientX, py: e.clientY, x: ventana.x, y: ventana.y, w: ventana.w, h: ventana.h }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const moverRedimension = (e: PointerEventReact<HTMLDivElement>) => {
    const r = redimensionRef.current
    if (!r) return
    const dx = e.clientX - r.px
    const dy = e.clientY - r.py
    const min = def.minimo
    let { x, y, w, h } = r
    // El borde opuesto al que se tira queda fijo, también al topar con el
    // mínimo; el gestor vuelve a acotar al área.
    if (r.lado.includes('e')) w = Math.max(min.w, r.w + dx)
    if (r.lado === 'o' || r.lado.includes('w')) {
      w = Math.max(min.w, r.w - dx)
      x = r.x + r.w - w
    }
    if (r.lado.includes('s')) h = Math.max(min.h, r.h + dy)
    if (r.lado.includes('n')) {
      h = Math.max(min.h, r.h - dy)
      y = r.y + r.h - h
    }
    sistema.redimensionar(id, { x, y, w, h })
  }

  const terminarRedimension = (e: PointerEventReact<HTMLDivElement>) => {
    if (!redimensionRef.current) return
    redimensionRef.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      // Ya liberada.
    }
  }

  // ---- menús de pestaña y de historial

  // Clic derecho en una pestaña: lo mismo que ofrece un navegador.
  const menuPestana = (e: MouseEventReact<HTMLElement>, t: Pestana) => {
    e.preventDefault()
    e.stopPropagation()
    const p = paginaDe(t)
    setMenu({
      x: e.clientX,
      y: e.clientY,
      opciones: [
        { etiqueta: 'Nueva pestaña', accion: () => sistema.nuevaPestana(id) },
        { etiqueta: 'Duplicar', accion: () => sistema.abrirEnPestana(id, p.app, p.props, p.titulo) },
        { separador: true },
        { etiqueta: 'Cerrar', accion: () => cerrarPestana(t.id) },
        {
          etiqueta: 'Cerrar las demás',
          deshabilitado: pestanas.length <= 1,
          // Una a una: el gestor trabaja sobre un espejo, así que cada
          // llamada ve la lista ya sin la anterior.
          accion: () => { for (const otra of pestanas) if (otra.id !== t.id) sistema.cerrarPestana(id, otra.id) },
        },
      ],
    })
  }

  // El historial entero de la pestaña activa como menú, con la página
  // actual marcada: mantener pulsado ← o clic derecho en él.
  const menuHistorial = (x: number, y: number) => {
    setMenu({
      x,
      y,
      opciones: ventana.historial.map((p, k) => ({
        etiqueta: p.titulo, marcado: k === ventana.indice, accion: () => sistema.irAPagina(id, k),
      })),
    })
  }

  // ---- geometría a pintar

  // En móvil toda ventana visible ocupa el área entera, aunque su estado
  // guardado siga siendo el que era (al volver a una pantalla ancha se
  // recupera).
  const g = movil ? { x: 0, y: 0, w: area.w, h: area.h } : ventana
  const mitad = Math.floor(area.w / 2)
  const geometriaPrevia = previa === 'maximizada'
    ? { x: 0, y: 0, w: area.w, h: area.h }
    : previa === 'izquierda'
      ? { x: 0, y: 0, w: mitad, h: area.h }
      : { x: mitad, y: 0, w: area.w - mitad, h: area.h }

  // Con varias pestañas: dentro de la barra de título si la ventana es ancha
  // (como Windows 11: a la izquierda de los controles); si no, en una fila
  // propia debajo. Con una sola, ni lo uno ni lo otro.
  const variasPestanas = pestanas.length > 1
  const integradas = variasPestanas && !movil && g.w > ANCHO_PESTANAS_INTEGRADAS

  const clases = ['ventana', estado]
  if (activa) clases.push('activa')
  if (movil) clases.push('movil')
  if (saliendo) clases.push(saliendo)

  const tira = (
    <Pestanas ventana={ventana} sistema={sistema} onCerrar={cerrarPestana} onMenu={menuPestana} />
  )

  return (
    <>
      {previa && (
        <div
          className="ventana-previa"
          style={{ left: geometriaPrevia.x, top: geometriaPrevia.y, width: geometriaPrevia.w, height: geometriaPrevia.h, zIndex: ventana.z }}
        />
      )}
      <div
        ref={marcoRef}
        className={clases.join(' ')}
        role="dialog"
        aria-label={ventana.titulo}
        tabIndex={-1}
        hidden={estado === 'minimizada'}
        style={{ left: g.x, top: g.y, width: g.w, height: g.h, zIndex: ventana.z }}
        onPointerDown={() => { if (!activa) sistema.enfocar(id) }}
        onKeyDown={teclas}
      >
        <div
          className={`ventana-titulo${integradas ? ' con-pestanas' : ''}`}
          onPointerDown={empezarArrastre}
          onPointerMove={moverArrastre}
          onPointerUp={e => terminarArrastre(e, true)}
          onPointerCancel={e => terminarArrastre(e, false)}
          onDoubleClick={dobleClicTitulo}
        >
          {integradas ? tira : (
            <>
              <span className="ventana-icono" style={{ background: def.color }} aria-hidden="true">{def.icono}</span>
              <span className="ventana-nombre">{ventana.titulo}</span>
              {!variasPestanas && (
                <button type="button" className="ventana-mas" aria-label="Nueva pestaña" title="Nueva pestaña (Ctrl+T)"
                  onClick={() => sistema.nuevaPestana(id)}>
                  {ICONO_MAS}
                </button>
              )}
            </>
          )}
          <div className="ventana-botones">
            {/* La ayuda va en todas las ventanas, siempre en el mismo sitio:
                abre el recorrido de esta app (o el general si no tiene). */}
            <button type="button" className="ayuda" aria-label="Ayuda de esta ventana"
              title="Cómo funciona esta pantalla (tecla ?)" data-guia="ventana-ayuda"
              onClick={() => window.dispatchEvent(new CustomEvent('integra:guia', { detail: { clave: claveGuiaDe(ventana.app) } }))}>
              ?
            </button>
            <button type="button" aria-label="Minimizar" title="Minimizar" onClick={() => salir('minimizando')}>
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M2 6h8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
            </button>
            {!movil && (
              <button
                type="button"
                aria-label={estado === 'maximizada' ? 'Restaurar' : 'Maximizar'}
                title={estado === 'maximizada' ? 'Restaurar' : 'Maximizar'}
                onClick={alternarMaximizada}
              >
                {estado === 'maximizada'
                  ? <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M4 4V2.5h5.5V8H8M2.5 4h5.5v5.5H2.5z" fill="none" stroke="currentColor" strokeWidth="1.2" /></svg>
                  : <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><rect x="2" y="2" width="8" height="8" rx="1" fill="none" stroke="currentColor" strokeWidth="1.3" /></svg>}
              </button>
            )}
            <button type="button" className="cerrar" aria-label="Cerrar" title="Cerrar" onClick={() => salir('cerrando')}>
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
            </button>
          </div>
        </div>
        {variasPestanas && !integradas && <div className="ventana-pestanas-fila">{tira}</div>}
        <Direccion ventana={ventana} sistema={sistema} movil={movil} onHistorial={menuHistorial} />
        <div className="ventana-cuerpo">{children}</div>
        {estado === 'normal' && !movil && LADOS.map(lado => (
          <div
            key={lado}
            className={`ventana-asa ${lado}`}
            onPointerDown={empezarRedimension(lado)}
            onPointerMove={moverRedimension}
            onPointerUp={terminarRedimension}
            onPointerCancel={terminarRedimension}
          />
        ))}
      </div>
      {menu && <MenuContextual x={menu.x} y={menu.y} opciones={menu.opciones} onCerrar={() => setMenu(null)} />}
    </>
  )
}

// ---------------------------------------------------------------- pestañas

type ArrastrePestana = { pestanaId: string; px: number; movido: boolean }

// La tira de pestañas: una por pestaña de la ventana (icono de la app que
// enseña ahora, título recortado, × al pasar el ratón) y + al final. Se
// reordenan arrastrando con el puntero: mientras se arrastra, la pestaña se
// coloca en el hueco sobre el que está el puntero, como en un navegador.
function Pestanas({ ventana, sistema, onCerrar, onMenu }: {
  ventana: VentanaTipo
  sistema: Sistema
  onCerrar: (pestanaId: string) => void
  onMenu: (e: MouseEventReact<HTMLElement>, t: Pestana) => void
}) {
  const { id, pestanas, pestanaActiva } = ventana
  const tiraRef = useRef<HTMLDivElement>(null)
  const arrastreRef = useRef<ArrastrePestana | null>(null)
  const [arrastrando, setArrastrando] = useState<string | null>(null)

  const empezar = (t: Pestana) => (e: PointerEventReact<HTMLDivElement>) => {
    // La rueda no debe hacer scroll automático (cierra la pestaña, ver
    // onAuxClick); el × tiene su propio clic.
    if (e.button === 1) e.preventDefault()
    if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return
    // Se activa al pulsar, no al soltar: más inmediato, como en Chrome.
    sistema.activarPestana(id, t.id)
    arrastreRef.current = { pestanaId: t.id, px: e.clientX, movido: false }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const mover = (e: PointerEventReact<HTMLDivElement>) => {
    const a = arrastreRef.current
    const tira = tiraRef.current
    if (!a || !tira) return
    if (!a.movido) {
      if (Math.abs(e.clientX - a.px) < UMBRAL_ARRASTRE) return
      a.movido = true
      setArrastrando(a.pestanaId)
    }
    // Índice del hueco bajo el puntero: la primera pestaña cuyo centro
    // queda a la derecha del puntero; pasado el último centro, la última.
    const elementos = Array.from(tira.querySelectorAll<HTMLElement>('[role="tab"]'))
    let destino = elementos.length - 1
    for (let i = 0; i < elementos.length; i++) {
      const r = elementos[i].getBoundingClientRect()
      if (e.clientX < r.left + r.width / 2) {
        destino = i
        break
      }
    }
    const actual = pestanas.findIndex(t => t.id === a.pestanaId)
    if (destino !== actual) sistema.moverPestana(id, a.pestanaId, destino)
  }

  const terminar = (e: PointerEventReact<HTMLDivElement>) => {
    if (!arrastreRef.current) return
    arrastreRef.current = null
    setArrastrando(null)
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      // Ya liberada.
    }
  }

  return (
    <div ref={tiraRef} className="ventana-pestanas" role="tablist" aria-label="Pestañas de la ventana">
      {pestanas.map(t => {
        const p = paginaDe(t)
        const app = APPS[p.app]
        const esActiva = t.id === pestanaActiva
        return (
          <div
            key={t.id}
            role="tab"
            aria-selected={esActiva}
            tabIndex={-1}
            className={`ventana-pestana${esActiva ? ' activa' : ''}${arrastrando === t.id ? ' arrastrando' : ''}`}
            title={p.titulo}
            onPointerDown={empezar(t)}
            onPointerMove={mover}
            onPointerUp={terminar}
            onPointerCancel={terminar}
            onAuxClick={e => { if (e.button === 1) { e.preventDefault(); onCerrar(t.id) } }}
            onContextMenu={e => onMenu(e, t)}
          >
            <span className="ventana-icono" style={{ background: app.color }} aria-hidden="true">{app.icono}</span>
            <span className="ventana-pestana-nombre">{p.titulo}</span>
            <button type="button" className="ventana-pestana-cerrar" aria-label={`Cerrar ${p.titulo}`}
              title="Cerrar pestaña (Ctrl+W)" tabIndex={-1}
              onClick={e => { e.stopPropagation(); onCerrar(t.id) }}>
              {ICONO_CERRAR}
            </button>
          </div>
        )
      })}
      <button type="button" className="ventana-mas" aria-label="Nueva pestaña" title="Nueva pestaña (Ctrl+T)"
        onClick={() => sistema.nuevaPestana(id)}>
        {ICONO_MAS}
      </button>
    </div>
  )
}

// ------------------------------------------------------ barra de dirección

// ← → ↑ y las migas de la pestaña activa: una por página del historial,
// clicables; las de «adelante», atenuadas. A la derecha, abrir la página
// actual en otra pestaña. En móvil se queda en ← ↑ y la miga actual.
function Direccion({ ventana, sistema, movil, onHistorial }: {
  ventana: VentanaTipo
  sistema: Sistema
  movil: boolean
  onHistorial: (x: number, y: number) => void
}) {
  const { id, historial, indice } = ventana
  const pulsacionRef = useRef<number | null>(null)
  // Tras una pulsación larga que abrió el menú, el clic que llega al soltar
  // no debe ir atrás además.
  const suprimirRef = useRef(false)

  useEffect(() => () => {
    if (pulsacionRef.current !== null) window.clearTimeout(pulsacionRef.current)
  }, [])

  const desplegar = (el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    onHistorial(r.left, r.bottom + 4)
  }

  const empezarPulsacion = (e: PointerEventReact<HTMLButtonElement>) => {
    if (e.button !== 0) return
    const el = e.currentTarget
    pulsacionRef.current = window.setTimeout(() => {
      pulsacionRef.current = null
      suprimirRef.current = true
      desplegar(el)
    }, PULSACION_LARGA)
  }
  const cancelarPulsacion = () => {
    if (pulsacionRef.current === null) return
    window.clearTimeout(pulsacionRef.current)
    pulsacionRef.current = null
  }
  const clicAtras = () => {
    if (suprimirRef.current) {
      suprimirRef.current = false
      return
    }
    sistema.atras(id)
  }

  const migas = movil ? [historial[indice]] : historial

  return (
    <div className="ventana-direccion">
      <button type="button" aria-label="Atrás" title="Atrás (Alt+←). Mantener pulsado: historial"
        disabled={!puedeAtras(ventana)} onClick={clicAtras}
        onPointerDown={empezarPulsacion} onPointerUp={cancelarPulsacion}
        onPointerLeave={cancelarPulsacion} onPointerCancel={cancelarPulsacion}
        onContextMenu={e => { e.preventDefault(); desplegar(e.currentTarget) }}>
        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M8.5 2.5 4 7l4.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
      </button>
      {!movil && (
        <button type="button" aria-label="Adelante" title="Adelante (Alt+→)"
          disabled={!puedeAdelante(ventana)} onClick={() => sistema.adelante(id)}>
          <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="m5.5 2.5 4.5 4.5-4.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
        </button>
      )}
      <button type="button" aria-label="Subir" title="Volver al inicio de la pestaña (Alt+↑)"
        disabled={indice === 0} onClick={() => sistema.irAPagina(id, 0)}>
        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M7 11.5v-9M2.5 7 7 2.5 11.5 7" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
      </button>
      <nav className="ventana-migas" aria-label="Ruta">
        {migas.map((p, k) => {
          const real = movil ? indice : k
          const clase = real === indice ? 'actual' : real > indice ? 'adelante' : ''
          return (
            <Fragment key={p.clave}>
              {k > 0 && <span className="ventana-miga-sep" aria-hidden="true">›</span>}
              <button type="button" className={`ventana-miga ${clase}`} title={p.titulo}
                aria-current={real === indice ? 'page' : undefined}
                onClick={() => sistema.irAPagina(id, real)}>
                {p.titulo}
              </button>
            </Fragment>
          )
        })}
      </nav>
      {!movil && (
        <button type="button" className="ventana-en-pestana" aria-label="Nueva pestaña con esta página"
          title="Abrir esta página en una pestaña nueva"
          onClick={() => sistema.abrirEnPestana(id, ventana.app, ventana.props, ventana.titulo)}>
          <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M6 3H3.5A1.5 1.5 0 0 0 2 4.5v6A1.5 1.5 0 0 0 3.5 12h6a1.5 1.5 0 0 0 1.5-1.5V8M8.5 2H12v3.5M12 2 6.5 7.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></svg>
        </button>
      )}
    </div>
  )
}
