import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode,
} from 'react'
import type { AppDef, AppId, EstadoVentana, Evento, Pagina, Pestana, Sistema, Ventana } from './tipos'
import { APPS } from './apps'
import { useSesion } from './sesion'

// Gestor de ventanas del escritorio: quién está abierta, dónde, en qué estado
// y delante de quién. Es el único sitio que toca la lista de ventanas; el
// marco (Ventana.tsx), la barra y el menú de inicio solo piden cambios.
//
// Cada ventana tiene pestañas (ver Pestana en tipos.ts) y cada pestaña un
// historial de páginas (ver Pagina): abrir un producto desde Productos no
// abre otra ventana, navega dentro de la pestaña, y ← / → se mueven por lo
// visitado como en un navegador. La ventana copia `historial`, `indice`,
// `app`, `props` y `titulo` de la pestaña activa para que quien no sabe de
// pestañas (barra de tareas, inicio, guía) siga leyendo lo de siempre.
//
// Toda acción pasa por `actualizar`, que trabaja sobre un espejo en ref de la
// lista y no sobre el estado de React: así dos acciones seguidas en el mismo
// manejador (restaurar + mover, cerrar + abrir…) ven la lista ya cambiada
// por la anterior, y `abrir` puede devolver el id de forma síncrona.

// Alto de la barra de tareas: el área de trabajo es la pantalla menos esto.
export const ALTO_BARRA = 52

// Desplazamiento entre ventanas abiertas en cascada, y esquina de origen.
const PASO_CASCADA = 28
const ORIGEN_CASCADA = 24
// Una ventana movida o redimensionada puede asomar por los lados, pero deja
// siempre esta franja de barra de título dentro del área para poder cogerla.
const TITULO_VISIBLE = 40
const RETRASO_GUARDADO = 300
const ANCHO_MOVIL = 768
const ESTADOS: EstadoVentana[] = ['normal', 'minimizada', 'maximizada', 'izquierda', 'derecha']

type Area = { w: number; h: number }
type Geometria = { x: number; y: number; w: number; h: number }
type Tamano = { w: number; h: number }

function medirArea(): Area {
  return { w: window.innerWidth, h: Math.max(0, window.innerHeight - ALTO_BARRA) }
}

function esMovil(): boolean {
  return window.innerWidth < ANCHO_MOVIL
}

// Tamaño: nunca por debajo del mínimo de la app ni por encima del área. Si el
// área es más pequeña que el mínimo (móvil, ventana diminuta) gana el mínimo:
// el marco ya la pinta a pantalla completa en ese caso.
function acotarTamano(t: Tamano, minimo: Tamano, area: Area): Tamano {
  return {
    w: Math.max(minimo.w, Math.min(Math.round(t.w), area.w)),
    h: Math.max(minimo.h, Math.min(Math.round(t.h), area.h)),
  }
}

// Posición laxa (mover, redimensionar): puede salirse por los lados y por
// abajo, nunca por arriba, y siempre con TITULO_VISIBLE px de barra dentro.
function acotarPosicion(g: Geometria, area: Area): Geometria {
  return {
    ...g,
    x: Math.round(Math.min(Math.max(g.x, TITULO_VISIBLE - g.w), area.w - TITULO_VISIBLE)),
    y: Math.round(Math.min(Math.max(g.y, 0), Math.max(0, area.h - TITULO_VISIBLE))),
  }
}

// Posición estricta (abrir, restaurar de localStorage): entera dentro del
// área si cabe; si no cabe, pegada al origen.
function encajar(g: Geometria, area: Area): Geometria {
  return {
    ...g,
    x: Math.round(Math.max(0, Math.min(g.x, area.w - g.w))),
    y: Math.round(Math.max(0, Math.min(g.y, area.h - g.h))),
  }
}

// Geometría que impone un estado fijo (maximizada o ajustada a una mitad), o
// null si el estado deja la geometría en manos del usuario.
function geometriaEstado(estado: EstadoVentana, area: Area): Geometria | null {
  const mitad = Math.floor(area.w / 2)
  switch (estado) {
    case 'maximizada': return { x: 0, y: 0, w: area.w, h: area.h }
    case 'izquierda': return { x: 0, y: 0, w: mitad, h: area.h }
    case 'derecha': return { x: mitad, y: 0, w: area.w - mitad, h: area.h }
    default: return null
  }
}

// Al salir de `normal` se guarda la geometría para volver a ella al restaurar;
// si ya se había salido (maximizada → minimizada) se conserva la que había.
function conAnterior(v: Ventana): Ventana['anterior'] {
  return v.estado === 'normal' ? { x: v.x, y: v.y, w: v.w, h: v.h } : v.anterior
}

function claveDe(usuarioId: number): string {
  return `integra.escritorio.ventanas.${usuarioId}`
}

// La pestaña que se ve. Si `pestanaActiva` no apunta a ninguna (no debería
// pasar) se toma la primera, para que la ventana nunca se quede sin página.
export function pestanaActivaDe(v: Ventana): Pestana {
  return v.pestanas.find(t => t.id === v.pestanaActiva) ?? v.pestanas[0]
}

// La ventana con sus pestañas puestas y la página actual de la activa
// copiada a `historial`, `indice`, `app`, `props` y `titulo`. Toda mutación
// de pestañas o historiales pasa por aquí para que las copias nunca se
// desincronicen.
function conPestanas(v: Ventana, pestanas: Pestana[], pestanaActiva: string): Ventana {
  const t = pestanas.find(x => x.id === pestanaActiva) ?? pestanas[0]
  const p = t.historial[t.indice]
  return {
    ...v, pestanas, pestanaActiva: t.id, historial: t.historial, indice: t.indice, app: p.app, props: p.props, titulo: p.titulo,
  }
}

// Cambia el historial de una pestaña concreta (la activa si no se dice).
function conHistorial(v: Ventana, historial: Pagina[], indice: number, pestanaId = v.pestanaActiva): Ventana {
  const pestanas = v.pestanas.map(t => (t.id === pestanaId ? { ...t, historial, indice } : t))
  return conPestanas(v, pestanas, v.pestanaActiva)
}

// Índice de la última página del historial que enseña `app`, o -1.
function indiceDe(t: Pestana, app: AppId): number {
  for (let i = t.historial.length - 1; i >= 0; i--) if (t.historial[i].app === app) return i
  return -1
}

// Del id de pestaña `${ventana}/t${n}` saca n (ver numeroDeId).
function numeroDePestana(id: string): number {
  const m = /\/t(\d+)$/.exec(id)
  return m ? Number(m[1]) : 0
}

// Lo que va a localStorage: por cada ventana, la página base de cada
// pestaña cuya base sea una app única (Productos, Pedidos, Publicación…).
// Lo navegado desde ahí (un editor, una previa) apunta a cosas que pueden
// haber cambiado y no se restaura; cada pestaña vuelve a empezar con una
// sola página, y las pestañas de diálogos desaparecen. `app`, `props` y
// `titulo` de la primera se repiten arriba por compatibilidad con lo
// guardado antes de haber pestañas (ver `cargar`).
function paraGuardar(lista: Ventana[]): Record<string, unknown>[] {
  const salida: Record<string, unknown>[] = []
  for (const v of lista) {
    const pestanas = v.pestanas
      .filter(t => APPS[t.historial[0]?.app]?.unica)
      .map(t => ({ app: t.historial[0].app, props: t.historial[0].props, titulo: t.historial[0].titulo }))
    if (pestanas.length === 0) continue
    const activa = v.pestanas.filter(t => APPS[t.historial[0]?.app]?.unica).findIndex(t => t.id === v.pestanaActiva)
    const { historial: _h, indice: _i, pestanas: _p, pestanaActiva: _a, ...resto } = v
    salida.push({ ...resto, ...pestanas[0], pestanas, activa: Math.max(0, activa) })
  }
  return salida
}

// Lee las ventanas guardadas y descarta lo que ya no tenga sentido: apps que
// no existen, diálogos (sus props apuntan a cosas que pueden haber cambiado),
// entradas malformadas y duplicados. La geometría se acota al área actual,
// que puede no ser la de la sesión anterior.
function cargar(clave: string, area: Area): Ventana[] {
  let crudo: string | null
  try {
    crudo = localStorage.getItem(clave)
  } catch {
    return []
  }
  if (!crudo) return []
  let lista: unknown
  try {
    lista = JSON.parse(crudo)
  } catch {
    return []
  }
  if (!Array.isArray(lista)) return []

  const salida: Ventana[] = []
  const ids = new Set<string>()
  // Apps ya restauradas en otras ventanas: una app única no va en dos
  // ventanas. Dentro de la misma ventana sí puede repetirse (Ctrl+T en
  // Productos abre otro Productos), así que se comprueba por ventana.
  const enOtras = new Set<AppId>()
  for (const cruda of lista as unknown[]) {
    if (!cruda || typeof cruda !== 'object') continue
    const v = cruda as Record<string, unknown>
    if (typeof v.id !== 'string' || ids.has(v.id)) continue

    // Formato con pestañas, o el anterior (una sola página arriba).
    const crudas = Array.isArray(v.pestanas) ? (v.pestanas as unknown[]) : [v]
    const bases: { app: AppId; def: AppDef; props: Record<string, unknown>; titulo: string }[] = []
    for (const c of crudas) {
      if (!c || typeof c !== 'object') continue
      const t = c as Record<string, unknown>
      const app = t.app as AppId
      const def = APPS[app]
      if (!def || !def.unica || enOtras.has(app)) continue
      bases.push({
        app,
        def,
        titulo: typeof t.titulo === 'string' && t.titulo ? t.titulo : def.nombre,
        props: t.props && typeof t.props === 'object' ? (t.props as Record<string, unknown>) : {},
      })
    }
    if (bases.length === 0) continue
    ids.add(v.id)
    for (const b of bases) enOtras.add(b.app)
    // La geometría mínima y por defecto es la de la primera pestaña, que
    // es la app con la que se abrió la ventana.
    const def = bases[0].def

    const estado = ESTADOS.includes(v.estado as EstadoVentana) ? (v.estado as EstadoVentana) : 'normal'
    const numero = (n: unknown, sino: number) => (typeof n === 'number' && Number.isFinite(n) ? n : sino)
    const libre = encajar({
      x: numero(v.x, ORIGEN_CASCADA),
      y: numero(v.y, ORIGEN_CASCADA),
      ...acotarTamano({ w: numero(v.w, def.tamano.w), h: numero(v.h, def.tamano.h) }, def.minimo, area),
    }, area)
    const a = v.anterior as Record<string, unknown> | undefined
    const anterior = a && typeof a === 'object'
      ? encajar({
        x: numero(a.x, libre.x),
        y: numero(a.y, libre.y),
        ...acotarTamano({ w: numero(a.w, def.tamano.w), h: numero(a.h, def.tamano.h) }, def.minimo, area),
      }, area)
      : undefined
    const fija = geometriaEstado(estado, area)

    // Cada pestaña se restaura con una sola página: su base (ver
    // paraGuardar). Los ids de pestaña se numeran de nuevo desde 1.
    const pestanas: Pestana[] = bases.map((b, k) => {
      const id = `${v.id}/t${k + 1}`
      return { id, historial: [{ clave: `${id}/0`, app: b.app, props: b.props, titulo: b.titulo }], indice: 0 }
    })
    const activa = typeof v.activa === 'number' && pestanas[v.activa] ? pestanas[v.activa] : pestanas[0]
    salida.push(conPestanas({
      id: v.id,
      app: activa.historial[0].app,
      titulo: activa.historial[0].titulo,
      props: activa.historial[0].props,
      historial: activa.historial,
      indice: 0,
      pestanas,
      pestanaActiva: activa.id,
      ...(fija ?? libre),
      estado,
      z: numero(v.z, 0),
      // Una ventana que se guardó fuera de `normal` sin geometría a la que
      // volver la recupera del tamaño por defecto para que restaurar funcione.
      anterior: anterior ?? (estado === 'normal' ? undefined : libre),
    }, pestanas, activa.id))
  }
  return salida
}

// Del id `${app}-${n}` saca n, para que el contador siga tras restaurar.
function numeroDeId(id: string): number {
  const m = /-(\d+)$/.exec(id)
  return m ? Number(m[1]) : 0
}

const Contexto = createContext<Sistema | null>(null)

export function ProveedorSistema({ children }: { children: ReactNode }) {
  const { usuario } = useSesion()
  const clave = claveDe(usuario.id)

  const [pantalla, setPantalla] = useState(() => ({ area: medirArea(), movil: esMovil() }))
  const areaRef = useRef(pantalla.area)
  const [ventanas, setVentanas] = useState<Ventana[]>(() => cargar(clave, areaRef.current))
  const ventanasRef = useRef(ventanas)
  // Contadores: z creciente (nunca se reordena el array, solo sube el z de la
  // enfocada), número de ventana para los ids y posición en la cascada.
  const zRef = useRef(ventanas.reduce((m, v) => Math.max(m, v.z), 0))
  const contadorRef = useRef(ventanas.reduce((m, v) => Math.max(m, numeroDeId(v.id)), 0))
  // Número de pestaña para los ids `${ventana}/t${n}`; sigue tras restaurar.
  const pestanasRef = useRef(ventanas.reduce((m, v) => v.pestanas.reduce((n, t) => Math.max(n, numeroDePestana(t.id)), m), 0))
  // Claves de las páginas navegadas: `${pestaña}/${n}`. La base es `/0`
  // (única por pestaña); las demás llevan un número que solo crece, para que
  // volver a abrir el mismo editor sea otra página con su estado a cero.
  const paginasRef = useRef(0)
  const cascadaRef = useRef(ventanas.length)
  const oyentesRef = useRef(new Map<Evento['nombre'], Set<(e: Evento) => void>>())

  const actualizar = useCallback((fn: (lista: Ventana[]) => Ventana[]) => {
    const nueva = fn(ventanasRef.current)
    if (nueva === ventanasRef.current) return
    ventanasRef.current = nueva
    setVentanas(nueva)
  }, [])

  // Cambia una sola ventana; si `fn` devuelve la misma, no hay re-render.
  const cambiar = useCallback((id: string, fn: (v: Ventana) => Ventana) => {
    actualizar(lista => {
      const i = lista.findIndex(v => v.id === id)
      if (i < 0) return lista
      const nueva = fn(lista[i])
      if (nueva === lista[i]) return lista
      const copia = lista.slice()
      copia[i] = nueva
      return copia
    })
  }, [actualizar])

  const activaDe = (lista: Ventana[]): string | null => {
    let mejor: Ventana | null = null
    for (const v of lista) {
      if (v.estado === 'minimizada') continue
      if (!mejor || v.z > mejor.z) mejor = v
    }
    return mejor ? mejor.id : null
  }

  const enfocar = useCallback((id: string) => {
    cambiar(id, v => (v.z === zRef.current ? v : { ...v, z: ++zRef.current }))
  }, [cambiar])

  const restaurar = useCallback((id: string) => {
    cambiar(id, v => {
      if (v.estado === 'normal') return v
      const def = APPS[v.app]
      const area = areaRef.current
      // Sin `anterior` (no debería pasar) se vuelve al tamaño por defecto.
      const g = v.anterior
        ? encajar({ ...v.anterior, ...acotarTamano(v.anterior, def.minimo, area) }, area)
        : encajar({ x: ORIGEN_CASCADA, y: ORIGEN_CASCADA, ...acotarTamano(def.tamano, def.minimo, area) }, area)
      return { ...v, ...g, estado: 'normal', anterior: undefined }
    })
  }, [cambiar])

  const abrir = useCallback((app: AppId, props?: Record<string, unknown>, opciones?: { titulo?: string }): string => {
    const def = APPS[app]
    if (!def) throw new Error(`App desconocida: ${app}`)

    if (def.unica) {
      // Cuenta tanto si es la página que se ve en alguna pestaña como si es
      // la base de una pestaña que ahora enseña otra cosa (Productos con un
      // editor delante): se vuelve a esa pestaña y a esa página en vez de
      // abrir un segundo Productos. Se prefiere la pestaña activa si vale.
      const tiene = (t: Pestana) => t.historial[t.indice].app === app || t.historial[0].app === app
      const existente = ventanasRef.current.find(v => v.pestanas.some(tiene))
      if (existente) {
        if (existente.estado === 'minimizada') restaurar(existente.id)
        const activa = pestanaActivaDe(existente)
        const t = tiene(activa) ? activa : existente.pestanas.find(tiene)!
        const i = indiceDe(t, app)
        if (t.id !== existente.pestanaActiva || (i >= 0 && i !== t.indice)) {
          cambiar(existente.id, v => conHistorial(conPestanas(v, v.pestanas, t.id), t.historial, i >= 0 ? i : t.indice, t.id))
        }
        enfocar(existente.id)
        return existente.id
      }
    }

    const area = areaRef.current
    const tamano = acotarTamano(def.tamano, def.minimo, area)
    // Cascada: cada ventana nueva 28 px más abajo y a la derecha que la
    // anterior; cuando la siguiente no cabría, se vuelve al origen.
    let n = cascadaRef.current
    let x = ORIGEN_CASCADA + n * PASO_CASCADA
    let y = ORIGEN_CASCADA + n * PASO_CASCADA
    if (x + tamano.w > area.w || y + tamano.h > area.h) {
      n = 0
      x = ORIGEN_CASCADA
      y = ORIGEN_CASCADA
    }
    cascadaRef.current = n + 1

    const id = `${app}-${++contadorRef.current}`
    const pestanaId = `${id}/t${++pestanasRef.current}`
    const pagina: Pagina = { clave: `${pestanaId}/0`, app, props: props ?? {}, titulo: opciones?.titulo ?? def.nombre }
    const pestana: Pestana = { id: pestanaId, historial: [pagina], indice: 0 }
    const nueva: Ventana = {
      id,
      app: pagina.app,
      titulo: pagina.titulo,
      props: pagina.props,
      historial: pestana.historial,
      indice: 0,
      pestanas: [pestana],
      pestanaActiva: pestanaId,
      ...encajar({ x, y, ...tamano }, area),
      estado: 'normal',
      z: ++zRef.current,
    }
    actualizar(lista => [...lista, nueva])
    return id
  }, [actualizar, cambiar, enfocar, restaurar])

  const cerrar = useCallback((id: string) => {
    actualizar(lista => (lista.some(v => v.id === id) ? lista.filter(v => v.id !== id) : lista))
  }, [actualizar])

  // ---- historial de la pestaña activa

  // La ventana se queda con su tamaño (es del usuario, no de la app), salvo
  // que no llegue al mínimo de la página que va a enseñar: entonces crece lo
  // justo. Maximizada o ajustada ya ocupa lo que hay.
  const crecerHasta = useCallback((v: Ventana, def: AppDef): Ventana => {
    if (v.estado !== 'normal') return v
    const area = areaRef.current
    const t = acotarTamano(v, def.minimo, area)
    return t.w === v.w && t.h === v.h ? v : { ...v, ...encajar({ x: v.x, y: v.y, ...t }, area) }
  }, [])

  const navegar = useCallback((id: string, app: AppId, props?: Record<string, unknown>, titulo?: string) => {
    const def = APPS[app]
    if (!def) throw new Error(`App desconocida: ${app}`)
    cambiar(id, v => {
      const pagina: Pagina = {
        clave: `${v.pestanaActiva}/${++paginasRef.current}`, app, props: props ?? {}, titulo: titulo ?? def.nombre,
      }
      const historial = [...v.historial.slice(0, v.indice + 1), pagina]
      return crecerHasta(conHistorial(v, historial, historial.length - 1), def)
    })
  }, [cambiar, crecerHasta])

  const irAPagina = useCallback((id: string, indice: number) => {
    cambiar(id, v => (indice !== v.indice && indice >= 0 && indice < v.historial.length
      ? crecerHasta(conHistorial(v, v.historial, indice), APPS[v.historial[indice].app])
      : v))
  }, [cambiar, crecerHasta])

  const atras = useCallback((id: string) => {
    cambiar(id, v => (v.indice > 0 ? conHistorial(v, v.historial, v.indice - 1) : v))
  }, [cambiar])

  const adelante = useCallback((id: string) => {
    cambiar(id, v => (v.indice < v.historial.length - 1 ? conHistorial(v, v.historial, v.indice + 1) : v))
  }, [cambiar])

  // Se identifica la página por su clave y no por «la actual»: quien llama
  // es la propia página, que puede haber quedado detrás (el usuario pulsó ←
  // mientras guardaba) o en otra pestaña, y aun así tiene que desaparecer
  // ella, no otra.
  const cerrarPagina = useCallback((id: string, clave: string) => {
    cambiar(id, v => {
      for (const t of v.pestanas) {
        const i = t.historial.findIndex(p => p.clave === clave)
        if (i < 0) continue
        if (i === 0) return v
        return conHistorial(v, t.historial.slice(0, i), Math.min(t.indice, i - 1), t.id)
      }
      return v
    })
  }, [cambiar])

  // ---- pestañas

  const nuevaPestana = useCallback((id: string, app?: AppId, props?: Record<string, unknown>, titulo?: string) => {
    cambiar(id, v => {
      // Sin app: la base de la ventana (la de la primera pestaña), que es lo
      // que el usuario entiende por «otra pestaña de esta ventana».
      const appReal = app ?? v.pestanas[0].historial[0].app
      const def = APPS[appReal]
      if (!def) throw new Error(`App desconocida: ${appReal}`)
      const pestanaId = `${id}/t${++pestanasRef.current}`
      const pagina: Pagina = { clave: `${pestanaId}/0`, app: appReal, props: props ?? {}, titulo: titulo ?? def.nombre }
      const pestana: Pestana = { id: pestanaId, historial: [pagina], indice: 0 }
      return crecerHasta(conPestanas(v, [...v.pestanas, pestana], pestanaId), def)
    })
  }, [cambiar, crecerHasta])

  const abrirEnPestana = useCallback((id: string, app: AppId, props?: Record<string, unknown>, titulo?: string) => {
    nuevaPestana(id, app, props, titulo)
  }, [nuevaPestana])

  const cerrarPestana = useCallback((id: string, pestanaId: string) => {
    const v = ventanasRef.current.find(x => x.id === id)
    if (!v || !v.pestanas.some(t => t.id === pestanaId)) return
    // La última pestaña es la ventana: se cierra entera.
    if (v.pestanas.length <= 1) {
      cerrar(id)
      return
    }
    cambiar(id, w => {
      const i = w.pestanas.findIndex(t => t.id === pestanaId)
      const pestanas = w.pestanas.filter(t => t.id !== pestanaId)
      // Al cerrar la activa pasa a verse la de su derecha (la que ocupa su
      // sitio), o la última si era la del final: como en un navegador.
      const activa = w.pestanaActiva === pestanaId ? pestanas[Math.min(i, pestanas.length - 1)].id : w.pestanaActiva
      return conPestanas(w, pestanas, activa)
    })
  }, [cambiar, cerrar])

  const activarPestana = useCallback((id: string, pestanaId: string) => {
    cambiar(id, v => {
      if (v.pestanaActiva === pestanaId) return v
      const t = v.pestanas.find(x => x.id === pestanaId)
      if (!t) return v
      return crecerHasta(conPestanas(v, v.pestanas, pestanaId), APPS[t.historial[t.indice].app])
    })
  }, [cambiar, crecerHasta])

  const moverPestana = useCallback((id: string, pestanaId: string, aIndice: number) => {
    cambiar(id, v => {
      const desde = v.pestanas.findIndex(t => t.id === pestanaId)
      const hasta = Math.max(0, Math.min(aIndice, v.pestanas.length - 1))
      if (desde < 0 || desde === hasta) return v
      const pestanas = v.pestanas.slice()
      const [t] = pestanas.splice(desde, 1)
      pestanas.splice(hasta, 0, t)
      return { ...v, pestanas }
    })
  }, [cambiar])

  const minimizar = useCallback((id: string) => {
    cambiar(id, v => (v.estado === 'minimizada' ? v : { ...v, estado: 'minimizada', anterior: conAnterior(v) }))
  }, [cambiar])

  const maximizar = useCallback((id: string) => {
    cambiar(id, v => {
      if (v.estado === 'maximizada') return v
      const g = geometriaEstado('maximizada', areaRef.current)!
      return { ...v, ...g, estado: 'maximizada', anterior: conAnterior(v) }
    })
  }, [cambiar])

  const ajustar = useCallback((id: string, lado: 'izquierda' | 'derecha') => {
    cambiar(id, v => {
      if (v.estado === lado) return v
      const g = geometriaEstado(lado, areaRef.current)!
      return { ...v, ...g, estado: lado, anterior: conAnterior(v) }
    })
  }, [cambiar])

  const mover = useCallback((id: string, x: number, y: number) => {
    cambiar(id, v => {
      const g = acotarPosicion({ x, y, w: v.w, h: v.h }, areaRef.current)
      return g.x === v.x && g.y === v.y ? v : { ...v, x: g.x, y: g.y }
    })
  }, [cambiar])

  const redimensionar = useCallback((id: string, geometria: Geometria) => {
    cambiar(id, v => {
      const area = areaRef.current
      const t = acotarTamano(geometria, APPS[v.app].minimo, area)
      // Si el tamaño pedido se ha acotado y se estaba tirando del borde
      // izquierdo o superior, se mantiene fijo el borde opuesto.
      const x = geometria.x !== v.x && t.w !== geometria.w ? geometria.x + geometria.w - t.w : geometria.x
      const y = geometria.y !== v.y && t.h !== geometria.h ? geometria.y + geometria.h - t.h : geometria.y
      const g = acotarPosicion({ x, y, ...t }, area)
      return g.x === v.x && g.y === v.y && g.w === v.w && g.h === v.h ? v : { ...v, ...g }
    })
  }, [cambiar])

  const retitular = useCallback((id: string, titulo: string) => {
    cambiar(id, v => {
      if (v.titulo === titulo) return v
      // También en el historial: al volver a esta página tiene que reaparecer.
      const historial = v.historial.slice()
      historial[v.indice] = { ...historial[v.indice], titulo }
      return conHistorial(v, historial, v.indice)
    })
  }, [cambiar])

  const minimizarTodas = useCallback(() => {
    actualizar(lista => (lista.every(v => v.estado === 'minimizada')
      ? lista
      : lista.map(v => (v.estado === 'minimizada' ? v : { ...v, estado: 'minimizada', anterior: conAnterior(v) }))))
  }, [actualizar])

  // Bus de eventos: síncrono y sin historial. Se copia el conjunto antes de
  // avisar para que un oyente pueda darse de baja durante el aviso.
  const suscribir = useCallback((nombre: Evento['nombre'], cb: (e: Evento) => void) => {
    let conjunto = oyentesRef.current.get(nombre)
    if (!conjunto) {
      conjunto = new Set()
      oyentesRef.current.set(nombre, conjunto)
    }
    conjunto.add(cb)
    return () => {
      conjunto.delete(cb)
    }
  }, [])

  const emitir = useCallback((e: Evento) => {
    const conjunto = oyentesRef.current.get(e.nombre)
    if (!conjunto) return
    for (const cb of Array.from(conjunto)) {
      try {
        cb(e)
      } catch (err) {
        // Un oyente roto no debe impedir que los demás se enteren.
        console.error(`Oyente de "${e.nombre}" falló`, err)
      }
    }
  }, [])

  // Área de trabajo: al cambiar el tamaño de la pantalla, las ventanas
  // maximizadas o ajustadas se recalculan y las normales se vuelven a acotar.
  useEffect(() => {
    const alCambiar = () => {
      const area = medirArea()
      areaRef.current = area
      setPantalla({ area, movil: esMovil() })
      actualizar(lista => lista.map(v => {
        const fija = geometriaEstado(v.estado, area)
        if (fija) return { ...v, ...fija }
        if (v.estado === 'minimizada') return v
        const g = acotarPosicion({ x: v.x, y: v.y, ...acotarTamano(v, APPS[v.app].minimo, area) }, area)
        return { ...v, ...g }
      }))
    }
    window.addEventListener('resize', alCambiar)
    return () => window.removeEventListener('resize', alCambiar)
  }, [actualizar])

  // Persistencia con retraso: cada cambio reprograma el guardado, así un
  // arrastre no escribe en localStorage sesenta veces por segundo. Solo se
  // guardan las ventanas cuya base es una app única, y solo esa página: los
  // diálogos no se restauran (ver `cargar`), y sus props pueden ser
  // voluminosas (una foto en edición).
  useEffect(() => {
    const temporizador = window.setTimeout(() => {
      try {
        localStorage.setItem(clave, JSON.stringify(paraGuardar(ventanas)))
      } catch {
        // Sin almacenamiento (modo privado, cuota): se pierde la sesión, no el trabajo.
      }
    }, RETRASO_GUARDADO)
    return () => window.clearTimeout(temporizador)
  }, [ventanas, clave])

  // Al desmontar (cierre de sesión) se guarda lo pendiente sin esperar.
  useEffect(() => () => {
    try {
      localStorage.setItem(clave, JSON.stringify(paraGuardar(ventanasRef.current)))
    } catch {
      // Igual que arriba.
    }
  }, [clave])

  // Atajos globales. Alt+F4 lo captura el navegador, así que el cierre va con
  // Ctrl+Alt+W. Se usan códigos físicos (KeyW…) para que no dependan de la
  // distribución del teclado. Dentro de un campo de texto no se interfiere.
  useEffect(() => {
    const alPulsar = (e: KeyboardEvent) => {
      const objetivo = e.target as HTMLElement | null
      if (objetivo && (
        objetivo.tagName === 'INPUT' || objetivo.tagName === 'TEXTAREA' || objetivo.tagName === 'SELECT' || objetivo.isContentEditable
      )) return

      const lista = ventanasRef.current
      const activa = activaDe(lista)

      if (e.altKey && !e.ctrlKey && !e.metaKey && e.key === 'Tab') {
        if (lista.length === 0) return
        e.preventDefault()
        const porZ = lista.slice().sort((a, b) => b.z - a.z)
        const i = porZ.findIndex(v => v.id === activa)
        const paso = e.shiftKey ? -1 : 1
        const siguiente = porZ[((i < 0 ? 0 : i + paso) + porZ.length) % porZ.length]
        if (siguiente.estado === 'minimizada') restaurar(siguiente.id)
        enfocar(siguiente.id)
        return
      }

      if (!(e.ctrlKey && e.altKey) || e.metaKey || e.shiftKey) return
      if (e.code === 'KeyD') {
        e.preventDefault()
        minimizarTodas()
        return
      }
      if (!activa) return
      const v = lista.find(w => w.id === activa)!
      switch (e.code) {
        case 'KeyW': cerrar(activa); break
        case 'KeyM': minimizar(activa); break
        case 'ArrowLeft': ajustar(activa, 'izquierda'); break
        case 'ArrowRight': ajustar(activa, 'derecha'); break
        case 'ArrowUp': if (v.estado === 'maximizada') restaurar(activa); else maximizar(activa); break
        default: return
      }
      e.preventDefault()
    }
    window.addEventListener('keydown', alPulsar)
    return () => window.removeEventListener('keydown', alPulsar)
  }, [ajustar, cerrar, enfocar, maximizar, minimizar, minimizarTodas, restaurar])

  const activa = useMemo(() => activaDe(ventanas), [ventanas])

  const valor = useMemo<Sistema>(() => ({
    ventanas,
    activa,
    abrir, cerrar, enfocar, minimizar, maximizar, restaurar, ajustar, mover, redimensionar, retitular, minimizarTodas,
    navegar, atras, adelante, irAPagina, cerrarPagina,
    nuevaPestana, abrirEnPestana, cerrarPestana, activarPestana, moverPestana,
    emitir, suscribir,
    area: pantalla.area,
    movil: pantalla.movil,
  }), [
    ventanas, activa, abrir, cerrar, enfocar, minimizar, maximizar, restaurar, ajustar, mover, redimensionar,
    retitular, minimizarTodas, navegar, atras, adelante, irAPagina, cerrarPagina,
    nuevaPestana, abrirEnPestana, cerrarPestana, activarPestana, moverPestana, emitir, suscribir, pantalla,
  ])

  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

// Si una ventana puede ir atrás o adelante en su historial. Lo usan el marco
// (para los botones ← →) y quien quiera saberlo sin repetir la aritmética.
export function puedeAtras(v: Ventana): boolean {
  return v.indice > 0
}
export function puedeAdelante(v: Ventana): boolean {
  return v.indice < v.historial.length - 1
}

// Id de la ventana en la que se está pintando una página. Lo provee App
// alrededor de cada página; fuera del escritorio (login, guía) es null.
export const ContextoVentana = createContext<string | null>(null)

export function useVentanaActual(): string | null {
  return useContext(ContextoVentana)
}

// Lo que usan las pantallas para «ir a» otra cosa: dentro de una ventana
// navega en ella (y ← vuelve); si no hay ventana actual abre una nueva. Así
// Productos, la mediateca o el panel no deciden si abren ventanas: lo decide
// dónde están. Fuera del escritorio (sin sistema) no hace nada: las
// pantallas conservan sus modales para ese caso.
export function useIr(): (app: AppId, props?: Record<string, unknown>, titulo?: string) => void {
  const sistema = useSistemaOpcional()
  const ventanaId = useVentanaActual()
  return useCallback((app: AppId, props?: Record<string, unknown>, titulo?: string) => {
    if (!sistema) return
    if (ventanaId) sistema.navegar(ventanaId, app, props, titulo)
    else sistema.abrir(app, props, titulo ? { titulo } : undefined)
  }, [sistema, ventanaId])
}

export function useSistema(): Sistema {
  const s = useContext(Contexto)
  if (!s) throw new Error('useSistema fuera de ProveedorSistema')
  return s
}

// Para piezas que también viven fuera del escritorio (login, guía…).
export function useSistemaOpcional(): Sistema | null {
  return useContext(Contexto)
}

// Se suscribe a un evento del bus mientras el componente está montado. El
// callback va en un ref para no darse de baja y alta en cada render; fuera
// del escritorio no hace nada.
export function useEvento<N extends Evento['nombre']>(
  nombre: N,
  cb: (e: Extract<Evento, { nombre: N }>) => void,
): void {
  const sistema = useSistemaOpcional()
  const cbRef = useRef(cb)
  cbRef.current = cb
  const suscribir = sistema?.suscribir
  useEffect(() => {
    if (!suscribir) return
    return suscribir(nombre, e => cbRef.current(e as Extract<Evento, { nombre: N }>))
  }, [suscribir, nombre])
}
