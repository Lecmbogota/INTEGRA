import {
  useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState,
  type FocusEvent as FocoReact, type KeyboardEvent as TeclaReact, type MouseEvent as RatonReact,
  type ReactNode, type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { num } from './api'

// Modo de vista de una lista, como en el explorador de Windows: la misma
// información se enseña como tabla, como filas compactas, como tarjetas o
// como iconos grandes. Cada pantalla decide cuáles admite; la elección se
// recuerda por pantalla para que Productos pueda quedarse en mosaico sin
// arrastrar a Pedidos.
export type ModoVista = 'detalles' | 'lista' | 'mosaico' | 'iconos'

const TODOS: ModoVista[] = ['detalles', 'lista', 'mosaico', 'iconos']

const NOMBRE: Record<ModoVista, string> = {
  detalles: 'Detalles',
  lista: 'Lista',
  mosaico: 'Mosaico',
  iconos: 'Iconos',
}

const PISTA: Record<ModoVista, string> = {
  detalles: 'Tabla con columnas',
  lista: 'Filas compactas, una línea por elemento',
  mosaico: 'Tarjetas con imagen y datos clave',
  iconos: 'Cuadrícula de iconos grandes',
}

const CLAVE = (clave: string) => `integra.vista.${clave}`

function leer(clave: string, porDefecto: ModoVista, admitidos: ModoVista[]): ModoVista {
  // localStorage puede no existir (navegación privada estricta) y lo guardado
  // puede ser de una versión que admitía un modo que ya no: en ambos casos
  // se vuelve al modo por defecto sin romper la pantalla.
  try {
    const v = window.localStorage.getItem(CLAVE(clave)) as ModoVista | null
    if (v && admitidos.includes(v)) return v
  } catch { /* sin almacenamiento: se usa el modo por defecto */ }
  return porDefecto
}

// useVista devuelve el modo actual de una pantalla y la función para
// cambiarlo. `clave` identifica la pantalla en localStorage.
export function useVista(
  clave: string,
  porDefecto: ModoVista,
  admitidos: ModoVista[] = TODOS,
): [ModoVista, (m: ModoVista) => void] {
  const [modo, setModo] = useState<ModoVista>(() => leer(clave, porDefecto, admitidos))
  // `admitidos` suele llegar como literal nuevo en cada render; se compara por
  // contenido para que `cambiar` no cambie de identidad sin motivo.
  const firma = admitidos.join(',')
  const cambiar = useCallback((m: ModoVista) => {
    if (!firma.split(',').includes(m)) return
    setModo(m)
    try { window.localStorage.setItem(CLAVE(clave), m) } catch { /* se pierde al recargar, nada más */ }
  }, [clave, firma])
  return [modo, cambiar]
}

// Iconos de 16×16 en currentColor, para que hereden el color del botón en
// los dos temas.
function Icono({ modo }: { modo: ModoVista }) {
  const comun = { width: 16, height: 16, viewBox: '0 0 16 16', fill: 'currentColor', 'aria-hidden': true }
  switch (modo) {
    case 'detalles':
      // Tabla: cabecera y tres filas partidas en columnas.
      return (
        <svg {...comun}>
          <path d="M1 2h14v2H1zM1 5.5h4v2H1zm5 0h4v2H6zm5 0h4v2h-4zM1 9h4v2H1zm5 0h4v2H6zm5 0h4v2h-4zM1 12.5h4v2H1zm5 0h4v2H6zm5 0h4v2h-4z" />
        </svg>
      )
    case 'lista':
      // Filas compactas: punto y línea.
      return (
        <svg {...comun}>
          <path d="M1 2.5h2v2H1zm3 0h11v2H4zM1 7h2v2H1zm3 0h11v2H4zm-3 4.5h2v2H1zm3 0h11v2H4z" />
        </svg>
      )
    case 'mosaico':
      // Tarjetas medianas: cuatro bloques con su «imagen» encima.
      return (
        <svg {...comun}>
          <path d="M1 1h6v4H1zm0 4.5h6v1.5H1zM9 1h6v4H9zm0 4.5h6v1.5H9zM1 9h6v4H1zm0 4.5h6V15H1zM9 9h6v4H9zm0 4.5h6V15H9z" />
        </svg>
      )
    case 'iconos':
      // Cuadrícula grande: cuatro cuadrados.
      return (
        <svg {...comun}>
          <path d="M1 1h6v6H1zm8 0h6v6H9zM1 9h6v6H1zm8 0h6v6H9z" />
        </svg>
      )
  }
}

// Imagen es el hueco de foto de una tarjeta o un icono: la miniatura si hay
// sha256 y, si no, un marco vacío con un icono, para que la rejilla no baile
// entre productos con foto y sin ella. Los hijos (una casilla, una pastilla)
// se posan encima.
export function Imagen({ sha, variante = 'miniatura_300', titulo, onClick, className = '', children }: {
  sha: string
  // Derivada del servidor: miniatura_300 para rejillas, web_800 para grande.
  variante?: string
  titulo?: string
  onClick?: () => void
  className?: string
  children?: ReactNode
}) {
  return (
    <div className={`vista-imagen ${onClick ? 'abrible' : ''} ${className}`} title={titulo}
      onClick={onClick ? (e) => { e.stopPropagation(); onClick() } : undefined}>
      {sha
        ? <img src={`/imagenes/${sha}/${variante}`} alt="" loading="lazy" />
        : (
          <svg width="32" height="32" viewBox="0 0 16 16" fill="currentColor" aria-label="Sin foto">
            <path d="M2 2h12v12H2zm1 1v7.6l3-3 3 3 2-2 3 3V3zm0 10h10v-.6l-3-3-2 2-3-3-2 2zM10 5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3z" />
          </svg>
        )}
      {children}
    </div>
  )
}

// SelectorVista es el grupo de botones que cambia el modo. Va a la derecha
// de la barra de filtros de cada pantalla. Atajo: Ctrl+Shift+1..4 (en el
// orden detalles, lista, mosaico, iconos) mientras el foco esté dentro de
// la pantalla que lo contiene —en el escritorio, dentro de su ventana— para
// que dos ventanas abiertas no cambien de vista a la vez.
export function SelectorVista({ modo, onCambiar, admitidos = TODOS }: {
  modo: ModoVista
  onCambiar: (m: ModoVista) => void
  admitidos?: ModoVista[]
}) {
  const raiz = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return
      // Se mira el código físico y no `key`: con Shift, la tecla 1 da «!».
      const n = /^Digit([1-4])$/.exec(e.code)
      if (!n) return
      const objetivo = TODOS[Number(n[1]) - 1]
      if (!admitidos.includes(objetivo)) return
      // Solo si el foco está en esta pantalla. Fuera del escritorio la
      // pantalla es `.contenido`; dentro, su `.ventana`, y vale también
      // que la ventana sea la activa aunque el foco esté en el cuerpo.
      // Una página que quedó atrás en el historial de la ventana (Productos
      // con un editor delante) sigue montada pero oculta: no responde.
      if (raiz.current?.closest('.pagina[hidden]')) return
      const ambito = raiz.current?.closest('.ventana, .contenido')
      const activo = document.activeElement
      const dentro = ambito
        ? ambito.contains(activo) || ambito.classList.contains('activa')
        : true
      if (!dentro) return
      e.preventDefault()
      onCambiar(objetivo)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [admitidos, onCambiar])

  return (
    <div className="selector-vista" role="group" aria-label="Modo de vista" ref={raiz}>
      {TODOS.filter((m) => admitidos.includes(m)).map((m) => (
        <button key={m} type="button" className={`badge ${modo === m ? 'activo' : ''}`}
          aria-pressed={modo === m} aria-label={NOMBRE[m]}
          title={`${NOMBRE[m]} — ${PISTA[m]} (Ctrl+Shift+${TODOS.indexOf(m) + 1})`}
          onClick={() => onCambiar(m)}>
          <Icono modo={m} />
        </button>
      ))}
    </div>
  )
}

// =====================================================================
// Comportamiento de explorador de archivos para cualquier lista: selección
// con modificadores y lazo, teclado, menú contextual, columnas y barra de
// estado. Son piezas sueltas para que cada pantalla monte las que quiera;
// Productos usa todas, Pedidos casi todas.
// =====================================================================

// ------------------------------------------------------------ selección

export type Clave = string | number

type Modificadores = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }

// Dónde abrir un menú y el `preventDefault` del evento que lo pidió (un
// clic derecho real o un punto inventado desde el teclado).
export type PuntoMenu = { clientX: number; clientY: number; preventDefault: () => void }

export type Seleccion<T, K extends Clave> = {
  seleccion: Set<K>
  // Desde dónde se extiende un rango con Shift: lo último que se seleccionó
  // o alternó sin Shift.
  ancla: K | null
  seleccionar: (k: K) => void
  alternar: (k: K) => void
  // Rango desde el ancla hasta `k`; con `sumar`, añadido a lo que hubiera
  // (Ctrl+Shift+clic) en vez de sustituirlo.
  rango: (k: K, sumar?: boolean) => void
  // Añade todos los elementos de la lista actual (la página que se ve).
  todo: () => void
  limpiar: () => void
  establecer: (s: Set<K>) => void
  // Interpreta los modificadores de un clic como el explorador: solo,
  // Ctrl alterna, Shift extiende.
  clic: (k: K, e: Modificadores) => void
  props: (item: T) => {
    'data-clave': string
    'aria-selected': boolean
    onClick: (e: RatonReact) => void
  }
  // Lazo: `onMouseDown` se posa en el contenedor de la lista y `marco` se
  // pinta dentro de él (tiene que ser `position: relative`).
  lazo: {
    onMouseDown: (e: RatonReact<HTMLElement>) => void
    marco: ReactNode
  }
}

// useSeleccion lleva la selección de una lista como el explorador: clic
// selecciona una, Ctrl+clic alterna, Shift+clic extiende desde el ancla y
// arrastrar por el fondo selecciona con un lazo lo que toca. Las claves
// pueden ser de otra página (la selección sobrevive a la paginación, como
// las casillas de antes): `todo` y `rango` solo miran la lista actual.
export function useSeleccion<T, K extends Clave>(items: T[], clave: (t: T) => K): Seleccion<T, K> {
  const [seleccion, setSeleccion] = useState<Set<K>>(() => new Set())
  const [ancla, setAncla] = useState<K | null>(null)
  const claves = useMemo(() => items.map(clave), [items, clave])
  // El lazo lee `data-clave` del DOM, que es texto: de ahí a la clave real.
  const porTexto = useMemo(() => new Map(claves.map((k) => [String(k), k])), [claves])

  // Las funciones leen el estado por ref para no cambiar de identidad en
  // cada render: cuelgan de manejadores de teclado que se recrean poco.
  const ref = useRef({ seleccion, ancla, claves })
  ref.current = { seleccion, ancla, claves }

  const seleccionar = useCallback((k: K) => { setSeleccion(new Set([k])); setAncla(k) }, [])
  const alternar = useCallback((k: K) => {
    setSeleccion((s) => {
      const n = new Set(s)
      if (n.has(k)) n.delete(k); else n.add(k)
      return n
    })
    setAncla(k)
  }, [])
  const rango = useCallback((k: K, sumar = false) => {
    const { ancla: a, claves: cs, seleccion: s } = ref.current
    const i = a === null ? -1 : cs.indexOf(a)
    const j = cs.indexOf(k)
    // Sin ancla en esta página no hay rango que extender: se empieza uno.
    if (i < 0 || j < 0) { setSeleccion(new Set([k])); setAncla(k); return }
    const tramo = cs.slice(Math.min(i, j), Math.max(i, j) + 1)
    setSeleccion(sumar ? new Set([...s, ...tramo]) : new Set(tramo))
  }, [])
  const todo = useCallback(() => setSeleccion((s) => new Set([...s, ...ref.current.claves])), [])
  const limpiar = useCallback(() => setSeleccion(new Set()), [])
  const establecer = useCallback((s: Set<K>) => setSeleccion(s), [])
  const clic = useCallback((k: K, e: Modificadores) => {
    if (e.shiftKey) rango(k, e.ctrlKey || e.metaKey)
    else if (e.ctrlKey || e.metaKey) alternar(k)
    else seleccionar(k)
  }, [rango, alternar, seleccionar])

  const props = useCallback((item: T) => {
    const k = clave(item)
    return {
      'data-clave': String(k),
      'aria-selected': seleccion.has(k),
      onClick: (e: RatonReact) => clic(k, e),
    }
  }, [clave, clic, seleccion])

  // Lazo. Empieza en el fondo del contenedor (no sobre un elemento ni un
  // control) y solo cuenta como lazo a partir de 4 px: un clic en el fondo
  // sin arrastrar limpia la selección, como en el explorador.
  const [marco, setMarco] = useState<{ x: number; y: number; w: number; h: number } | null>(null)
  const onMouseDown = useCallback((e: RatonReact<HTMLElement>) => {
    if (e.button !== 0) return
    const objetivo = e.target as HTMLElement
    if (objetivo.closest('[data-clave], thead, button, input, select, textarea, a, label, .lazo-ignorar')) return
    const cont = e.currentTarget
    const origen = { x: e.clientX, y: e.clientY }
    const base = e.ctrlKey || e.metaKey ? new Set(ref.current.seleccion) : new Set<K>()
    let activo = false
    let ultima = ''
    const mover = (ev: MouseEvent) => {
      if (!activo && Math.hypot(ev.clientX - origen.x, ev.clientY - origen.y) < 4) return
      activo = true
      const izq = Math.min(origen.x, ev.clientX), der = Math.max(origen.x, ev.clientX)
      const arr = Math.min(origen.y, ev.clientY), aba = Math.max(origen.y, ev.clientY)
      const rc = cont.getBoundingClientRect()
      setMarco({ x: izq - rc.left + cont.scrollLeft, y: arr - rc.top + cont.scrollTop, w: der - izq, h: aba - arr })
      const tocados = new Set(base)
      const partes: string[] = []
      cont.querySelectorAll<HTMLElement>('[data-clave]').forEach((el) => {
        const r = el.getBoundingClientRect()
        if (r.right < izq || r.left > der || r.bottom < arr || r.top > aba) return
        const texto = el.dataset.clave ?? ''
        const k = porTexto.get(texto)
        if (k !== undefined) { tocados.add(k); partes.push(texto) }
      })
      // Solo se cambia el estado cuando cambia lo tocado: el ratón manda
      // decenas de eventos por segundo y cada uno repintaría la lista.
      const firma = partes.join('|')
      if (firma !== ultima) { ultima = firma; setSeleccion(tocados) }
    }
    const soltar = (ev: MouseEvent) => {
      window.removeEventListener('mousemove', mover)
      window.removeEventListener('mouseup', soltar)
      setMarco(null)
      if (!activo && !(ev.ctrlKey || ev.metaKey)) setSeleccion(new Set())
    }
    window.addEventListener('mousemove', mover)
    window.addEventListener('mouseup', soltar)
    // Que el arrastre no seleccione texto. preventDefault también impide
    // que el contenedor tome el foco, así que se le da a mano.
    e.preventDefault()
    cont.focus({ preventScroll: true })
  }, [porTexto])

  const lazo = useMemo(() => ({
    onMouseDown,
    marco: marco
      ? <div className="lazo" aria-hidden style={{ left: marco.x, top: marco.y, width: marco.w, height: marco.h }} />
      : null,
  }), [onMouseDown, marco])

  return useMemo(() => ({
    seleccion, ancla, seleccionar, alternar, rango, todo, limpiar, establecer, clic, props, lazo,
  }), [seleccion, ancla, seleccionar, alternar, rango, todo, limpiar, establecer, clic, props, lazo])
}

// ------------------------------------------------------------ teclado

export type OpcionesTeclado<T, K extends Clave> = {
  clave: (t: T) => K
  seleccion?: Seleccion<T, K>
  // Enter o doble clic sobre un elemento.
  abrir?: (item: T) => void
  // Texto(s) por los que salta el tecleo: nombre, referencia…
  texto?: (item: T) => string | string[]
  // Clic derecho, tecla Menú o Shift+F10 sobre un elemento.
  contextual?: (item: T, e: PuntoMenu) => void
  // Ctrl+C con lo seleccionado (o el elemento con foco).
  copiar?: (items: T[]) => void
  // `grid` para la tabla de detalles, `listbox` para el resto.
  rol?: 'grid' | 'listbox'
}

// Sin acentos ni mayúsculas: quien teclea «cam» quiere «Cámara».
const normalizar = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

// Tiempo tras el que lo tecleado se olvida y se empieza otra búsqueda.
const REINICIO_TECLEO_MS = 800

// useTecladoLista mueve un foco de fila por la lista con el teclado, como
// el explorador: flechas (en cuadrícula también ←→ y saltos por fila según
// el ancho real), Inicio/Fin, RePág/AvPág, Enter abre, Espacio alterna,
// Ctrl+A todo, Escape limpia, Ctrl+C copia, y escribir salta a la fila
// que empieza así. `ref` va en el contenedor, que recibe el foco real; la
// fila con foco se anuncia con aria-activedescendant.
export function useTecladoLista<T, K extends Clave, E extends HTMLElement = HTMLDivElement>(
  ref: RefObject<E>,
  filas: T[],
  opciones: OpcionesTeclado<T, K>,
) {
  const base = useId()
  const [foco, setFoco] = useState<K | null>(null)
  // Las opciones llegan como objeto nuevo en cada render; el manejador de
  // teclado lee la última por ref y así no se recrea a cada pulsación.
  const oRef = useRef(opciones)
  oRef.current = opciones
  const claves = useMemo(() => filas.map(opciones.clave), [filas, opciones.clave])
  const indices = useMemo(() => new Map(claves.map((k, i) => [k, i])), [claves])
  const textos = useMemo(() => filas.map((f) => {
    const t = opciones.texto?.(f) ?? ''
    return (Array.isArray(t) ? t : [t]).map(normalizar)
  }), [filas, opciones.texto])
  const escrito = useRef({ texto: '', hora: 0 })

  const idDe = useCallback((k: K) => `${base}-${String(k)}`, [base])
  const enfocar = useCallback((k: K | null, desplazar = true) => {
    setFoco(k)
    if (k !== null && desplazar) document.getElementById(idDe(k))?.scrollIntoView({ block: 'nearest' })
  }, [idDe])

  // Cuántos elementos caben por fila: los que comparten la altura del
  // primero. En lista y tabla sale 1; en mosaico e iconos, lo que dé el
  // ancho real del contenedor.
  const columnas = useCallback(() => {
    const cont = ref.current
    if (!cont) return 1
    const els = cont.querySelectorAll<HTMLElement>('[data-clave]')
    if (els.length < 2) return 1
    const alto = els[0].offsetTop
    let c = 1
    while (c < els.length && els[c].offsetTop === alto) c++
    return c
  }, [ref])

  const onKeyDown = useCallback((e: TeclaReact<HTMLElement>) => {
    const op = oRef.current
    const objetivo = e.target as HTMLElement
    // Dentro de un campo de texto las teclas son suyas.
    if (objetivo.closest('input:not([type="checkbox"]), textarea, select, [contenteditable="true"]')) return
    // En una casilla o un botón, Espacio y Enter son lo que ya hacen.
    const enControl = objetivo !== e.currentTarget && !!objetivo.closest('button, input, a')
    const n = claves.length
    const sel = op.seleccion
    const actual = foco === null ? -1 : (indices.get(foco) ?? -1)
    const ctrl = e.ctrlKey || e.metaKey
    const irA = (i: number) => {
      if (n === 0) return
      const k = claves[Math.max(0, Math.min(n - 1, i))]
      enfocar(k)
      if (sel) {
        if (e.shiftKey) sel.rango(k, ctrl)
        else if (!ctrl) sel.seleccionar(k)
      }
      e.preventDefault()
    }
    const porPagina = () => {
      const cont = ref.current
      const el = foco === null ? null : document.getElementById(idDe(foco))
      const altoFila = el?.offsetHeight || 36
      const visible = Math.min(cont?.clientHeight ?? window.innerHeight, window.innerHeight)
      return Math.max(1, Math.floor(visible / altoFila) - 1) * columnas()
    }
    switch (e.key) {
      case 'ArrowDown': irA(actual < 0 ? 0 : actual + columnas()); return
      case 'ArrowUp': irA(actual < 0 ? n - 1 : actual - columnas()); return
      case 'ArrowRight': if (columnas() > 1) irA(actual < 0 ? 0 : actual + 1); return
      case 'ArrowLeft': if (columnas() > 1) irA(actual < 0 ? n - 1 : actual - 1); return
      case 'Home': irA(0); return
      case 'End': irA(n - 1); return
      case 'PageDown': irA(actual < 0 ? 0 : actual + porPagina()); return
      case 'PageUp': irA(actual < 0 ? n - 1 : actual - porPagina()); return
    }
    if (enControl) return
    if (ctrl && (e.key === 'a' || e.key === 'A') && sel) { sel.todo(); e.preventDefault(); return }
    if (ctrl && (e.key === 'c' || e.key === 'C') && op.copiar) {
      const elegidos = sel ? filas.filter((f) => sel.seleccion.has(op.clave(f))) : []
      const lote = elegidos.length > 0 ? elegidos : actual >= 0 ? [filas[actual]] : []
      if (lote.length > 0) { op.copiar(lote); e.preventDefault() }
      return
    }
    if (e.key === 'Escape' && sel) { sel.limpiar(); e.preventDefault(); return }
    if (e.key === 'Enter' && actual >= 0 && op.abrir) { op.abrir(filas[actual]); e.preventDefault(); return }
    if (e.key === ' ' && actual >= 0 && sel) {
      if (e.shiftKey) sel.rango(claves[actual], true); else sel.alternar(claves[actual])
      e.preventDefault()
      return
    }
    if ((e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) && actual >= 0 && op.contextual) {
      // El menú se abre junto al elemento con foco, no donde esté el ratón.
      const r = document.getElementById(idDe(claves[actual]))?.getBoundingClientRect()
      op.contextual(filas[actual], {
        clientX: r ? r.left + Math.min(48, r.width / 2) : 0,
        clientY: r ? r.top + r.height / 2 : 0,
        preventDefault: () => {},
      })
      e.preventDefault()
      return
    }
    // Escribir salta a la fila que empieza así. Repetir la misma letra
    // recorre las que empiezan por ella, como en el explorador.
    if (e.key.length === 1 && !ctrl && !e.altKey && e.key !== ' ') {
      const ahora = Date.now()
      const b = escrito.current
      b.texto = ahora - b.hora > REINICIO_TECLEO_MS ? normalizar(e.key) : b.texto + normalizar(e.key)
      b.hora = ahora
      const repetida = b.texto.length > 1 && b.texto.split('').every((c) => c === b.texto[0])
      const buscado = repetida ? b.texto[0] : b.texto
      const desde = repetida || b.texto.length === 1 ? actual + 1 : 0
      for (let paso = 0; paso < n; paso++) {
        const i = (desde + paso) % n
        if (textos[i].some((t) => t.startsWith(buscado))) { irA(i); return }
      }
      e.preventDefault()
    }
  }, [claves, indices, textos, filas, foco, enfocar, idDe, columnas, ref])

  const rol = opciones.rol ?? 'listbox'
  const propsContenedor = {
    ref,
    tabIndex: 0,
    role: rol,
    'aria-multiselectable': opciones.seleccion ? true : undefined,
    'aria-activedescendant': foco !== null && indices.has(foco) ? idDe(foco) : undefined,
    onKeyDown,
    // Al entrar con Tab sin fila con foco, la primera lo toma (sin
    // seleccionarla) para que las flechas tengan de dónde partir.
    onFocus: (e: FocoReact<HTMLElement>) => {
      if (e.target === e.currentTarget && foco === null && claves.length > 0) enfocar(claves[0], false)
    },
  }

  const propsFila = (item: T) => {
    const op = oRef.current
    const k = op.clave(item)
    const sel = op.seleccion
    return {
      id: idDe(k),
      role: rol === 'grid' ? 'row' : 'option',
      'data-clave': String(k),
      'aria-selected': sel ? sel.seleccion.has(k) : foco === k,
      className: foco === k ? 'enfocada' : '',
      onClick: (e: RatonReact) => { setFoco(k); sel?.clic(k, e) },
      onDoubleClick: (e: RatonReact) => {
        if ((e.target as HTMLElement).closest('button, input, a, label, select')) return
        op.abrir?.(item)
      },
      onContextMenu: (e: RatonReact) => {
        const ctx = op.contextual
        if (!ctx) return
        if ((e.target as HTMLElement).closest('input, textarea, select')) return
        e.preventDefault()
        setFoco(k)
        // Clic derecho sobre algo no seleccionado: pasa a ser la selección,
        // como en el explorador; sobre algo seleccionado, se conserva.
        if (sel && !sel.seleccion.has(k)) sel.seleccionar(k)
        ctx(item, e)
      },
    }
  }

  return { foco, enfocar, idDe, propsContenedor, propsFila }
}

// ------------------------------------------------------------ menú contextual

export type OpcionMenu = {
  etiqueta: string
  atajo?: string
  icono?: ReactNode
  peligroso?: boolean
  deshabilitado?: boolean
  // Línea de separación encima de esta opción.
  separador?: boolean
  // Marca ✓ a la izquierda (columnas visibles, orden actual).
  marcado?: boolean
  submenu?: OpcionMenu[]
  accion?: () => void
}

// useMenuContextual pinta un menú flotante donde se pidió, acotado a la
// ventana del navegador, que se cierra con Escape, clic fuera, rueda o
// cambio de tamaño. `Menu` es el nodo que hay que colocar en el árbol
// (`{menu.Menu}`); va por portal al body para que el marco de una ventana
// del escritorio (con transform) no lo recorte ni lo desplace.
export function useMenuContextual() {
  const [estado, setEstado] = useState<{ n: number; x: number; y: number; opciones: OpcionMenu[] } | null>(null)
  // Quién tenía el foco al abrir (la lista): se le devuelve al cerrar para
  // que las flechas sigan funcionando sin volver a pulsar en ella.
  const previo = useRef<HTMLElement | null>(null)
  const abrir = useCallback((e: PuntoMenu, opciones: OpcionMenu[]) => {
    e.preventDefault()
    if (opciones.length === 0) return
    previo.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    // `n` cambia en cada apertura: el menú se remonta entero y no hereda el
    // submenú que quedó abierto en el de la fila anterior.
    setEstado((s) => ({ n: (s?.n ?? 0) + 1, x: e.clientX, y: e.clientY, opciones }))
  }, [])
  const cerrar = useCallback(() => {
    setEstado(null)
    const el = previo.current
    previo.current = null
    if (el && el.isConnected) el.focus({ preventScroll: true })
  }, [])
  const Menu = estado
    ? createPortal(
      <MenuFlotante key={estado.n} x={estado.x} y={estado.y} opciones={estado.opciones} cerrar={cerrar} />,
      document.body,
    )
    : null
  return { abrir, cerrar, abierto: estado !== null, Menu }
}

function MenuFlotante({ x, y, opciones, cerrar }: { x: number; y: number; opciones: OpcionMenu[]; cerrar: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)

  // Se mide una vez pintado y se recoloca para que quepa entero.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setPos({
      left: Math.max(4, Math.min(x, window.innerWidth - r.width - 4)),
      top: Math.max(4, Math.min(y, window.innerHeight - r.height - 4)),
    })
  }, [x, y, opciones])

  useEffect(() => {
    const fuera = (e: Event) => { if (!ref.current?.contains(e.target as Node)) cerrar() }
    const tecla = (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Tab') { e.preventDefault(); e.stopPropagation(); cerrar() }
    }
    // En captura: un clic derecho sobre otra fila cierra este y abre el
    // suyo sin que el orden de los manejadores importe.
    document.addEventListener('mousedown', fuera, true)
    document.addEventListener('contextmenu', fuera, true)
    document.addEventListener('keydown', tecla, true)
    window.addEventListener('resize', cerrar)
    window.addEventListener('wheel', cerrar, { passive: true })
    return () => {
      document.removeEventListener('mousedown', fuera, true)
      document.removeEventListener('contextmenu', fuera, true)
      document.removeEventListener('keydown', tecla, true)
      window.removeEventListener('resize', cerrar)
      window.removeEventListener('wheel', cerrar)
    }
  }, [cerrar])

  // El foco entra en el menú para que las flechas y Enter funcionen desde
  // el primer momento (también se llega desde el teclado con Shift+F10).
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>('[role="menuitem"]:not(:disabled)')?.focus()
  }, [])

  return (
    <div ref={ref} className="menu-lista" role="menu"
      style={{ left: pos?.left ?? x, top: pos?.top ?? y, visibility: pos ? 'visible' : 'hidden' }}>
      <NivelMenu opciones={opciones} cerrar={cerrar} />
    </div>
  )
}

// Un nivel del menú: sus opciones y, como mucho, un submenú abierto.
function NivelMenu({ opciones, cerrar, onVolver }: {
  opciones: OpcionMenu[]
  cerrar: () => void
  // En un submenú, ← vuelve al nivel de arriba.
  onVolver?: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [abierto, setAbierto] = useState<number | null>(null)
  // Si el submenú se abrió con → toma el foco; si fue pasando el ratón, no:
  // robarlo dejaría al teclado en un sitio que el ratón va a cerrar.
  const porTeclado = useRef(false)

  const elementos = () => Array.from(
    ref.current?.querySelectorAll<HTMLElement>(':scope > .menu-entrada > [role="menuitem"]:not(:disabled)') ?? [],
  )
  const onKeyDown = (e: TeclaReact<HTMLDivElement>) => {
    // Si el foco está en un submenú, las teclas son suyas: el submenú las
    // atiende y las detiene; las que no atiende tampoco son de este nivel.
    const items = elementos()
    const i = items.indexOf(document.activeElement as HTMLElement)
    if (i < 0) return
    const ir = (j: number) => { items[(j + items.length) % items.length]?.focus(); e.preventDefault(); e.stopPropagation() }
    switch (e.key) {
      case 'ArrowDown': ir(i + 1); return
      case 'ArrowUp': ir(i - 1); return
      case 'Home': ir(0); return
      case 'End': ir(items.length - 1); return
      case 'ArrowRight': {
        const idx = Number((document.activeElement as HTMLElement | null)?.dataset.indice ?? -1)
        if (idx >= 0 && opciones[idx]?.submenu) {
          porTeclado.current = true
          setAbierto(idx)
          e.preventDefault()
          e.stopPropagation()
        }
        return
      }
      case 'ArrowLeft':
        if (onVolver) { onVolver(); e.preventDefault(); e.stopPropagation() }
        return
    }
  }

  return (
    <div ref={ref} className="menu-nivel" onKeyDown={onKeyDown}>
      {opciones.map((o, i) => (
        <div key={i} className="menu-entrada"
          onMouseEnter={() => { porTeclado.current = false; setAbierto(o.submenu ? i : null) }}>
          {o.separador && i > 0 && <div className="menu-separador" role="separator" />}
          <button type="button" role="menuitem" data-indice={i}
            className={`${o.peligroso ? 'peligroso' : ''} ${o.marcado ? 'marcado' : ''}`}
            disabled={o.deshabilitado}
            aria-haspopup={o.submenu ? 'menu' : undefined}
            aria-expanded={o.submenu ? abierto === i : undefined}
            onClick={() => {
              if (o.submenu) { porTeclado.current = true; setAbierto(abierto === i ? null : i); return }
              o.accion?.()
              cerrar()
            }}>
            <span className="menu-icono" aria-hidden>{o.marcado ? '✓' : o.icono}</span>
            <span className="menu-texto">{o.etiqueta}</span>
            {o.atajo && <span className="menu-atajo">{o.atajo}</span>}
            {o.submenu && <span className="menu-flecha" aria-hidden>›</span>}
          </button>
          {o.submenu && abierto === i && (
            <Submenu enfocar={porTeclado.current}>
              <NivelMenu opciones={o.submenu} cerrar={cerrar}
                onVolver={() => { setAbierto(null); elementos()[i]?.focus() }} />
            </Submenu>
          )}
        </div>
      ))}
    </div>
  )
}

// Submenú a la derecha de su opción; si no cabe, a la izquierda o más
// arriba. Con `enfocar` toma el foco al abrirse para que ↓ siga funcionando.
function Submenu({ enfocar, children }: { enfocar: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [ajuste, setAjuste] = useState<{ izquierda: boolean; subir: number }>({ izquierda: false, subir: 0 })
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setAjuste({
      izquierda: r.right > window.innerWidth - 4,
      subir: Math.max(0, r.bottom - window.innerHeight + 4),
    })
    if (enfocar) el.querySelector<HTMLElement>('[role="menuitem"]:not(:disabled)')?.focus()
  }, [enfocar])
  return (
    <div ref={ref} className={`menu-lista submenu ${ajuste.izquierda ? 'izquierda' : ''}`} role="menu"
      style={ajuste.subir ? { top: -7 - ajuste.subir } : undefined}>
      {children}
    </div>
  )
}

// ------------------------------------------------------------ columnas

export type ColumnaDef = {
  id: string
  titulo: string
  // Ancho en px por defecto (100) y mínimo al redimensionar (60).
  ancho?: number
  minimo?: number
  // Campo por el que ordena el servidor al pulsar la cabecera; sin él no
  // ordena.
  orden?: string
  // Clases para th y td (num, sku, oculto-movil…).
  clase?: string
  // data-guia del th, para la guía.
  guia?: string
  // Ni se oculta ni se mueve (la casilla, las acciones).
  fija?: boolean
  // Absorbe el ancho sobrante de la tabla; no se redimensiona.
  flexible?: boolean
  // Oculta hasta que se elija desde «Elegir columnas».
  oculta?: boolean
}
export type Columna = ColumnaDef & { ancho: number; visible: boolean }

type EstadoColumnas = { orden: string[]; anchos: Record<string, number>; ocultas: string[] }

export type Columnas = {
  // Visibles, en orden.
  columnas: Columna[]
  // Todas en orden, con `visible`.
  todas: Columna[]
  // Suma de anchos: min-width de la tabla para que las columnas conserven
  // el suyo y sobre lo que sobre se desplace de lado.
  anchoMinimo: number
  colgroup: ReactNode
  redimensionar: (id: string, ancho: number) => void
  // Solo en el DOM, sin guardar: mientras se arrastra el borde.
  previsualizar: (id: string, ancho: number) => void
  // Ajusta al contenido de la tabla dada.
  ajustar: (id: string, tabla: HTMLTableElement | null) => void
  // `antesDe` null la manda al final.
  mover: (id: string, antesDe: string | null) => void
  alternar: (id: string) => void
  restablecer: () => void
  // «Elegir columnas» (con submenú) y «Restablecer columnas».
  opcionesMenu: () => OpcionMenu[]
}

const CLAVE_COLUMNAS = (clave: string) => `integra.columnas.${clave}`
const MINIMO_COLUMNA = 60
const MAXIMO_AJUSTE = 640

// Las fijas vuelven a su sitio de la definición (la casilla al principio,
// las acciones al final) pase lo que pase con las demás.
function fijar(orden: string[], definicion: ColumnaDef[]): string[] {
  const fijas = new Set(definicion.filter((c) => c.fija).map((c) => c.id))
  const res = orden.filter((id) => !fijas.has(id))
  definicion.forEach((c, i) => { if (c.fija) res.splice(Math.min(i, res.length), 0, c.id) })
  return res
}

function estadoInicial(definicion: ColumnaDef[]): EstadoColumnas {
  return {
    orden: definicion.map((c) => c.id),
    anchos: {},
    ocultas: definicion.filter((c) => c.oculta && !c.fija).map((c) => c.id),
  }
}

function leerColumnas(clave: string, definicion: ColumnaDef[]): EstadoColumnas {
  const base = estadoInicial(definicion)
  try {
    const crudo = window.localStorage.getItem(CLAVE_COLUMNAS(clave))
    if (!crudo) return base
    const g = JSON.parse(crudo) as Partial<EstadoColumnas>
    const ids = new Set(definicion.map((c) => c.id))
    // Columnas guardadas que ya no existen se descartan; las nuevas de la
    // definición se cuelan en su sitio, detrás de su vecina de definición.
    const orden = (Array.isArray(g.orden) ? g.orden : []).filter((id) => ids.has(id))
    definicion.forEach((c, i) => {
      if (orden.includes(c.id)) return
      const previa = i > 0 ? orden.indexOf(definicion[i - 1].id) : -1
      orden.splice(previa + 1, 0, c.id)
    })
    const anchos: Record<string, number> = {}
    for (const [id, a] of Object.entries(g.anchos ?? {})) {
      if (ids.has(id) && typeof a === 'number' && a >= MINIMO_COLUMNA) anchos[id] = a
    }
    const ocultas = Array.isArray(g.ocultas) ? g.ocultas.filter((id) => ids.has(id)) : base.ocultas
    return { orden: fijar(orden, definicion), anchos, ocultas }
  } catch {
    return base
  }
}

// Ancho natural del contenido de una celda: el texto más largo que
// contiene, medido con su propia fuente, más el relleno de quien lo
// envuelve (una pastilla). No se toca el DOM: medir de verdad exigiría
// soltar la tabla a `auto` y volverla a fijar.
function anchoContenido(celda: Element, ctx: CanvasRenderingContext2D): number {
  let max = 0
  const paseo = document.createTreeWalker(celda, NodeFilter.SHOW_TEXT)
  let n: Node | null
  while ((n = paseo.nextNode())) {
    const texto = n.textContent?.trim()
    const padre = n.parentElement
    if (!texto || !padre) continue
    const st = getComputedStyle(padre)
    ctx.font = `${st.fontStyle} ${st.fontWeight} ${st.fontSize} ${st.fontFamily}`
    max = Math.max(max, ctx.measureText(texto).width + parseFloat(st.paddingLeft) + parseFloat(st.paddingRight))
  }
  return max
}

// useColumnas lleva las columnas de una tabla de detalles como las del
// explorador: ancho, orden y visibilidad, guardados por pantalla en
// localStorage. La tabla se pinta con `table-layout: fixed` y el
// `colgroup` que devuelve; la cabecera la pinta CabeceraColumnas.
export function useColumnas(clave: string, definicion: ColumnaDef[]): Columnas {
  const [estado, setEstado] = useState(() => leerColumnas(clave, definicion))
  const refsCol = useRef(new Map<string, HTMLTableColElement>())

  const guardar = useCallback((f: (e: EstadoColumnas) => EstadoColumnas) => {
    setEstado((e) => {
      const n = f(e)
      try { window.localStorage.setItem(CLAVE_COLUMNAS(clave), JSON.stringify(n)) } catch { /* se pierde al recargar */ }
      return n
    })
  }, [clave])

  const todas = useMemo<Columna[]>(() => {
    const porId = new Map(definicion.map((c) => [c.id, c]))
    const ocultas = new Set(estado.ocultas)
    return estado.orden.flatMap((id) => {
      const d = porId.get(id)
      if (!d) return []
      return [{ ...d, ancho: estado.anchos[id] ?? d.ancho ?? 100, visible: !!d.fija || !ocultas.has(id) }]
    })
  }, [definicion, estado])
  const columnas = useMemo(() => todas.filter((c) => c.visible), [todas])
  const anchoMinimo = useMemo(() => columnas.reduce((s, c) => s + c.ancho, 0), [columnas])

  const colgroup = (
    <colgroup>
      {columnas.map((c) => (
        <col key={c.id} data-columna={c.id}
          style={c.flexible ? undefined : { width: c.ancho }}
          ref={(el) => { if (el) refsCol.current.set(c.id, el); else refsCol.current.delete(c.id) }} />
      ))}
    </colgroup>
  )

  const minimoDe = useCallback((id: string) =>
    Math.max(MINIMO_COLUMNA, definicion.find((c) => c.id === id)?.minimo ?? 0), [definicion])

  const previsualizar = useCallback((id: string, ancho: number) => {
    const col = refsCol.current.get(id)
    if (col) col.style.width = `${Math.max(minimoDe(id), ancho)}px`
  }, [minimoDe])

  const redimensionar = useCallback((id: string, ancho: number) => {
    const a = Math.round(Math.max(minimoDe(id), ancho))
    guardar((e) => ({ ...e, anchos: { ...e.anchos, [id]: a } }))
  }, [guardar, minimoDe])

  const ajustar = useCallback((id: string, tabla: HTMLTableElement | null) => {
    if (!tabla) return
    const i = columnas.findIndex((c) => c.id === id)
    if (i < 0) return
    const ctx = document.createElement('canvas').getContext('2d')
    if (!ctx) return
    let max = 0
    // Las celdas con colspan (el detalle desplegado de un pedido) no son de
    // esta columna aunque ocupen su hueco.
    tabla.querySelectorAll(`thead > tr > th:nth-child(${i + 1}), tbody > tr > td:nth-child(${i + 1}):not([colspan])`)
      .forEach((celda) => { max = Math.max(max, anchoContenido(celda, ctx)) })
    // 24 del relleno de la celda y un poco de aire para que no roce.
    redimensionar(id, Math.min(MAXIMO_AJUSTE, Math.ceil(max) + 24 + 8))
  }, [columnas, redimensionar])

  const mover = useCallback((id: string, antesDe: string | null) => {
    if (definicion.find((c) => c.id === id)?.fija) return
    guardar((e) => {
      const orden = e.orden.filter((x) => x !== id)
      const i = antesDe === null ? -1 : orden.indexOf(antesDe)
      orden.splice(i < 0 ? orden.length : i, 0, id)
      return { ...e, orden: fijar(orden, definicion) }
    })
  }, [guardar, definicion])

  const alternar = useCallback((id: string) => {
    const d = definicion.find((c) => c.id === id)
    if (!d || d.fija) return
    guardar((e) => {
      const ocultas = new Set(e.ocultas)
      if (ocultas.has(id)) {
        ocultas.delete(id)
      } else {
        // Al menos una columna no fija tiene que quedar: una tabla de solo
        // casillas no dice nada.
        const visibles = definicion.filter((c) => !c.fija && !ocultas.has(c.id))
        if (visibles.length <= 1) return e
        ocultas.add(id)
      }
      return { ...e, ocultas: [...ocultas] }
    })
  }, [guardar, definicion])

  const restablecer = useCallback(() => guardar(() => estadoInicial(definicion)), [guardar, definicion])

  const opcionesMenu = useCallback((): OpcionMenu[] => [
    {
      etiqueta: 'Elegir columnas',
      submenu: todas.filter((c) => !c.fija).map((c) => ({
        etiqueta: c.titulo, marcado: c.visible, accion: () => alternar(c.id),
      })),
    },
    { etiqueta: 'Restablecer columnas', accion: restablecer },
  ], [todas, alternar, restablecer])

  return useMemo(() => ({
    columnas, todas, anchoMinimo, colgroup, redimensionar, previsualizar, ajustar, mover, alternar, restablecer, opcionesMenu,
  }), [columnas, todas, anchoMinimo, colgroup, redimensionar, previsualizar, ajustar, mover, alternar, restablecer, opcionesMenu])
}

// CabeceraColumnas pinta el <thead> de una tabla con useColumnas: clic
// ordena (si la columna ordena), arrastrar el borde derecho redimensiona
// (doble clic ajusta al contenido), arrastrar la cabecera la mueve y el
// clic derecho ofrece orden, ajuste, ocultar y «Elegir columnas».
export function CabeceraColumnas({ col, orden, onOrdenar, menu, contenido, atributos, refTabla }: {
  col: Columnas
  // Orden actual, para la flecha y aria-sort.
  orden?: { campo: string; desc: boolean }
  // Sin `desc`, pulsar alterna como siempre; con él, fija ese sentido.
  onOrdenar?: (campo: string, desc?: boolean) => void
  menu?: ReturnType<typeof useMenuContextual>
  // Sustituye el título de una columna (la casilla de «todos»).
  contenido?: (c: Columna) => ReactNode
  // Atributos extra del <tr> (data-guia).
  atributos?: Record<string, string>
  refTabla?: RefObject<HTMLTableElement>
}) {
  const [indicador, setIndicador] = useState<{ id: string; lado: 'antes' | 'despues' } | null>(null)
  // Qué columna se arrastra: dataTransfer no se puede leer en dragover.
  const arrastrando = useRef<string | null>(null)

  const iniciarRedimension = (c: Columna, e: RatonReact) => {
    e.preventDefault()
    e.stopPropagation()
    const x0 = e.clientX
    const a0 = c.ancho
    const min = Math.max(MINIMO_COLUMNA, c.minimo ?? 0)
    let ancho = a0
    const tabla = refTabla?.current
    const mover = (ev: MouseEvent) => {
      ancho = Math.max(min, a0 + ev.clientX - x0)
      col.previsualizar(c.id, ancho)
      // El min-width de la tabla acompaña para que la columna crezca de
      // verdad en vez de robarle a las demás.
      if (tabla) tabla.style.minWidth = `${col.anchoMinimo - a0 + ancho}px`
    }
    const soltar = () => {
      window.removeEventListener('mousemove', mover)
      window.removeEventListener('mouseup', soltar)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      col.redimensionar(c.id, ancho)
    }
    window.addEventListener('mousemove', mover)
    window.addEventListener('mouseup', soltar)
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
  }

  const siguiente = (id: string) => {
    const i = col.columnas.findIndex((c) => c.id === id)
    return col.columnas[i + 1]?.id ?? null
  }

  const opcionesCabecera = (c: Columna): OpcionMenu[] => {
    const res: OpcionMenu[] = []
    if (c.orden && onOrdenar) {
      const campo = c.orden
      res.push(
        { etiqueta: 'Orden ascendente', marcado: orden?.campo === campo && !orden.desc, accion: () => onOrdenar(campo, false) },
        { etiqueta: 'Orden descendente', marcado: orden?.campo === campo && orden.desc, accion: () => onOrdenar(campo, true) },
      )
    }
    if (!c.fija) {
      res.push(
        { etiqueta: 'Ajustar al contenido', separador: true, accion: () => col.ajustar(c.id, refTabla?.current ?? null) },
        { etiqueta: 'Ocultar columna', accion: () => col.alternar(c.id) },
      )
    }
    col.opcionesMenu().forEach((o, i) => res.push(i === 0 ? { ...o, separador: true } : o))
    return res
  }

  return (
    <thead>
      <tr {...atributos}>
        {col.columnas.map((c) => {
          const ordenada = !!c.orden && orden?.campo === c.orden
          return (
            <th key={c.id}
              className={[
                c.clase ?? '',
                c.orden && onOrdenar ? 'ordenable' : '',
                ordenada ? 'ordenada' : '',
                indicador?.id === c.id ? `soltar-${indicador.lado}` : '',
              ].join(' ').trim()}
              data-guia={c.guia}
              aria-sort={ordenada ? (orden?.desc ? 'descending' : 'ascending') : undefined}
              draggable={!c.fija}
              onClick={c.orden && onOrdenar ? () => onOrdenar(c.orden!) : undefined}
              onContextMenu={menu ? (e) => menu.abrir(e, opcionesCabecera(c)) : undefined}
              onDragStart={(e) => {
                if (c.fija) { e.preventDefault(); return }
                arrastrando.current = c.id
                e.dataTransfer.setData('integra/columna', c.id)
                e.dataTransfer.effectAllowed = 'move'
              }}
              onDragOver={(e) => {
                if (!arrastrando.current || arrastrando.current === c.id || c.fija) return
                e.preventDefault()
                e.dataTransfer.dropEffect = 'move'
                const r = e.currentTarget.getBoundingClientRect()
                const lado = e.clientX < r.left + r.width / 2 ? 'antes' : 'despues'
                if (indicador?.id !== c.id || indicador.lado !== lado) setIndicador({ id: c.id, lado })
              }}
              onDragLeave={(e) => {
                if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setIndicador(null)
              }}
              onDrop={(e) => {
                e.preventDefault()
                const id = arrastrando.current
                arrastrando.current = null
                setIndicador(null)
                if (!id || id === c.id || c.fija) return
                const r = e.currentTarget.getBoundingClientRect()
                const antes = e.clientX < r.left + r.width / 2
                col.mover(id, antes ? c.id : siguiente(c.id))
              }}
              onDragEnd={() => { arrastrando.current = null; setIndicador(null) }}>
              <span className="cabecera-texto">
                {contenido?.(c) ?? c.titulo}
                {ordenada && <span className="cabecera-orden" aria-hidden>{orden?.desc ? ' ↓' : ' ↑'}</span>}
              </span>
              {!c.fija && !c.flexible && (
                <span className="asa-columna" role="presentation" title="Arrastra para cambiar el ancho; doble clic ajusta al contenido"
                  onMouseDown={(e) => iniciarRedimension(c, e)}
                  onClick={(e) => e.stopPropagation()}
                  onDoubleClick={(e) => { e.stopPropagation(); col.ajustar(c.id, refTabla?.current ?? null) }}
                  onDragStart={(e) => { e.preventDefault(); e.stopPropagation() }} />
              )}
            </th>
          )
        })}
      </tr>
    </thead>
  )
}

// ------------------------------------------------------------ barra de estado

// BarraEstado va al pie de una lista, como la del explorador: cuántos
// elementos hay, cuántos están seleccionados y lo que la pantalla quiera
// sumar (unidades, valor).
export function BarraEstado({ total, seleccionados, nombre = 'elementos', children }: {
  total: number
  seleccionados: number
  nombre?: string
  children?: ReactNode
}) {
  return (
    <div className="barra-estado" role="status">
      <span>{num(total)} {nombre}</span>
      {seleccionados > 0 && <span>{num(seleccionados)} seleccionados</span>}
      {children}
    </div>
  )
}
