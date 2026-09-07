import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode,
} from 'react'
import type { AppId, EstadoVentana, Evento, Sistema, Ventana } from './tipos'
import { APPS } from './apps'
import { useSesion } from './sesion'

// Gestor de ventanas del escritorio: quién está abierta, dónde, en qué estado
// y delante de quién. Es el único sitio que toca la lista de ventanas; el
// marco (Ventana.tsx), la barra y el menú de inicio solo piden cambios.
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
  const vistas = new Set<string>()
  for (const cruda of lista as unknown[]) {
    if (!cruda || typeof cruda !== 'object') continue
    const v = cruda as Record<string, unknown>
    const app = v.app as AppId
    const def = APPS[app]
    if (!def || !def.unica) continue
    if (typeof v.id !== 'string' || vistas.has(v.id) || vistas.has(app)) continue
    vistas.add(v.id)
    vistas.add(app)

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

    salida.push({
      id: v.id,
      app,
      titulo: typeof v.titulo === 'string' && v.titulo ? v.titulo : def.nombre,
      ...(fija ?? libre),
      estado,
      z: numero(v.z, 0),
      props: v.props && typeof v.props === 'object' ? (v.props as Record<string, unknown>) : {},
      // Una ventana que se guardó fuera de `normal` sin geometría a la que
      // volver la recupera del tamaño por defecto para que restaurar funcione.
      anterior: anterior ?? (estado === 'normal' ? undefined : libre),
    })
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
      const existente = ventanasRef.current.find(v => v.app === app)
      if (existente) {
        if (existente.estado === 'minimizada') restaurar(existente.id)
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
    const nueva: Ventana = {
      id,
      app,
      titulo: opciones?.titulo ?? def.nombre,
      ...encajar({ x, y, ...tamano }, area),
      estado: 'normal',
      z: ++zRef.current,
      props: props ?? {},
    }
    actualizar(lista => [...lista, nueva])
    return id
  }, [actualizar, enfocar, restaurar])

  const cerrar = useCallback((id: string) => {
    actualizar(lista => (lista.some(v => v.id === id) ? lista.filter(v => v.id !== id) : lista))
  }, [actualizar])

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
    cambiar(id, v => (v.titulo === titulo ? v : { ...v, titulo }))
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
  // guardan las apps únicas: los diálogos no se restauran (ver `cargar`), y
  // sus props pueden ser voluminosas (una foto en edición).
  useEffect(() => {
    const temporizador = window.setTimeout(() => {
      try {
        localStorage.setItem(clave, JSON.stringify(ventanas.filter(v => APPS[v.app]?.unica)))
      } catch {
        // Sin almacenamiento (modo privado, cuota): se pierde la sesión, no el trabajo.
      }
    }, RETRASO_GUARDADO)
    return () => window.clearTimeout(temporizador)
  }, [ventanas, clave])

  // Al desmontar (cierre de sesión) se guarda lo pendiente sin esperar.
  useEffect(() => () => {
    try {
      localStorage.setItem(clave, JSON.stringify(ventanasRef.current.filter(v => APPS[v.app]?.unica)))
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
    emitir, suscribir,
    area: pantalla.area,
    movil: pantalla.movil,
  }), [
    ventanas, activa, abrir, cerrar, enfocar, minimizar, maximizar, restaurar, ajustar, mover, redimensionar,
    retitular, minimizarTodas, emitir, suscribir, pantalla,
  ])

  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
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
