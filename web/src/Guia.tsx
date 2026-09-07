import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Seccion } from './Sidebar'
import type { Paso } from './GuiaPasos'

export type { Paso }

// Recorrido guiado sobre la interfaz real.
//
// No es una pantalla aparte con capturas: cada paso lleva a la sección que
// toca, resalta el elemento de verdad —el botón, el filtro, la lista— y lo
// explica al lado. Cuenta el flujo entero en el orden en que se trabaja:
// lo que llega de Odoo, lo que se completa en Integra, cómo se publica y
// cómo vuelven los pedidos. Habla en lenguaje de negocio: aquí no hay API,
// ni worker, ni base de datos.
//
// Arranca solo la primera vez que entra cada usuario y queda «Ver guía» en
// el menú para repetirlo. Si un elemento no está en pantalla (una tabla
// vacía, el menú plegado en el móvil), el paso se enseña centrado —o como
// hoja inferior en el móvil— y sigue.

// Aire alrededor del elemento resaltado.
const MARGEN_FOCO = 8
// Distancia entre el recuadro resaltado y el globo (cabe la flecha).
const SEPARACION = 14
// El globo nunca se acerca más que esto al borde de la ventana.
const MARGEN_VENTANA = 12
// A partir de esta fracción de la ventana un elemento es «alto»: no se
// intenta enseñarlo entero, se muestra su inicio y se recorta el foco.
const FRACCION_ALTO = 0.7
// Por debajo de este ancho el menú lateral es un cajón cerrado y las cosas
// se tocan con el dedo: el globo sin objetivo va como hoja inferior.
const ANCHO_MOVIL = 1024
// Cuánto se espera a que aparezca el elemento de un paso.
const ESPERA_APARECER = 3000
// Cuánto se espera si el elemento existe pero no se ve (el menú cerrado en
// el móvil): no va a aparecer solo, así que poco.
const ESPERA_INVISIBLE = 500
// Lo que tarda el desplazamiento suave hasta el elemento.
const ESPERA_SCROLL = 450

type Caja = { left: number; top: number; width: number; height: number }
type Lado = 'abajo' | 'arriba' | 'derecha' | 'izquierda' | 'superpuesto'
type Colocacion = { lado: Lado; left: number; top: number; flecha: number }

const limitar = (v: number, min: number, max: number) => Math.max(min, Math.min(max, v))

// El elemento existe, pero ¿se ve? El menú lateral cerrado en el móvil sigue
// en el DOM con visibility:hidden y desplazado fuera de pantalla; una tabla
// plegada lleva display:none. Nada de eso lo arregla un scrollIntoView.
function seVe(el: HTMLElement): boolean {
  if (typeof el.checkVisibility === 'function') return el.checkVisibility({ checkVisibilityCSS: true })
  return getComputedStyle(el).visibility !== 'hidden' && (el.offsetWidth > 0 || el.offsetHeight > 0)
}

// Recuadro del foco: el elemento con su margen, recortado a la ventana. Para
// un elemento más alto que la ventana el recorte es lo que hace que el marco
// se vea, en vez de un rectángulo que se pierde por arriba y por abajo.
function cajaFoco(r: DOMRect, vw: number, vh: number): Caja {
  const x1 = Math.max(4, r.left - MARGEN_FOCO)
  const y1 = Math.max(4, r.top - MARGEN_FOCO)
  const x2 = Math.min(vw - 4, r.right + MARGEN_FOCO)
  const y2 = Math.min(vh - 4, r.bottom + MARGEN_FOCO)
  return { left: x1, top: y1, width: Math.max(0, x2 - x1), height: Math.max(0, y2 - y1) }
}

// Dónde va el globo respecto al foco, con su tamaño real ya medido. Se prueba
// en orden debajo, encima, a la derecha y a la izquierda, y se queda el
// primer sitio donde cabe entero con el margen de ventana. Si no cabe en
// ninguno (un panel que ocupa la pantalla) se superpone al propio elemento,
// pegado a su esquina inferior derecha, que es donde menos tapa: la cabecera
// del panel queda a la vista. La flecha apunta al centro del foco, pero sin
// salirse de la arista del globo.
function colocar(foco: Caja, w: number, h: number, vw: number, vh: number): Colocacion {
  const derechaFoco = foco.left + foco.width
  const abajoFoco = foco.top + foco.height
  const cx = foco.left + foco.width / 2
  const cy = foco.top + foco.height / 2
  const leftCentrado = limitar(cx - w / 2, MARGEN_VENTANA, vw - w - MARGEN_VENTANA)
  const topCentrado = limitar(cy - h / 2, MARGEN_VENTANA, vh - h - MARGEN_VENTANA)
  const flechaX = (left: number) => limitar(cx - left, 22, w - 22)
  const flechaY = (top: number) => limitar(cy - top, 22, h - 22)

  if (abajoFoco + SEPARACION + h <= vh - MARGEN_VENTANA) {
    return { lado: 'abajo', left: leftCentrado, top: abajoFoco + SEPARACION, flecha: flechaX(leftCentrado) }
  }
  if (foco.top - SEPARACION - h >= MARGEN_VENTANA) {
    return { lado: 'arriba', left: leftCentrado, top: foco.top - SEPARACION - h, flecha: flechaX(leftCentrado) }
  }
  if (derechaFoco + SEPARACION + w <= vw - MARGEN_VENTANA) {
    return { lado: 'derecha', left: derechaFoco + SEPARACION, top: topCentrado, flecha: flechaY(topCentrado) }
  }
  if (foco.left - SEPARACION - w >= MARGEN_VENTANA) {
    return { lado: 'izquierda', left: foco.left - SEPARACION - w, top: topCentrado, flecha: flechaY(topCentrado) }
  }
  return {
    lado: 'superpuesto',
    left: limitar(derechaFoco - w - MARGEN_VENTANA, MARGEN_VENTANA, vw - w - MARGEN_VENTANA),
    top: limitar(abajoFoco - h - MARGEN_VENTANA, MARGEN_VENTANA, vh - h - MARGEN_VENTANA),
    flecha: 0,
  }
}

// El mismo motor sirve para el recorrido general y para el de cada pantalla
// o diálogo: se le pasan los pasos. Los recorridos que se abren dentro de un
// diálogo no navegan entre secciones, así que `seccion` e `irA` son
// opcionales; un paso con sección se ignora si no hay forma de ir.
export function Guia({ pasos, nombre, seccion, irA, onCerrar }: {
  pasos: Paso[]
  nombre?: string
  seccion?: Seccion
  irA?: (s: Seccion) => void
  onCerrar: () => void
}) {
  const [i, setI] = useState(0)
  const [rect, setRect] = useState<DOMRect | null>(null)
  // Mientras se busca el elemento el globo está montado pero invisible: hace
  // falta montarlo para medirlo, y no enseñarlo para que no se le vea saltar
  // desde donde estaba en el paso anterior.
  const [buscando, setBuscando] = useState(true)
  const [colocacion, setColocacion] = useState<Colocacion | null>(null)
  // Cambia cuando el globo cambia de tamaño o la ventana de ancho: obliga a
  // recolocar aunque el elemento no se haya movido.
  const [tic, setTic] = useState(0)
  const capaRef = useRef<HTMLDivElement>(null)
  const globoRef = useRef<HTMLDivElement>(null)
  const textoRef = useRef<HTMLDivElement>(null)
  const siguienteRef = useRef<HTMLButtonElement>(null)
  const paso = pasos[i]
  const ultimo = i === pasos.length - 1

  // Ir a la sección del paso antes de buscar su elemento.
  useEffect(() => {
    if (paso.seccion && irA && paso.seccion !== seccion) irA(paso.seccion)
  }, [i]) // eslint-disable-line react-hooks/exhaustive-deps

  // Localizar el elemento a resaltar. La sección acaba de cambiar y su
  // contenido puede tardar en pintarse (una lista que se pide al servidor),
  // así que se vigila el DOM un rato; si no aparece, el paso va sin foco.
  useEffect(() => {
    setRect(null)
    setBuscando(true)
    if (!paso.objetivo) { setBuscando(false); return }
    const selector = paso.objetivo
    let vivo = true
    let observador: MutationObserver | null = null
    let limite: number | undefined
    let limiteInvisible: number | undefined
    let esperaScroll: number | undefined
    // El elemento ya localizado: solo a ese se le sigue la pista al
    // desplazar o cambiar la ventana.
    let encontrado = false

    const parar = () => {
      observador?.disconnect()
      observador = null
      clearTimeout(limite)
      clearTimeout(limiteInvisible)
      clearTimeout(esperaScroll)
    }
    const rendirse = () => {
      if (!vivo) return
      parar()
      setRect(null)
      setBuscando(false)
    }
    const medir = () => {
      if (!vivo || !encontrado) return
      // Se vuelve a buscar por si la sección repintó y el nodo es otro.
      const el = document.querySelector<HTMLElement>(selector)
      if (el) setRect(el.getBoundingClientRect())
    }
    const mostrar = (el: HTMLElement) => {
      parar()
      encontrado = true
      const vh = window.innerHeight
      const r = el.getBoundingClientRect()
      const alto = r.height > vh * FRACCION_ALTO
      // Un elemento alto no cabe: basta con que se vea su inicio. Uno normal
      // se centra, salvo que ya esté a la vista, y entonces no se mueve nada.
      const yaBien = alto ? r.top >= 0 && r.top <= vh * 0.25 : r.top >= 0 && r.bottom <= vh
      if (yaBien) { setRect(r); setBuscando(false); return }
      el.scrollIntoView({ block: alto ? 'start' : 'center', behavior: 'smooth' })
      esperaScroll = window.setTimeout(() => {
        if (!vivo) return
        setRect(el.getBoundingClientRect())
        setBuscando(false)
      }, ESPERA_SCROLL)
    }
    const intentar = () => {
      if (!vivo || encontrado) return
      const el = document.querySelector<HTMLElement>(selector)
      if (!el) return
      if (!seVe(el)) {
        if (limiteInvisible === undefined) limiteInvisible = window.setTimeout(rendirse, ESPERA_INVISIBLE)
        return
      }
      mostrar(el)
    }

    intentar()
    if (!encontrado) {
      observador = new MutationObserver(intentar)
      observador.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'style', 'hidden'] })
      limite = window.setTimeout(rendirse, ESPERA_APARECER)
    }
    window.addEventListener('resize', medir)
    window.addEventListener('scroll', medir, true)
    return () => {
      vivo = false
      parar()
      window.removeEventListener('resize', medir)
      window.removeEventListener('scroll', medir, true)
    }
  }, [i, seccion]) // eslint-disable-line react-hooks/exhaustive-deps

  // Recolocar cuando el globo cambia de tamaño (otro texto, otra fuente) o
  // la ventana de ancho: en los pasos sin foco la clase del globo depende
  // del ancho, y sin elemento que medir nadie más provoca el repintado.
  useEffect(() => {
    const globo = globoRef.current
    const avisar = () => setTic((t) => t + 1)
    const ro = globo && typeof ResizeObserver === 'function' ? new ResizeObserver(avisar) : null
    if (globo) ro?.observe(globo)
    window.addEventListener('resize', avisar)
    return () => {
      ro?.disconnect()
      window.removeEventListener('resize', avisar)
    }
  }, [])

  // Con el elemento localizado y el globo pintado (aunque invisible), se mide
  // el globo de verdad y se decide dónde va, antes de que se vea.
  useLayoutEffect(() => {
    const globo = globoRef.current
    if (!globo || !rect || buscando) { setColocacion(null); return }
    const vw = window.innerWidth, vh = window.innerHeight
    setColocacion(colocar(cajaFoco(rect, vw, vh), globo.offsetWidth, globo.offsetHeight, vw, vh))
  }, [i, rect, buscando, tic])

  // Teclado: Escape cierra, las flechas navegan. Enter solo cuando el foco no
  // está en un botón, que entonces ya hace lo suyo (si no, «Anterior» con
  // Enter avanzaría). El tabulador da vueltas dentro del globo: detrás no hay
  // nada que se pueda usar.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { onCerrar(); return }
      if (e.key === 'ArrowRight') { e.preventDefault(); ultimo ? onCerrar() : setI(i + 1); return }
      if (e.key === 'ArrowLeft') { e.preventDefault(); if (i > 0) setI(i - 1); return }
      if (e.key === 'Enter' && (e.target as HTMLElement | null)?.tagName !== 'BUTTON') {
        e.preventDefault(); ultimo ? onCerrar() : setI(i + 1); return
      }
      if (e.key === 'Tab' && globoRef.current) {
        const nodos = globoRef.current.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
        if (!nodos.length) return
        const primero = nodos[0], final = nodos[nodos.length - 1]
        const activo = document.activeElement
        const fuera = !globoRef.current.contains(activo)
        if (e.shiftKey && (fuera || activo === primero)) { e.preventDefault(); final.focus() }
        else if (!e.shiftKey && (fuera || activo === final)) { e.preventDefault(); primero.focus() }
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [i, ultimo, onCerrar])

  // Al enseñar cada paso, el foco de teclado va a «Siguiente»: Enter o
  // espacio avanzan, y el lector de pantalla entra por el diálogo.
  useEffect(() => {
    if (!buscando) siguienteRef.current?.focus({ preventScroll: true })
  }, [i, buscando])

  // La rueda y el dedo sobre la capa no desplazan el fondo: el elemento
  // resaltado se iría de debajo del foco. Solo se deja desplazar el texto
  // del globo, y solo cuando de verdad tiene más de lo que cabe. React
  // registra estos eventos como pasivos, por eso se hace a mano.
  useEffect(() => {
    const capa = capaRef.current
    if (!capa) return
    const bloquear = (e: Event) => {
      const texto = textoRef.current
      const dentro = texto && texto.contains(e.target as Node) && texto.scrollHeight > texto.clientHeight
      if (!dentro) e.preventDefault()
    }
    capa.addEventListener('wheel', bloquear, { passive: false })
    capa.addEventListener('touchmove', bloquear, { passive: false })
    return () => {
      capa.removeEventListener('wheel', bloquear)
      capa.removeEventListener('touchmove', bloquear)
    }
  }, [])

  const vw = window.innerWidth, vh = window.innerHeight
  const movil = vw < ANCHO_MOVIL
  const foco = rect && !buscando ? cajaFoco(rect, vw, vh) : null
  // Cómo se enseña el globo: anclado al foco, centrado, o como hoja inferior
  // en el móvil cuando no hay nada que resaltar.
  const modo = buscando ? 'buscando' : foco ? 'anclado' : movil ? 'hoja' : 'centrado'
  const visible = modo === 'centrado' || modo === 'hoja' || (modo === 'anclado' && colocacion !== null)
  const claseGlobo = [
    'guia-globo',
    modo === 'anclado' ? (colocacion?.lado ?? '') : modo === 'buscando' ? '' : modo,
    visible ? '' : 'oculto',
  ].filter(Boolean).join(' ')
  const estiloGlobo: React.CSSProperties = modo === 'anclado' && colocacion
    ? { left: colocacion.left, top: colocacion.top, '--flecha': `${colocacion.flecha}px` } as React.CSSProperties
    : {}
  const progreso = `${Math.round(((i + 1) / pasos.length) * 100)}%`

  return (
    <div ref={capaRef} className={`guia-capa ${foco ? '' : 'sin-foco'}`} role="dialog" aria-modal="true" aria-labelledby="guia-titulo">
      {foco && <div className="guia-foco" style={foco} />}
      <div ref={globoRef} className={claseGlobo} style={estiloGlobo}>
        <div className="guia-progreso">
          <span>{nombre ? `${nombre} · ` : ''}Paso {i + 1} de {pasos.length}</span>
          <span className="guia-puntos" aria-hidden="true">
            {pasos.map((_, k) => <i key={k} className={k === i ? 'activo' : k < i ? 'hecho' : ''} />)}
          </span>
          <span className="guia-barra" aria-hidden="true"><i style={{ width: progreso }} /></span>
        </div>
        <h3 id="guia-titulo" aria-live="polite">{paso.titulo}</h3>
        <div ref={textoRef} className="guia-texto">{paso.texto}</div>
        <div className="guia-acciones">
          <button type="button" className="enlace" onClick={onCerrar}>Saltar</button>
          <span className="crece" />
          {i > 0 && <button type="button" onClick={() => setI(i - 1)}>Anterior</button>}
          <button ref={siguienteRef} type="button" className="primario" onClick={() => (ultimo ? onCerrar() : setI(i + 1))}>
            {ultimo ? 'Terminar' : 'Siguiente'}
          </button>
        </div>
      </div>
    </div>
  )
}
