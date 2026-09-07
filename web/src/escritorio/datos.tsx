import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import {
  api, type Atencion, type BusquedaMasiva, type Categoria, type Marca, type Resumen,
  type ResumenOrdenes, type StockAlmacen,
} from '../api'
import type { Datos } from './tipos'
import { useEvento } from './sistema'

// Datos globales del escritorio: lo que antes cargaba App y repartía por
// props. Viven aquí y no en cada ventana porque los leen varias apps a la
// vez (el panel, el catálogo, la barra con sus insignias, los widgets) y
// cada una abre y cierra su ventana cuando quiere: cargados por ventana se
// pedirían siete veces y se verían distintos en cada una.

const Contexto = createContext<Datos | null>(null)

export function ProveedorDatos({ children }: { children: ReactNode }) {
  // Lo que Integra tiene en marcha ahora mismo. Vive aquí y no dentro de la
  // pantalla porque el número va en el menú: es lo que contesta «pulsé un
  // botón, ¿está pasando algo?» sin tener que entrar a mirar. Cada 15 s basta;
  // el detalle, con refresco rápido, está en la pantalla de Actividad.
  const [tareasActivas, setTareasActivas] = useState(0)
  useEffect(() => {
    let vivo = true
    const mirar = () => {
      api.actividad(1)
        .then((a) => { if (vivo) setTareasActivas(a.activas) })
        .catch(() => {})
    }
    mirar()
    const t = window.setInterval(mirar, 15000)
    return () => { vivo = false; window.clearInterval(t) }
  }, [])

  const [resumen, setResumen] = useState<Resumen | null>(null)
  const [marcas, setMarcas] = useState<Marca[]>([])
  const [categorias, setCategorias] = useState<Categoria[]>([])
  const [atencion, setAtencion] = useState<Atencion[]>([])
  const [stock, setStock] = useState<StockAlmacen[]>([])
  const [pedidos, setPedidos] = useState<ResumenOrdenes | null>(null)
  const [alertas, setAlertas] = useState(0)

  const [error, setError] = useState<string | null>(null)
  const [sincronizando, setSincronizando] = useState(false)
  const [masivo, setMasivo] = useState<BusquedaMasiva | null>(null)

  const cargarPanel = useCallback(async () => {
    try {
      const [r, m, cats, a, s, p, al] = await Promise.all([
        api.resumen(), api.marcas(), api.categorias().catch(() => []),
        api.atencion(), api.stock(),
        api.resumenOrdenes().catch(() => null),
        api.alertas().catch(() => []),
      ])
      setResumen(r); setMarcas(m); setCategorias(cats); setAtencion(a); setStock(s); setPedidos(p)
      setAlertas(al.length)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'no se pudo contactar con la API')
    }
  }, [])

  useEffect(() => { void cargarPanel() }, [cargarPanel])

  // Recarga corta para cuando una ventana toca un producto o sus fotos: lo
  // que cambia con eso son los números del resumen y los motivos de
  // atención. Marcas, stock y pedidos no, y no merece la pena repedirlos.
  const recargarAtencion = useCallback(async () => {
    try {
      const [r, a] = await Promise.all([api.resumen(), api.atencion()])
      setResumen(r); setAtencion(a)
    } catch {
      // La carga completa ya enseña el error si la API no contesta; aquí
      // se deja el dato anterior, que es mejor que un aviso por cada tecla.
    }
  }, [])

  // Al abrir la página se consulta si quedó un barrido de imágenes en marcha.
  useEffect(() => {
    api.estadoBusquedaMasiva()
      .then((m) => setMasivo(m.total > 0 ? m : null))
      .catch(() => { /* sin API no hay barrido que mostrar */ })
  }, [])

  // Mientras el barrido corre en el servidor, se sondea su progreso.
  useEffect(() => {
    if (!masivo?.en_curso) return
    const t = setInterval(() => {
      api.estadoBusquedaMasiva().then((m) => {
        setMasivo(m)
        if (!m.en_curso) void cargarPanel()
      }).catch(() => { /* la API puede estar reiniciándose; se reintenta */ })
    }, 4000)
    return () => clearInterval(t)
  }, [masivo?.en_curso, cargarPanel])

  const lanzarMasivo = useCallback(async () => {
    try {
      await api.buscarImagenesMasivo()
      const m = await api.estadoBusquedaMasiva()
      setMasivo(m.total > 0 ? m : null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  const sincronizar = useCallback(async () => {
    setSincronizando(true)
    try {
      await api.sincronizar()
      // La sincronización corre en segundo plano; se refresca al cabo de un
      // rato para que se vean los datos nuevos.
      setTimeout(() => { void cargarPanel(); setSincronizando(false) }, 12000)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setSincronizando(false)
    }
  }, [cargarPanel])

  // Quien edita un producto o le cambia las fotos lo dice por el bus; aquí
  // se recargan resumen y atención para que insignias, panel y widgets lo
  // vean sin que cada ventana tenga que saber de las demás. Sin gestor de
  // ventanas (fuera del escritorio) no hay bus y useEvento no hace nada.
  useEvento('producto-cambiado', () => { void recargarAtencion() })
  useEvento('fotos-cambiadas', () => { void recargarAtencion() })

  const valor: Datos = {
    resumen, marcas, categorias, atencion, stock, pedidos, alertas, tareasActivas, masivo, error,
    recargar: cargarPanel, sincronizar, sincronizando, lanzarMasivo,
  }
  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

export function useDatos(): Datos {
  const d = useContext(Contexto)
  if (!d) throw new Error('useDatos fuera de ProveedorDatos')
  return d
}
