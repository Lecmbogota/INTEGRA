import { useEffect, useState } from 'react'
import type { Seccion } from './Sidebar'
import { PASOS } from './GuiaPasos'

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
// vacía, el menú plegado en el móvil), el paso se enseña centrado y sigue.

const MARGEN = 8
const ANCHO_GLOBO = 380

export function Guia({ seccion, irA, onCerrar }: {
  seccion: Seccion
  irA: (s: Seccion) => void
  onCerrar: () => void
}) {
  const [i, setI] = useState(0)
  const [rect, setRect] = useState<DOMRect | null>(null)
  const paso = PASOS[i]
  const ultimo = i === PASOS.length - 1

  // Ir a la sección del paso antes de buscar su elemento.
  useEffect(() => {
    if (paso.seccion && paso.seccion !== seccion) irA(paso.seccion)
  }, [i]) // eslint-disable-line react-hooks/exhaustive-deps

  // Localizar el elemento a resaltar. La sección acaba de cambiar y su
  // contenido puede tardar en pintarse (una lista que se pide al servidor),
  // así que se reintenta un rato; si no aparece, el paso va centrado.
  useEffect(() => {
    setRect(null)
    if (!paso.objetivo) return
    let vivo = true
    let intentos = 0
    const medir = () => {
      const el = document.querySelector<HTMLElement>(paso.objetivo!)
      if (el && vivo) setRect(el.getBoundingClientRect())
    }
    const buscar = () => {
      if (!vivo) return
      const el = document.querySelector<HTMLElement>(paso.objetivo!)
      if (el) {
        el.scrollIntoView({ block: 'center', behavior: 'smooth' })
        setTimeout(medir, 380)
        return
      }
      if (intentos++ < 25) setTimeout(buscar, 120)
    }
    buscar()
    window.addEventListener('resize', medir)
    window.addEventListener('scroll', medir, true)
    return () => {
      vivo = false
      window.removeEventListener('resize', medir)
      window.removeEventListener('scroll', medir, true)
    }
  }, [i, seccion]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCerrar()
      if (e.key === 'ArrowRight' || e.key === 'Enter') { e.preventDefault(); ultimo ? onCerrar() : setI(i + 1) }
      if (e.key === 'ArrowLeft' && i > 0) { e.preventDefault(); setI(i - 1) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [i, ultimo, onCerrar])

  // Dónde va el globo: debajo del elemento si cabe, si no encima; y dentro
  // de la ventana en horizontal. Sin elemento, en el centro.
  let estiloGlobo: React.CSSProperties
  let estiloFoco: React.CSSProperties | null = null
  const vw = window.innerWidth, vh = window.innerHeight
  const ancho = Math.min(ANCHO_GLOBO, vw - 24)
  if (rect) {
    const x = Math.max(4, rect.left - MARGEN)
    const y = Math.max(4, rect.top - MARGEN)
    const w = Math.min(vw - x - 4, rect.width + 2 * MARGEN)
    const h = Math.min(vh - y - 4, rect.height + 2 * MARGEN)
    estiloFoco = { left: x, top: y, width: w, height: h }
    const left = Math.max(12, Math.min(vw - ancho - 12, rect.left + rect.width / 2 - ancho / 2))
    const cabeDebajo = rect.bottom + 12 + 260 < vh
    estiloGlobo = cabeDebajo
      ? { left, top: Math.min(vh - 40, rect.bottom + 14), width: ancho }
      : { left, bottom: Math.max(12, vh - rect.top + 14), width: ancho }
  } else {
    estiloGlobo = { left: '50%', top: '50%', transform: 'translate(-50%, -50%)', width: ancho }
  }

  return (
    <div className="guia-capa" role="dialog" aria-modal="true" aria-label={paso.titulo}>
      {estiloFoco && <div className="guia-foco" style={estiloFoco} />}
      <div className={`guia-globo ${rect ? '' : 'centrado'}`} style={estiloGlobo}>
        <div className="guia-progreso">
          <span>Paso {i + 1} de {PASOS.length}</span>
          <span className="guia-puntos" aria-hidden="true">
            {PASOS.map((_, k) => <i key={k} className={k === i ? 'activo' : k < i ? 'hecho' : ''} />)}
          </span>
        </div>
        <h3>{paso.titulo}</h3>
        <div className="guia-texto">{paso.texto}</div>
        <div className="guia-acciones">
          <button type="button" className="enlace" onClick={onCerrar}>Saltar</button>
          <span className="crece" />
          {i > 0 && <button type="button" onClick={() => setI(i - 1)}>Anterior</button>}
          <button type="button" className="primario" onClick={() => (ultimo ? onCerrar() : setI(i + 1))}>
            {ultimo ? 'Terminar' : 'Siguiente'}
          </button>
        </div>
      </div>
    </div>
  )
}
