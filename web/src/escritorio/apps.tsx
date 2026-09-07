import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  api, num, type FiltroCatalogo, type ImagenBanco, type Producto,
} from '../api'
import type {
  AppDef, AppId, PropsAsignarFoto, PropsEdicionMasiva, PropsEditar, PropsEditorFoto,
  PropsPreview, PropsSelectorMediateca, PropsVentana,
} from './tipos'
import { useDatos } from './datos'
import { useSistema } from './sistema'
import { Panel } from '../Panel'
import { Catalogo } from '../Catalogo'
import { Mediateca, DialogoAsignar } from '../Mediateca'
import { Publicacion } from '../Publicacion'
import { Pedidos } from '../Pedidos'
import { Actividad } from '../Actividad'
import { Automatizacion } from '../Automatizacion'
import { Integraciones } from '../Integraciones'
import { Mapeos } from '../Mapeos'
import { Atributos } from '../Atributos'
import { Cuentas } from '../Cuentas'
import { Canales } from '../Canales'
import { Usuarios } from '../Usuarios'
import { Avisos } from '../Avisos'
import { Configuracion } from './Configuracion'
import { Ayuda, type Progreso } from '../Ayuda'
import { Editar } from '../Editar'
import { Preview } from '../Preview'
import { EditorFoto } from '../EditorFoto'
import { PlantillaMasiva } from '../PlantillaMasiva'
import { EdicionMasiva } from '../EdicionMasiva'
import { SelectorMediateca } from '../SelectorMediateca'
import type { Recorrido } from '../guias'
import type { Seccion } from '../Sidebar'
import './apps.css'

// Registro de apps: cada pantalla y cada diálogo de Integra, con lo que el
// escritorio necesita para pintarlos (icono, tamaño, si admite varias
// ventanas) y con el `render` que los envuelve. Las pantallas son las mismas
// de siempre; lo que cambia es que los datos globales vienen de useDatos y
// que los resultados de un diálogo salen por el bus en vez de por callbacks.

// Progreso de los recorridos e «iniciar uno»: los guarda App, que es quien
// pinta la guía por encima de todo; el centro de ayuda los lee de aquí.
export const ContextoAyuda = createContext<{ progreso: Progreso; iniciar: (rec: Recorrido, paso: number) => void } | null>(null)

// Orden del menú de inicio y del buscador. Los diálogos no salen en menús.
export const ORDEN_APPS: AppId[] = [
  'panel', 'catalogo', 'mediateca', 'publicacion', 'pedidos', 'actividad', 'automatizacion',
  'integraciones', 'categorias', 'atributos', 'canales', 'usuarios', 'avisos', 'configuracion', 'ayuda',
  'editar', 'preview', 'editor-foto', 'plantilla', 'edicion-masiva', 'selector-mediateca', 'asignar-foto',
]

// Las apps de sección coinciden con las secciones de la guía (Sidebar.Seccion).
const SECCIONES = new Set<string>([
  'panel', 'catalogo', 'mediateca', 'publicacion', 'pedidos', 'actividad', 'automatizacion',
  'integraciones', 'categorias', 'atributos', 'canales', 'usuarios', 'avisos',
])

// ---------------------------------------------------------------- iconos

// Mismo trazo que los del menú lateral, a 24 px.
const svg = {
  width: 24, height: 24, viewBox: '0 0 24 24', fill: 'none',
  stroke: 'currentColor', strokeWidth: 1.8,
  strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const,
}

const ICONOS: Record<AppId, ReactNode> = {
  panel: <svg {...svg}><rect x="3" y="3" width="7" height="9" rx="1" /><rect x="14" y="3" width="7" height="5" rx="1" /><rect x="14" y="12" width="7" height="9" rx="1" /><rect x="3" y="16" width="7" height="5" rx="1" /></svg>,
  catalogo: <svg {...svg}><path d="M21 8v8a2 2 0 0 1-1 1.7l-7 4a2 2 0 0 1-2 0l-7-4A2 2 0 0 1 3 16V8a2 2 0 0 1 1-1.7l7-4a2 2 0 0 1 2 0l7 4A2 2 0 0 1 21 8z" /><path d="m3.3 7 8.7 5 8.7-5" /><path d="M12 22V12" /></svg>,
  mediateca: <svg {...svg}><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="9" cy="9" r="2" /><path d="m21 15-4.6-4.6a2 2 0 0 0-2.8 0L3 21" /></svg>,
  publicacion: <svg {...svg}><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="m7 9 5-5 5 5" /><path d="M12 4v12" /></svg>,
  pedidos: <svg {...svg}><circle cx="9" cy="20" r="1.4" /><circle cx="18" cy="20" r="1.4" /><path d="M2 3h2.5l2.4 12.1a2 2 0 0 0 2 1.6h8.7a2 2 0 0 0 2-1.6L21 7H5.6" /></svg>,
  actividad: <svg {...svg}><path d="M3 12h4l3 8 4-16 3 8h4" /></svg>,
  automatizacion: <svg {...svg}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.2 1.9" /></svg>,
  integraciones: <svg {...svg}><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" /><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></svg>,
  categorias: <svg {...svg}><rect x="9" y="3" width="10" height="5" rx="1" /><rect x="12" y="16" width="10" height="5" rx="1" /><path d="M5 3v13a2 2 0 0 0 2 2h5" /><path d="M9 5.5H5" /></svg>,
  atributos: <svg {...svg}><path d="M12.6 2.7 21 11a2 2 0 0 1 0 2.8l-7.2 7.2a2 2 0 0 1-2.8 0L2.7 12.6A2 2 0 0 1 2 11V4a2 2 0 0 1 2-2h7a2 2 0 0 1 1.6.7z" /><circle cx="7" cy="7" r="1.2" /></svg>,
  canales: <svg {...svg}><path d="M9 2v6" /><path d="M15 2v6" /><path d="M6 8h12v3a6 6 0 0 1-6 6 6 6 0 0 1-6-6z" /><path d="M12 17v5" /></svg>,
  usuarios: <svg {...svg}><circle cx="9" cy="8" r="3.2" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0" /><path d="M16.5 5.4a3.2 3.2 0 0 1 0 5.2" /><path d="M18 14.4a6.5 6.5 0 0 1 3.5 5.6" /></svg>,
  avisos: <svg {...svg}><path d="M18 9a6 6 0 1 0-12 0c0 5-2 6-2 6h16s-2-1-2-6" /><path d="M10.3 20a2 2 0 0 0 3.4 0" /></svg>,
  configuracion: <svg {...svg}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></svg>,
  ayuda: <svg {...svg}><circle cx="12" cy="12" r="10" /><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3" /><path d="M12 17h.01" /></svg>,
  editar: <svg {...svg}><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg>,
  preview: <svg {...svg}><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></svg>,
  'editor-foto': <svg {...svg}><path d="M6 2v14a2 2 0 0 0 2 2h14" /><path d="M18 22V8a2 2 0 0 0-2-2H2" /></svg>,
  plantilla: <svg {...svg}><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M3 9h18" /><path d="M3 15h18" /><path d="M9 3v18" /></svg>,
  'edicion-masiva': <svg {...svg}><path d="m12 2 9 5-9 5-9-5z" /><path d="m3 12 9 5 9-5" /><path d="m3 17 9 5 9-5" /></svg>,
  'selector-mediateca': <svg {...svg}><rect x="3" y="5" width="14" height="14" rx="2" /><circle cx="8" cy="10" r="1.5" /><path d="m17 15-3.5-3.5a2 2 0 0 0-2.8 0L4 19" /><path d="M19 3v6" /><path d="M16 6h6" /></svg>,
  'asignar-foto': <svg {...svg}><path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7" /><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7" /></svg>,
}

// ------------------------------------------------------------ utilidades

// Las funciones del sistema cambian de identidad con cada ventana que se
// mueve; usarlas como dependencia de un efecto lo relanzaría sin parar.
// Un ref con la última versión permite llamarlas desde un efecto sin
// depender de ellas.
function useUltimo<T>(v: T) {
  const r = useRef(v)
  r.current = v
  return r
}

// No hay GET /api/productos/{id}: el editor se alimenta de la lista. Se busca
// por SKU (que quien abre la ventana conoce) y se elige la variante pedida.
// Si no viene el SKU, se saca de la vista previa, que sí se pide por id. Los
// productos fuera del catálogo no salen en la lista normal: se prueba
// también con ellos antes de rendirse.
async function cargarProducto(varianteId: number, sku?: string): Promise<Producto | null> {
  const clave = sku || (await api.preview(varianteId)).producto.sku
  const buscar = (excluidos: boolean) => api.productos({ q: clave, limite: 20, excluidos })
  const hallado = (await buscar(false)).items.find((x) => x.id === varianteId)
    ?? (await buscar(true)).items.find((x) => x.id === varianteId)
  return hallado ?? null
}

// ------------------------------------------------------------- secciones

function CatalogoApp() {
  const { marcas, categorias, recargar } = useDatos()
  const sis = useSistema()
  return (
    <Catalogo marcas={marcas} categorias={categorias}
      onVer={(id) => sis.abrir('preview', { varianteId: id })}
      onCambio={() => void recargar()} />
  )
}

function MediatecaApp() {
  const { masivo, lanzarMasivo } = useDatos()
  const sis = useSistema()
  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Imágenes</h1>
          <div className="sub">Banco propio de Integra — las de Odoo son miniaturas que ningún canal acepta</div>
        </div>
        <button onClick={() => void lanzarMasivo()} disabled={masivo?.en_curso ?? false}
          title="Busca en internet por SKU las fotos de los productos que aún no tienen una imagen apta para los cuatro canales.">
          {masivo?.en_curso ? 'Buscando…' : 'Buscar imágenes faltantes'}
        </button>
      </header>

      {masivo && (
        <div className="nota-previa barrido">
          {masivo.en_curso ? (
            <>
              <strong>Buscando imágenes:</strong> {num(masivo.procesados)}/{num(masivo.total)} productos
              · {num(masivo.fotos_agregadas)} fotos descargadas
              {masivo.fallos > 0 && <> · {num(masivo.fallos)} búsquedas fallidas</>}
              {masivo.ultimo && <span className="tenue"> · último: {masivo.ultimo}</span>}
              <span className="pista-barrido">
                <span className="relleno" style={{ width: `${(masivo.procesados / Math.max(1, masivo.total)) * 100}%` }} />
              </span>
            </>
          ) : (
            <><strong>Búsqueda {masivo.mensaje.startsWith('abortada') ? 'abortada' : 'terminada'}:</strong> {masivo.mensaje}</>
          )}
        </div>
      )}

      <Mediateca onVer={(id) => sis.abrir('preview', { varianteId: id })} />
    </>
  )
}

function CategoriasApp({ props }: PropsVentana) {
  // El canal llega en props cuando la abre la vista previa («ver el mapeo
  // de tal canal»); desde el menú no hay canal y Mapeos elige el primero.
  const canal = typeof props.canal === 'string' ? props.canal : undefined
  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Categorías</h1>
          <div className="sub">Equivalencia entre el árbol de Odoo y el de cada canal</div>
        </div>
      </header>
      <Mapeos canalInicial={canal} />
    </>
  )
}

function IntegracionesApp() {
  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Odoo</h1>
          <div className="sub">Instancias conectadas como fuente del catálogo (SKU, nombre y stock)</div>
        </div>
      </header>
      <div className="rejilla-1">
        <Integraciones />
      </div>
    </>
  )
}

function CanalesApp() {
  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Canales</h1>
          <div className="sub">Cuentas conectadas y comisiones que compensa el precio publicado</div>
        </div>
      </header>
      <div className="rejilla-1">
        <Cuentas />
        <Canales />
      </div>
    </>
  )
}

// El centro de ayuda de siempre, sin su capa modal: la ventana es la capa.
// Explica por defecto la pantalla que está más adelante (sin contar a sí
// mismo), que es la que el usuario estaba mirando cuando pidió ayuda.
function CentroAyuda({ cerrar }: PropsVentana) {
  const sis = useSistema()
  const ayuda = useContext(ContextoAyuda)
  const seccion = useMemo<Seccion>(() => {
    const delante = sis.ventanas
      .filter((v) => v.app !== 'ayuda' && v.estado !== 'minimizada' && SECCIONES.has(v.app))
      .sort((a, b) => b.z - a.z)[0]
    return (delante?.app as Seccion | undefined) ?? 'panel'
  }, [sis.ventanas])
  return (
    <div className="app-dialogo">
      <Ayuda seccion={seccion} progreso={ayuda?.progreso ?? {}} onCerrar={cerrar}
        onIniciar={(rec, paso) => { ayuda?.iniciar(rec, paso); cerrar() }} />
    </div>
  )
}

// -------------------------------------------------------------- diálogos

function EditarApp({ ventanaId, props, cerrar }: PropsVentana) {
  const { varianteId, pestana, sku } = props as PropsEditar
  const { marcas } = useDatos()
  const sis = useUltimo(useSistema())
  const [producto, setProducto] = useState<Producto | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let vivo = true
    cargarProducto(varianteId, sku)
      .then((p) => {
        if (!vivo) return
        if (!p) { setError(`No se encontró el producto ${sku ?? varianteId} en el catálogo.`); return }
        setProducto(p)
        sis.current.retitular(ventanaId, `Editar · ${p.sku || p.nombre}`)
      })
      .catch((e) => { if (vivo) setError(e instanceof Error ? e.message : String(e)) })
    return () => { vivo = false }
  }, [varianteId, sku, ventanaId, sis])

  return (
    <div className="app-dialogo">
      {error && <div className="aviso-caja">Error: {error}</div>}
      {!error && !producto && <div className="nota-carga">Cargando el producto…</div>}
      {producto && (
        <Editar producto={producto} marcas={marcas} pestanaInicial={pestana}
          onCerrar={cerrar}
          onGuardado={() => {
            sis.current.emitir({ nombre: 'producto-cambiado', varianteId })
            cerrar()
          }} />
      )}
    </div>
  )
}

function PreviewApp({ props, cerrar }: PropsVentana) {
  const { varianteId } = props as PropsPreview
  const sis = useSistema()
  return (
    <div className="app-dialogo">
      <Preview varianteId={varianteId} onCerrar={cerrar}
        // Quien atiende «ir a arreglar» (App) abre el editor o las
        // categorías; esta ventana ya cumplió y se cierra, como antes.
        onIr={(d) => { sis.emitir({ nombre: 'ir-a-arreglar', destino: d }); cerrar() }}
        onCambio={() => sis.emitir({ nombre: 'fotos-cambiadas', varianteId })} />
    </div>
  )
}

function EditorFotoApp({ props, cerrar }: PropsVentana) {
  const { foto } = props as PropsEditorFoto
  const sis = useSistema()
  return (
    <div className="app-dialogo">
      <EditorFoto foto={foto} onCerrar={cerrar}
        onGuardada={() => { sis.emitir({ nombre: 'fotos-cambiadas' }); cerrar() }} />
    </div>
  )
}

// La plantilla en su ventana lleva su propio filtro: ya no hay una lista
// detrás que comparta el suyo, así que el diálogo es la única verdad sobre
// qué se descarga. El total se pide con la lista más corta posible.
function PlantillaApp({ cerrar }: PropsVentana) {
  const { marcas, categorias } = useDatos()
  const sis = useSistema()
  const [filtro, setFiltro] = useState<FiltroCatalogo>({})
  const [total, setTotal] = useState(0)
  useEffect(() => {
    let vivo = true
    api.productos({ ...filtro, limite: 1 })
      .then((p) => { if (vivo) setTotal(p.total) })
      .catch(() => { /* el total es orientativo; sin API la descarga ya avisará */ })
    return () => { vivo = false }
  }, [filtro])
  return (
    <div className="app-dialogo">
      <PlantillaMasiva filtro={filtro} total={total} marcas={marcas} categorias={categorias}
        onFiltrar={(c) => setFiltro((f) => ({ ...f, ...c }))}
        onCerrar={cerrar}
        // No se cierra al aplicar: el resumen de lo que cambió es lo que el
        // operador necesita leer justo después.
        onAplicado={() => sis.emitir({ nombre: 'producto-cambiado' })} />
    </div>
  )
}

function EdicionMasivaApp({ props, cerrar }: PropsVentana) {
  const { seleccion, cuantos } = props as PropsEdicionMasiva
  const sis = useSistema()
  return (
    <div className="app-dialogo">
      <EdicionMasiva ids={seleccion.ids ?? []} filtro={seleccion.filtro ?? {}} totalFiltro={cuantos}
        onCerrar={cerrar}
        onAplicado={() => { sis.emitir({ nombre: 'producto-cambiado' }); cerrar() }} />
    </div>
  )
}

function SelectorMediatecaApp({ props, cerrar }: PropsVentana) {
  const { varianteId } = props as PropsSelectorMediateca
  const sis = useSistema()
  return (
    <div className="app-dialogo">
      <SelectorMediateca varianteId={varianteId} onCerrar={cerrar}
        onElegidas={() => { sis.emitir({ nombre: 'fotos-cambiadas', varianteId }); cerrar() }} />
    </div>
  )
}

// Asignar fotos del banco a un producto. Por props solo viajan los ids: las
// imágenes se recuperan del banco (las más recientes, que es de donde se
// marcan) y se filtran. El enlace en sí es el mismo que hace la mediateca.
function AsignarFotoApp({ ventanaId, props, cerrar }: PropsVentana) {
  const { imagenIds } = props as PropsAsignarFoto
  const sis = useUltimo(useSistema())
  const [imagenes, setImagenes] = useState<ImagenBanco[] | null>(null)
  const [ocupada, setOcupada] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let vivo = true
    api.banco({ limite: 200 })
      .then((p) => {
        if (!vivo) return
        // En el orden en que se marcaron: con «la primera como portada»
        // importa cuál es la primera.
        const porId = new Map(p.items.map((i) => [i.id, i]))
        const lista = imagenIds.map((id) => porId.get(id)).filter((i): i is ImagenBanco => !!i)
        if (lista.length === 0) { setError('Las imágenes ya no están en el banco.'); return }
        setImagenes(lista)
        sis.current.retitular(ventanaId, lista.length === 1 ? 'Asignar la foto' : `Asignar ${num(lista.length)} fotos`)
      })
      .catch((e) => { if (vivo) setError(e instanceof Error ? e.message : String(e)) })
    return () => { vivo = false }
  }, [imagenIds, ventanaId, sis])

  async function confirmar(producto: Producto, principal: boolean) {
    if (!imagenes) return
    setOcupada(true)
    setError(null)
    try {
      if (imagenes.length === 1) await api.asociarDelBanco(imagenes[0].id, producto.id, principal)
      else await api.asociarVariasDelBanco(imagenes.map((i) => i.id), producto.id, principal)
      sis.current.emitir({ nombre: 'fotos-cambiadas', varianteId: producto.id })
      cerrar()
    } catch (e) {
      setError(`No se pudo asignar: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupada(false)
    }
  }

  return (
    <div className="app-dialogo">
      {error && <div className="aviso-caja">Error: {error}</div>}
      {!error && !imagenes && <div className="nota-carga">Cargando las imágenes…</div>}
      {imagenes && (
        <DialogoAsignar imagenes={imagenes} ocupada={ocupada} onCerrar={cerrar}
          onConfirmar={(p, principal) => void confirmar(p, principal)} />
      )}
    </div>
  )
}

// -------------------------------------------------------------- registro

function seccion(id: AppId, nombre: string, descripcion: string, color: string,
  render: (p: PropsVentana) => ReactNode, extra?: Partial<AppDef>): AppDef {
  return {
    id, nombre, descripcion, color, icono: ICONOS[id],
    tamano: { w: 1100, h: 720 }, minimo: { w: 640, h: 420 },
    unica: true, enEscritorio: true, anclada: true,
    render: (p) => <div className="app-seccion">{render(p)}</div>,
    ...extra,
  }
}

function dialogo(id: AppId, nombre: string, descripcion: string, color: string,
  tamano: { w: number; h: number }, minimo: { w: number; h: number },
  render: (p: PropsVentana) => ReactNode): AppDef {
  return {
    id, nombre, descripcion, color, icono: ICONOS[id], tamano, minimo,
    unica: false, enEscritorio: false, anclada: false, render,
  }
}

export const APPS: Record<AppId, AppDef> = {
  panel: seccion('panel', 'Panel', 'Resumen del catálogo y qué bloquea la publicación', '#2563eb',
    () => <Panel />, { tamano: { w: 1200, h: 800 } }),
  catalogo: seccion('catalogo', 'Productos', 'El catálogo de Odoo: buscar, filtrar, editar y publicar', '#7c3aed',
    () => <CatalogoApp />, { tamano: { w: 1240, h: 800 } }),
  mediateca: seccion('mediateca', 'Imágenes', 'Banco de fotos de Integra y búsqueda de las que faltan', '#db2777',
    () => <MediatecaApp />, { tamano: { w: 1200, h: 800 } }),
  publicacion: seccion('publicacion', 'Publicación', 'Qué está en cada canal y qué falta por subir', '#0891b2',
    () => <Publicacion />),
  pedidos: seccion('pedidos', 'Pedidos', 'Órdenes recibidas de los canales y su paso a Odoo', '#ea580c',
    () => <Pedidos />),
  actividad: seccion('actividad', 'Actividad', 'Trabajos en marcha y terminados, con su detalle', '#16a34a',
    () => <Actividad />),
  automatizacion: seccion('automatizacion', 'Automatización', 'Horarios de sincronización y alertas', '#ca8a04',
    () => <Automatizacion />),
  integraciones: seccion('integraciones', 'Odoo', 'Instancias de Odoo conectadas como fuente del catálogo', '#4f46e5',
    () => <IntegracionesApp />, { tamano: { w: 900, h: 640 } }),
  categorias: seccion('categorias', 'Categorías', 'Equivalencia entre las categorías de Odoo y las de cada canal', '#0d9488',
    (p) => <CategoriasApp {...p} />),
  atributos: seccion('atributos', 'Atributos', 'Fichas técnicas que piden los canales, por producto', '#9333ea',
    () => <Atributos />),
  canales: seccion('canales', 'Canales', 'Cuentas conectadas y comisiones de cada marketplace', '#e11d48',
    () => <CanalesApp />, { tamano: { w: 1000, h: 700 } }),
  usuarios: seccion('usuarios', 'Usuarios', 'Quién entra en Integra y con qué permisos', '#475569',
    () => <Usuarios />, { soloAdmin: true, tamano: { w: 900, h: 620 }, anclada: false }),
  avisos: seccion('avisos', 'Avisos', 'Alertas abiertas y cerradas de todos los canales', '#dc2626',
    () => <Avisos />, { soloAdmin: true, tamano: { w: 1000, h: 700 }, anclada: false }),
  configuracion: seccion('configuracion', 'Configuración', 'Tema, fondo, apps ancladas y widgets del escritorio', '#64748b',
    () => <Configuracion />, { tamano: { w: 820, h: 620 }, minimo: { w: 520, h: 400 }, anclada: false }),
  ayuda: seccion('ayuda', 'Ayuda', 'Recorridos guiados, búsqueda y manual completo', '#0284c7',
    (p) => <CentroAyuda {...p} />, { tamano: { w: 1100, h: 760 }, anclada: false }),

  editar: dialogo('editar', 'Editar producto', 'Precio, promoción, títulos por canal, envío y notas',
    '#7c3aed', { w: 780, h: 740 }, { w: 520, h: 480 }, (p) => <EditarApp {...p} />),
  preview: dialogo('preview', 'Vista previa', 'Cómo verá cada canal la ficha y qué le falta',
    '#0891b2', { w: 1200, h: 800 }, { w: 640, h: 480 }, (p) => <PreviewApp {...p} />),
  'editor-foto': dialogo('editor-foto', 'Editar foto', 'Recortar, girar, encajar en cuadrado y cambiar formato',
    '#db2777', { w: 1300, h: 860 }, { w: 720, h: 520 }, (p) => <EditorFotoApp {...p} />),
  plantilla: dialogo('plantilla', 'Actualizar por plantilla', 'Descargar el catálogo en Excel y subirlo con cambios',
    '#16a34a', { w: 920, h: 720 }, { w: 560, h: 440 }, (p) => <PlantillaApp {...p} />),
  'edicion-masiva': dialogo('edicion-masiva', 'Editar en masa', 'Cambiar precios de muchos productos a la vez',
    '#9333ea', { w: 740, h: 640 }, { w: 480, h: 420 }, (p) => <EdicionMasivaApp {...p} />),
  'selector-mediateca': dialogo('selector-mediateca', 'Elegir de la mediateca', 'Fotos del banco para un producto',
    '#db2777', { w: 1000, h: 720 }, { w: 600, h: 440 }, (p) => <SelectorMediatecaApp {...p} />),
  'asignar-foto': dialogo('asignar-foto', 'Asignar a producto', 'Enlazar fotos del banco a un producto',
    '#0d9488', { w: 720, h: 660 }, { w: 480, h: 440 }, (p) => <AsignarFotoApp {...p} />),
}
