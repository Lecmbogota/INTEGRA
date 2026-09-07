import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { api, num } from './api'
import { Guia } from './Guia'
import { Hoja } from './Hoja'
import { PASOS_EDITOR_FOTO } from './guias/editorFoto'

// Editor de fotos. Lo que hace falta para dejar una foto como la piden los
// canales sin salir de Integra: recortar y enderezar, corregir el color,
// blanquear el fondo, encajarla en un lienzo cuadrado con margen y exportar
// a JPEG del tamaño correcto. Todo pasa en el navegador sobre la foto
// original; al servidor solo viaja el resultado.
//
// Tres zonas: el lienzo de edición (donde se recorta), la vista previa del
// resultado (lo que se va a guardar, con guías de encuadre) y el panel de
// ajustes, agrupado y plegado: quien abre el editor ve la foto, no cuarenta
// controles.
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
type Grupo = 'recorte' | 'color' | 'fondo' | 'lienzo' | 'exportar'

const PROPORCIONES: [Proporcion, string][] = [
  ['libre', 'Libre'], ['1:1', 'Cuadrado'], ['4:3', '4:3'], ['3:4', '3:4'], ['16:9', '16:9'],
]
const LADOS = [800, 1000, 1200, 1600, 2000]
const ANCHO_TRABAJO = 620
const ALTO_TRABAJO = 520

// Ajustes con los que arranca el editor y a los que vuelve «Deshacer todo».
const INICIAL = {
  rot: 0, flipH: false, flipV: false, angulo: 0, recorte: null as Rect | null, proporcion: 'libre' as Proporcion,
  quitarBlancos: false,
  brillo: 100, contraste: 100, saturacion: 100, temperatura: 0, nitidez: 0, autoNiveles: false,
  quitarFondo: false, tolerancia: 25, transparente: false,
  blanquear: false, umbral: 235, fondo: '#ffffff',
  cuadrado: true, lado: 1200, margen: 5, ampliar: false,
  formato: 'image/jpeg' as Formato, calidad: 88,
}
type Ajustes = typeof INICIAL

// Autoajustar: lo que casi siempre hace falta en una foto de producto, de
// una vez. Recorta al producto, endereza los niveles, blanquea el fondo, la
// centra en un cuadrado de 1200 con margen y le da un punto de nitidez.
// Cada cosa se puede deshacer después en su grupo.
const AUTO: Partial<Ajustes> = {
  quitarBlancos: true, autoNiveles: true, nitidez: 15, blanquear: true, umbral: 235,
  cuadrado: true, lado: 1200, margen: 5, fondo: '#ffffff', ampliar: false,
  formato: 'image/jpeg', calidad: 88, transparente: false,
}

// Ajustes rápidos: lo que pide cada destino, en un clic.
const RAPIDOS: { nombre: string; pista: string; cambios: Partial<Ajustes> }[] = [
  { nombre: 'Marketplace', pista: 'MercadoLibre y Falabella: cuadrada 1200, fondo blanco, margen 5 %, JPEG',
    cambios: { cuadrado: true, lado: 1200, margen: 5, fondo: '#ffffff', blanquear: true, formato: 'image/jpeg', calidad: 88, quitarBlancos: true } },
  { nombre: 'Tienda propia', pista: 'WooCommerce y Shopify: hasta 1600 px, sin lienzo, JPEG',
    cambios: { cuadrado: false, lado: 1600, formato: 'image/jpeg', calidad: 85, quitarBlancos: false, blanquear: false } },
  { nombre: 'Solo recortar', pista: 'Guardar el recorte tal cual, sin lienzo ni cambios de color',
    cambios: { cuadrado: false, lado: 2000, ampliar: false, blanquear: false, quitarBlancos: false, brillo: 100, contraste: 100, saturacion: 100, temperatura: 0, nitidez: 0, autoNiveles: false } },
]

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

// ------------------------------------------------------------ tratamiento

// orientar aplica giro de 90°, volteo y el ángulo fino de enderezado. Con
// ángulo, la foto se amplía lo justo para que no asomen esquinas vacías: es
// lo que hace cualquier editor al enderezar un horizonte.
function orientar(img: HTMLImageElement, rot: number, flipH: boolean, flipV: boolean, angulo: number): HTMLCanvasElement {
  const girada = rot % 2 === 1
  const W = girada ? img.naturalHeight : img.naturalWidth
  const H = girada ? img.naturalWidth : img.naturalHeight
  const c = lienzo(W, H)
  const ctx = c.getContext('2d')!
  const rad = (angulo * Math.PI) / 180
  const cobertura = Math.abs(Math.cos(rad)) + Math.abs(Math.sin(rad)) * Math.max(W / H, H / W)
  ctx.translate(W / 2, H / 2)
  ctx.rotate((rot * Math.PI) / 2 + rad)
  ctx.scale(cobertura * (flipH ? -1 : 1), cobertura * (flipV ? -1 : 1))
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

// quitarFondo borra el fondo por relleno desde los bordes: todo lo que está
// conectado con el borde de la foto y se parece al color del borde pasa a
// transparente. Es lo que hace la varita mágica de cualquier editor, y en
// una foto de producto sobre fondo liso —que es el caso de casi todas—
// funciona sin ninguna inteligencia detrás. Lo que queda encerrado dentro
// del producto (el hueco de un asa) no se toca. La máscara se calcula a
// tamaño reducido y se escala con suavizado, que además ablanda el borde.
function quitarFondo(c: HTMLCanvasElement, tolerancia: number): HTMLCanvasElement {
  const esc = Math.min(1, 1200 / Math.max(c.width, c.height))
  const w = Math.max(2, Math.round(c.width * esc))
  const h = Math.max(2, Math.round(c.height * esc))
  const t = lienzo(w, h)
  const tctx = t.getContext('2d')!
  tctx.drawImage(c, 0, 0, w, h)
  const d = tctx.getImageData(0, 0, w, h).data

  // Color de referencia: la media de los píxeles del borde.
  let r = 0, g = 0, b = 0, n = 0
  const sumar = (i: number) => { r += d[i]; g += d[i + 1]; b += d[i + 2]; n++ }
  for (let x = 0; x < w; x++) { sumar(x * 4); sumar(((h - 1) * w + x) * 4) }
  for (let y = 0; y < h; y++) { sumar((y * w) * 4); sumar((y * w + w - 1) * 4) }
  r /= n; g /= n; b /= n
  const lim = tolerancia * 2.5
  const lim2 = lim * lim

  const fondo = new Uint8Array(w * h)
  const cola = new Int32Array(w * h)
  let qi = 0, qn = 0
  const probar = (p: number) => {
    if (fondo[p]) return
    const i = p * 4
    const dr = d[i] - r, dg = d[i + 1] - g, db = d[i + 2] - b
    if (d[i + 3] < 16 || dr * dr + dg * dg + db * db <= lim2) { fondo[p] = 1; cola[qn++] = p }
  }
  for (let x = 0; x < w; x++) { probar(x); probar((h - 1) * w + x) }
  for (let y = 0; y < h; y++) { probar(y * w); probar(y * w + w - 1) }
  while (qi < qn) {
    const p = cola[qi++]
    const x = p % w, y = (p - x) / w
    if (x > 0) probar(p - 1)
    if (x < w - 1) probar(p + 1)
    if (y > 0) probar(p - w)
    if (y < h - 1) probar(p + w)
  }

  const mascara = lienzo(w, h)
  const mctx = mascara.getContext('2d')!
  const md = mctx.createImageData(w, h)
  for (let p = 0; p < w * h; p++) md.data[p * 4 + 3] = fondo[p] ? 0 : 255
  mctx.putImageData(md, 0, 0)

  const out = lienzo(c.width, c.height)
  const octx = out.getContext('2d')!
  octx.drawImage(c, 0, 0)
  octx.globalCompositeOperation = 'destination-in'
  octx.imageSmoothingEnabled = true
  octx.filter = `blur(${Math.max(0.6, c.width / 1500).toFixed(1)}px)`
  octx.drawImage(mascara, 0, 0, c.width, c.height)
  return out
}

function filtrar(c: HTMLCanvasElement, brillo: number, contraste: number, saturacion: number): HTMLCanvasElement {
  if (brillo === 100 && contraste === 100 && saturacion === 100) return c
  const out = lienzo(c.width, c.height)
  const ctx = out.getContext('2d')!
  ctx.filter = `brightness(${brillo}%) contrast(${contraste}%) saturate(${saturacion}%)`
  ctx.drawImage(c, 0, 0)
  return out
}

type Encaje = { cuadrado: boolean; lado: number; margen: number; fondo: string; ampliar: boolean; rellenar: boolean }

// encajar decide el tamaño final. En lienzo cuadrado la foto se centra sobre
// un fondo del lado pedido con su margen, que es lo que MercadoLibre y
// Falabella enseñan mejor. Sin lienzo, solo se reduce si es más grande que
// el lado pedido; ampliar una foto pequeña no le añade detalle y se deja
// como decisión explícita.
function encajar(c: HTMLCanvasElement, e: Encaje): HTMLCanvasElement {
  if (e.cuadrado) {
    const out = lienzo(e.lado, e.lado)
    const ctx = out.getContext('2d')!
    if (e.rellenar) {
      ctx.fillStyle = e.fondo
      ctx.fillRect(0, 0, e.lado, e.lado)
    }
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

type Pixeles = { autoNiveles: boolean; temperatura: number; blanquear: boolean; umbral: number; nitidez: number }

// retocarPixeles hace lo que el filtro del navegador no sabe: niveles
// automáticos, temperatura de color, blanquear el fondo y nitidez. Va al
// final, sobre el tamaño de salida, que es donde cuesta menos y donde la
// nitidez tiene sentido (afilar antes de reducir se pierde al reducir).
function retocarPixeles(c: HTMLCanvasElement, p: Pixeles): HTMLCanvasElement {
  if (!p.autoNiveles && p.temperatura === 0 && !p.blanquear && p.nitidez === 0) return c
  const w = c.width, h = c.height
  const out = lienzo(w, h)
  const ctx = out.getContext('2d')!
  ctx.drawImage(c, 0, 0)
  const img = ctx.getImageData(0, 0, w, h)
  const d = img.data

  if (p.autoNiveles) {
    // Se estiran los niveles entre los percentiles 0,5 y 99,5 de luminancia:
    // los extremos absolutos suelen ser ruido o el propio fondo blanco.
    const hist = new Uint32Array(256)
    for (let i = 0; i < d.length; i += 4) {
      hist[(d[i] * 299 + d[i + 1] * 587 + d[i + 2] * 114) / 1000 | 0]++
    }
    const total = w * h
    let lo = 0, hi = 255, acc = 0
    for (let v = 0; v < 256; v++) { acc += hist[v]; if (acc > total * 0.005) { lo = v; break } }
    acc = 0
    for (let v = 255; v >= 0; v--) { acc += hist[v]; if (acc > total * 0.005) { hi = v; break } }
    if (hi - lo > 20 && (lo > 0 || hi < 255)) {
      const k = 255 / (hi - lo)
      for (let i = 0; i < d.length; i += 4) {
        d[i] = Math.max(0, Math.min(255, (d[i] - lo) * k))
        d[i + 1] = Math.max(0, Math.min(255, (d[i + 1] - lo) * k))
        d[i + 2] = Math.max(0, Math.min(255, (d[i + 2] - lo) * k))
      }
    }
  }

  if (p.temperatura !== 0) {
    // Cálido sube el rojo y baja el azul; frío al revés. ±100 es ±25 %.
    const t = p.temperatura / 400
    for (let i = 0; i < d.length; i += 4) {
      d[i] = Math.max(0, Math.min(255, d[i] * (1 + t)))
      d[i + 2] = Math.max(0, Math.min(255, d[i + 2] * (1 - t)))
    }
  }

  if (p.blanquear) {
    // Lo casi blanco pasa a blanco puro: MercadoLibre exige fondo blanco y
    // las fotos de estudio salen con un gris 245 que en la ficha se nota.
    const u = p.umbral
    for (let i = 0; i < d.length; i += 4) {
      if (d[i] >= u && d[i + 1] >= u && d[i + 2] >= u) { d[i] = 255; d[i + 1] = 255; d[i + 2] = 255 }
    }
  }

  if (p.nitidez > 0) {
    // Máscara de enfoque: la imagen menos su versión desenfocada, sumada con
    // una fuerza. Un 3×3 basta para una foto de producto a 1200 px.
    const k = p.nitidez / 100
    const src = new Uint8ClampedArray(d)
    for (let y = 1; y < h - 1; y++) {
      for (let x = 1; x < w - 1; x++) {
        const i = (y * w + x) * 4
        for (let ch = 0; ch < 3; ch++) {
          const j = i + ch
          const desenf = (src[j - 4] + src[j + 4] + src[j - w * 4] + src[j + w * 4]) / 4
          d[j] = Math.max(0, Math.min(255, src[j] + (src[j] - desenf) * k * 2))
        }
      }
    }
  }

  ctx.putImageData(img, 0, 0)
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
  return { x: Math.max(0, Math.min(r.x, W - w)), y: Math.max(0, Math.min(r.y, H - h)), w, h }
}

// ------------------------------------------------------------- componente

export function EditorFoto({ foto, onCerrar, onGuardada, enVentana }: {
  foto: FotoEditable
  onCerrar: () => void
  onGuardada: (modo: 'reemplazar' | 'nueva') => void
  // Como página de una ventana del escritorio: sin velo, sin tarjeta y sin
  // Escape (la ventana ya encuadra y ← ya vuelve). Ver Hoja.tsx.
  enVentana?: boolean
}) {
  const [fuente, setFuente] = useState<HTMLImageElement | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [a, setA] = useState<Ajustes>(INICIAL)
  const [abierto, setAbierto] = useState<Grupo | null>(null)
  const [modo, setModo] = useState<'reemplazar' | 'nueva'>('reemplazar')
  const [guardando, setGuardando] = useState(false)
  const [guias, setGuias] = useState(true)
  const [verOriginal, setVerOriginal] = useState(false)
  const [resultado, setResultado] = useState<{ ancho: number; alto: number; bytes: number; ocupacion: number } | null>(null)
  const [calculando, setCalculando] = useState(false)
  const [ayuda, setAyuda] = useState(false)

  const lienzoTrabajo = useRef<HTMLCanvasElement>(null)
  const lienzoResultado = useRef<HTMLCanvasElement>(null)
  const orientada = useRef<HTMLCanvasElement | null>(null)
  const [escala, setEscala] = useState(1)
  const arrastre = useRef<{ asa: Asa; x0: number; y0: number; inicio: Rect } | null>(null)

  const poner = (cambios: Partial<Ajustes>) => setA((prev) => ({ ...prev, ...cambios }))

  // La foto original, entera: editar sobre la miniatura tiraría resolución.
  useEffect(() => {
    const img = new Image()
    img.onload = () => setFuente(img)
    img.onerror = () => setError('No se pudo cargar la foto original.')
    img.src = `/imagenes/${foto.sha256}`
  }, [foto.sha256])

  // Escape cierra el editor, salvo que la ayuda esté abierta encima: ahí
  // cierra la ayuda y el editor se queda con sus ajustes.
  useEffect(() => {
    if (enVentana) return
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !guardando && !ayuda) onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar, guardando, ayuda, enVentana])

  // Orientar es caro en fotos grandes: se hace una vez por giro y se guarda.
  useEffect(() => {
    if (!fuente) return
    orientada.current = orientar(fuente, a.rot, a.flipH, a.flipV, a.angulo)
    const o = orientada.current
    const esc = Math.min(1, ANCHO_TRABAJO / o.width, ALTO_TRABAJO / o.height)
    setEscala(esc)
    const c = lienzoTrabajo.current
    if (c) {
      c.width = Math.round(o.width * esc)
      c.height = Math.round(o.height * esc)
      const ctx = c.getContext('2d')!
      ctx.imageSmoothingQuality = 'high'
      ctx.drawImage(o, 0, 0, c.width, c.height)
    }
  }, [fuente, a.rot, a.flipH, a.flipV, a.angulo])

  // Girar 90° o voltear cambia el marco: el recorte anterior deja de tener
  // sentido y se quita. El ángulo fino no, porque se ajusta mirando el recorte.
  useEffect(() => { setA((prev) => ({ ...prev, recorte: null })) }, [a.rot, a.flipH, a.flipV])

  const procesar = useCallback((): HTMLCanvasElement | null => {
    const o = orientada.current
    if (!o) return null
    let c: HTMLCanvasElement = a.recorte ? recortar(o, a.recorte) : o
    if (a.quitarBlancos) c = recortar(c, bordesBlancos(c))
    c = filtrar(c, a.brillo, a.contraste, a.saturacion)
    if (a.quitarFondo) c = quitarFondo(c, a.tolerancia)
    // El lienzo se rellena salvo que se pida transparencia, que solo existe
    // en PNG: un JPEG con fondo transparente saldría negro.
    const rellenar = !(a.transparente && a.formato === 'image/png')
    c = encajar(c, { cuadrado: a.cuadrado, lado: a.lado, margen: a.margen, fondo: a.fondo, ampliar: a.ampliar, rellenar })
    return retocarPixeles(c, { autoNiveles: a.autoNiveles, temperatura: a.temperatura, blanquear: a.blanquear, umbral: a.umbral, nitidez: a.nitidez })
  }, [a])

  // La vista del resultado se recalcula con retraso: mover un deslizador
  // dispara decenas de cambios y cada uno es un repintado de la foto entera.
  useEffect(() => {
    if (!fuente) return
    let vigente = true
    setCalculando(true)
    const t = setTimeout(() => {
      const v = lienzoResultado.current
      if (!v) return
      const ctx = v.getContext('2d')!
      ctx.imageSmoothingQuality = 'high'

      if (verOriginal && orientada.current) {
        const o = orientada.current
        const esc = Math.min(1, 300 / o.width, 300 / o.height)
        v.width = Math.round(o.width * esc)
        v.height = Math.round(o.height * esc)
        ctx.drawImage(o, 0, 0, v.width, v.height)
        setCalculando(false)
        return
      }

      const c = procesar()
      if (!c) return
      const esc = Math.min(1, 300 / c.width, 300 / c.height)
      v.width = Math.round(c.width * esc)
      v.height = Math.round(c.height * esc)
      ctx.drawImage(c, 0, 0, v.width, v.height)

      // Dónde quedó el producto dentro del resultado.
      const caja = bordesBlancos(c)
      const ocupacion = Math.max(caja.w / c.width, caja.h / c.height)

      if (guias) {
        ctx.save()
        ctx.strokeStyle = 'rgba(0,0,0,.18)'
        ctx.lineWidth = 1
        for (let k = 1; k <= 2; k++) {
          ctx.beginPath(); ctx.moveTo((v.width * k) / 3, 0); ctx.lineTo((v.width * k) / 3, v.height); ctx.stroke()
          ctx.beginPath(); ctx.moveTo(0, (v.height * k) / 3); ctx.lineTo(v.width, (v.height * k) / 3); ctx.stroke()
        }
        if (a.cuadrado) {
          const m = (v.width * a.margen) / 100
          ctx.setLineDash([6, 4])
          ctx.strokeStyle = 'rgba(37,99,235,.9)'
          ctx.lineWidth = 1.5
          ctx.strokeRect(m + 0.5, m + 0.5, v.width - 2 * m - 1, v.height - 2 * m - 1)
        }
        ctx.setLineDash([4, 3])
        ctx.strokeStyle = ocupacion >= 0.7 && ocupacion <= 0.95 ? 'rgba(22,163,74,.95)' : 'rgba(217,119,6,.95)'
        ctx.lineWidth = 1.5
        ctx.strokeRect(caja.x * esc + 0.5, caja.y * esc + 0.5, caja.w * esc - 1, caja.h * esc - 1)
        ctx.restore()
      }
      aBlob(c, a.formato, a.calidad)
        .then((b) => { if (vigente) setResultado({ ancho: c.width, alto: c.height, bytes: b.size, ocupacion }) })
        .catch(() => { if (vigente) setResultado({ ancho: c.width, alto: c.height, bytes: 0, ocupacion }) })
        .finally(() => { if (vigente) setCalculando(false) })
    }, 220)
    return () => { vigente = false; clearTimeout(t) }
  }, [fuente, procesar, a.formato, a.calidad, a.cuadrado, a.margen, guias, verOriginal])

  // ---- recorte con el ratón sobre el lienzo de trabajo

  const dims = () => {
    const o = orientada.current
    return o ? { W: o.width, H: o.height } : { W: 1, H: 1 }
  }
  function rectActual(): Rect {
    const { W, H } = dims()
    return a.recorte ?? { x: 0, y: 0, w: W, h: H }
  }
  function empezar(asa: Asa, e: React.PointerEvent) {
    e.preventDefault()
    e.stopPropagation()
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    arrastre.current = { asa, x0: e.clientX, y0: e.clientY, inicio: rectActual() }
  }
  function mover(e: React.PointerEvent) {
    const d = arrastre.current
    if (!d) return
    const { W, H } = dims()
    const dx = (e.clientX - d.x0) / escala
    const dy = (e.clientY - d.y0) / escala
    const r = { ...d.inicio }
    const rz = razon(a.proporcion)
    if (d.asa === 'mover') {
      r.x += dx; r.y += dy
    } else {
      if (d.asa === 'ne' || d.asa === 'se') r.w += dx
      if (d.asa === 'nw' || d.asa === 'sw') { r.x += dx; r.w -= dx }
      if (d.asa === 'sw' || d.asa === 'se') r.h += dy
      if (d.asa === 'nw' || d.asa === 'ne') { r.y += dy; r.h -= dy }
      if (rz) {
        const h = r.w / rz
        if (d.asa === 'nw' || d.asa === 'ne') r.y += r.h - h
        r.h = h
      }
      if (r.w < 16) r.w = 16
      if (r.h < 16) r.h = 16
    }
    poner({ recorte: limitar(r, W, H) })
  }
  function soltar() { arrastre.current = null }

  function recorteCentrado(p: Proporcion) {
    const { W, H } = dims()
    const rz = razon(p)
    if (!rz) { poner({ proporcion: p }); return }
    let w = W, h = W / rz
    if (h > H) { h = H; w = H * rz }
    poner({ proporcion: p, recorte: { x: (W - w) / 2, y: (H - h) / 2, w, h } })
  }

  async function guardar() {
    const c = procesar()
    if (!c) return
    setGuardando(true)
    setError(null)
    try {
      const blob = await aBlob(c, a.formato, a.calidad)
      const ext = a.formato === 'image/png' ? 'png' : 'jpg'
      const archivo = new File([blob], `${foto.sha256.slice(0, 10)}-editada.${ext}`, { type: a.formato })
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
    if (menor < 600) avisos.push({ texto: `${resultado.ancho}×${resultado.alto}: por debajo de 600 px MercadoLibre la rechaza`, malo: true })
    else if (menor < 1200) avisos.push({ texto: 'Vale para publicar; 1200 px de lado se ve mejor en MercadoLibre y Falabella', malo: false })
    else avisos.push({ texto: 'Tamaño correcto para los cuatro canales', malo: false })
    if (resultado.ancho !== resultado.alto) avisos.push({ texto: 'No es cuadrada: en MercadoLibre y Falabella la ficha se ve con bandas', malo: false })
    if (a.formato === 'image/png') avisos.push({ texto: 'PNG pesa más y Falabella prefiere JPEG; úsalo solo si hace falta transparencia', malo: false })
    if (resultado.bytes > 3 * 1024 * 1024) avisos.push({ texto: 'Pesa más de 3 MB: baja la calidad o el lado', malo: true })
    const pct = Math.round(resultado.ocupacion * 100)
    if (resultado.ocupacion < 0.6) avisos.push({ texto: `El producto ocupa el ${pct} % del lienzo: se ve pequeño. Baja el margen o recorta más cerca (ideal 75–90 %)`, malo: true })
    else if (resultado.ocupacion < 0.75) avisos.push({ texto: `El producto ocupa el ${pct} %; podría verse algo más grande (ideal 75–90 %)`, malo: false })
    else if (resultado.ocupacion > 0.95) avisos.push({ texto: `El producto ocupa el ${pct} %: toca los bordes y la miniatura lo cortará. Sube el margen`, malo: true })
    else avisos.push({ texto: `El producto ocupa el ${pct} % del lienzo: bien encuadrado`, malo: false })
  }

  const { W, H } = dims()
  const r = rectActual()
  const caja = { left: r.x * escala, top: r.y * escala, width: r.w * escala, height: r.h * escala }
  const tocado = JSON.stringify(a) !== JSON.stringify(INICIAL)

  // Resúmenes de cada grupo plegado: lo que está puesto, sin abrirlo.
  const resumen: Record<Grupo, string> = {
    recorte: [
      a.recorte ? `${Math.round(r.w)}×${Math.round(r.h)}` : 'sin recorte',
      a.rot ? `${a.rot * 90}°` : '', a.angulo ? `${a.angulo > 0 ? '+' : ''}${a.angulo}°` : '',
      a.flipH ? 'volteada ↔' : '', a.flipV ? 'volteada ↕' : '', a.quitarBlancos ? 'sin bordes blancos' : '',
    ].filter(Boolean).join(' · '),
    color: [
      a.brillo !== 100 ? `brillo ${a.brillo}` : '', a.contraste !== 100 ? `contraste ${a.contraste}` : '',
      a.saturacion !== 100 ? `saturación ${a.saturacion}` : '', a.temperatura ? `temperatura ${a.temperatura > 0 ? '+' : ''}${a.temperatura}` : '',
      a.nitidez ? `nitidez ${a.nitidez}` : '', a.autoNiveles ? 'niveles automáticos' : '',
    ].filter(Boolean).join(' · ') || 'sin cambios',
    fondo: [
      a.quitarFondo ? (a.transparente && a.formato === 'image/png' ? 'fondo quitado · transparente' : `fondo quitado (tol. ${a.tolerancia})`) : '',
      a.blanquear ? `blanquear desde ${a.umbral}` : '',
      a.cuadrado && !(a.transparente && a.formato === 'image/png') ? `lienzo ${a.fondo}` : '',
    ].filter(Boolean).join(' · ') || 'sin cambios',
    lienzo: a.cuadrado ? `cuadrado ${num(a.lado)} px · margen ${a.margen} %${a.ampliar ? ' · ampliar' : ''}` : `hasta ${num(a.lado)} px, sin lienzo${a.ampliar ? ' · ampliar' : ''}`,
    exportar: a.formato === 'image/png' ? 'PNG' : `JPEG · calidad ${a.calidad}`,
  }

  const grupo = (id: Grupo, titulo: string, contenido: ReactNode) => (
    <GrupoAjustes titulo={titulo} resumen={resumen[id]} abierto={abierto === id} guia={`ef-grupo-${id}`}
      onToggle={() => setAbierto(abierto === id ? null : id)}>
      {contenido}
    </GrupoAjustes>
  )

  return (
    <Hoja enVentana={enVentana} clase="hoja-editor hoja-editor-foto" alFondo={() => { if (!guardando) onCerrar() }}>
        <header className="hoja-cabecera">
          <div>
            <h2>Editar foto</h2>
            <div className="sub">Original {foto.ancho}×{foto.alto} · {foto.formato}</div>
          </div>
          <div className="grupo-acciones">
            <button onClick={() => setA(INICIAL)} disabled={!tocado || guardando} data-guia="ef-deshacer">Deshacer todo</button>
            <button type="button" className="mini" title="Cómo funciona esta pantalla" onClick={() => setAyuda(true)}>?</button>
            <button onClick={onCerrar} disabled={guardando}>Cerrar ✕</button>
          </div>
        </header>
        {ayuda && <Guia pasos={PASOS_EDITOR_FOTO} nombre="Editor de fotos" onCerrar={() => setAyuda(false)} />}

        {error && <div className="aviso-caja">{error}</div>}
        {!fuente && !error && <div className="vacio">Cargando la foto original…</div>}

        {fuente && (
          <div className="editor3">
            {/* 1. Lienzo de edición */}
            <section className="editor-zona" data-guia="ef-edicion">
              <div className="editor-zona-titulo">
                <strong>Edición</strong>
                <span className="tenue mini-texto">Arrastra la caja o sus esquinas para recortar · {Math.round(r.w)}×{Math.round(r.h)} px</span>
              </div>
              <div className="editor-marco">
                <div className="editor-lienzo" style={{ width: Math.round(W * escala), height: Math.round(H * escala) }}
                  onPointerMove={mover} onPointerUp={soltar} onPointerCancel={soltar}>
                  <canvas ref={lienzoTrabajo} />
                  <div className="recorte-caja" style={caja} onPointerDown={(e) => empezar('mover', e)}>
                    {(['nw', 'ne', 'sw', 'se'] as Asa[]).map((h) => (
                      <span key={h} className={`recorte-asa ${h}`} onPointerDown={(e) => empezar(h, e)} />
                    ))}
                  </div>
                </div>
              </div>
              <div className="filtros" style={{ marginTop: 8 }} data-guia="ef-herramientas">
                <div className="grupo-badges">
                  {PROPORCIONES.map(([id, nombre]) => (
                    <button key={id} type="button" className={`badge ${a.proporcion === id ? 'activo' : ''}`}
                      onClick={() => recorteCentrado(id)}>{nombre}</button>
                  ))}
                </div>
                <button type="button" className="enlace" onClick={() => poner({ recorte: null, proporcion: 'libre' })}>Quitar recorte</button>
                <span className="tenue">·</span>
                <button type="button" onClick={() => poner({ rot: (a.rot + 3) % 4 })} title="90° a la izquierda">↺</button>
                <button type="button" onClick={() => poner({ rot: (a.rot + 1) % 4 })} title="90° a la derecha">↻</button>
                <button type="button" onClick={() => poner({ flipH: !a.flipH })} title="Voltear horizontal">↔</button>
                <button type="button" onClick={() => poner({ flipV: !a.flipV })} title="Voltear vertical">↕</button>
              </div>
            </section>

            {/* 2. Vista previa del resultado */}
            <section className="editor-zona" data-guia="ef-resultado">
              <div className="editor-zona-titulo">
                <strong>Así quedará</strong>
                <span className="tenue mini-texto">{calculando ? 'calculando…' : resultado ? `${resultado.ancho}×${resultado.alto} · ${tamano(resultado.bytes)} · ${a.formato === 'image/png' ? 'PNG' : 'JPEG'}` : ''}</span>
              </div>
              <div className="editor-resultado">
                <canvas ref={lienzoResultado} />
              </div>
              <div className="filtros" style={{ marginTop: 8, justifyContent: 'center' }} data-guia="ef-comparar">
                <label className="casilla mini-texto"
                  title="Tercios, área útil que deja el margen (azul) y contorno del producto detectado (verde si llena bien, naranja si no)">
                  <input type="checkbox" checked={guias} onChange={(e) => setGuias(e.target.checked)} />
                  Guías
                </label>
                <button type="button" className="mini"
                  onPointerDown={() => setVerOriginal(true)} onPointerUp={() => setVerOriginal(false)}
                  onPointerLeave={() => setVerOriginal(false)}
                  title="Mantén pulsado para comparar con la foto sin editar">
                  Ver original
                </button>
              </div>
              <ul className="lista-faltantes editor-avisos" data-guia="ef-avisos">
                {avisos.map((av, i) => <li key={i} style={{ color: av.malo ? 'var(--error)' : 'var(--ok)' }}>{av.texto}</li>)}
              </ul>
            </section>

            {/* 3. Ajustes */}
            <aside className="editor-ajustes">
              <div className="editor-zona-titulo"><strong>Ajustes</strong></div>
              <button type="button" className="primario editor-auto" onClick={() => poner(AUTO)} data-guia="ef-auto"
                title="Recorta al producto, endereza los niveles, blanquea el fondo, la centra en un cuadrado de 1200 con margen y le da nitidez. Cada cosa se puede deshacer en su grupo.">
                ✦ Autoajustar
              </button>
              <div className="filtros" style={{ marginBottom: 4 }} data-guia="ef-rapidos">
                <span className="tenue mini-texto">Rápidos:</span>
                <div className="grupo-badges">
                  {RAPIDOS.map((rp) => (
                    <button key={rp.nombre} type="button" className="badge" title={rp.pista} onClick={() => poner(rp.cambios)}>{rp.nombre}</button>
                  ))}
                </div>
              </div>

              {grupo('recorte', 'Recorte y giro', (
                <>
                  <Deslizador nombre="Enderezar" valor={a.angulo} min={-15} max={15} paso={0.5} unidad="°" onCambio={(v) => poner({ angulo: v })} />
                  <div className="filtros">
                    <button type="button" onClick={() => poner({ rot: (a.rot + 3) % 4 })}>↺ 90°</button>
                    <button type="button" onClick={() => poner({ rot: (a.rot + 1) % 4 })}>↻ 90°</button>
                    <button type="button" onClick={() => poner({ flipH: !a.flipH })}>Voltear ↔</button>
                    <button type="button" onClick={() => poner({ flipV: !a.flipV })}>Voltear ↕</button>
                  </div>
                  <label className="casilla" title="Recorta el borde blanco o transparente que sobra alrededor del producto">
                    <input type="checkbox" checked={a.quitarBlancos} onChange={(e) => poner({ quitarBlancos: e.target.checked })} />
                    Quitar bordes blancos sobrantes
                  </label>
                  <button type="button" className="enlace" onClick={() => poner({ rot: 0, flipH: false, flipV: false, angulo: 0, recorte: null, proporcion: 'libre', quitarBlancos: false })}>Restablecer grupo</button>
                </>
              ))}

              {grupo('color', 'Color y nitidez', (
                <>
                  <Deslizador nombre="Brillo" valor={a.brillo} onCambio={(v) => poner({ brillo: v })} />
                  <Deslizador nombre="Contraste" valor={a.contraste} onCambio={(v) => poner({ contraste: v })} />
                  <Deslizador nombre="Saturación" valor={a.saturacion} onCambio={(v) => poner({ saturacion: v })} />
                  <Deslizador nombre="Temperatura" valor={a.temperatura} min={-100} max={100} unidad="" onCambio={(v) => poner({ temperatura: v })} />
                  <Deslizador nombre="Nitidez" valor={a.nitidez} min={0} max={100} unidad="" onCambio={(v) => poner({ nitidez: v })} />
                  <label className="casilla" title="Estira los niveles para que el negro sea negro y el blanco, blanco. Arregla fotos lavadas.">
                    <input type="checkbox" checked={a.autoNiveles} onChange={(e) => poner({ autoNiveles: e.target.checked })} />
                    Niveles automáticos
                  </label>
                  <button type="button" className="enlace" onClick={() => poner({ brillo: 100, contraste: 100, saturacion: 100, temperatura: 0, nitidez: 0, autoNiveles: false })}>Restablecer grupo</button>
                </>
              ))}

              {grupo('fondo', 'Fondo', (
                <>
                  <label className="casilla" title="Borra el fondo liso conectado con los bordes de la foto. Funciona en fotos de producto sobre fondo uniforme; sube la tolerancia si quedan restos, bájala si se come el producto">
                    <input type="checkbox" checked={a.quitarFondo} onChange={(e) => poner({ quitarFondo: e.target.checked })} />
                    Quitar el fondo
                  </label>
                  {a.quitarFondo && (
                    <>
                      <Deslizador nombre="Tolerancia" valor={a.tolerancia} min={2} max={80} unidad="" onCambio={(v) => poner({ tolerancia: v })} />
                      <label className="casilla" title="Guardar con el fondo transparente. Obliga a PNG: JPEG no tiene transparencia">
                        <input type="checkbox" checked={a.transparente}
                          onChange={(e) => poner(e.target.checked ? { transparente: true, formato: 'image/png' } : { transparente: false })} />
                        Dejar el fondo transparente (PNG)
                      </label>
                      {!a.transparente && <div className="tenue mini-texto">Sin transparencia, el fondo quitado se rellena con el color del lienzo.</div>}
                    </>
                  )}
                  <label className="casilla" title="Lo casi blanco pasa a blanco puro: MercadoLibre exige fondo blanco y las fotos de estudio salen con un gris que en la ficha se nota">
                    <input type="checkbox" checked={a.blanquear} onChange={(e) => poner({ blanquear: e.target.checked })} />
                    Blanquear el fondo
                  </label>
                  {a.blanquear && <Deslizador nombre="Desde" valor={a.umbral} min={200} max={254} unidad="" onCambio={(v) => poner({ umbral: v })} />}
                  <label className="fila-campo">
                    <span>Color del lienzo</span>
                    <input type="color" value={a.fondo} onChange={(e) => poner({ fondo: e.target.value })} disabled={!a.cuadrado} />
                    <button type="button" className="enlace" onClick={() => poner({ fondo: '#ffffff' })}>blanco</button>
                  </label>
                  {!a.cuadrado && <div className="tenue mini-texto">El color del lienzo solo se usa con lienzo cuadrado.</div>}
                </>
              ))}

              {grupo('lienzo', 'Lienzo y tamaño', (
                <>
                  <label className="casilla" title="Centra la foto en un cuadrado con fondo y margen: es como mejor se ve en MercadoLibre y Falabella">
                    <input type="checkbox" checked={a.cuadrado} onChange={(e) => poner({ cuadrado: e.target.checked })} />
                    Lienzo cuadrado con fondo
                  </label>
                  <div className="filtros">
                    <span className="tenue mini-texto">Lado:</span>
                    <div className="grupo-badges">
                      {LADOS.map((l) => (
                        <button key={l} type="button" className={`badge ${a.lado === l ? 'activo' : ''}`} onClick={() => poner({ lado: l })}>{num(l)}</button>
                      ))}
                    </div>
                  </div>
                  {a.cuadrado && <Deslizador nombre="Margen" valor={a.margen} min={0} max={25} unidad="%" onCambio={(v) => poner({ margen: v })} />}
                  <label className="casilla" title="Si la foto es más pequeña que el lado, estirarla. No añade detalle: solo píxeles.">
                    <input type="checkbox" checked={a.ampliar} onChange={(e) => poner({ ampliar: e.target.checked })} />
                    Ampliar si es más pequeña
                  </label>
                </>
              ))}

              {grupo('exportar', 'Formato de salida', (
                <>
                  <div className="filtros">
                    <div className="grupo-badges">
                      <button type="button" className={`badge ${a.formato === 'image/jpeg' ? 'activo' : ''}`} onClick={() => poner({ formato: 'image/jpeg', transparente: false })}>JPEG</button>
                      <button type="button" className={`badge ${a.formato === 'image/png' ? 'activo' : ''}`} onClick={() => poner({ formato: 'image/png' })}>PNG</button>
                    </div>
                  </div>
                  {a.formato === 'image/jpeg' && <Deslizador nombre="Calidad" valor={a.calidad} min={50} max={100} unidad="" onCambio={(v) => poner({ calidad: v })} />}
                </>
              ))}

              <div className="editor-guardar" data-guia="ef-guardar">
                <label className="casilla">
                  <input type="radio" name="modo" checked={modo === 'reemplazar'} onChange={() => setModo('reemplazar')} />
                  Reemplazar la original en sus productos
                </label>
                <label className="casilla">
                  <input type="radio" name="modo" checked={modo === 'nueva'} onChange={() => setModo('nueva')} />
                  Guardar como nueva y conservar la original
                </label>
                <button type="button" className="primario" onClick={() => void guardar()} disabled={guardando || !resultado}>
                  {guardando ? 'Guardando…' : 'Guardar'}
                </button>
              </div>
            </aside>
          </div>
        )}
    </Hoja>
  )
}

// GrupoAjustes es una sección plegable del panel: título, resumen de lo que
// hay puesto cuando está cerrada, y los controles cuando está abierta.
function GrupoAjustes({ titulo, resumen, abierto, onToggle, guia, children }: {
  titulo: string; resumen: string; abierto: boolean; onToggle: () => void; guia?: string; children: ReactNode
}) {
  return (
    <div className={`grupo-ajustes ${abierto ? 'abierto' : ''}`}>
      <button type="button" className="grupo-cabecera" onClick={onToggle} aria-expanded={abierto} data-guia={guia}>
        <span className="grupo-flecha">{abierto ? '▾' : '▸'}</span>
        <span className="grupo-titulo">{titulo}</span>
        {!abierto && <span className="grupo-resumen">{resumen}</span>}
      </button>
      {abierto && <div className="grupo-cuerpo">{children}</div>}
    </div>
  )
}

function Deslizador({ nombre, valor, min = 50, max = 150, paso = 1, unidad = '%', onCambio }: {
  nombre: string; valor: number; min?: number; max?: number; paso?: number; unidad?: string; onCambio: (v: number) => void
}) {
  return (
    <label className="fila-campo">
      <span>{nombre}</span>
      <input type="range" min={min} max={max} step={paso} value={valor} onChange={(e) => onCambio(Number(e.target.value))} />
      <span className="tenue mini-texto" style={{ minWidth: 44, textAlign: 'right' }}>{valor}{unidad}</span>
    </label>
  )
}
