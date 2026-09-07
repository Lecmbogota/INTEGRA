import { useCallback, useEffect, useRef, useState } from 'react'
import { api, num } from './api'

// Editor de fotos. Lo justo para dejar una foto como la piden los canales sin
// salir de Integra: recortar, girar, enderezar el color, quitar el borde
// blanco sobrante, encajarla en un lienzo cuadrado con fondo blanco y margen,
// y exportarla a JPEG del tamaño correcto. Todo pasa en el navegador sobre la
// foto original; al servidor solo viaja el resultado.
//
// Lo que se guarda es una imagen nueva del banco. «Reemplazar» la pone en el
// lugar de la original en todos los productos que la usaban —misma posición,
// misma portada— y borra la vieja; «como nueva» la añade detrás y conserva
// la original, para cuando se quieren las dos.

export type FotoEditable = { id: number; sha256: string; ancho: number; alto: number; formato: string }

type Rect = { x: number; y: number; w: number; h: number }
type Proporcion = 'libre' | '1:1' | '4:3' | '3:4' | '16:9'
type Formato = 'image/jpeg' | 'image/png'
type Asa = 'mover' | 'nw' | 'ne' | 'sw' | 'se'

const PROPORCIONES: [Proporcion, string][] = [
  ['libre', 'Libre'], ['1:1', 'Cuadrado'], ['4:3', '4:3'], ['3:4', '3:4'], ['16:9', '16:9'],
]
const LADOS = [800, 1000, 1200, 1600, 2000]
const ANCHO_TRABAJO = 560

function razon(p: Proporcion): number | null {
  switch (p) {
    case '1:1': return 1
    case '4:3': return 4 / 3
    case '3:4': return 3 / 4
    case '16:9': return 16 / 9
    default: return null
  }
}

function tamano(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

function lienzo(w: number, h: number): HTMLCanvasElement {
  const c = document.createElement('canvas')
  c.width = Math.max(1, Math.round(w))
  c.height = Math.max(1, Math.round(h))
  return c
}

// orientar aplica giro y volteo. Es el primer paso porque el recorte se
// dibuja sobre la imagen ya orientada: nadie recorta una foto tumbada.
function orientar(img: HTMLImageElement, rot: number, flipH: boolean, flipV: boolean): HTMLCanvasElement {
  const girada = rot % 2 === 1
  const c = lienzo(girada ? img.naturalHeight : img.naturalWidth, girada ? img.naturalWidth : img.naturalHeight)
  const ctx = c.getContext('2d')!
  ctx.translate(c.width / 2, c.height / 2)
  ctx.rotate((rot * Math.PI) / 2)
  ctx.scale(flipH ? -1 : 1, flipV ? -1 : 1)
  ctx.drawImage(img, -img.naturalWidth / 2, -img.naturalHeight / 2)
  return c
}

function recortar(c: HTMLCanvasElement, r: Rect): HTMLCanvasElement {
  const out = lienzo(r.w, r.h)
  out.getContext('2d')!.drawImage(c, r.x, r.y, r.w, r.h, 0, 0, out.width, out.height)
  return out
}

// bordesBlancos busca el rectángulo que contiene lo que no es fondo blanco
// (ni transparente). Se mira una copia reducida: recorrer los 35 millones de
// píxeles de una foto de 7000×5000 tardaría segundos por cada ajuste.
function bordesBlancos(c: HTMLCanvasElement, umbral = 240): Rect {
  const esc = Math.min(1, 1000 / Math.max(c.width, c.height))
  const w = Math.max(1, Math.round(c.width * esc))
  const h = Math.max(1, Math.round(c.height * esc))
  const t = lienzo(w, h)
  const ctx = t.getContext('2d')!
  ctx.drawImage(c, 0, 0, w, h)
  const d = ctx.getImageData(0, 0, w, h).data
  let x0 = w, y0 = h, x1 = -1, y1 = -1
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = (y * w + x) * 4
      if (d[i + 3] < 16) continue
      if (d[i] < umbral || d[i + 1] < umbral || d[i + 2] < umbral) {
        if (x < x0) x0 = x
        if (x > x1) x1 = x
        if (y < y0) y0 = y
        if (y > y1) y1 = y
      }
    }
  }
  if (x1 < 0) return { x: 0, y: 0, w: c.width, h: c.height }
  const x = Math.max(0, Math.floor(x0 / esc) - 1)
  const y = Math.max(0, Math.floor(y0 / esc) - 1)
  return {
    x, y,
    w: Math.min(c.width - x, Math.ceil((x1 - x0 + 1) / esc) + 2),
    h: Math.min(c.height - y, Math.ceil((y1 - y0 + 1) / esc) + 2),
  }
}

function filtrar(c: HTMLCanvasElement, brillo: number, contraste: number, saturacion: number): HTMLCanvasElement {
  if (brillo === 100 && contraste === 100 && saturacion === 100) return c
  const out = lienzo(c.width, c.height)
  const ctx = out.getContext('2d')!
  ctx.filter = `brightness(${brillo}%) contrast(${contraste}%) saturate(${saturacion}%)`
  ctx.drawImage(c, 0, 0)
  return out
}

type Encaje = {
  cuadrado: boolean; lado: number; margen: number; fondo: string; ampliar: boolean; rellenar: boolean
}

// encajar decide el tamaño final. En lienzo cuadrado la foto se centra sobre
// un fondo del lado pedido con su margen, que es lo que MercadoLibre y
// Falabella enseñan mejor. Sin lienzo, solo se reduce si es más grande que
// el lado pedido; ampliar una foto pequeña no le añade detalle y se deja
// como decisión explícita.
function encajar(c: HTMLCanvasElement, e: Encaje): HTMLCanvasElement {
  if (e.cuadrado) {
    const out = lienzo(e.lado, e.lado)
    const ctx = out.getContext('2d')!
    ctx.fillStyle = e.fondo
    ctx.fillRect(0, 0, e.lado, e.lado)
    const util = e.lado * (1 - (2 * e.margen) / 100)
    let esc = Math.min(util / c.width, util / c.height)
    if (!e.ampliar) esc = Math.min(esc, 1)
    const w = c.width * esc, h = c.height * esc
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(c, (e.lado - w) / 2, (e.lado - h) / 2, w, h)
    return out
  }
  const mayor = Math.max(c.width, c.height)
  let esc = e.lado / mayor
  if (!e.ampliar) esc = Math.min(esc, 1)
  const out = lienzo(c.width * esc, c.height * esc)
  const ctx = out.getContext('2d')!
  if (e.rellenar) {
    ctx.fillStyle = e.fondo
    ctx.fillRect(0, 0, out.width, out.height)
  }
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(c, 0, 0, out.width, out.height)
  return out
}

function aBlob(c: HTMLCanvasElement, formato: Formato, calidad: number): Promise<Blob> {
  return new Promise((res, rej) => {
    c.toBlob((b) => (b ? res(b) : rej(new Error('no se pudo codificar la imagen'))), formato, calidad / 100)
  })
}

function limitar(r: Rect, W: number, H: number, min = 16): Rect {
  const w = Math.max(min, Math.min(r.w, W))
  const h = Math.max(min, Math.min(r.h, H))
  return {
    x: Math.max(0, Math.min(r.x, W - w)),
    y: Math.max(0, Math.min(r.y, H - h)),
    w, h,
  }
}

export function EditorFoto({ foto, onCerrar, onGuardada }: {
  foto: FotoEditable
  onCerrar: () => void
  onGuardada: (modo: 'reemplazar' | 'nueva') => void
}) {
  const [fuente, setFuente] = useState<HTMLImageElement | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [rot, setRot] = useState(0)
  const [flipH, setFlipH] = useState(false)
  const [flipV, setFlipV] = useState(false)
  const [recorte, setRecorte] = useState<Rect | null>(null)
  const [proporcion, setProporcion] = useState<Proporcion>('libre')
  const [brillo, setBrillo] = useState(100)
  const [contraste, setContraste] = useState(100)
  const [saturacion, setSaturacion] = useState(100)
  const [quitarBlancos, setQuitarBlancos] = useState(false)
  const [cuadrado, setCuadrado] = useState(true)
  const [lado, setLado] = useState(1200)
  const [margen, setMargen] = useState(5)
  const [fondo, setFondo] = useState('#ffffff')
  const [ampliar, setAmpliar] = useState(false)
  const [formato, setFormato] = useState<Formato>('image/jpeg')
  const [calidad, setCalidad] = useState(88)
  const [modo, setModo] = useState<'reemplazar' | 'nueva'>('reemplazar')
  const [guardando, setGuardando] = useState(false)

  const [resultado, setResultado] = useState<{ ancho: number; alto: number; bytes: number } | null>(null)
  // Guías sobre el resultado: el área útil que deja el margen, los tercios
  // y el contorno del producto detectado. Y cuánto del lienzo ocupa el
  // producto, que es lo que decide si la ficha se ve grande o perdida.
  const [guias, setGuias] = useState(true)
  const [ocupacion, setOcupacion] = useState<number | null>(null)
  const lienzoTrabajo = useRef<HTMLCanvasElement>(null)
  const lienzoResultado = useRef<HTMLCanvasElement>(null)
  const orientada = useRef<HTMLCanvasElement | null>(null)
  const [escala, setEscala] = useState(1)
  const arrastre = useRef<{ asa: Asa; x0: number; y0: number; inicio: Rect } | null>(null)

  // La foto original, entera: editar sobre la miniatura tiraría resolución.
  useEffect(() => {
    const img = new Image()
    img.onload = () => setFuente(img)
    img.onerror = () => setError('No se pudo cargar la foto original.')
    img.src = `/imagenes/${foto.sha256}`
  }, [foto.sha256])

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !guardando) onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar, guardando])

  // Orientar es caro en fotos grandes: se hace una vez por giro y se guarda.
  useEffect(() => {
    if (!fuente) return
    orientada.current = orientar(fuente, rot, flipH, flipV)
    const o = orientada.current
    setRecorte(null)
    const esc = Math.min(1, ANCHO_TRABAJO / o.width, 420 / o.height)
    setEscala(esc)
    const c = lienzoTrabajo.current
    if (c) {
      c.width = Math.round(o.width * esc)
      c.height = Math.round(o.height * esc)
      const ctx = c.getContext('2d')!
      ctx.imageSmoothingQuality = 'high'
      ctx.drawImage(o, 0, 0, c.width, c.height)
    }
  }, [fuente, rot, flipH, flipV])

  const procesar = useCallback((): HTMLCanvasElement | null => {
    const o = orientada.current
    if (!o) return null
    let c: HTMLCanvasElement = recorte ? recortar(o, recorte) : o
    if (quitarBlancos) c = recortar(c, bordesBlancos(c))
    c = filtrar(c, brillo, contraste, saturacion)
    return encajar(c, { cuadrado, lado, margen, fondo, ampliar, rellenar: formato === 'image/jpeg' })
  }, [recorte, quitarBlancos, brillo, contraste, saturacion, cuadrado, lado, margen, fondo, ampliar, formato])

  // La vista del resultado se recalcula con retraso: mover un deslizador
  // dispara decenas de cambios y cada uno es un repintado de la foto entera.
  useEffect(() => {
    if (!fuente) return
    let vigente = true
    const t = setTimeout(() => {
      const c = procesar()
      const v = lienzoResultado.current
      if (!c || !v) return
      const esc = Math.min(1, 280 / c.width, 280 / c.height)
      v.width = Math.round(c.width * esc)
      v.height = Math.round(c.height * esc)
      const ctx = v.getContext('2d')!
      ctx.imageSmoothingQuality = 'high'
      ctx.drawImage(c, 0, 0, v.width, v.height)

      // Dónde quedó el producto dentro del resultado.
      const caja = bordesBlancos(c)
      setOcupacion(Math.max(caja.w / c.width, caja.h / c.height))

      if (guias) {
        ctx.save()
        // Tercios: la referencia clásica para centrar y equilibrar.
        ctx.strokeStyle = 'rgba(0,0,0,.18)'
        ctx.lineWidth = 1
        for (let k = 1; k <= 2; k++) {
          ctx.beginPath(); ctx.moveTo((v.width * k) / 3, 0); ctx.lineTo((v.width * k) / 3, v.height); ctx.stroke()
          ctx.beginPath(); ctx.moveTo(0, (v.height * k) / 3); ctx.lineTo(v.width, (v.height * k) / 3); ctx.stroke()
        }
        // Área útil: lo que deja el margen. Es donde debería vivir el producto.
        if (cuadrado) {
          const m = (v.width * margen) / 100
          ctx.setLineDash([6, 4])
          ctx.strokeStyle = 'rgba(37,99,235,.9)'
          ctx.lineWidth = 1.5
          ctx.strokeRect(m + 0.5, m + 0.5, v.width - 2 * m - 1, v.height - 2 * m - 1)
        }
        // Contorno del producto detectado: verde si llena bien, naranja si no.
        const bien = Math.max(caja.w / c.width, caja.h / c.height) >= 0.7
        ctx.setLineDash([4, 3])
        ctx.strokeStyle = bien ? 'rgba(22,163,74,.95)' : 'rgba(217,119,6,.95)'
        ctx.lineWidth = 1.5
        ctx.strokeRect(caja.x * esc + 0.5, caja.y * esc + 0.5, caja.w * esc - 1, caja.h * esc - 1)
        ctx.restore()
      }
      aBlob(c, formato, calidad)
        .then((b) => { if (vigente) setResultado({ ancho: c.width, alto: c.height, bytes: b.size }) })
        .catch(() => { if (vigente) setResultado({ ancho: c.width, alto: c.height, bytes: 0 }) })
    }, 200)
    return () => { vigente = false; clearTimeout(t) }
  }, [fuente, procesar, formato, calidad, guias, cuadrado, margen])

  // ---- recorte con el ratón sobre el lienzo de trabajo

  const dims = () => {
    const o = orientada.current
    return o ? { W: o.width, H: o.height } : { W: 1, H: 1 }
  }

  function rectActual(): Rect {
    const { W, H } = dims()
    return recorte ?? { x: 0, y: 0, w: W, h: H }
  }

  function empezar(asa: Asa, e: React.PointerEvent) {
    e.preventDefault()
    e.stopPropagation()
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    arrastre.current = { asa, x0: e.clientX, y0: e.clientY, inicio: rectActual() }
  }

  function mover(e: React.PointerEvent) {
    const a = arrastre.current
    if (!a) return
    const { W, H } = dims()
    const dx = (e.clientX - a.x0) / escala
    const dy = (e.clientY - a.y0) / escala
    const r = { ...a.inicio }
    const rz = razon(proporcion)
    if (a.asa === 'mover') {
      r.x += dx; r.y += dy
    } else {
      // Cada asa mueve su esquina; con proporción fija, el alto sigue al ancho.
      if (a.asa === 'ne' || a.asa === 'se') r.w += dx
      if (a.asa === 'nw' || a.asa === 'sw') { r.x += dx; r.w -= dx }
      if (a.asa === 'sw' || a.asa === 'se') r.h += dy
      if (a.asa === 'nw' || a.asa === 'ne') { r.y += dy; r.h -= dy }
      if (rz) {
        const h = r.w / rz
        if (a.asa === 'nw' || a.asa === 'ne') r.y += r.h - h
        r.h = h
      }
      if (r.w < 16) r.w = 16
      if (r.h < 16) r.h = 16
    }
    setRecorte(limitar(r, W, H))
  }

  function soltar() { arrastre.current = null }

  function recorteCentrado(p: Proporcion) {
    setProporcion(p)
    const { W, H } = dims()
    const rz = razon(p)
    if (!rz) return
    let w = W, h = W / rz
    if (h > H) { h = H; w = H * rz }
    setRecorte({ x: (W - w) / 2, y: (H - h) / 2, w, h })
  }

  function reiniciar() {
    setRot(0); setFlipH(false); setFlipV(false); setRecorte(null); setProporcion('libre')
    setBrillo(100); setContraste(100); setSaturacion(100); setQuitarBlancos(false)
    setCuadrado(true); setLado(1200); setMargen(5); setFondo('#ffffff'); setAmpliar(false)
    setFormato('image/jpeg'); setCalidad(88)
  }

  async function guardar() {
    const c = procesar()
    if (!c) return
    setGuardando(true)
    setError(null)
    try {
      const blob = await aBlob(c, formato, calidad)
      const ext = formato === 'image/png' ? 'png' : 'jpg'
      const archivo = new File([blob], `${foto.sha256.slice(0, 10)}-editada.${ext}`, { type: formato })
      await api.guardarEdicion(foto.id, archivo, modo)
      onGuardada(modo)
    } catch (e) {
      setError(`No se pudo guardar: ${e instanceof Error ? e.message : String(e)}`)
      setGuardando(false)
    }
  }

  // Lo que dirían los canales de la foto resultante, antes de guardarla.
  const avisos: { texto: string; malo: boolean }[] = []
  if (resultado) {
    const menor = Math.min(resultado.ancho, resultado.alto)
    if (menor < 600) avisos.push({ texto: `Lado menor de ${resultado.ancho}×${resultado.alto}: por debajo de 600 px MercadoLibre la rechaza`, malo: true })
    else if (menor < 1200) avisos.push({ texto: 'Vale para publicar; 1200 px de lado se ve mejor en MercadoLibre y Falabella', malo: false })
    else avisos.push({ texto: 'Tamaño correcto para los cuatro canales', malo: false })
    if (resultado.ancho !== resultado.alto) avisos.push({ texto: 'No es cuadrada: en MercadoLibre y Falabella la ficha se ve con bandas', malo: false })
    if (formato === 'image/png') avisos.push({ texto: 'PNG pesa más y Falabella prefiere JPEG; úsalo solo si hace falta transparencia', malo: false })
    if (resultado.bytes > 3 * 1024 * 1024) avisos.push({ texto: 'Pesa más de 3 MB: baja la calidad o el lado', malo: true })
  }
  // Cuánto del lienzo ocupa el producto. Los marketplaces enseñan mejor la
  // ficha cuando el producto llena entre el 75 y el 90 %: menos se ve
  // perdido en blanco, más se pega a los bordes y la miniatura lo corta.
  if (ocupacion !== null) {
    const pct = Math.round(ocupacion * 100)
    if (ocupacion < 0.6) avisos.push({ texto: `El producto ocupa el ${pct} % del lienzo: se ve pequeño. Baja el margen o recorta más cerca (lo ideal es 75–90 %)`, malo: true })
    else if (ocupacion < 0.75) avisos.push({ texto: `El producto ocupa el ${pct} %; podría verse algo más grande (lo ideal es 75–90 %)`, malo: false })
    else if (ocupacion > 0.95) avisos.push({ texto: `El producto ocupa el ${pct} %: toca los bordes y la miniatura lo cortará. Sube el margen`, malo: true })
    else avisos.push({ texto: `El producto ocupa el ${pct} % del lienzo: bien encuadrado`, malo: false })
  }

  const { W, H } = dims()
  const r = rectActual()
  const caja = { left: r.x * escala, top: r.y * escala, width: r.w * escala, height: r.h * escala }

  return (
    <div className="capa" onClick={() => { if (!guardando) onCerrar() }}>
      <div className="hoja hoja-editor hoja-ancha" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Editar foto</h2>
            <div className="sub">Original {foto.ancho}×{foto.alto} · {foto.formato}</div>
          </div>
          <button onClick={onCerrar} disabled={guardando}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">{error}</div>}
        {!fuente && !error && <div className="vacio">Cargando la foto original…</div>}

        {fuente && (
          <div className="editor">
            <div>
              {/* Lienzo de trabajo: la foto orientada, con el recorte encima. */}
              <div className="editor-lienzo" style={{ width: Math.round(W * escala), height: Math.round(H * escala) }}
                onPointerMove={mover} onPointerUp={soltar} onPointerCancel={soltar}>
                <canvas ref={lienzoTrabajo} />
                <div className="recorte-caja" style={caja} onPointerDown={(e) => empezar('mover', e)}>
                  {(['nw', 'ne', 'sw', 'se'] as Asa[]).map((a) => (
                    <span key={a} className={`recorte-asa ${a}`} onPointerDown={(e) => empezar(a, e)} />
                  ))}
                </div>
              </div>

              <div className="filtros" style={{ marginTop: 10 }}>
                <span className="tenue">Recorte:</span>
                <div className="grupo-badges">
                  {PROPORCIONES.map(([id, nombre]) => (
                    <button key={id} type="button" className={`badge ${proporcion === id ? 'activo' : ''}`}
                      onClick={() => recorteCentrado(id)}>{nombre}</button>
                  ))}
                </div>
                <button type="button" className="enlace" onClick={() => { setRecorte(null); setProporcion('libre') }}>Todo</button>
                <span className="tenue mini-texto">
                  {Math.round(r.w)}×{Math.round(r.h)} px
                </span>
              </div>

              <div className="filtros">
                <span className="tenue">Girar:</span>
                <button type="button" onClick={() => setRot((v) => (v + 3) % 4)} title="90° a la izquierda">↺ 90°</button>
                <button type="button" onClick={() => setRot((v) => (v + 1) % 4)} title="90° a la derecha">↻ 90°</button>
                <button type="button" onClick={() => setFlipH((v) => !v)}>Voltear ↔</button>
                <button type="button" onClick={() => setFlipV((v) => !v)}>Voltear ↕</button>
                <label className="casilla" title="Recorta el borde blanco o transparente que sobra alrededor del producto">
                  <input type="checkbox" checked={quitarBlancos} onChange={(e) => setQuitarBlancos(e.target.checked)} />
                  Quitar bordes blancos
                </label>
              </div>

              <div className="editor-deslizadores">
                <Deslizador nombre="Brillo" valor={brillo} onCambio={setBrillo} />
                <Deslizador nombre="Contraste" valor={contraste} onCambio={setContraste} />
                <Deslizador nombre="Saturación" valor={saturacion} onCambio={setSaturacion} />
              </div>
            </div>

            <div className="editor-controles">
              <div className="editor-resultado">
                <canvas ref={lienzoResultado} />
                {resultado && (
                  <div className="tenue mini-texto">
                    Resultado: {resultado.ancho}×{resultado.alto} · {tamano(resultado.bytes)} · {formato === 'image/png' ? 'PNG' : 'JPEG'}
                  </div>
                )}
                <label className="casilla mini-texto" style={{ justifyContent: 'center', marginTop: 4 }}
                  title="Tercios, área útil que deja el margen (azul) y contorno del producto detectado (verde si llena bien, naranja si no)">
                  <input type="checkbox" checked={guias} onChange={(e) => setGuias(e.target.checked)} />
                  Guías de encuadre
                </label>
                <ul className="lista-faltantes" style={{ marginTop: 6 }}>
                  {avisos.map((a, i) => <li key={i} className={a.malo ? 'mal' : ''} style={{ color: a.malo ? 'var(--error)' : 'var(--ok)' }}>{a.texto}</li>)}
                </ul>
              </div>

              <label className="casilla" title="Centra la foto en un cuadrado con fondo y margen: es como mejor se ve en MercadoLibre y Falabella">
                <input type="checkbox" checked={cuadrado} onChange={(e) => setCuadrado(e.target.checked)} />
                Lienzo cuadrado con fondo
              </label>
              <div className="filtros">
                <span className="tenue">Lado:</span>
                <div className="grupo-badges">
                  {LADOS.map((l) => (
                    <button key={l} type="button" className={`badge ${lado === l ? 'activo' : ''}`} onClick={() => setLado(l)}>{num(l)}</button>
                  ))}
                </div>
              </div>
              {cuadrado && (
                <>
                  <Deslizador nombre="Margen" valor={margen} min={0} max={25} unidad="%" onCambio={setMargen} />
                  <label className="fila-campo">
                    <span>Fondo</span>
                    <input type="color" value={fondo} onChange={(e) => setFondo(e.target.value)} />
                    <button type="button" className="enlace" onClick={() => setFondo('#ffffff')}>blanco</button>
                  </label>
                </>
              )}
              <label className="casilla" title="Si la foto es más pequeña que el lado, estirarla. No añade detalle: solo píxeles.">
                <input type="checkbox" checked={ampliar} onChange={(e) => setAmpliar(e.target.checked)} />
                Ampliar si es más pequeña
              </label>

              <div className="filtros">
                <span className="tenue">Formato:</span>
                <div className="grupo-badges">
                  <button type="button" className={`badge ${formato === 'image/jpeg' ? 'activo' : ''}`} onClick={() => setFormato('image/jpeg')}>JPEG</button>
                  <button type="button" className={`badge ${formato === 'image/png' ? 'activo' : ''}`} onClick={() => setFormato('image/png')}>PNG</button>
                </div>
              </div>
              {formato === 'image/jpeg' && <Deslizador nombre="Calidad" valor={calidad} min={50} max={100} unidad="" onCambio={setCalidad} />}

              <div className="editor-guardar">
                <label className="casilla">
                  <input type="radio" name="modo" checked={modo === 'reemplazar'} onChange={() => setModo('reemplazar')} />
                  Reemplazar la original en los productos que la usan
                </label>
                <label className="casilla">
                  <input type="radio" name="modo" checked={modo === 'nueva'} onChange={() => setModo('nueva')} />
                  Guardar como foto nueva y conservar la original
                </label>
              </div>

              <div className="grupo-acciones">
                <button type="button" onClick={reiniciar} disabled={guardando}>Deshacer todo</button>
                <button type="button" className="primario" onClick={() => void guardar()} disabled={guardando || !resultado}>
                  {guardando ? 'Guardando…' : 'Guardar'}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function Deslizador({ nombre, valor, min = 50, max = 150, unidad = '%', onCambio }: {
  nombre: string; valor: number; min?: number; max?: number; unidad?: string; onCambio: (v: number) => void
}) {
  return (
    <label className="fila-campo">
      <span>{nombre}</span>
      <input type="range" min={min} max={max} value={valor} onChange={(e) => onCambio(Number(e.target.value))} />
      <span className="tenue mini-texto" style={{ minWidth: 40, textAlign: 'right' }}>{valor}{unidad}</span>
    </label>
  )
}
