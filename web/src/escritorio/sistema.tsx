import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode,
} from 'react'
import type { AppId, EstadoVentana, Evento, Pagina, Sistema, Ventana } from './tipos'
import { APPS } from './apps'
import { useSesion } from './sesion'

// Gestor de ventanas del escritorio: quién está abierta, dónde, en qué estado
// y delante de quién. Es el único sitio que toca la lista de ventanas; el
// marco (Ventana.tsx), la barra y el menú de inicio solo piden cambios.
//
// Cada ventana lleva un historial de páginas (ver Pagina en tipos.ts): abrir
// un producto desde Productos no abre otra ventana, navega dentro de la
// misma, y ← / → se mueven por lo visitado como en un navegador.
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

// La ventana con `historial` e `indice` puestos y la página actual copiada a
// `app`, `props` y `titulo`. Toda mutación del historial pasa por aquí para
// que la copia nunca se desincronice.
function conPagina(v: Ventana, historial: Pagina[], indice: number): Ventana {
  const p = historial[indice]
  return { ...v, historial, indice, app: p.app, props: p.props, titulo: p.titulo }
}

// Índice de la última página del historial que enseña `app`, o -1.
function indiceDe(v: Ventana, app: AppId): number {
  for (let i = v.historial.length - 1; i >= 0; i--) if (v.historial[i].app === app) return i
  return -1
}

// Lo que va a localStorage: solo la página base de cada ventana (la app de
// sección con la que se abrió). Lo navegado desde ahí (un editor, una
// previa) apunta a cosas que pueden haber cambiado y no se restaura; el
// historial vuelve a empezar con una sola página.
function paraGuardar(lista: Ventana[]): Record<string, unknown>[] {
  return lista
    .filter(v => APPS[v.historial[0]?.app]?.unica)
    .map(v => {
      const base = v.historial[0]
      const { historial: _h, indice: _i, ...resto } = v
      return { ...resto, app: base.app, props: base.props, titulo: base.titulo }
    })
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

    // Se restaura con una sola página: la base (ver paraGuardar).
    const pagina: Pagina = {
      clave: `${v.id}/0`,
      app,
      titulo: typeof v.titulo === 'string' && v.titulo ? v.titulo : def.nombre,
      props: v.props && typeof v.props === 'object' ? (v.props as Record<string, unknown>) : {},
    }
    salida.push({
      id: v.id,
      app: pagina.app,
      titulo: pagina.titulo,
      props: pagina.props,
      historial: [pagina],
      indice: 0,
      ...(fija ?? libre),
      estado,
      z: numero(v.z, 0),
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
  // Claves de las páginas navegadas: `${ventana}/${n}`. La base es `/0`
  // (única por ventana); las demás llevan un número que solo crece, para que
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
      // Cuenta tanto si es la página que se ve como si es la base de una
      // ventana que ahora enseña otra cosa (Productos con un editor delante):
      // se vuelve a esa página en vez de abrir un segundo Productos.
      const existente = ventanasRef.current.find(v => v.app === app || v.historial[0].app === app)
      if (existente) {
        if (existente.estado === 'minimizada') restaurar(existente.id)
        const i = indiceDe(existente, app)
        if (i >= 0 && i !== existente.indice) cambiar(existente.id, v => conPagina(v, v.historial, i))
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
    const pagina: Pagina = { clave: `${id}/0`, app, props: props ?? {}, titulo: opciones?.titulo ?? def.nombre }
    const nueva: Ventana = {
      id,
      app: pagina.app,
      titulo: pagina.titulo,
      props: pagina.props,
      historial: [pagina],
      indice: 0,
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

  // ---- historial de la ventana

  const navegar = useCallback((id: string, app: AppId, props?: Record<string, unknown>, titulo?: string) => {
    const def = APPS[app]
    if (!def) throw new Error(`App desconocida: ${app}`)
    cambiar(id, v => {
      const pagina: Pagina = { clave: `${id}/${++paginasRef.current}`, app, props: props ?? {}, titulo: titulo ?? def.nombre }
      const historial = [...v.historial.slice(0, v.indice + 1), pagina]
      const nueva = conPagina(v, historial, historial.length - 1)
      // La ventana se queda con su tamaño (es del usuario, no de la app),
      // salvo que no llegue al mínimo de la página nueva: entonces crece lo
      // justo. Maximizada o ajustada ya ocupa lo que hay.
      if (nueva.estado !== 'normal') return nueva
      const area = areaRef.current
      const t = acotarTamano(nueva, def.minimo, area)
      return t.w === nueva.w && t.h === nueva.h ? nueva : { ...nueva, ...encajar({ x: nueva.x, y: nueva.y, ...t }, area) }
    })
  }, [cambiar])

  const atras = useCallback((id: string) => {
    cambiar(id, v => (v.indice > 0 ? conPagina(v, v.historial, v.indice - 1) : v))
  }, [cambiar])

  const adelante = useCallback((id: string) => {
    cambiar(id, v => (v.indice < v.historial.length - 1 ? conPagina(v, v.historial, v.indice + 1) : v))
  }, [cambiar])

  // Se identifica la página por su clave y no por «la actual»: quien llama
  // es la propia página, que puede haber quedado detrás (el usuario pulsó ←
  // mientras guardaba) y aun así tiene que desaparecer ella, no otra.
  const cerrarPagina = useCallback((id: string, clave: string) => {
    cambiar(id, v => {
      const i = v.historial.findIndex(p => p.clave === clave)
      if (i <= 0) return v
      return conPagina(v, v.historial.slice(0, i), Math.min(v.indice, i - 1))
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
      return conPagina(v, historial, v.indice)
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
    navegar, atras, adelante, cerrarPagina,
    emitir, suscribir,
    area: pantalla.area,
    movil: pantalla.movil,
  }), [
    ventanas, activa, abrir, cerrar, enfocar, minimizar, maximizar, restaurar, ajustar, mover, redimensionar,
    retitular, minimizarTodas, navegar, atras, adelante, cerrarPagina, emitir, suscribir, pantalla,
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
