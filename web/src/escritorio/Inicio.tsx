import {
  useCallback, useEffect, useMemo, useRef, useState,
  type KeyboardEvent as KeyboardEventReact, type MouseEvent as MouseEventReact,
  type PointerEvent as PointerEventReact, type ReactNode, type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'
import type { AppId, Pagina, Ventana } from './tipos'
import { useSistema } from './sistema'
import { usePreferencias } from './preferencias'
import { useDatos } from './datos'
import { APPS } from './apps'
import { useSesion } from './sesion'
import { api, money, type Orden, type Producto } from '../api'
import { RECORRIDOS } from '../guias'
import { MenuContextual, hace, nombreRol, type OpcionMenu } from './Escritorio'

// El menú de inicio: dos paneles (buscador universal, ancladas, recientes y
// todas las apps a la izquierda; «Tu día» con lo urgente y acciones rápidas a
// la derecha) y el pie con el usuario. Se pinta por portal al body por lo
// mismo que el menú contextual: el cristal de la barra (backdrop-filter)
// atraparía un `position: fixed` dentro de ella. Los estilos viven en
// inicio.css.

const RETRASO_BUSQUEDA = 200 // ms sin teclear antes de preguntar al servidor
const MAX_PRODUCTOS = 6
const MAX_PEDIDOS = 5
const MAX_GUIA = 5
const MAX_RECIENTES_VISIBLES = 8
const DURACION_CIERRE = 140 // ms; tiene que coincidir con la animación de inicio.css

// Sin acentos ni mayúsculas, para que «categoria» encuentre «Categorías».
function normalizar(s: string): string {
  // \p{M}: las marcas diacríticas que NFD separa de la letra base.
  return s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
}

function reducirMovimiento(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// ------------------------------------------------------------ recientes
// Las últimas fichas abiertas (vista previa, editor, editor de fotos). Las
// apunta la barra de tareas aunque el menú esté cerrado (useRegistroRecientes)
// y las lee el menú. Se guardan por usuario en localStorage.

export type Reciente = {
  app: AppId
  props: Record<string, unknown>
  titulo: string
  // Segunda línea (SKU · marca), si se resolvió.
  detalle?: string
  // ISO de la última vez que se abrió.
  cuando: string
  // sha256 de la portada: '' si el producto no tiene foto; ausente si aún no
  // se ha preguntado (el menú lo resuelve al pintar y lo deja guardado).
  portada_sha?: string
}

const APPS_RECIENTES: ReadonlySet<AppId> = new Set<AppId>(['preview', 'editar', 'editor-foto'])
const MAX_RECIENTES = 12
const EVENTO_RECIENTES = 'integra:recientes'

function claveAlmacen(usuarioId: number): string { return `integra.recientes.${usuarioId}` }

// Identidad de una entrada: la misma ficha abierta dos veces es una sola.
function identidadReciente(app: AppId, props: Record<string, unknown>): string {
  if (typeof props.varianteId === 'number') return `${app}:${props.varianteId}`
  const foto = props.foto as { id?: unknown } | undefined
  if (foto && typeof foto.id === 'number') return `${app}:foto-${foto.id}`
  return `${app}:${JSON.stringify(props)}`
}

// El editor de fotos ya trae el sha en sus props: no hay que preguntarlo.
function shaDeProps(props: Record<string, unknown>): string | undefined {
  const foto = props.foto as { sha256?: unknown } | undefined
  return foto && typeof foto.sha256 === 'string' ? foto.sha256 : undefined
}

export function leerRecientes(usuarioId: number): Reciente[] {
  try {
    const crudo = localStorage.getItem(claveAlmacen(usuarioId))
    if (!crudo) return []
    const lista: unknown = JSON.parse(crudo)
    if (!Array.isArray(lista)) return []
    return lista.filter((r): r is Reciente => {
      const x = r as Partial<Reciente> | null
      return !!x && typeof x === 'object' && typeof x.app === 'string' && APPS[x.app] != null
        && typeof x.titulo === 'string' && typeof x.cuando === 'string' && !!x.props && typeof x.props === 'object'
    })
  } catch {
    return []
  }
}

function guardarRecientes(usuarioId: number, lista: Reciente[]) {
  try {
    localStorage.setItem(claveAlmacen(usuarioId), JSON.stringify(lista.slice(0, MAX_RECIENTES)))
  } catch {
    // Sin almacenamiento (modo privado, cuota llena) los recientes son un
    // extra que se pierde; no merece un aviso.
  }
  window.dispatchEvent(new CustomEvent(EVENTO_RECIENTES))
}

// Apunta una ficha abierta al principio de la lista, sin duplicados.
export function registrarReciente(usuarioId: number, r: Omit<Reciente, 'cuando'>) {
  const id = identidadReciente(r.app, r.props)
  const lista = leerRecientes(usuarioId)
  const previa = lista.find((x) => identidadReciente(x.app, x.props) === id)
  const resto = lista.filter((x) => identidadReciente(x.app, x.props) !== id)
  guardarRecientes(usuarioId, [{ ...previa, ...r, cuando: new Date().toISOString() }, ...resto])
}

// Cambia datos de una entrada sin moverla ni tocar su fecha: el título que
// la ventana pone al cargar, la portada resuelta después.
function actualizarReciente(usuarioId: number, app: AppId, props: Record<string, unknown>, cambios: Partial<Reciente>) {
  const id = identidadReciente(app, props)
  const lista = leerRecientes(usuarioId)
  const i = lista.findIndex((x) => identidadReciente(x.app, x.props) === id)
  if (i < 0) return
  lista[i] = { ...lista[i], ...cambios }
  guardarRecientes(usuarioId, lista)
}

function quitarReciente(usuarioId: number, r: Reciente) {
  const id = identidadReciente(r.app, r.props)
  guardarRecientes(usuarioId, leerRecientes(usuarioId).filter((x) => identidadReciente(x.app, x.props) !== id))
}

// Observa las ventanas y apunta cada página nueva de una app «de ficha». Lo
// usa la barra de tareas, que está montada siempre; así se registra aunque
// el menú esté cerrado. Si la ventana cambia luego de título (el editor pone
// «Editar · SKU» al cargar), la entrada se actualiza sin moverse.
export function useRegistroRecientes(ventanas: Ventana[], usuarioId: number) {
  // Página (ventana/clave) → título con el que se apuntó.
  const vistas = useRef(new Map<string, string>())
  useEffect(() => {
    const vivas = new Set<string>()
    for (const v of ventanas) {
      if (!APPS_RECIENTES.has(v.app)) continue
      const pagina = v.historial[v.indice] as Pagina | undefined
      const clave = `${v.id}/${pagina?.clave ?? v.app}`
      vivas.add(clave)
      const apuntado = vistas.current.get(clave)
      if (apuntado === undefined) {
        vistas.current.set(clave, v.titulo)
        registrarReciente(usuarioId, { app: v.app, props: v.props, titulo: v.titulo, portada_sha: shaDeProps(v.props) })
      } else if (apuntado !== v.titulo) {
        vistas.current.set(clave, v.titulo)
        actualizarReciente(usuarioId, v.app, v.props, { titulo: v.titulo })
      }
    }
    // Las páginas cerradas se olvidan para que, si se reabren, cuenten como nuevas.
    for (const k of Array.from(vistas.current.keys())) if (!vivas.has(k)) vistas.current.delete(k)
  }, [ventanas, usuarioId])
}

// -------------------------------------------------------------- iconos

const trazo = { fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round', strokeLinejoin: 'round' } as const

const ICONO_PRODUCTO = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <path d="M12 3 4 7v10l8 4 8-4V7z" /><path d="M4 7l8 4 8-4M12 11v10" />
  </svg>
)
const ICONO_PEDIDO = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <path d="M6 7h12l1 13H5z" /><path d="M9 10V6a3 3 0 0 1 6 0v4" />
  </svg>
)
const ICONO_GUIA = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <circle cx="12" cy="12" r="9" /><path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.7.3-1 .9-1 1.7M12 17h.01" />
  </svg>
)
const ICONO_LUPA = (
  <svg viewBox="0 0 24 24" width="20" height="20" {...trazo} aria-hidden>
    <circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />
  </svg>
)
const ICONO_SYNC = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <path d="M20 12a8 8 0 0 1-14.3 4.9M4 12a8 8 0 0 1 14.3-4.9" /><path d="M20 4v4h-4M4 20v-4h4" />
  </svg>
)
const ICONO_TEMA = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <path d="M12 3a9 9 0 1 0 9 9c0-.5 0-1-.1-1.4A6 6 0 0 1 12.4 3z" />
  </svg>
)
const ICONO_CANDADO = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <rect x="5" y="11" width="14" height="10" rx="2" /><path d="M8 11V7a4 4 0 0 1 8 0v4" />
  </svg>
)
const ICONO_RAYO = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <path d="M13 3 4 14h7l-1 7 9-11h-7z" />
  </svg>
)
const ICONO_WIDGET = (
  <svg viewBox="0 0 24 24" width="24" height="24" {...trazo} aria-hidden>
    <rect x="4" y="4" width="7" height="7" rx="1.5" /><rect x="13" y="4" width="7" height="7" rx="1.5" />
    <rect x="4" y="13" width="7" height="7" rx="1.5" /><path d="M16.5 14v6M13.5 17h6" />
  </svg>
)
const ICONO_FLECHA = (
  <svg viewBox="0 0 24 24" width="16" height="16" {...trazo} aria-hidden><path d="m9 6 6 6-6 6" /></svg>
)

// ----------------------------------------------------- búsqueda: modelo

type Grupo = 'acciones' | 'apps' | 'productos' | 'pedidos' | 'guia'
// Orden de desempate entre grupos con la misma relevancia.
const ORDEN_GRUPOS: Grupo[] = ['acciones', 'apps', 'productos', 'pedidos', 'guia']
const TITULOS: Record<Grupo, string> = {
  acciones: 'Acciones', apps: 'Apps', productos: 'Productos', pedidos: 'Pedidos', guia: 'Ayuda y guías',
}

type Resultado = {
  clave: string
  grupo: Grupo
  // Relevancia: manda dentro del grupo y, el mejor de cada grupo, entre grupos.
  puntos: number
  icono: ReactNode
  color?: string
  // Portada del producto; sustituye al icono.
  miniatura?: string
  titulo: string
  detalle?: string
  // Dato a la derecha (precio, importe).
  meta?: string
  pastilla?: { texto: string; clase: string }
  // Qué hace Enter y Ctrl+Enter, para enseñarlo en la fila seleccionada.
  pista?: string
  // `alternativa`: Ctrl+Enter.
  accion: (alternativa: boolean) => void
}

// Relevancia de un texto frente a la consulta (ambos ya normalizados):
// igual > empieza por > una palabra empieza por > contiene > todas las
// palabras de la consulta aparecen en cualquier orden. 0 = no coincide.
function puntuar(texto: string, q: string): number {
  if (!q) return 0
  const t = normalizar(texto)
  if (t === q) return 100
  if (t.startsWith(q)) return 80
  if (t.split(/[\s·,./()-]+/).some((p) => p.startsWith(q))) return 60
  if (t.includes(q)) return 40
  const partes = q.split(/\s+/).filter(Boolean)
  if (partes.length > 1 && partes.every((p) => t.includes(p))) return 25
  return 0
}

// Otras formas de llamar a cada app. Normalizadas ya (sin acentos).
const SINONIMOS: Partial<Record<AppId, string[]>> = {
  panel: ['inicio', 'resumen', 'cifras', 'tablero', 'dashboard'],
  catalogo: ['productos', 'articulos', 'inventario', 'referencias', 'sku', 'fichas'],
  mediateca: ['fotos', 'imagenes', 'fotografias', 'galeria', 'banco de imagenes'],
  publicacion: ['ventas', 'vender', 'publicar', 'marketplace', 'mercadolibre', 'falabella', 'tienda'],
  pedidos: ['ordenes', 'compras', 'clientes', 'despachos', 'envios'],
  actividad: ['tareas', 'trabajos', 'procesos', 'cola', 'historial'],
  automatizacion: ['alertas', 'reglas', 'programadas', 'vigilancia'],
  integraciones: ['odoo', 'erp', 'conexion', 'sincronizacion'],
  categorias: ['arbol', 'rubros', 'mapeo'],
  atributos: ['caracteristicas', 'campos', 'ficha tecnica'],
  canales: ['cuentas', 'credenciales', 'tiendas', 'comisiones'],
  usuarios: ['personas', 'equipo', 'accesos', 'roles', 'permisos'],
  avisos: ['correos', 'notificaciones', 'email'],
  configuracion: ['ajustes', 'opciones', 'preferencias', 'tema', 'fondo', 'widgets', 'escritorio'],
  ayuda: ['guia', 'tutorial', 'manual', 'recorrido', 'como funciona'],
}

const ESTADOS_PEDIDO: Record<string, { texto: string; clase: string }> = {
  received: { texto: 'Recibido', clase: 'aviso' },
  mapped: { texto: 'Mapeado', clase: 'aviso' },
  created_in_odoo: { texto: 'En Odoo', clase: 'ok' },
  failed: { texto: 'Falló', clase: 'bloqueante' },
  ignored: { texto: 'Ignorado', clase: 'neutra' },
}

function estadoProducto(p: Producto): { texto: string; clase: string } {
  if (p.excluido) return { texto: 'Excluido', clase: 'neutra' }
  if (p.problemas.length > 0) return { texto: 'Atención', clase: 'aviso' }
  if (p.publicado && p.publicado.length > 0) return { texto: 'Publicado', clase: 'ok' }
  return { texto: 'Pendiente', clase: 'neutra' }
}

function miniatura(sha: string | undefined): string | undefined {
  return sha ? `/imagenes/${sha}/miniatura_300` : undefined
}

// Resalta en `texto` la consulta (entera y, si no, palabra a palabra). Se
// busca sobre el texto normalizado y se traduce cada índice al original,
// para que «categoria» resalte «Categoría» con su acento.
function Resaltar({ texto, q }: { texto: string; q: string }) {
  if (!q || texto.length > 240) return <>{texto}</>
  const letras = Array.from(texto)
  const mapa: number[] = []
  let norm = ''
  letras.forEach((l, i) => {
    const n = normalizar(l)
    for (let j = 0; j < n.length; j++) mapa.push(i)
    norm += n
  })
  const partes = norm.includes(q) ? [q] : q.split(/\s+/).filter(Boolean)
  const rangos: [number, number][] = []
  for (const p of partes) {
    let desde = 0
    for (;;) {
      const k = norm.indexOf(p, desde)
      if (k < 0) break
      rangos.push([mapa[k], mapa[k + p.length - 1] + 1])
      desde = k + p.length
    }
  }
  if (rangos.length === 0) return <>{texto}</>
  rangos.sort((a, b) => a[0] - b[0])
  const trozos: ReactNode[] = []
  let cursor = 0
  for (const [a, b] of rangos) {
    if (a < cursor) continue
    if (a > cursor) trozos.push(letras.slice(cursor, a).join(''))
    trozos.push(<mark key={a}>{letras.slice(a, b).join('')}</mark>)
    cursor = b
  }
  if (cursor < letras.length) trozos.push(letras.slice(cursor).join(''))
  return <>{trozos}</>
}

// Abre la app de ayuda y le pide que enseñe un recorrido concreto. No hay
// contrato para pasarle props a `ayuda`, así que se avisa por un evento
// global que Ayuda escucha. Va en un setTimeout para que la ventana ya esté
// montada y suscrita cuando llegue.
function abrirGuia(sistema: ReturnType<typeof useSistema>, clave: string, paso: number) {
  sistema.abrir('ayuda')
  setTimeout(() => {
    window.dispatchEvent(new CustomEvent('integra:guia', { detail: { clave, paso } }))
  }, 0)
}

function useMedia(consulta: string): boolean {
  const [ok, setOk] = useState(() => window.matchMedia(consulta).matches)
  useEffect(() => {
    const m = window.matchMedia(consulta)
    const f = () => setOk(m.matches)
    f()
    m.addEventListener('change', f)
    return () => m.removeEventListener('change', f)
  }, [consulta])
  return ok
}

function saludo(d: Date): string {
  const h = d.getHours()
  return h < 12 ? 'Buenos días' : h < 20 ? 'Buenas tardes' : 'Buenas noches'
}
const FMT_DIA = new Intl.DateTimeFormat('es-CO', { weekday: 'long', day: 'numeric', month: 'long' })

// ------------------------------------------------------------- envoltura

// Mantiene el panel montado un instante tras cerrarse para que la animación
// de salida se vea; el panel de verdad (PanelInicio) se monta limpio en cada
// apertura, así no hay que reiniciar estado a mano.
export function Inicio({ abierto, onCerrar }: { abierto: boolean; onCerrar: () => void }) {
  const [mostrar, setMostrar] = useState(abierto)
  const [cerrando, setCerrando] = useState(false)
  const mostrarRef = useRef(false)
  useEffect(() => {
    if (abierto) { mostrarRef.current = true; setMostrar(true); setCerrando(false); return }
    if (!mostrarRef.current) return
    mostrarRef.current = false
    if (reducirMovimiento()) { setMostrar(false); return }
    setCerrando(true)
    const t = setTimeout(() => { setMostrar(false); setCerrando(false) }, DURACION_CIERRE)
    return () => clearTimeout(t)
  }, [abierto])
  if (!mostrar) return null
  return <PanelInicio abierto={abierto} cerrando={cerrando} onCerrar={onCerrar} />
}

// ----------------------------------------------------------------- panel

function PanelInicio({ abierto, cerrando, onCerrar }: { abierto: boolean; cerrando: boolean; onCerrar: () => void }) {
  const sistema = useSistema()
  const { prefs, poner } = usePreferencias()
  const datos = useDatos()
  const { usuario, salir } = useSesion()
  const esAdmin = usuario.role === 'admin'
  const apilado = useMedia('(max-width: 899px)')
  const sistemaOscuro = useMedia('(prefers-color-scheme: dark)')
  const oscuro = prefs.tema === 'oscuro' || (prefs.tema === 'sistema' && sistemaOscuro)

  const [q, setQ] = useState('')
  const [vista, setVista] = useState<'inicio' | 'todas'>('inicio')
  const [productos, setProductos] = useState<Producto[]>([])
  // Los pedidos no tienen búsqueda en el servidor: se traen los últimos 50
  // la primera vez que hace falta y se filtran aquí.
  const [ordenes, setOrdenes] = useState<Orden[] | null>(null)
  const [buscando, setBuscando] = useState(false)
  const [sel, setSel] = useState(0)
  const [menu, setMenu] = useState<{ x: number; y: number; opciones: OpcionMenu[] } | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const listaRef = useRef<HTMLDivElement>(null)
  const izqRef = useRef<HTMLDivElement>(null)

  const onCerrarRef = useRef(onCerrar)
  onCerrarRef.current = onCerrar

  // Autofoco al abrir: en el propio efecto (el DOM ya está), sin esperar a un
  // requestAnimationFrame que en una pestaña oculta no llega nunca. Al
  // cerrarse, si el foco se quedó en el body, vuelve al botón de inicio para
  // que quien navega con teclado o lector de pantalla no se pierda.
  useEffect(() => {
    input.current?.focus({ preventScroll: true })
    return () => {
      if (document.activeElement === document.body || document.activeElement === null) {
        document.querySelector<HTMLElement>('.barra-inicio')?.focus()
      }
    }
  }, [])

  // Clic fuera: cierra. Escape se trata en el teclado del diálogo (primero
  // vacía el buscador). El botón de la barra que abre el menú no cuenta como
  // «fuera»: pulsarlo lo cierra sin volver a abrirlo.
  useEffect(() => {
    if (!abierto) return
    const abajo = (e: PointerEvent) => {
      const t = e.target as Element | null
      if (!t || panel.current?.contains(t)) return
      if (t.closest('[data-abre-inicio], .menu-contextual')) return
      onCerrarRef.current()
    }
    document.addEventListener('pointerdown', abajo, true)
    return () => document.removeEventListener('pointerdown', abajo, true)
  }, [abierto])

  // Modo acciones: «>» delante enseña solo las acciones.
  const modoAcciones = q.trimStart().startsWith('>')
  const consulta = normalizar((modoAcciones ? q.trimStart().slice(1) : q).trim())
  const hayBusqueda = q.trim().length > 0

  // Productos: con retraso para no pedir al servidor en cada tecla.
  useEffect(() => {
    if (modoAcciones || consulta.length < 2) { setProductos([]); setBuscando(false); return }
    let vivo = true
    setBuscando(true)
    const t = setTimeout(() => {
      api.productos({ q: consulta, limite: MAX_PRODUCTOS })
        .then((p) => { if (vivo) setProductos(p.items) })
        .catch(() => { if (vivo) setProductos([]) })
        .finally(() => { if (vivo) setBuscando(false) })
    }, RETRASO_BUSQUEDA)
    return () => { vivo = false; clearTimeout(t) }
  }, [consulta, modoAcciones])

  // Pedidos: una sola vez por apertura, en cuanto se busca algo.
  useEffect(() => {
    if (modoAcciones || consulta.length < 2 || ordenes !== null) return
    let vivo = true
    api.ordenes(50).then((o) => { if (vivo) setOrdenes(o) }).catch(() => { if (vivo) setOrdenes([]) })
    return () => { vivo = false }
  }, [consulta, modoAcciones, ordenes])

  const visible = useCallback((id: AppId) => APPS[id] != null && (!APPS[id].soloAdmin || esAdmin), [esAdmin])
  // Solo las apps de una instancia (secciones) se pueden abrir sin props;
  // los diálogos (editar, preview…) necesitan un producto y no van aquí.
  const todas = useMemo(
    () => Object.values(APPS)
      .filter((a) => a.unica && visible(a.id))
      .sort((a, b) => a.nombre.localeCompare(b.nombre, 'es')),
    [visible],
  )
  const ancladas = useMemo(() => {
    const lista = prefs.ancladas.filter(visible)
    // Si no hay nada anclado, se recomiendan las que el registro marca.
    return lista.length > 0 ? lista : todas.filter((a) => a.anclada).map((a) => a.id)
  }, [prefs.ancladas, todas, visible])

  const insignia = useCallback((app: AppId): number => {
    switch (app) {
      case 'catalogo': return datos.resumen?.en_atencion ?? 0
      case 'pedidos': return (datos.pedidos?.recibidos ?? 0) + (datos.pedidos?.fallidos ?? 0)
      case 'actividad': return datos.tareasActivas
      case 'automatizacion': return datos.alertas
      default: return 0
    }
  }, [datos.resumen, datos.pedidos, datos.tareasActivas, datos.alertas])

  const cerrar = useCallback(() => onCerrarRef.current(), [])
  const elegirApp = useCallback((app: AppId, props?: Record<string, unknown>, opciones?: { titulo?: string }) => {
    cerrar()
    sistema.abrir(app, props, opciones)
  }, [cerrar, sistema])
  const cambiarTema = useCallback(() => poner({ tema: oscuro ? 'claro' : 'oscuro' }), [poner, oscuro])
  const bloquear = useCallback(() => { cerrar(); salir() }, [cerrar, salir])

  // Acciones: lo que se hace, no lo que se abre. Sincronizar y cambiar el
  // tema no cierran el menú: el efecto se ve en «Tu día».
  const acciones = useMemo(() => [
    { clave: 'sincronizar', titulo: datos.sincronizando ? 'Sincronizando…' : 'Sincronizar ahora', detalle: 'Trae de Odoo productos, precios y existencias', palabras: 'sync odoo actualizar refrescar traer', icono: ICONO_SYNC, accion: () => { if (!datos.sincronizando) void datos.sincronizar(); setQ('') } },
    { clave: 'tema', titulo: oscuro ? 'Tema claro' : 'Tema oscuro', detalle: 'Cambia el aspecto de todo el escritorio', palabras: 'tema oscuro claro noche dia modo apariencia', icono: ICONO_TEMA, accion: () => { cambiarTema(); setQ('') } },
    { clave: 'publicar', titulo: 'Publicar lo listo', detalle: 'Abre Publicación con lo que ya puede salir a los canales', palabras: 'publicar vender canales listo', icono: ICONO_RAYO, accion: () => elegirApp('publicacion') },
    { clave: 'traer-pedidos', titulo: 'Traer pedidos', detalle: 'Abre Pedidos para pedir a los canales lo nuevo', palabras: 'pedidos ordenes ventas ingerir', icono: ICONO_PEDIDO, accion: () => elegirApp('pedidos') },
    { clave: 'configuracion', titulo: 'Configuración', detalle: 'Tema, fondo, barra, apps ancladas y widgets', palabras: 'ajustes opciones preferencias', icono: APPS.configuracion?.icono ?? ICONO_WIDGET, accion: () => elegirApp('configuracion') },
    { clave: 'widget', titulo: 'Añadir widget', detalle: 'Un bloque nuevo para el escritorio, desde Configuración', palabras: 'widget escritorio bloque anadir', icono: ICONO_WIDGET, accion: () => elegirApp('configuracion') },
    { clave: 'bloquear', titulo: 'Bloquear', detalle: 'Cierra la sesión; volverás a la pantalla de entrada', palabras: 'salir cerrar sesion logout bloquear', icono: ICONO_CANDADO, accion: bloquear },
  ], [datos, oscuro, cambiarTema, elegirApp, bloquear])

  // ----- resultados: cada fuente puntúa lo suyo; luego se ordena dentro
  // de cada grupo y los grupos entre sí por su mejor resultado.
  const resultados = useMemo<Resultado[]>(() => {
    if (!hayBusqueda) return []
    const r: Resultado[] = []

    for (const a of acciones) {
      const puntos = modoAcciones && !consulta ? 1
        : Math.max(puntuar(a.titulo, consulta), puntuar(a.detalle, consulta) * 0.5, puntuar(a.palabras, consulta) * 0.7)
      if (puntos > 0) r.push({ clave: `accion-${a.clave}`, grupo: 'acciones', puntos, icono: a.icono, titulo: a.titulo, detalle: a.detalle, accion: a.accion })
    }
    if (modoAcciones) return ordenar(r)

    for (const a of todas) {
      const sinonimos = SINONIMOS[a.id] ?? []
      const puntos = Math.max(
        puntuar(a.nombre, consulta),
        puntuar(a.descripcion, consulta) * 0.6,
        ...sinonimos.map((s) => puntuar(s, consulta) * 0.7),
      )
      if (puntos > 0) {
        r.push({
          clave: `app-${a.id}`, grupo: 'apps', puntos, icono: a.icono, color: a.color, titulo: a.nombre, detalle: a.descripcion,
          pista: '↵ abrir', accion: () => elegirApp(a.id),
        })
      }
    }
    for (const p of productos) {
      // El servidor ya decidió que coincide (quizá por código de barras o
      // descripción): un mínimo para que no desaparezca aunque aquí no se vea.
      const puntos = Math.max(30, puntuar(p.nombre, consulta), puntuar(p.sku, consulta), puntuar(p.marca, consulta) * 0.8)
      r.push({
        clave: `prod-${p.id}`, grupo: 'productos', puntos, icono: ICONO_PRODUCTO, miniatura: miniatura(p.portada_sha),
        titulo: p.nombre, detalle: [p.sku, p.marca].filter(Boolean).join(' · '),
        meta: `${money(p.precio)} · ${p.stock} ud.`, pastilla: estadoProducto(p),
        pista: '↵ vista previa · Ctrl+↵ editar',
        accion: (alt) => (alt ? elegirApp('editar', { varianteId: p.id, sku: p.sku }) : elegirApp('preview', { varianteId: p.id }, { titulo: p.nombre })),
      })
    }
    if (ordenes && consulta.length >= 2) {
      let n = 0
      for (const o of ordenes) {
        if (n >= MAX_PEDIDOS) break
        const numero = o.numero || o.external_id
        const puntos = Math.max(puntuar(numero, consulta), puntuar(o.comprador, consulta), puntuar(o.canal, consulta) * 0.6)
        if (puntos === 0) continue
        n++
        r.push({
          clave: `ped-${o.id}`, grupo: 'pedidos', puntos, icono: ICONO_PEDIDO, titulo: numero,
          detalle: [o.comprador, o.canal].filter(Boolean).join(' · '), meta: money(o.total),
          pastilla: ESTADOS_PEDIDO[o.estado] ?? { texto: o.estado, clase: 'neutra' },
          pista: '↵ abrir Pedidos', accion: () => elegirApp('pedidos'),
        })
      }
    }
    let n = 0
    for (const rec of RECORRIDOS) {
      if (n >= MAX_GUIA) break
      const puntos = Math.max(puntuar(rec.nombre, consulta), puntuar(rec.resumen, consulta) * 0.6)
      if (puntos > 0) {
        r.push({ clave: `guia-${rec.clave}`, grupo: 'guia', puntos, icono: ICONO_GUIA, titulo: rec.nombre, detalle: rec.resumen, pista: '↵ ver el recorrido', accion: () => { cerrar(); abrirGuia(sistema, rec.clave, 0) } })
        n++
      }
      rec.pasos.forEach((paso, i) => {
        if (n >= MAX_GUIA) return
        const pp = puntuar(paso.titulo, consulta) * 0.9
        if (pp > 0) {
          r.push({ clave: `guia-${rec.clave}-${i}`, grupo: 'guia', puntos: pp, icono: ICONO_GUIA, titulo: paso.titulo, detalle: `${rec.nombre} · paso ${i + 1}`, pista: '↵ ir al paso', accion: () => { cerrar(); abrirGuia(sistema, rec.clave, i) } })
          n++
        }
      })
    }
    return ordenar(r)
  }, [hayBusqueda, modoAcciones, consulta, acciones, todas, productos, ordenes, elegirApp, cerrar, sistema])

  // Grupos presentes, en el orden en que salen los resultados ya ordenados.
  const grupos = useMemo(() => {
    const vistos: Grupo[] = []
    for (const x of resultados) if (!vistos.includes(x.grupo)) vistos.push(x.grupo)
    return vistos
  }, [resultados])

  // Si cambian los resultados, la selección no puede apuntar fuera de ellos.
  useEffect(() => { setSel((s) => Math.min(s, Math.max(0, resultados.length - 1))) }, [resultados.length])
  useEffect(() => {
    listaRef.current?.querySelector<HTMLElement>(`[data-idx="${sel}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  // ----- teclado del buscador
  const teclasBuscador = (e: KeyboardEventReact<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (hayBusqueda) setSel((s) => Math.min(s + 1, Math.max(0, resultados.length - 1)))
      // Sin búsqueda, ↓ baja al primer mosaico.
      else izqRef.current?.querySelector<HTMLElement>('.inicio-mosaico, .inicio-fila')?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSel((s) => Math.max(s - 1, 0))
    } else if (e.key === 'Enter') {
      const r = resultados[sel]
      if (r) { e.preventDefault(); r.accion(e.ctrlKey || e.metaKey) }
    } else if (e.key === 'Tab' && !e.shiftKey) {
      // Tab salta a «Tu día»; lo de en medio se recorre con las flechas.
      const primero = panel.current?.querySelector<HTMLElement>('.inicio-der button:not(:disabled)')
      if (primero) { e.preventDefault(); primero.focus() }
    }
  }

  // ----- teclado del diálogo: Escape y foco atrapado.
  const teclasDialogo = (e: KeyboardEventReact<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      if (menu) { setMenu(null); return }
      if (e.target === input.current && hayBusqueda) { setQ(''); return }
      if (vista === 'todas' && !hayBusqueda) { setVista('inicio'); return }
      cerrar()
      return
    }
    if (e.key !== 'Tab') return
    const focables = Array.from(panel.current?.querySelectorAll<HTMLElement>(
      'input, button:not(:disabled), [tabindex]:not([tabindex="-1"])',
    ) ?? []).filter((el) => el.offsetParent !== null)
    if (focables.length === 0) return
    const primero = focables[0]
    const ultimo = focables[focables.length - 1]
    if (e.shiftKey && document.activeElement === primero) { e.preventDefault(); ultimo.focus() }
    else if (!e.shiftKey && document.activeElement === ultimo) { e.preventDefault(); primero.focus() }
  }

  // ----- menú contextual de una app
  const contextualApp = (app: AppId, e: MouseEventReact<HTMLElement>) => {
    e.preventDefault()
    const anclada = prefs.ancladas.includes(app)
    const enEscritorio = prefs.escritorio.includes(app)
    setMenu({
      x: e.clientX, y: e.clientY,
      opciones: [
        { etiqueta: 'Abrir', accion: () => elegirApp(app) },
        { separador: true },
        {
          etiqueta: anclada ? 'Desanclar de la barra' : 'Anclar a la barra',
          accion: () => poner({ ancladas: anclada ? prefs.ancladas.filter((a) => a !== app) : [...prefs.ancladas, app] }),
        },
        {
          etiqueta: enEscritorio ? 'Quitar del escritorio' : 'Añadir al escritorio',
          accion: () => poner({ escritorio: enEscritorio ? prefs.escritorio.filter((a) => a !== app) : [...prefs.escritorio, app] }),
        },
      ],
    })
  }

  const clases = ['inicio']
  if (prefs.barraCentrada) clases.push('centrada')
  if (sistema.movil) clases.push('movil')
  if (apilado) clases.push('apilado')
  if (cerrando) clases.push('cerrando')

  const ahora = new Date()
  let idx = -1

  return createPortal(
    <div ref={panel} className={clases.join(' ')} role="dialog" aria-modal="true" aria-label="Inicio" onKeyDown={teclasDialogo}>
      <div className="inicio-paneles">
        <div ref={izqRef} className="inicio-izq">
          <div className="inicio-buscador">
            <span className="inicio-buscador-icono" aria-hidden>{ICONO_LUPA}</span>
            <input
              ref={input}
              type="search"
              value={q}
              placeholder={modoAcciones ? 'Escribe una acción…' : 'Buscar apps, productos, pedidos y ayuda'}
              aria-label="Buscar"
              autoComplete="off"
              spellCheck={false}
              role="combobox"
              aria-expanded={hayBusqueda}
              aria-controls="inicio-resultados"
              aria-activedescendant={hayBusqueda && resultados[sel] ? `inicio-r-${sel}` : undefined}
              aria-describedby="inicio-buscador-pista"
              onChange={(e) => { setQ(e.target.value); setSel(0) }}
              onKeyDown={teclasBuscador}
            />
            {buscando
              ? <span className="inicio-buscando" aria-live="polite">Buscando…</span>
              : hayBusqueda
                ? <button type="button" className="inicio-limpiar" aria-label="Vaciar la búsqueda" onClick={() => { setQ(''); input.current?.focus() }}>×</button>
                : <kbd className="inicio-kbd" title="Abrir y cerrar el inicio">Meta</kbd>}
            <span id="inicio-buscador-pista" hidden>Flechas para moverte, Enter para abrir, «&gt;» para ver solo acciones</span>
          </div>

          {hayBusqueda ? (
            <div ref={listaRef} id="inicio-resultados" className="inicio-resultados" role="listbox" aria-label="Resultados">
              {resultados.length === 0 && !buscando && (
                <div className="inicio-vacio">
                  <p>Nada que coincida con «{q.trim()}».</p>
                  <div className="inicio-vacio-sugerencias">
                    <button type="button" onClick={() => elegirApp('catalogo')}>Abrir Productos</button>
                    <button type="button" onClick={() => elegirApp('ayuda')}>Ver la ayuda</button>
                    {!modoAcciones && <button type="button" onClick={() => { setQ('>'); input.current?.focus() }}>Ver acciones</button>}
                  </div>
                </div>
              )}
              {grupos.map((g) => (
                <section key={g} className="inicio-grupo" role="group" aria-label={TITULOS[g]}>
                  <h3>{TITULOS[g]}</h3>
                  {resultados.filter((x) => x.grupo === g).map((x) => {
                    idx++
                    const i = idx
                    const activo = i === sel
                    return (
                      <button
                        key={x.clave}
                        type="button"
                        id={`inicio-r-${i}`}
                        data-idx={i}
                        role="option"
                        aria-selected={activo}
                        tabIndex={-1}
                        className={`inicio-resultado${activo ? ' seleccionado' : ''}`}
                        onMouseEnter={() => setSel(i)}
                        onClick={(e) => x.accion(e.ctrlKey || e.metaKey)}
                      >
                        {x.miniatura
                          ? <img className="inicio-resultado-foto" src={x.miniatura} alt="" loading="lazy" />
                          : <span className={`inicio-resultado-icono${x.color ? '' : ' neutro'}`} style={x.color ? { background: x.color } : undefined} aria-hidden>{x.icono}</span>}
                        <span className="inicio-resultado-texto">
                          <span className="inicio-resultado-titulo"><Resaltar texto={x.titulo} q={consulta} /></span>
                          {x.detalle && <span className="inicio-resultado-detalle"><Resaltar texto={x.detalle} q={consulta} /></span>}
                        </span>
                        <span className="inicio-resultado-lado">
                          {x.meta && <span className="inicio-resultado-meta">{x.meta}</span>}
                          {x.pastilla && <span className={`pastilla ${x.pastilla.clase}`}>{x.pastilla.texto}</span>}
                          {activo && x.pista && <span className="inicio-resultado-pista" aria-hidden>{x.pista}</span>}
                        </span>
                      </button>
                    )
                  })}
                </section>
              ))}
            </div>
          ) : vista === 'todas' ? (
            <TodasLasApps todas={todas} onAtras={() => setVista('inicio')} onAbrir={elegirApp} onContextual={contextualApp} />
          ) : (
            <div className="inicio-cuerpo inicio-vista">
              {ancladas.length > 0 && (
                <section className="inicio-seccion" aria-label="Ancladas">
                  <div className="inicio-seccion-cabecera">
                    <h3>Ancladas</h3>
                    <button type="button" className="inicio-enlace" onClick={() => setVista('todas')}>Todas las apps {ICONO_FLECHA}</button>
                  </div>
                  <Mosaicos ids={ancladas} insignia={insignia} onAbrir={elegirApp} onContextual={contextualApp}
                    onReordenar={(orden) => poner({ ancladas: orden })} />
                </section>
              )}
              <Recientes usuarioId={usuario.id} onAbrir={elegirApp} onMenu={(x, y, opciones) => setMenu({ x, y, opciones })} />
            </div>
          )}
        </div>

        <aside className="inicio-der" aria-label="Tu día">
          <div className="inicio-saludo">
            <div className="inicio-saludo-hola">{saludo(ahora)}, {usuario.name.split(' ')[0]}</div>
            <div className="inicio-saludo-fecha">{FMT_DIA.format(ahora)}</div>
          </div>
          <h3>Tu día</h3>
          <div className="inicio-tarjetas">
            <Tarjeta n={datos.resumen?.en_atencion ?? 0} texto="requieren atención" cero="Ningún producto pendiente" color={APPS.catalogo?.color} icono={APPS.catalogo?.icono} onClick={() => elegirApp('catalogo')} />
            <Tarjeta n={(datos.pedidos?.recibidos ?? 0) + (datos.pedidos?.fallidos ?? 0)}
              texto={datos.pedidos?.fallidos ? `pedidos: ${datos.pedidos.recibidos} recibidos, ${datos.pedidos.fallidos} fallidos` : 'pedidos recibidos'}
              cero="Sin pedidos por atender" color={APPS.pedidos?.color} icono={APPS.pedidos?.icono} grave={(datos.pedidos?.fallidos ?? 0) > 0} onClick={() => elegirApp('pedidos')} />
            <Tarjeta n={datos.tareasActivas} texto={datos.tareasActivas === 1 ? 'trabajo en marcha' : 'trabajos en marcha'} cero="Nada en marcha" color={APPS.actividad?.color} icono={APPS.actividad?.icono} latido onClick={() => elegirApp('actividad')} />
            <Tarjeta n={datos.alertas} texto={datos.alertas === 1 ? 'aviso abierto' : 'avisos abiertos'} cero="Sin avisos abiertos" color={APPS.automatizacion?.color} icono={APPS.automatizacion?.icono} onClick={() => elegirApp('automatizacion')} />
            <button type="button" className="inicio-tarjeta tenue" onClick={() => elegirApp('integraciones')}>
              <span className="inicio-tarjeta-icono" style={{ background: APPS.integraciones?.color }} aria-hidden>{ICONO_SYNC}</span>
              <span className="inicio-tarjeta-texto">
                <span className="inicio-tarjeta-titulo">
                  {datos.sincronizando ? 'Sincronizando…'
                    : datos.resumen?.ultima_sincronizacion ? `Sincronizado ${hace(datos.resumen.ultima_sincronizacion)}` : 'Sin sincronizar aún'}
                </span>
                <span className="inicio-tarjeta-sub">{datos.error ? 'Sin respuesta de la API' : 'Odoo → Integra'}</span>
              </span>
            </button>
          </div>
          <h3>Acciones rápidas</h3>
          <div className="inicio-acciones">
            <button type="button" className={`inicio-accion${datos.sincronizando ? ' ocupada' : ''}`} disabled={datos.sincronizando}
              aria-label={datos.sincronizando ? 'Sincronizando' : 'Sincronizar ahora'} onClick={() => { void datos.sincronizar() }}>
              <span className="inicio-accion-icono" aria-hidden>{ICONO_SYNC}</span>
              <span>{datos.sincronizando ? 'Sincronizando…' : 'Sincronizar ahora'}</span>
            </button>
            <button type="button" className="inicio-accion" aria-label="Publicar lo listo" onClick={() => elegirApp('publicacion')}>
              <span className="inicio-accion-icono" aria-hidden>{ICONO_RAYO}</span><span>Publicar lo listo</span>
            </button>
            <button type="button" className="inicio-accion" aria-label="Traer pedidos" onClick={() => elegirApp('pedidos')}>
              <span className="inicio-accion-icono" aria-hidden>{ICONO_PEDIDO}</span><span>Traer pedidos</span>
            </button>
            <button type="button" className="inicio-accion" aria-label={oscuro ? 'Cambiar a tema claro' : 'Cambiar a tema oscuro'} onClick={cambiarTema}>
              <span className="inicio-accion-icono" aria-hidden>{ICONO_TEMA}</span><span>{oscuro ? 'Tema claro' : 'Tema oscuro'}</span>
            </button>
            <button type="button" className="inicio-accion" aria-label="Bloquear (cerrar sesión)" onClick={bloquear}>
              <span className="inicio-accion-icono" aria-hidden>{ICONO_CANDADO}</span><span>Bloquear</span>
            </button>
          </div>
        </aside>
      </div>

      <footer className="inicio-pie">
        <div className="inicio-usuario">
          <span className="avatar" aria-hidden>{usuario.name.trim().charAt(0).toUpperCase() || '?'}</span>
          <span className="inicio-usuario-texto">
            <span className="inicio-usuario-nombre">{usuario.name}</span>
            <span className="inicio-usuario-rol">{nombreRol(usuario.role)}</span>
          </span>
        </div>
        <div className="inicio-pie-botones">
          <button type="button" aria-label="Configuración" onClick={() => elegirApp('configuracion')}>Configuración</button>
          <button type="button" aria-label="Ayuda" data-guia="ver-guia" onClick={() => elegirApp('ayuda')}>Ayuda</button>
          <button type="button" className="peligro" aria-label="Cerrar sesión" onClick={bloquear}>Cerrar sesión</button>
        </div>
      </footer>

      {menu && <MenuContextual x={menu.x} y={menu.y} opciones={menu.opciones} onCerrar={() => setMenu(null)} />}
    </div>,
    document.body,
  )
}

// Ordena por relevancia: dentro de cada grupo por puntos (estable), y los
// grupos entre sí por su mejor resultado, con ORDEN_GRUPOS de desempate.
function ordenar(r: Resultado[]): Resultado[] {
  const mejor = new Map<Grupo, number>()
  for (const x of r) mejor.set(x.grupo, Math.max(mejor.get(x.grupo) ?? 0, x.puntos))
  return r.map((x, i) => ({ x, i })).sort((a, b) => {
    if (a.x.grupo !== b.x.grupo) {
      const d = (mejor.get(b.x.grupo) ?? 0) - (mejor.get(a.x.grupo) ?? 0)
      return d !== 0 ? d : ORDEN_GRUPOS.indexOf(a.x.grupo) - ORDEN_GRUPOS.indexOf(b.x.grupo)
    }
    return b.x.puntos - a.x.puntos || a.i - b.i
  }).map(({ x }) => x)
}

// ------------------------------------------------------------- tarjeta

function Tarjeta({ n, texto, cero, color, icono, grave, latido, onClick }: {
  n: number; texto: string; cero: string; color?: string; icono?: ReactNode; grave?: boolean; latido?: boolean; onClick: () => void
}) {
  const hay = n > 0
  return (
    <button type="button" className={`inicio-tarjeta${hay ? (grave ? ' grave' : ' pendiente') : ' tenue'}`} onClick={onClick}>
      <span className="inicio-tarjeta-icono" style={{ background: color }} aria-hidden>{icono}</span>
      <span className="inicio-tarjeta-texto">
        {hay
          ? <span className="inicio-tarjeta-titulo"><strong>{n > 999 ? '999+' : n}</strong> {texto}</span>
          : <span className="inicio-tarjeta-titulo">{cero}</span>}
      </span>
      {hay && latido && <span className="punto-animado" aria-hidden />}
      <span className="inicio-tarjeta-flecha" aria-hidden>{ICONO_FLECHA}</span>
    </button>
  )
}

// ------------------------------------------------------------ mosaicos
// Las ancladas, en rejilla y reordenables arrastrando. Un arrastre no
// reordena en vivo (la rejilla saltaría bajo el dedo): resalta el mosaico
// sobre el que se está y se mueve al soltar.

function Mosaicos({ ids, insignia, onAbrir, onContextual, onReordenar }: {
  ids: AppId[]
  insignia: (app: AppId) => number
  onAbrir: (app: AppId) => void
  onContextual: (app: AppId, e: MouseEventReact<HTMLElement>) => void
  onReordenar: (orden: AppId[]) => void
}) {
  const [arrastre, setArrastre] = useState<{ id: AppId; dx: number; dy: number; sobre: number | null } | null>(null)
  const origen = useRef<{ id: AppId; idx: number; x: number; y: number; movido: boolean; sobre: number | null } | null>(null)
  // Tras un arrastre el navegador dispara click: hay que ignorarlo.
  const ignorarClick = useRef(false)

  const bajar = (id: AppId, idx: number, e: PointerEventReact<HTMLButtonElement>) => {
    if (e.button !== 0) return
    origen.current = { id, idx, x: e.clientX, y: e.clientY, movido: false, sobre: null }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  const mover = (e: PointerEventReact<HTMLButtonElement>) => {
    const o = origen.current
    if (!o) return
    const dx = e.clientX - o.x
    const dy = e.clientY - o.y
    if (!o.movido && Math.hypot(dx, dy) < 6) return
    o.movido = true
    // El mosaico arrastrado no recibe punteros (CSS): elementFromPoint ve el de debajo.
    const bajo = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('[data-mosaico]')
    o.sobre = bajo ? Number(bajo.dataset.mosaico) : null
    setArrastre({ id: o.id, dx, dy, sobre: o.sobre })
  }
  const soltar = (e: PointerEventReact<HTMLButtonElement>) => {
    const o = origen.current
    origen.current = null
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
    setArrastre(null)
    if (!o?.movido) return
    ignorarClick.current = true
    if (o.sobre !== null && o.sobre !== o.idx) {
      const orden = ids.filter((x) => x !== o.id)
      orden.splice(o.sobre, 0, o.id)
      onReordenar(orden)
    }
  }
  const pulsar = (id: AppId) => {
    if (ignorarClick.current) { ignorarClick.current = false; return }
    onAbrir(id)
  }

  return (
    <div className="inicio-mosaicos">
      {ids.map((id, i) => {
        const a = APPS[id]
        const n = insignia(id)
        const cls = ['inicio-mosaico']
        if (arrastre?.id === id) cls.push('arrastrando')
        if (arrastre && arrastre.sobre === i && arrastre.id !== id) cls.push('destino')
        const estilo = arrastre?.id === id
          ? { '--dx': `${arrastre.dx}px`, '--dy': `${arrastre.dy}px`, '--i': i } as CSSProperties
          : { '--i': i } as CSSProperties
        return (
          <button key={id} type="button" className={cls.join(' ')} data-guia={`menu-${id}`} data-mosaico={i}
            style={estilo} title={a.descripcion} aria-label={n > 0 ? `${a.nombre}, ${n} pendientes` : a.nombre}
            onClick={() => pulsar(id)} onContextMenu={(e) => onContextual(id, e)}
            onPointerDown={(e) => bajar(id, i, e)} onPointerMove={mover} onPointerUp={soltar} onPointerCancel={soltar}>
            <span className="inicio-mosaico-icono" style={{ background: a.color }} aria-hidden>
              {a.icono}
              {n > 0 && <span className="inicio-insignia">{n > 99 ? '99+' : n}</span>}
            </span>
            <span className="inicio-mosaico-nombre">{a.nombre}</span>
          </button>
        )
      })}
    </div>
  )
}

// ----------------------------------------------------------- recientes

function Recientes({ usuarioId, onAbrir, onMenu }: {
  usuarioId: number
  onAbrir: (app: AppId, props?: Record<string, unknown>, opciones?: { titulo?: string }) => void
  onMenu: (x: number, y: number, opciones: OpcionMenu[]) => void
}) {
  const [lista, setLista] = useState(() => leerRecientes(usuarioId))
  useEffect(() => {
    const f = () => setLista(leerRecientes(usuarioId))
    f()
    window.addEventListener(EVENTO_RECIENTES, f)
    return () => window.removeEventListener(EVENTO_RECIENTES, f)
  }, [usuarioId])

  // Portada y nombre de los que aún no lo tienen: la vista previa se abre
  // solo con el id, así que se pregunta una vez y queda guardado.
  const pedidos = useRef(new Set<string>())
  useEffect(() => {
    for (const r of lista.slice(0, MAX_RECIENTES_VISIBLES)) {
      const id = identidadReciente(r.app, r.props)
      const vid = r.props.varianteId
      if (typeof vid !== 'number' || pedidos.current.has(id)) continue
      const faltaPortada = r.portada_sha === undefined
      const faltaNombre = r.titulo === APPS[r.app]?.nombre
      if (!faltaPortada && !faltaNombre) continue
      pedidos.current.add(id)
      if (faltaPortada) {
        api.imagenes(vid)
          .then((im) => actualizarReciente(usuarioId, r.app, r.props, { portada_sha: (im.find((x) => x.principal) ?? im[0])?.sha256 ?? '' }))
          .catch(() => { /* sin portada se pinta el icono de la app */ })
      }
      if (faltaNombre) {
        api.preview(vid)
          .then((p) => actualizarReciente(usuarioId, r.app, r.props, {
            titulo: p.producto.nombre_odoo || r.titulo,
            detalle: [p.producto.sku, p.producto.marca].filter(Boolean).join(' · '),
          }))
          .catch(() => { /* se queda el título de la ventana */ })
      }
    }
  }, [lista, usuarioId])

  const visibles = lista.slice(0, MAX_RECIENTES_VISIBLES)
  if (visibles.length === 0) {
    // Vacío pero presente: quien lo ve la primera vez sabe qué va a pasar aquí.
    return (
      <section className="inicio-seccion" aria-label="Recientes">
        <div className="inicio-seccion-cabecera"><h3>Recientes</h3></div>
        <p className="inicio-recientes-vacio">Las fichas que abras (vista previa, editor, editor de fotos) aparecerán aquí.</p>
      </section>
    )
  }

  const contextual = (r: Reciente, e: MouseEventReact<HTMLElement>) => {
    e.preventDefault()
    onMenu(e.clientX, e.clientY, [
      { etiqueta: 'Abrir', accion: () => onAbrir(r.app, r.props, { titulo: r.titulo }) },
      { separador: true },
      { etiqueta: 'Quitar de recientes', accion: () => quitarReciente(usuarioId, r) },
      { etiqueta: 'Vaciar recientes', accion: () => guardarRecientes(usuarioId, []) },
    ])
  }

  return (
    <section className="inicio-seccion" aria-label="Recientes">
      <div className="inicio-seccion-cabecera"><h3>Recientes</h3></div>
      <div className="inicio-recientes">
        {visibles.map((r) => {
          const a = APPS[r.app]
          const foto = miniatura(r.portada_sha)
          return (
            <button key={identidadReciente(r.app, r.props)} type="button" className="inicio-reciente"
              title={r.titulo} onClick={() => onAbrir(r.app, r.props, { titulo: r.titulo })} onContextMenu={(e) => contextual(r, e)}>
              {foto
                ? <img className="inicio-reciente-foto" src={foto} alt="" loading="lazy" />
                : <span className="inicio-reciente-icono" style={{ background: a?.color }} aria-hidden>{a?.icono}</span>}
              <span className="inicio-reciente-texto">
                <span className="inicio-reciente-titulo">{r.titulo}</span>
                <span className="inicio-reciente-sub">{r.detalle ?? a?.nombre} · {hace(r.cuando)}</span>
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}

// ------------------------------------------------------- todas las apps
// Lista alfabética con índice de letras: solo las letras que existen, y
// pulsar una desplaza la lista hasta su grupo.

function TodasLasApps({ todas, onAtras, onAbrir, onContextual }: {
  todas: { id: AppId; nombre: string; descripcion: string; icono: ReactNode; color: string }[]
  onAtras: () => void
  onAbrir: (app: AppId) => void
  onContextual: (app: AppId, e: MouseEventReact<HTMLElement>) => void
}) {
  // El contenedor que se desplaza (en apilado no se desplaza él sino el
  // conjunto: entonces la letra activa solo cambia al pulsarla).
  const contenedor = useRef<HTMLDivElement>(null)
  const grupos = useMemo(() => {
    const m = new Map<string, typeof todas>()
    for (const a of todas) {
      const letra = normalizar(a.nombre.charAt(0)).toUpperCase() || '#'
      m.set(letra, [...(m.get(letra) ?? []), a])
    }
    return Array.from(m.entries())
  }, [todas])
  const [letraActiva, setLetraActiva] = useState(grupos[0]?.[0] ?? '')

  // La letra activa sigue al desplazamiento.
  useEffect(() => {
    const el = contenedor.current
    if (!el) return
    const f = () => {
      const tope = el.getBoundingClientRect().top + 60
      let actual = grupos[0]?.[0] ?? ''
      for (const [letra] of grupos) {
        const s = el.querySelector<HTMLElement>(`[data-letra="${letra}"]`)
        if (s && s.getBoundingClientRect().top <= tope) actual = letra
      }
      setLetraActiva(actual)
    }
    el.addEventListener('scroll', f, { passive: true })
    return () => el.removeEventListener('scroll', f)
  }, [grupos, contenedor])

  const saltar = (letra: string) => {
    contenedor.current?.querySelector<HTMLElement>(`[data-letra="${letra}"]`)?.scrollIntoView({ block: 'start', behavior: reducirMovimiento() ? 'auto' : 'smooth' })
    setLetraActiva(letra)
  }

  return (
    <div className="inicio-cuerpo inicio-vista inicio-todas">
      <div className="inicio-seccion-cabecera pegada">
        <button type="button" className="inicio-enlace" onClick={onAtras} aria-label="Volver al inicio">
          <span className="inicio-enlace-atras" aria-hidden>{ICONO_FLECHA}</span> Atrás
        </button>
        <h3>Todas las apps</h3>
      </div>
      <div ref={contenedor} className="inicio-todas-cuerpo">
        <div className="inicio-lista">
          {grupos.map(([letra, apps]) => (
            <section key={letra} data-letra={letra} className="inicio-letra" aria-label={`Apps con ${letra}`}>
              <h4>{letra}</h4>
              {apps.map((a) => (
                <button key={a.id} type="button" className="inicio-fila" data-guia={`menu-${a.id}`}
                  onClick={() => onAbrir(a.id)} onContextMenu={(e) => onContextual(a.id, e)}>
                  <span className="inicio-fila-icono" style={{ background: a.color }} aria-hidden>{a.icono}</span>
                  <span className="inicio-fila-texto">
                    <span className="inicio-fila-nombre">{a.nombre}</span>
                    <span className="inicio-fila-desc">{a.descripcion}</span>
                  </span>
                </button>
              ))}
            </section>
          ))}
        </div>
        <nav className="inicio-abc" aria-label="Ir a la letra">
          {grupos.map(([letra]) => (
            <button key={letra} type="button" className={letra === letraActiva ? 'activa' : undefined}
              aria-label={`Ir a ${letra}`} aria-current={letra === letraActiva ? 'true' : undefined} onClick={() => saltar(letra)}>
              {letra}
            </button>
          ))}
        </nav>
      </div>
    </div>
  )
}
