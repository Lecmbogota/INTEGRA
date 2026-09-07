import {
  useEffect, useRef, useState, type KeyboardEvent as KeyboardEventReact, type MouseEvent as MouseEventReact,
  type PointerEvent as PointerEventReact, type ReactNode,
} from 'react'
import type { Ventana as VentanaTipo } from './tipos'
import { APPS } from './apps'
import { puedeAdelante, puedeAtras, useSistema } from './sistema'
import './ventanas.css'

// Marco de una ventana: barra de título, mover, redimensionar, botones. No
// sabe nada de la app que contiene; solo pide cambios al gestor (useSistema)
// y pinta la geometría que este le da. La geometría de la ventana (x, y, w, h)
// es siempre la real, también maximizada o ajustada: el gestor la recalcula.
// El icono y el título son los de la página actual del historial: al
// navegar de Productos a un editor, la barra lo dice.

// Píxeles que hay que mover el puntero antes de considerar que se arrastra:
// evita que un clic en la barra de título desplace la ventana un píxel.
const UMBRAL_ARRASTRE = 3
// Distancia al borde del área a la que se ofrece maximizar / ajustar.
const MARGEN_BORDE = 6
// Duración de las animaciones de salida; la misma que en ventanas.css.
const DURACION_SALIDA = 120

type Lado = 'n' | 's' | 'e' | 'o' | 'ne' | 'nw' | 'se' | 'sw'
const LADOS: Lado[] = ['n', 's', 'e', 'o', 'ne', 'nw', 'se', 'sw']
type Previa = 'maximizada' | 'izquierda' | 'derecha' | null
type Salida = 'cerrando' | 'minimizando' | null

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

export function Ventana({ ventana, children }: { ventana: VentanaTipo; children: ReactNode }) {
  const sistema = useSistema()
  const def = APPS[ventana.app]
  const { area, movil } = sistema
  const activa = sistema.activa === ventana.id
  const { id, estado } = ventana

  const marcoRef = useRef<HTMLDivElement>(null)
  const arrastreRef = useRef<Arrastre | null>(null)
  const redimensionRef = useRef<Redimension | null>(null)
  const previaRef = useRef<Previa>(null)
  const [previa, setPrevia] = useState<Previa>(null)
  const [saliendo, setSaliendo] = useState<Salida>(null)
  const temporizadorRef = useRef<number | null>(null)

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

  // Alt+← / Alt+→ con el foco dentro de la ventana recorren su historial,
  // como en un navegador (y se le quita al navegador, que con Alt+← se iría
  // de Integra). Los eventos de las páginas suben hasta el marco.
  const atrasAdelante = (e: KeyboardEventReact<HTMLDivElement>) => {
    if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
    if (e.key === 'ArrowLeft' && puedeAtras(ventana)) sistema.atras(id)
    else if (e.key === 'ArrowRight' && puedeAdelante(ventana)) sistema.adelante(id)
    else return
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
    if ((e.target as HTMLElement).closest('button')) return
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
    if (movil || (e.target as HTMLElement).closest('button')) return
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

  const clases = ['ventana', estado]
  if (activa) clases.push('activa')
  if (movil) clases.push('movil')
  if (saliendo) clases.push(saliendo)

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
        onKeyDown={atrasAdelante}
      >
        <div
          className="ventana-titulo"
          onPointerDown={empezarArrastre}
          onPointerMove={moverArrastre}
          onPointerUp={e => terminarArrastre(e, true)}
          onPointerCancel={e => terminarArrastre(e, false)}
          onDoubleClick={dobleClicTitulo}
        >
          {/* Historial de la ventana: lo que se abrió desde aquí (una previa,
              un editor) se ve en esta misma ventana, y estos vuelven. */}
          <div className="ventana-historial">
            <button type="button" aria-label="Atrás" title="Atrás (Alt+←)"
              disabled={!puedeAtras(ventana)} onClick={() => sistema.atras(id)}>
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M7.5 2 3.5 6l4 4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
            </button>
            <button type="button" aria-label="Adelante" title="Adelante (Alt+→)"
              disabled={!puedeAdelante(ventana)} onClick={() => sistema.adelante(id)}>
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m4.5 2 4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
            </button>
          </div>
          <span className="ventana-icono" style={{ background: def.color }} aria-hidden="true">{def.icono}</span>
          <span className="ventana-nombre">{ventana.titulo}</span>
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
    </>
  )
}
