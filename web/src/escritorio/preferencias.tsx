import {
  createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode,
} from 'react'
import type { AppId, Disposicion, Fondo, Preferencias, WidgetInstancia, WidgetTipo } from './tipos'
import { APPS, ORDEN_APPS } from './apps'
import { useSesion } from './sesion'

// Preferencias del escritorio: tema, fondo, apps ancladas, tamaño del texto…
// Se guardan por usuario en localStorage y se aplican al <html> como
// atributos de datos para que el CSS (tema.css) haga el resto sin JS.

// Fondos incluidos. Solo CSS (degradados) para no cargar imágenes ni pesar
// nada: el escritorio pinta `css` como `background`. Mezcla de claros y
// oscuros para que haya opción coherente con cualquiera de los dos temas.
export const FONDOS: { id: string; nombre: string; css: string }[] = [
  {
    id: 'amanecer', nombre: 'Amanecer',
    css: 'linear-gradient(160deg, #dbeafe 0%, #e0e7ff 40%, #fce7f3 100%)',
  },
  {
    id: 'niebla', nombre: 'Niebla',
    css: 'radial-gradient(120% 90% at 20% 10%, #f8fafc 0%, #e2e8f0 55%, #cbd5e1 100%)',
  },
  {
    id: 'menta', nombre: 'Menta',
    css: 'linear-gradient(135deg, #ecfdf5 0%, #d1fae5 50%, #cffafe 100%)',
  },
  {
    id: 'arena', nombre: 'Arena',
    css: 'linear-gradient(150deg, #fef3c7 0%, #fde68a 45%, #fdba74 100%)',
  },
  {
    id: 'lavanda', nombre: 'Lavanda',
    css: 'conic-gradient(from 210deg at 70% 30%, #ede9fe, #e0e7ff, #fae8ff, #ede9fe)',
  },
  {
    id: 'oceano', nombre: 'Océano',
    css: 'linear-gradient(165deg, #0f172a 0%, #1e3a8a 55%, #0e7490 100%)',
  },
  {
    id: 'medianoche', nombre: 'Medianoche',
    css: 'radial-gradient(110% 80% at 30% 0%, #1e293b 0%, #0f172a 60%, #020617 100%)',
  },
  {
    id: 'grafito', nombre: 'Grafito',
    css: 'linear-gradient(180deg, #27272a 0%, #18181b 100%)',
  },
  {
    id: 'aurora', nombre: 'Aurora',
    css: 'linear-gradient(200deg, #042f2e 0%, #134e4a 35%, #312e81 75%, #1e1b4b 100%)',
  },
  {
    id: 'brasa', nombre: 'Brasa',
    css: 'conic-gradient(from 140deg at 80% 80%, #1c1917, #431407, #7c2d12, #1c1917)',
  },
]

// Ancladas por defecto: las seis secciones del día a día.
const ANCLADAS_DEFECTO: AppId[] = ['panel', 'catalogo', 'mediateca', 'publicacion', 'pedidos', 'actividad']

// Disposición inicial de los widgets, en celdas de 24 px, pensada para
// 1920×1080 (78×41 celdas útiles descontando la barra y el margen). Los
// iconos van a la izquierda (se autocolocan en columnas); los widgets ocupan
// la mitad derecha: reloj arriba a la derecha, las cifras a su izquierda,
// debajo tres columnas (bloqueos | pedidos+actividad | sincronizar+bodegas)
// y la lista de prioridad ancha abajo. En pantallas menores el escritorio
// los encaja dentro del área sin tocar lo guardado.
function disposicionPorDefecto(): Disposicion {
  const w = (id: string, tipo: WidgetTipo, x: number, y: number, w: number, h: number): WidgetInstancia =>
    ({ id, tipo, x, y, w, h })
  return {
    iconos: {},
    widgets: [
      w('reloj', 'reloj', 62, 0, 16, 7),
      w('cifras', 'cifras', 32, 0, 29, 7),
      w('bloqueos', 'bloqueos', 32, 8, 15, 12),
      w('pedidos', 'pedidos', 48, 8, 13, 6),
      w('actividad', 'actividad', 48, 15, 13, 5),
      w('sincronizacion', 'sincronizacion', 62, 8, 16, 5),
      w('bodegas', 'bodegas', 62, 14, 16, 6),
      w('prioridad', 'prioridad', 32, 21, 46, 20),
    ],
  }
}

// La configuración la usa para «Restablecer disposición».
export { disposicionPorDefecto }

function preferenciasPorDefecto(): Preferencias {
  return {
    tema: 'sistema',
    fondo: { tipo: 'preset', id: FONDOS[0].id },
    ancladas: ANCLADAS_DEFECTO.filter(id => APPS[id]),
    escritorio: ORDEN_APPS.filter(id => APPS[id]?.enEscritorio),
    tamanoTexto: 'normal',
    barraCentrada: true,
    disposicion: disposicionPorDefecto(),
  }
}

// Tipos de widget válidos. Es un objeto y no una lista para que TypeScript
// avise si aparece un tipo nuevo en tipos.ts y se olvida aquí.
const TIPOS: Record<WidgetTipo, true> = {
  cifras: true, cifra: true, bloqueos: true, bodegas: true, prioridad: true,
  pedidos: true, actividad: true, sincronizacion: true, reloj: true, atajos: true,
}

const esEntero = (n: unknown): n is number => typeof n === 'number' && Number.isInteger(n) && n >= 0

// Sanea una disposición guardada: iconos de apps conocidas con posición
// entera, widgets de tipo conocido con geometría entera (tamaño ≥ 1). Lo que
// no cuadre se descarta en vez de romper el escritorio.
function sanearDisposicion(g: unknown): Disposicion | null {
  if (!g || typeof g !== 'object') return null
  const d = g as Partial<Disposicion>
  if (!Array.isArray(d.widgets) || !d.iconos || typeof d.iconos !== 'object') return null
  const iconos: Disposicion['iconos'] = {}
  for (const [id, p] of Object.entries(d.iconos as Record<string, unknown>)) {
    if (!APPS[id as AppId] || !p || typeof p !== 'object') continue
    const { x, y } = p as { x?: unknown; y?: unknown }
    if (esEntero(x) && esEntero(y)) iconos[id as AppId] = { x, y }
  }
  const vistos = new Set<string>()
  const widgets: WidgetInstancia[] = []
  for (const w of d.widgets as unknown[]) {
    if (!w || typeof w !== 'object') continue
    const i = w as Partial<WidgetInstancia>
    if (typeof i.tipo !== 'string' || !TIPOS[i.tipo as WidgetTipo]) continue
    if (!esEntero(i.x) || !esEntero(i.y) || !esEntero(i.w) || !esEntero(i.h) || i.w < 1 || i.h < 1) continue
    const id = typeof i.id === 'string' && i.id && !vistos.has(i.id) ? i.id : `${i.tipo}-${widgets.length}-${Date.now().toString(36)}`
    vistos.add(id)
    widgets.push({
      id, tipo: i.tipo as WidgetTipo, x: i.x, y: i.y, w: i.w, h: i.h,
      ...(i.config && typeof i.config === 'object' ? { config: i.config as Record<string, unknown> } : {}),
    })
  }
  return { iconos, widgets }
}

// Migración desde las prefs anteriores, que tenían `widgets: {atencion,
// pedidos, actividad}` como interruptores: se parte de la disposición por
// defecto y se quita lo que el usuario había apagado (atención era lo que
// hoy es «bloqueos»).
function migrarWidgetsViejos(viejo: unknown): Disposicion {
  const base = disposicionPorDefecto()
  if (!viejo || typeof viejo !== 'object') return base
  const v = viejo as Record<string, unknown>
  const apagados = new Set<WidgetTipo>()
  if (v.atencion === false) apagados.add('bloqueos')
  if (v.pedidos === false) apagados.add('pedidos')
  if (v.actividad === false) apagados.add('actividad')
  return { ...base, widgets: base.widgets.filter(w => !apagados.has(w.tipo)) }
}

// Fotos de Unsplash incluidas. Se enlazan a su CDN (images.unsplash.com),
// que sirve cualquier tamaño por parámetros y no exige clave: el escritorio
// pide 1920 px y la configuración, miniaturas de 320. Los identificadores
// son los del fichero de cada foto; el nombre es descriptivo, no el título
// del autor.
export const FOTOS: { id: string; nombre: string }[] = [
  { id: '1506905925346-21bda4d32df4', nombre: 'Cumbres sobre las nubes' },
  { id: '1469474968028-56623f02e42e', nombre: 'Colinas al atardecer' },
  { id: '1470770841072-f978cf4d019e', nombre: 'Cabaña en el lago' },
  { id: '1447752875215-b2761acb3c5d', nombre: 'Puente en el bosque' },
  { id: '1500530855697-b586d89ba3ee', nombre: 'Carretera entre rocas' },
  { id: '1519681393784-d120267933ba', nombre: 'Vía Láctea' },
  { id: '1501785888041-af3ef285b470', nombre: 'Lago turquesa' },
  { id: '1441974231531-c6227db76b6e', nombre: 'Bosque de pinos' },
  { id: '1475924156734-496f6cac6ec1', nombre: 'Costa al atardecer' },
  { id: '1483728642387-6c3bdd6c93e5', nombre: 'Picos nevados' },
  { id: '1508739773434-c26b3d09e071', nombre: 'Montañas al alba' },
  { id: '1536431311719-398b6704d4cc', nombre: 'Cumbre estrellada' },
  { id: '1557683316-973673baf926', nombre: 'Degradado azul' },
  { id: '1557682250-33bd709cbe85', nombre: 'Degradado violeta' },
  { id: '1618005182384-a83a8bd57fbe', nombre: 'Ondas de color' },
  { id: '1550684848-fac1c5b4e853', nombre: 'Gotas de color' },
  { id: '1493246507139-91e8fad9978e', nombre: 'Lago de montaña' },
  { id: '1472214103451-9374bd1c798e', nombre: 'Valle verde' },
  { id: '1490730141103-6cac27aaab94', nombre: 'Reflejo al atardecer' },
  { id: '1477346611705-65d1883cee1e', nombre: 'Cordillera al anochecer' },
]

// URL de una foto de Unsplash al ancho pedido (recorte proporcional).
export function urlFoto(id: string, ancho: number): string {
  return `https://images.unsplash.com/photo-${id}?auto=format&fit=crop&w=${ancho}&q=75`
}

// Devuelve el `background` CSS de un fondo, sea preset, color, degradado o foto.
// Lo usan el escritorio (para pintarlo) y la configuración (vista previa).
export function cssFondo(f: Fondo): string {
  if (f.tipo === 'color') return f.color
  if (f.tipo === 'degradado') return `linear-gradient(160deg, ${f.desde} 0%, ${f.hasta} 100%)`
  // Un color de base debajo de la foto: mientras carga, o si la red falla,
  // el escritorio no se queda en blanco.
  if (f.tipo === 'imagen') return `#1e293b url("${f.url}") center / cover no-repeat`
  return (FONDOS.find(x => x.id === f.id) ?? FONDOS[0]).css
}

function claveDe(usuarioId: number) { return `integra.escritorio.prefs.${usuarioId}` }

// Lee lo guardado y lo funde con los valores por defecto: si en una versión
// nueva aparece una preferencia, quien ya tenía prefs guardadas no se queda
// sin ella. Las listas de apps se filtran por si alguna app dejó de existir.
function cargar(usuarioId: number): Preferencias {
  const base = preferenciasPorDefecto()
  try {
    const crudo = localStorage.getItem(claveDe(usuarioId))
    if (!crudo) return base
    const g = JSON.parse(crudo) as Partial<Preferencias> & { widgets?: unknown }
    const soloConocidas = (xs: unknown): AppId[] | null =>
      Array.isArray(xs) ? (xs as AppId[]).filter(id => typeof id === 'string' && APPS[id]) : null
    return {
      tema: g.tema === 'claro' || g.tema === 'oscuro' || g.tema === 'sistema' ? g.tema : base.tema,
      fondo: g.fondo && typeof g.fondo === 'object' && 'tipo' in g.fondo ? g.fondo : base.fondo,
      ancladas: soloConocidas(g.ancladas) ?? base.ancladas,
      escritorio: soloConocidas(g.escritorio) ?? base.escritorio,
      tamanoTexto: g.tamanoTexto === 'grande' ? 'grande' : 'normal',
      barraCentrada: typeof g.barraCentrada === 'boolean' ? g.barraCentrada : base.barraCentrada,
      disposicion: sanearDisposicion(g.disposicion) ?? migrarWidgetsViejos(g.widgets),
    }
  } catch {
    return base
  }
}

type Contexto = { prefs: Preferencias; poner: (p: Partial<Preferencias>) => void }
const Ctx = createContext<Contexto | null>(null)

export function ProveedorPreferencias({ children }: { children: ReactNode }) {
  const { usuario } = useSesion()
  const [prefs, setPrefs] = useState<Preferencias>(() => cargar(usuario.id))

  // Si cambia el usuario (salir y entrar con otro sin recargar), se leen sus
  // preferencias, no las del anterior.
  useEffect(() => { setPrefs(cargar(usuario.id)) }, [usuario.id])

  const poner = useCallback((p: Partial<Preferencias>) => {
    setPrefs(actual => {
      const nuevo = { ...actual, ...p }
      try {
        localStorage.setItem(claveDe(usuario.id), JSON.stringify(nuevo))
      } catch {
        // Sin espacio o en modo privado: se pierde al recargar, nada más.
      }
      return nuevo
    })
  }, [usuario.id])

  // Tema → <html data-tema="claro|oscuro"> y color-scheme. Con 'sistema' se
  // sigue la preferencia del SO y se reacciona si cambia en caliente.
  useEffect(() => {
    const html = document.documentElement
    const aplicar = (oscuro: boolean) => {
      html.dataset.tema = oscuro ? 'oscuro' : 'claro'
      html.style.colorScheme = oscuro ? 'dark' : 'light'
    }
    if (prefs.tema !== 'sistema') { aplicar(prefs.tema === 'oscuro'); return }
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    aplicar(mq.matches)
    const alCambiar = (e: MediaQueryListEvent) => aplicar(e.matches)
    mq.addEventListener('change', alCambiar)
    return () => mq.removeEventListener('change', alCambiar)
  }, [prefs.tema])

  // Tamaño de texto → <html data-texto="grande"> (tema.css escala a partir de ahí).
  useEffect(() => {
    const html = document.documentElement
    if (prefs.tamanoTexto === 'grande') html.dataset.texto = 'grande'
    else delete html.dataset.texto
  }, [prefs.tamanoTexto])

  const valor = useMemo(() => ({ prefs, poner }), [prefs, poner])
  return <Ctx.Provider value={valor}>{children}</Ctx.Provider>
}

export function usePreferencias(): Contexto {
  const c = useContext(Ctx)
  if (!c) throw new Error('usePreferencias fuera de ProveedorPreferencias')
  return c
}
