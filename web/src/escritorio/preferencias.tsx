import {
  createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode,
} from 'react'
import type { AppId, Fondo, Preferencias } from './tipos'
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

function preferenciasPorDefecto(): Preferencias {
  return {
    tema: 'sistema',
    fondo: { tipo: 'preset', id: FONDOS[0].id },
    ancladas: ANCLADAS_DEFECTO.filter(id => APPS[id]),
    escritorio: ORDEN_APPS.filter(id => APPS[id]?.enEscritorio),
    tamanoTexto: 'normal',
    widgets: { atencion: true, pedidos: true, actividad: true },
    barraCentrada: true,
  }
}

// Devuelve el `background` CSS de un fondo, sea preset, color o degradado.
// Lo usan el escritorio (para pintarlo) y la configuración (vista previa).
export function cssFondo(f: Fondo): string {
  if (f.tipo === 'color') return f.color
  if (f.tipo === 'degradado') return `linear-gradient(160deg, ${f.desde} 0%, ${f.hasta} 100%)`
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
    const g = JSON.parse(crudo) as Partial<Preferencias>
    const soloConocidas = (xs: unknown): AppId[] | null =>
      Array.isArray(xs) ? (xs as AppId[]).filter(id => typeof id === 'string' && APPS[id]) : null
    return {
      tema: g.tema === 'claro' || g.tema === 'oscuro' || g.tema === 'sistema' ? g.tema : base.tema,
      fondo: g.fondo && typeof g.fondo === 'object' && 'tipo' in g.fondo ? g.fondo : base.fondo,
      ancladas: soloConocidas(g.ancladas) ?? base.ancladas,
      escritorio: soloConocidas(g.escritorio) ?? base.escritorio,
      tamanoTexto: g.tamanoTexto === 'grande' ? 'grande' : 'normal',
      widgets: { ...base.widgets, ...(g.widgets ?? {}) },
      barraCentrada: typeof g.barraCentrada === 'boolean' ? g.barraCentrada : base.barraCentrada,
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
