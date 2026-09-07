import { useEffect, useMemo, useRef, useState, type ChangeEvent, type DragEvent, type ReactNode } from 'react'
import { api, money, motivo, num, type Categoria, type Marca, type PaginaProductos, type Producto, rolActual , type CuentaCanal } from './api'
import { Editar, type Pestana } from './Editar'
import { EdicionMasiva } from './EdicionMasiva'
import { PlantillaMasiva } from './PlantillaMasiva'
import { confirmar } from './escritorio/Dialogos'
import { useEvento, useIr, useSistemaOpcional, useVentanaActual } from './escritorio/sistema'
import {
  BarraEstado, CabeceraColumnas, Imagen, SelectorVista,
  useColumnas, useMenuContextual, useSeleccion, useTecladoLista, useVista,
  type Columna, type ColumnaDef, type OpcionMenu,
} from './Vista'

const POR_PAGINA = 50
// Motivos que impiden publicar. Un producto con cualquiera de ellos no sale al
// canal, así que la pantalla los marca distinto de los que solo empeoran la
// ficha.
// Los canales por su nombre y los estados por lo que significan, para que la
// pastilla se lea sin conocer las claves internas.
const NOMBRE_CANAL: Record<string, string> = {
  mercadolibre: 'MercadoLibre', falabella: 'Falabella',
  woocommerce: 'WooCommerce', shopify: 'Shopify',
}
const ESTADO_CANAL: Record<string, string> = {
  published: 'publicado', paused: 'retirado de la venta',
  error: 'falló el último envío', pending: 'en cola',
}

const BLOQUEANTES = new Set([
  'missing_sku', 'duplicate_sku', 'missing_description', 'missing_price', 'price_below_cost',
  // Sin foto no publica ningún canal.
  'missing_image',
])
const bloqueante = (m: string) => BLOQUEANTES.has(m)

// Columnas de la vista de detalles. Las ocultas por defecto (categoría, EAN,
// peso, condición) se eligen desde el menú de la cabecera; el orden y los
// anchos se guardan por usuario en localStorage (useColumnas).
const COLUMNAS: ColumnaDef[] = [
  { id: 'check', titulo: '', ancho: 34, minimo: 34, fija: true, clase: 'col-check' },
  { id: 'sku', titulo: 'Referencia', ancho: 130, orden: 'sku', clase: 'oculto-movil' },
  { id: 'nombre', titulo: 'Producto', ancho: 320, orden: 'nombre' },
  { id: 'marca', titulo: 'Marca', ancho: 130, orden: 'marca', clase: 'oculto-movil' },
  { id: 'categoria', titulo: 'Categoría', ancho: 160, clase: 'oculto-movil', oculta: true },
  { id: 'precio', titulo: 'Precio', ancho: 120, orden: 'precio', clase: 'num' },
  { id: 'stock', titulo: 'Stock', ancho: 80, orden: 'stock', clase: 'num' },
  { id: 'publicacion', titulo: 'Publicación', ancho: 160, guia: 'cat-publicacion' },
  { id: 'estado', titulo: 'Estado', ancho: 200, guia: 'estado' },
  { id: 'ean', titulo: 'EAN', ancho: 130, clase: 'oculto-movil', oculta: true },
  { id: 'peso', titulo: 'Peso', ancho: 80, clase: 'num oculto-movil', oculta: true },
  { id: 'condicion', titulo: 'Condición', ancho: 110, clase: 'oculto-movil', oculta: true },
  { id: 'acciones', titulo: '', ancho: 170, fija: true, flexible: true },
]
const CONDICION: Record<Producto['condicion'], string> = { nuevo: 'Nuevo', usado: 'Usado', reacondicionado: 'Reacondicionado' }

const CLAVE_PANEL = 'integra.catalogo.panel'
// Funciones estables para los hooks de lista: si se crearan en cada render,
// recalcularían sus memos por nada.
const claveDe = (p: Producto) => p.id
const textoDe = (p: Producto) => [p.nombre, p.sku]

// AbrirEditor es lo que pide la vista previa al pulsar «Poner el peso»: que
// esta pantalla abra el editor de ese producto ya en la pestaña del peso.
export type AbrirEditor = { id: number; sku: string; pestana: Pestana }

export function Catalogo({ marcas, categorias = [], onVer, onCambio, abrir = null, onAbierto, refresco = 0 }: {
  marcas: Marca[]
  categorias?: Categoria[]
  onVer: (varianteId: number) => void
  onCambio: () => void
  abrir?: AbrirEditor | null
  onAbierto?: () => void
  // Sube cuando algo cambió fuera de esta pantalla (la vista previa quitó
  // una foto): la lista se vuelve a pedir sin tocar filtros ni página.
  refresco?: number
}) {
  const [pagina, setPagina] = useState<PaginaProductos | null>(null)
  const [busqueda, setBusqueda] = useState('')
  const [marca, setMarca] = useState('')
  const [categoria, setCategoria] = useState('')
  const [soloProblemas, setSoloProblemas] = useState(false)
  const [verExcluidos, setVerExcluidos] = useState(false)
  const [sinPrecio, setSinPrecio] = useState(false)
  // Lo que aún no está en ningún canal. Es el filtro que contesta «¿qué me
  // falta por subir?», que antes obligaba a comparar dos pantallas a ojo.
  const [sinPublicar, setSinPublicar] = useState(false)
  // Criterios que solo se usan desde la plantilla, de momento. Viven aquí y no
  // allí porque el filtro es uno solo: lo que se descarga tiene que ser lo
  // mismo que se ve en la lista de detrás.
  const [sinFoto, setSinFoto] = useState(false)
  const [sinDescripcion, setSinDescripcion] = useState(false)
  const [sinEAN, setSinEAN] = useState(false)
  const [conPromo, setConPromo] = useState(false)
  const [orden, setOrden] = useState('nombre')
  const [ordenDesc, setOrdenDesc] = useState(false)
  const [offset, setOffset] = useState(0)
  const [editando, setEditando] = useState<Producto | null>(null)
  const [pestanaEditor, setPestanaEditor] = useState<Pestana>('venta')
  // La petición de abrir un producto concreto se resuelve cuando llega la
  // lista que lo contiene, no antes: la lista se recarga al buscarlo.
  const pendiente = useRef<AbrirEditor | null>(null)
  const [version, setVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [cargando, setCargando] = useState(true)
  const [masiva, setMasiva] = useState(false)
  const [plantilla, setPlantilla] = useState(false)
  const [publicando, setPublicando] = useState(false)
  const esAdmin = rolActual() === 'admin'
  const [despublicando, setDespublicando] = useState(false)
  // El resultado de publicar tiene su propio aviso: el banner de error de la
  // pantalla dice «no se pudo cargar la lista», que no es lo que pasó.
  const [aviso, setAviso] = useState<{ texto: string; malo: boolean } | null>(null)
  // Cómo se enseña la lista: la tabla de siempre o tarjetas/iconos con la
  // portada, que es lo que permite reconocer un producto de un vistazo.
  const [vista, setVista] = useVista('catalogo', 'detalles')
  // Cuentas de canal, para el submenú «Publicar en…». Se piden una vez: el
  // menú se arma en el instante del clic derecho y no puede esperar.
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  useEffect(() => { api.cuentas().then(setCuentas).catch(() => setCuentas([])) }, [])
  // Panel lateral de detalles, como el de vista previa del explorador.
  const [panel, setPanel] = useState(() => {
    try { return window.localStorage.getItem(CLAVE_PANEL) === '1' } catch { return false }
  })
  const alternarPanel = () => {
    setPanel((p) => {
      try { window.localStorage.setItem(CLAVE_PANEL, p ? '0' : '1') } catch { /* se pierde al recargar */ }
      return !p
    })
  }
  // Sobre qué producto se está arrastrando un archivo, y qué se está
  // subiendo. Van aparte del aviso: son transitorios y se ven en la barra.
  const [arrastreSobre, setArrastreSobre] = useState<number | null>(null)
  const [subiendo, setSubiendo] = useState<{ sku: string; hechas: number; total: number } | null>(null)
  // Confirmaciones cortas («SKU copiado») en la barra de estado, dos segundos.
  const [nota, setNota] = useState<string | null>(null)
  useEffect(() => {
    if (nota === null) return
    const t = window.setTimeout(() => setNota(null), 2000)
    return () => window.clearTimeout(t)
  }, [nota])

  // Dentro del escritorio, los diálogos son páginas de esta misma ventana:
  // se navega a ellos, ← vuelve a la lista tal como estaba, y el resultado
  // vuelve por el bus (la página no sabe quién la abrió). Fuera (sistema
  // null) siguen siendo modales de esta pantalla, sin cambios.
  const sistema = useSistemaOpcional()
  const ventanaId = useVentanaActual()
  const ir = useIr()
  useEvento('producto-cambiado', () => setVersion((v) => v + 1))
  useEvento('fotos-cambiadas', () => setVersion((v) => v + 1))

  // La lista de la página actual, con identidad estable mientras no llegue
  // otra: de ella cuelgan la selección, el teclado y los totales.
  const items = useMemo(() => pagina?.items ?? [], [pagina])
  // Selección de explorador: clic, Ctrl, Shift, lazo y también las casillas
  // de siempre. `marcados` es el nombre que ya usaban las acciones en masa.
  const seleccion = useSeleccion(items, claveDe)
  const marcados = seleccion.seleccion
  const menu = useMenuContextual()
  const col = useColumnas('catalogo', COLUMNAS)
  const refLista = useRef<HTMLDivElement>(null)
  const refTabla = useRef<HTMLTableElement>(null)
  const teclado = useTecladoLista(refLista, items, {
    clave: claveDe,
    seleccion,
    abrir: (p) => onVer(p.id),
    texto: textoDe,
    contextual: (p, e) => menu.abrir(e, opcionesDe(p)),
    copiar: (ps) => void copiar(ps.map((p) => p.sku || p.nombre).join('\n'), ps.length === 1 ? 'SKU copiado' : `${num(ps.length)} SKU copiados`),
    rol: vista === 'detalles' ? 'grid' : 'listbox',
  })
  // Lo que enseña el panel de detalles: la fila con foco y, si no hay, la
  // última seleccionada.
  const actual = useMemo(() => {
    const porId = new Map(items.map((p) => [p.id, p]))
    return (teclado.foco !== null ? porId.get(teclado.foco) : undefined)
      ?? (seleccion.ancla !== null ? porId.get(seleccion.ancla) : undefined)
      ?? null
  }, [items, teclado.foco, seleccion.ancla])

  function abrirEditor(p: Producto, pestana: Pestana) {
    if (sistema) {
      ir('editar', { varianteId: p.id, sku: p.sku, pestana }, `Editar · ${p.sku || p.nombre}`)
    } else {
      setPestanaEditor(pestana)
      setEditando(p)
    }
  }
  function abrirPlantilla() {
    if (sistema) ir('plantilla', {})
    else setPlantilla(true)
  }
  function abrirMasiva() {
    if (sistema) {
      const cuantos = pagina?.total ?? 0
      ir('edicion-masiva', { seleccion: { ids: [...marcados], filtro: filtroActual }, cuantos },
        marcados.size > 0 ? `Editar en masa · ${num(marcados.size)} marcados` : `Editar en masa · ${num(cuantos)} del filtro`)
    } else {
      setMasiva(true)
    }
  }
  // La vista previa en otra pestaña de esta ventana. `abrirEnPestana` llega
  // con las pestañas del escritorio; si esa parte aún no está, ventana
  // nueva, que es lo que había.
  function abrirEnPestana(p: Producto) {
    if (!sistema) return
    const titulo = `Vista previa · ${p.sku || p.nombre}`
    const s = sistema as typeof sistema & {
      abrirEnPestana?: (id: string, app: 'preview', props: Record<string, unknown>, titulo?: string) => void
    }
    if (ventanaId && typeof s.abrirEnPestana === 'function') s.abrirEnPestana(ventanaId, 'preview', { varianteId: p.id }, titulo)
    else sistema.abrir('preview', { varianteId: p.id }, { titulo })
  }

  async function copiar(texto: string, confirmacion: string) {
    try {
      await navigator.clipboard.writeText(texto)
      setNota(confirmacion)
    } catch {
      setAviso({ texto: 'El navegador no dejó copiar al portapapeles.', malo: true })
    }
  }

  // Excluir saca el producto del catálogo (gastos, activos, servicios que
  // Odoo trae mezclados) sin borrarlo: se vuelve con «Ver excluidos».
  async function alternarExclusion(ps: Producto[], excluir: boolean) {
    const n = ps.length
    const quien = n === 1 ? `«${ps[0].nombre}»` : `${num(n)} productos`
    const pl = n === 1 ? '' : 'n'
    const ok = await confirmar({
      titulo: excluir ? 'Excluir del catálogo' : 'Incluir en el catálogo',
      texto: excluir
        ? `${quien} dejará${pl} de aparecer en el catálogo y no se publicará${pl} en ningún canal. No se borra nada: se vuelve desde «Ver excluidos».`
        : `${quien} volverá${pl} al catálogo y podrá${pl} publicarse en los canales.`,
      aceptar: excluir ? 'Excluir' : 'Incluir',
      peligroso: excluir,
    })
    if (!ok) return
    setAviso(null)
    const fallos: string[] = []
    for (const p of ps) {
      try { await api.editarProducto(p.id, { excluido: excluir }) } catch { fallos.push(p.sku || p.nombre) }
    }
    if (fallos.length > 0) {
      setAviso({ texto: `No se pudo cambiar ${fallos.length === 1 ? 'el producto' : 'los productos'} ${fallos.join(', ')}.`, malo: true })
    } else {
      setNota(excluir ? `${quien} fuera del catálogo` : `${quien} de vuelta en el catálogo`)
    }
    seleccion.limpiar()
    if (sistema) sistema.emitir({ nombre: 'producto-cambiado' }); else setVersion((v) => v + 1)
    onCambio()
  }

  // Fotos soltadas desde el equipo sobre una fila: se suben en serie a ese
  // producto. En serie y no a la vez porque el servidor las ordena por
  // llegada y así la primera que se soltó queda primera.
  async function subirFotos(p: Producto, archivos: File[]) {
    const fotos = archivos.filter((f) => f.type.startsWith('image/'))
    if (fotos.length === 0) {
      setAviso({ texto: 'Solo se pueden soltar imágenes (JPG, PNG, WebP).', malo: true })
      return
    }
    const nombre = p.sku || p.nombre
    const fallos: string[] = []
    for (let i = 0; i < fotos.length; i++) {
      setSubiendo({ sku: nombre, hechas: i, total: fotos.length })
      try {
        await api.subirImagen(p.id, fotos[i])
      } catch (e) {
        fallos.push(`${fotos[i].name}: ${e instanceof Error ? e.message : String(e)}`)
      }
    }
    setSubiendo(null)
    const subidas = fotos.length - fallos.length
    if (fallos.length > 0) {
      setAviso({ texto: `${num(subidas)} de ${num(fotos.length)} fotos subidas a ${nombre}. Fallaron: ${fallos.join(' · ')}`, malo: true })
    } else {
      setNota(`${num(subidas)} foto${subidas === 1 ? '' : 's'} subida${subidas === 1 ? '' : 's'} a ${nombre}`)
    }
    if (subidas > 0) {
      // Por el bus llega también a esta pantalla (useEvento de arriba) y a
      // la vista previa si está abierta; fuera del escritorio, a mano.
      if (sistema) sistema.emitir({ nombre: 'fotos-cambiadas', varianteId: p.id }); else setVersion((v) => v + 1)
      onCambio()
    }
  }
  // Manejadores de arrastre de archivos para una fila, una tarjeta o el
  // panel. Solo reaccionan a archivos: arrastrar una cabecera de columna
  // también pasa por aquí y no debe encender nada.
  const propsSoltar = (p: Producto) => ({
    onDragOver: (e: DragEvent<HTMLElement>) => {
      if (!e.dataTransfer.types.includes('Files')) return
      e.preventDefault()
      e.dataTransfer.dropEffect = 'copy'
      if (arrastreSobre !== p.id) setArrastreSobre(p.id)
    },
    onDragLeave: (e: DragEvent<HTMLElement>) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null) && arrastreSobre === p.id) setArrastreSobre(null)
    },
    onDrop: (e: DragEvent<HTMLElement>) => {
      if (!e.dataTransfer.types.includes('Files')) return
      e.preventDefault()
      setArrastreSobre(null)
      void subirFotos(p, Array.from(e.dataTransfer.files))
    },
  })

  // Publicar lo seleccionado. Planificar la cuenta entera es lo correcto para
  // la corrida nocturna, pero quien acaba de arreglar tres fichas quiere
  // verlas en el canal sin esperar a que pase por delante todo el catálogo.
  // Despublicar quita la ficha del canal. El producto NO se borra: viene de
  // Odoo y sigue en Integra intacto, listo para volver a publicarse cuando se
  // quiera. Lo que se pierde es lo que vivía en el canal —historial,
  // preguntas, reseñas, posición en el buscador— y la dirección de la ficha.
  //
  // Es distinto de «Retirar de la venta»: eso la deja pausada y se puede
  // reabrir con su historial entero. Por eso son dos acciones y no una.
  async function despublicarDe(cuentas: CuentaCanal[]) {
    setAviso(null)
    setDespublicando(false)
    setPublicando(true)
    try {
      const ids = [...marcados]
      let total = 0
      for (const c of cuentas) total += (await api.borrarPublicaciones(c.id, ids)).encoladas
      seleccion.limpiar()
      setAviso({
        texto: `${num(total)} publicaciones encoladas para quitarse del canal. Los productos siguen en Integra.`,
        malo: false,
      })
    } catch (e) {
      setAviso({ texto: `No se pudo despublicar: ${e instanceof Error ? e.message : String(e)}`, malo: true })
    } finally {
      setPublicando(false)
    }
  }

  // Encola `ids` en las cuentas dadas. Desde la barra van todas las
  // activas; desde el menú contextual, la que se eligió.
  async function publicarEn(ids: number[], activas: CuentaCanal[]) {
    setError(null)
    setAviso(null)
    setPublicando(true)
    try {
      if (activas.length === 0) {
        setAviso({ texto: 'No hay ninguna cuenta de canal activa: configúrala en Canales antes de publicar.', malo: true })
        return
      }
      const partes: string[] = []
      for (const c of activas) {
        const p = await api.planificar(c.id, ids)
        const total = p.publicar + p.precio + p.stock
        partes.push(total === 0
          ? `${c.canal}: nada que enviar${p.no_listos > 0 ? ` (${num(p.no_listos)} sin requisitos)` : ''}`
          : `${c.canal}: ${num(total)} envíos`)
      }
      setAviso({
        texto: `${partes.join(' · ')}. El worker los procesa en segundo plano; su avance se ve en Publicación.`,
        malo: false,
      })
    } catch (e) {
      setAviso({
        texto: `No se pudo publicar la selección: ${e instanceof Error ? e.message : String(e)}`,
        malo: true,
      })
    } finally {
      setPublicando(false)
    }
  }

  async function publicarSeleccion() {
    // Se vuelven a pedir las cuentas: la lista de arriba puede ser de hace
    // un rato y alguien pudo activar una entre medias.
    let activas: CuentaCanal[]
    try {
      activas = (await api.cuentas()).filter((c) => c.activa)
    } catch (e) {
      setAviso({ texto: `No se pudo publicar la selección: ${e instanceof Error ? e.message : String(e)}`, malo: true })
      return
    }
    await publicarEn([...marcados], activas)
  }

  // Menú contextual de un producto. Si está seleccionado, las acciones en
  // lote van sobre toda la selección; si no, sobre él solo (al abrir el
  // menú pasa a ser la selección, pero ese estado aún no se ve aquí).
  function opcionesDe(p: Producto): OpcionMenu[] {
    const ids = marcados.has(p.id) ? [...marcados] : [p.id]
    const conjunto = new Set(ids)
    const elegidos = items.filter((x) => conjunto.has(x.id))
    const varios = ids.length > 1
    const cuantos = num(ids.length)
    const activas = cuentas.filter((c) => c.activa)
    const nombreCuenta = (c: CuentaCanal) => NOMBRE_CANAL[c.canal] ?? c.canal_nombre ?? c.canal
    return [
      { etiqueta: 'Ver en canales', atajo: 'Enter', accion: () => onVer(p.id) },
      { etiqueta: 'Editar', accion: () => abrirEditor(p, 'venta') },
      { etiqueta: 'Abrir en pestaña nueva', deshabilitado: !sistema, accion: () => abrirEnPestana(p) },
      {
        etiqueta: varios ? `Publicar ${cuantos} en…` : 'Publicar en…',
        separador: true,
        deshabilitado: publicando,
        submenu: [
          { etiqueta: 'Todos los canales activos', deshabilitado: activas.length === 0, accion: () => void publicarEn(ids, activas) },
          ...activas.map((c, i) => ({
            etiqueta: nombreCuenta(c), separador: i === 0, accion: () => void publicarEn(ids, [c]),
          })),
        ],
      },
      ...(esAdmin ? [{
        etiqueta: varios ? `Despublicar ${cuantos}…` : 'Despublicar…',
        deshabilitado: publicando,
        accion: () => setDespublicando(true),
      }] : []),
      {
        etiqueta: varios ? `Copiar ${cuantos} SKU` : 'Copiar SKU',
        atajo: 'Ctrl+C',
        separador: true,
        accion: () => void copiar(elegidos.map((x) => x.sku).filter(Boolean).join('\n'), varios ? `${cuantos} SKU copiados` : 'SKU copiado'),
      },
      {
        etiqueta: varios ? `Copiar ${cuantos} nombres` : 'Copiar nombre',
        accion: () => void copiar(elegidos.map((x) => x.nombre).join('\n'), varios ? `${cuantos} nombres copiados` : 'Nombre copiado'),
      },
      {
        etiqueta: p.excluido
          ? (varios ? `Incluir ${cuantos} en el catálogo` : 'Incluir en el catálogo')
          : (varios ? `Excluir ${cuantos} del catálogo` : 'Excluir del catálogo'),
        separador: true,
        peligroso: !p.excluido,
        accion: () => void alternarExclusion(elegidos, !p.excluido),
      },
    ]
  }

  const filtroActual = {
    q: busqueda, marca, categoria, problemas: soloProblemas,
    excluidos: verExcluidos, sin_precio: sinPrecio, sin_publicar: sinPublicar,
    sin_foto: sinFoto, sin_descripcion: sinDescripcion, sin_ean: sinEAN, con_promo: conPromo,
    orden, desc: ordenDesc,
  }
  const hayFiltro = !!(busqueda || marca || categoria || soloProblemas || verExcluidos || sinPrecio || sinPublicar)

  // Pulsar una cabecera ordena por ella; volver a pulsarla invierte el
  // sentido (desde el menú de la cabecera se fija un sentido concreto). Se
  // vuelve a la primera página porque, si no, se sigue viendo la página 3
  // de un orden que ya no existe.
  const ordenarPor = (campo: string, desc?: boolean) => {
    if (desc !== undefined) { setOrden(campo); setOrdenDesc(desc) }
    else if (orden === campo) setOrdenDesc(!ordenDesc)
    else { setOrden(campo); setOrdenDesc(false) }
    setOffset(0)
  }

  // La casilla de una fila: alterna, y con Shift extiende el rango como en
  // el explorador. El evento de cambio de una casilla es el clic, así que
  // trae los modificadores.
  const casilla = (p: Producto) => (e: ChangeEvent<HTMLInputElement>) => {
    const ne = e.nativeEvent as MouseEvent
    if (ne.shiftKey) seleccion.rango(p.id, true); else seleccion.alternar(p.id)
  }
  // `every` sobre una lista vacía da true, y eso dejaría la casilla de
  // «seleccionar todo» marcada en una página sin productos.
  const paginaEntera = items.length > 0 && items.every((p) => marcados.has(p.id))
  const alternarPagina = () => {
    if (paginaEntera) {
      const s = new Set(marcados)
      for (const p of items) s.delete(p.id)
      seleccion.establecer(s)
    } else {
      seleccion.todo()
    }
  }

  // Totales de la barra de estado: de la selección si la hay, si no de la
  // página cargada. Solo cuenta lo cargado: el valor es precio × stock de
  // las filas que hay en pantalla, no del filtro entero.
  const totales = useMemo(() => {
    const base = marcados.size > 0 ? items.filter((p) => marcados.has(p.id)) : items
    let unidades = 0, valor = 0, sinPrecio = 0
    for (const p of base) {
      unidades += p.stock
      if (p.precio !== null) valor += p.precio * p.stock; else sinPrecio++
    }
    return { unidades, valor, sinPrecio, cuantos: base.length, deSeleccion: marcados.size > 0 }
  }, [items, marcados])

  // Al venir de la vista previa se busca el producto por su referencia en vez
  // de confiar en que esté en la página actual: esta pantalla se monta de
  // cero al cambiar de sección y la primera página son cincuenta de muchos.
  useEffect(() => {
    if (!abrir) return
    pendiente.current = abrir
    setBusqueda(abrir.sku)
    setOffset(0)
    setVersion((v) => v + 1)
  }, [abrir])

  // La búsqueda se retrasa 300 ms para no lanzar una consulta por tecla.
  // `vigente` descarta la respuesta de una consulta que ya quedó atrás: al
  // teclear rápido hay varias en vuelo y no siempre vuelven en orden, así que
  // sin esto la lista puede quedarse mostrando el resultado de un texto viejo.
  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.productos({
        q: busqueda, marca, categoria, problemas: soloProblemas,
        excluidos: verExcluidos, sin_precio: sinPrecio, sin_publicar: sinPublicar,
        sin_foto: sinFoto, sin_descripcion: sinDescripcion, sin_ean: sinEAN, con_promo: conPromo,
        orden, desc: ordenDesc,
        limite: POR_PAGINA, offset,
      })
        .then((p) => {
          if (!vigente) return
          setPagina(p)
          // Un error de hace dos búsquedas no debe seguir en pantalla cuando
          // la siguiente ya trajo datos buenos.
          setError(null)
          const a = pendiente.current
          if (a) {
            pendiente.current = null
            const item = p.items.find((x) => x.id === a.id)
            if (item) {
              abrirEditor(item, a.pestana)
            } else {
              setAviso({ texto: `${a.sku} no aparece en la lista con los filtros actuales.`, malo: true })
            }
            onAbierto?.()
          }
        })
        .catch((e) => {
          if (!vigente) return
          setError(e instanceof Error ? e.message : String(e))
        })
        .finally(() => { if (vigente) setCargando(false) })
    }, 300)
    return () => { vigente = false; clearTimeout(t) }
  }, [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio, sinPublicar,
    sinFoto, sinDescripcion, sinEAN, conPromo, orden, ordenDesc, offset, version, refresco])

  // Al cambiar un filtro se vuelve a la primera página: quedarse en la página 7
  // de un resultado que ahora tiene 2 muestra una tabla vacía sin explicación.
  // Cambiar de filtro limpia la selección: aplicar una operación a productos
  // que ya no se ven en pantalla sería una sorpresa desagradable.
  const limpiarSeleccion = seleccion.limpiar
  useEffect(() => { setOffset(0); limpiarSeleccion() },
    [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio, sinPublicar,
      sinFoto, sinDescripcion, sinEAN, conPromo, limpiarSeleccion])

  // Clases de una fila en cualquiera de las cuatro vistas: foco de teclado,
  // selección, archivo encima y excluida del catálogo.
  const claseFila = (p: Producto) => [
    'clicable', teclado.foco === p.id ? 'enfocada' : '',
    marcados.has(p.id) ? 'marcada' : '',
    arrastreSobre === p.id ? 'soltar-aqui' : '',
    p.excluido ? 'excluida' : '',
  ].join(' ').trim()
  const TITULO_FILA = 'Doble clic o Enter: ver cómo quedaría en cada canal · clic derecho: más acciones'

  // Una celda de la tabla de detalles según la columna. Las columnas se
  // pintan en el orden y con la visibilidad que eligió quien la usa.
  const celda = (c: Columna, p: Producto): { clase?: string; etiqueta?: string; contenido: ReactNode } => {
    switch (c.id) {
      case 'check':
        return {
          clase: 'col-check',
          contenido: (
            <label className="casilla">
              <input type="checkbox" checked={marcados.has(p.id)} onChange={casilla(p)} />
              {/* En tarjeta la casilla queda suelta sin nada que la
                  nombre; en la tabla el encabezado ya lo dice. */}
              <span className="solo-movil mini-texto tenue">Seleccionar</span>
            </label>
          ),
        }
      case 'sku':
        return { clase: 'sku oculto-movil', contenido: p.sku || <span className="tenue">—</span> }
      case 'nombre':
        return {
          clase: 'titulo-tarjeta',
          contenido: (
            <>
              <div>{p.nombre}</div>
              {p.categoria && <div className="categoria">{p.categoria}</div>}
              {/* La columna Referencia se esconde en el móvil, pero saber
                  qué SKU es sigue siendo lo primero que se busca. */}
              <div className="solo-movil mini-texto tenue">
                Ref. {p.sku || '—'}{p.marca ? ` · ${p.marca}` : ''}
              </div>
            </>
          ),
        }
      case 'marca':
        return { clase: 'oculto-movil', contenido: p.marca || <span className="tenue">—</span> }
      case 'categoria':
        return { clase: 'oculto-movil', contenido: p.categoria || <span className="tenue">—</span> }
      case 'precio':
        return { clase: 'num', etiqueta: 'Precio', contenido: <PrecioDe p={p} /> }
      case 'stock':
        return { clase: 'num', etiqueta: 'Stock', contenido: num(p.stock) }
      // Dónde vive la ficha. Sin esto había que ir canal por canal a
      // adivinar si el producto estaba subido y a cuál.
      case 'publicacion':
        return { clase: 'apilada', etiqueta: 'Publicación', contenido: <div className="etiquetas"><Publicacion p={p} /></div> }
      case 'estado':
        return { clase: 'apilada', etiqueta: 'Estado', contenido: <div className="etiquetas"><Estado p={p} /></div> }
      case 'ean':
        return { clase: 'sku oculto-movil', contenido: p.barcode || <span className="tenue">—</span> }
      case 'peso':
        return { clase: 'num oculto-movil', contenido: p.peso > 0 ? `${p.peso} kg` : <span className="tenue">—</span> }
      case 'condicion':
        return { clase: 'oculto-movil', contenido: CONDICION[p.condicion] ?? p.condicion }
      case 'acciones':
        return {
          clase: 'acciones-fila',
          contenido: (
            <>
              {/* Tocar la tarjeta abre la vista previa, pero eso no se ve;
                  en el móvil el botón lo hace explícito. */}
              <button className="solo-movil"
                onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>
                Ver en canales
              </button>
              <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }} data-guia="editar"
                title="Editar precio, marca y descripción">
                Editar
              </button>
            </>
          ),
        }
      default:
        return { contenido: null }
    }
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Productos</h1>
          <div className="sub">
            {verExcluidos
              ? 'Fuera del catálogo — gastos, activos fijos y servicios'
              : 'Precio, marca, descripción e imágenes son de Integra; Odoo solo aporta SKU, nombre y stock'}
          </div>
        </div>
      </header>

      {aviso && (
        <div className={aviso.malo ? 'aviso-caja' : 'nota-previa'}>
          <div className="fila">
            <span className="expande-recorta">{aviso.texto}</span>
            <button onClick={() => setAviso(null)}>Cerrar</button>
          </div>
        </div>
      )}

      {error && (
        <div className="aviso-caja">
          <div className="fila">
            <span className="expande-recorta">No se pudo cargar la lista: {error}</span>
            <button onClick={() => setVersion((v) => v + 1)}>Reintentar</button>
          </div>
        </div>
      )}

      <section className="panel">
        <div className="cuerpo">
          <div className="filtros">
            <input className="crece" placeholder="Buscar por nombre o referencia…" data-guia="buscar"
              value={busqueda} onChange={(e) => setBusqueda(e.target.value)} />
            <select value={marca} onChange={(e) => setMarca(e.target.value)} data-guia="cat-marca">
              <option value="">Todas las marcas</option>
              {marcas.filter((m) => m.cantidad > 0).map((m) => (
                <option key={m.codigo} value={m.codigo}>{m.nombre} ({m.cantidad})</option>
              ))}
            </select>
            <select value={categoria} onChange={(e) => setCategoria(e.target.value)}>
              <option value="">Todas las categorías</option>
              {categorias.filter((c) => c.cantidad > 0).map((c) => (
                <option key={c.nombre} value={c.nombre}>{c.nombre} ({c.cantidad})</option>
              ))}
            </select>
            <label className="casilla">
              <input type="checkbox" checked={soloProblemas} data-guia="cat-problemas"
                onChange={(e) => setSoloProblemas(e.target.checked)} />
              Solo con problemas
            </label>
            <label className="casilla">
              <input type="checkbox" checked={sinPrecio}
                onChange={(e) => { setSinPrecio(e.target.checked); setOffset(0) }} />
              Sin precio
            </label>
            <label className="casilla" title="Los que no tienen ficha en ningún canal">
              <input type="checkbox" data-guia="pendientes" checked={sinPublicar}
                onChange={(e) => { setSinPublicar(e.target.checked); setOffset(0) }} />
              Pendientes por publicar
            </label>
            <label className="casilla">
              <input type="checkbox" checked={verExcluidos} data-guia="cat-excluidos"
                onChange={(e) => setVerExcluidos(e.target.checked)} />
              Ver excluidos
            </label>
            <SelectorVista modo={vista} onCambiar={setVista} />
            <button type="button" className={`badge boton-panel ${panel ? 'activo' : ''}`} aria-pressed={panel}
              title="Panel lateral con la ficha del producto con foco"
              onClick={alternarPanel}>
              Detalles
            </button>
          </div>
        </div>

        {(marcados.size > 0 || (pagina?.total ?? 0) > 0) && (
          <div className="barra-masiva" data-guia="cat-barra">
            <span className="crece">
              {marcados.size > 0
                ? `${num(marcados.size)} seleccionados`
                : cargando
                  ? 'Buscando…'
                  : `${num(pagina?.total ?? 0)} productos en el filtro actual`}
            </span>
            {/* En el móvil no hay cabecera de tabla, así que la casilla de
                «seleccionar toda la página» desaparece; este botón la sustituye. */}
            {items.length > 0 && (
              <button className="solo-movil" onClick={alternarPagina}>
                {paginaEntera ? 'Quitar esta página' : 'Marcar esta página'}
              </button>
            )}
            {marcados.size > 0 && (
              <button onClick={seleccion.limpiar}>Limpiar selección</button>
            )}
            {marcados.size > 0 && (
              <button className="primario" onClick={() => void publicarSeleccion()} data-guia="publicar"
                disabled={publicando}
                title="Encola el envío a los canales de los productos marcados">
                {publicando ? 'Encolando…' : `Publicar ${num(marcados.size)}`}
              </button>
            )}
            {marcados.size > 0 && esAdmin && (
              <button onClick={() => setDespublicando(true)} disabled={publicando}
                title="Quita la ficha del canal. El producto sigue en Integra.">
                Despublicar
              </button>
            )}
            <button onClick={abrirPlantilla} data-guia="plantilla"
              title="Descarga el catálogo como hoja de Excel, edítalo y súbelo para actualizar precios y promociones de muchos productos a la vez.">
              Actualizar por plantilla
            </button>
            <button className="primario" onClick={abrirMasiva} data-guia="cat-editar-masa">
              Editar en masa
            </button>
          </div>
        )}

        <div className="explorador-cuerpo">
        {/* El contenedor recibe el foco real (teclado) y el lazo; las filas
            llevan data-clave y aria-selected. */}
        <div {...teclado.propsContenedor} className={`tabla-envoltorio explorador vista-${vista}`}
          aria-label="Productos" aria-busy={cargando}
          onMouseDown={seleccion.lazo.onMouseDown}>
          {vista === 'detalles' && (
          <table className="tabla-tarjetas tabla-columnas" ref={refTabla} style={{ minWidth: col.anchoMinimo }}>
            {col.colgroup}
            <CabeceraColumnas col={col} orden={{ campo: orden, desc: ordenDesc }} onOrdenar={ordenarPor}
              menu={menu} refTabla={refTabla} atributos={{ 'data-guia': 'cat-cabecera' }}
              contenido={(c) => c.id === 'check'
                ? (
                  <input type="checkbox" checked={paginaEntera} data-guia="cat-seleccionar"
                    title="Seleccionar los de esta página"
                    onClick={(e) => e.stopPropagation()}
                    onChange={alternarPagina} />
                )
                : undefined} />
            <tbody>
              {items.map((p) => (
                <tr key={p.id} {...teclado.propsFila(p)} {...propsSoltar(p)} data-guia="fila"
                  className={claseFila(p)} title={TITULO_FILA}>
                  {col.columnas.map((c) => {
                    const { clase, etiqueta, contenido } = celda(c, p)
                    return (
                      <td key={c.id} className={clase} data-etiqueta={etiqueta}
                        onClick={c.id === 'check' ? (e) => e.stopPropagation() : undefined}>
                        {contenido}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          )}

          {/* Los otros modos comparten las mismas acciones que la fila de la
              tabla: casilla, vista previa al abrir, Editar y Ver en canales. */}
          {vista === 'lista' && items.length > 0 && (
            <div className="vista-lista">
              {items.map((p) => (
                <div key={p.id} {...teclado.propsFila(p)} {...propsSoltar(p)} className={`fila-lista ${claseFila(p)}`} title={TITULO_FILA}>
                  <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                    onClick={(e) => e.stopPropagation()} onChange={casilla(p)} />
                  <span className="sku">{p.sku || '—'}</span>
                  <span className="principal" title={p.nombre}>
                    {p.nombre}{p.marca && <span className="tenue"> · {p.marca}</span>}
                  </span>
                  <span className="num"><PrecioDe p={p} /></span>
                  <span className="dato">{num(p.stock)} en stock</span>
                  <span className="etiquetas">
                    <Publicacion p={p} />
                    <Estado p={p} resumen />
                  </span>
                  <span className="vista-acciones">
                    <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver en canales</button>
                    <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}
                      title="Editar precio, marca y descripción">
                      Editar
                    </button>
                  </span>
                </div>
              ))}
            </div>
          )}

          {vista === 'mosaico' && items.length > 0 && (
            <div className="vista-mosaico">
              {items.map((p) => (
                <div key={p.id} {...teclado.propsFila(p)} {...propsSoltar(p)} className={`tarjeta-vista ${claseFila(p)}`} title={TITULO_FILA}>
                  <Imagen sha={p.portada_sha}>
                    <label className="marca-esquina" onClick={(e) => e.stopPropagation()}>
                      <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                        onChange={casilla(p)} />
                    </label>
                  </Imagen>
                  <div className="cuerpo-tarjeta">
                    <div className="titulo" title={p.nombre}>{p.nombre}</div>
                    <div className="sku">{p.sku || '—'}{p.marca ? ` · ${p.marca}` : ''}</div>
                    <div className="datos">
                      <span className="num"><PrecioDe p={p} /></span>
                      <span>{num(p.stock)} en stock</span>
                      {p.categoria && <span className="recorta" title={p.categoria}>{p.categoria}</span>}
                    </div>
                    <div className="etiquetas">
                      <Publicacion p={p} />
                      <Estado p={p} />
                    </div>
                    <div className="vista-acciones">
                      <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver en canales</button>
                      <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}
                        title="Editar precio, marca y descripción">
                        Editar
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {vista === 'iconos' && items.length > 0 && (
            <div className="vista-iconos">
              {items.map((p) => (
                <div key={p.id} {...teclado.propsFila(p)} {...propsSoltar(p)} className={`icono-vista ${claseFila(p)}`}
                  title={`${p.nombre} — ${TITULO_FILA}`}>
                  <Imagen sha={p.portada_sha}>
                    <label className="marca-esquina" onClick={(e) => e.stopPropagation()}>
                      <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                        onChange={casilla(p)} />
                    </label>
                  </Imagen>
                  <div className="nombre">{p.nombre}</div>
                  <div className="sku">{p.sku || '—'}</div>
                  <div className="etiquetas">
                    <Publicacion p={p} resumen />
                    <Estado p={p} resumen />
                  </div>
                  <div className="vista-acciones">
                    <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver</button>
                    <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}>Editar</button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {pagina === null && cargando && !error && (
            <div className="vacio">Cargando productos…</div>
          )}
          {pagina && pagina.items.length === 0 && !cargando && (
            <div className="vacio">
              {hayFiltro
                ? 'Ningún producto coincide con el filtro. Prueba a quitar alguna condición.'
                : 'Todavía no hay productos. Sincroniza con Odoo para traer el catálogo.'}
            </div>
          )}
          {seleccion.lazo.marco}
        </div>

        {panel && (
          <PanelDetalles p={actual} seleccionados={marcados.size}
            arrastre={actual !== null && arrastreSobre === actual.id}
            propsSoltar={propsSoltar}
            onVer={onVer} onEditar={(p) => abrirEditor(p, 'venta')} onCerrar={alternarPanel} />
        )}
        </div>

        <BarraEstado total={pagina?.total ?? 0} seleccionados={marcados.size} nombre="productos">
          {totales.cuantos > 0 && (
            <span className="num"
              title={totales.deSeleccion
                ? 'Suma de la selección (solo lo que hay cargado en esta página)'
                : 'Suma de esta página, a precio × stock'}>
              {totales.deSeleccion ? 'Selección' : 'Página'}: {num(totales.unidades)} unidades · {money(totales.valor)} en inventario
              {totales.sinPrecio > 0 && ` (${num(totales.sinPrecio)} sin precio)`}
            </span>
          )}
          {subiendo && (
            <span>Subiendo foto {num(subiendo.hechas + 1)} de {num(subiendo.total)} a {subiendo.sku}…</span>
          )}
          {nota && <span className="nota-estado">{nota}</span>}
        </BarraEstado>

        {pagina && pagina.total > 0 && (
          <div className="paginacion" data-guia="cat-paginacion">
            <button disabled={offset === 0 || cargando}
              onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))}>
              ← Anterior
            </button>
            <span className="tenue">
              {pagina.total === 0 ? 0 : offset + 1}–{Math.min(offset + POR_PAGINA, pagina.total)} de {num(pagina.total)}
            </span>
            <button disabled={offset + POR_PAGINA >= pagina.total || cargando}
              onClick={() => setOffset(offset + POR_PAGINA)}>
              Siguiente →
            </button>
          </div>
        )}
      </section>

      {menu.Menu}

      {despublicando && (
        <DialogoDespublicar
          cuantos={marcados.size}
          onCerrar={() => setDespublicando(false)}
          onConfirmar={(cs) => void despublicarDe(cs)}
        />
      )}

      {plantilla && (
        <PlantillaMasiva filtro={filtroActual} total={pagina?.total ?? 0}
          marcas={marcas} categorias={categorias}
          onFiltrar={(c) => {
            // Los filtros del diálogo son los mismos de la lista: cambiarlos
            // aquí mueve también lo que se ve detrás, que es lo que evita
            // descargar una cosa y encontrarse otra al cerrar.
            if ('marca' in c) setMarca(c.marca ?? '')
            if ('categoria' in c) setCategoria(c.categoria ?? '')
            if ('q' in c) setBusqueda(c.q ?? '')
            if ('problemas' in c) setSoloProblemas(!!c.problemas)
            if ('excluidos' in c) setVerExcluidos(!!c.excluidos)
            if ('sin_precio' in c) setSinPrecio(!!c.sin_precio)
            if ('sin_publicar' in c) setSinPublicar(!!c.sin_publicar)
            if ('sin_foto' in c) setSinFoto(!!c.sin_foto)
            if ('sin_descripcion' in c) setSinDescripcion(!!c.sin_descripcion)
            if ('sin_ean' in c) setSinEAN(!!c.sin_ean)
            if ('con_promo' in c) setConPromo(!!c.con_promo)
          }}
          onCerrar={() => setPlantilla(false)}
          onAplicado={() => {
            // No se cierra el diálogo al aplicar: el resumen de lo que cambió
            // es lo que el operador necesita leer justo después.
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {masiva && (
        <EdicionMasiva ids={[...marcados]} filtro={filtroActual}
          totalFiltro={pagina?.total ?? 0}
          onCerrar={() => setMasiva(false)}
          onAplicado={() => {
            setMasiva(false)
            seleccion.limpiar()
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {editando !== null && (
        <Editar producto={editando} marcas={marcas} pestanaInicial={pestanaEditor}
          onCerrar={() => setEditando(null)}
          onGuardado={() => {
            setEditando(null)
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}
    </>
  )
}

// Panel lateral de detalles, como el de vista previa del explorador: la
// ficha del producto con foco (o del último seleccionado), la portada
// grande y las mismas acciones que la fila. También admite soltar fotos.
function PanelDetalles({ p, seleccionados, arrastre, propsSoltar, onVer, onEditar, onCerrar }: {
  p: Producto | null
  seleccionados: number
  arrastre: boolean
  propsSoltar: (p: Producto) => Record<string, (e: DragEvent<HTMLElement>) => void>
  onVer: (id: number) => void
  onEditar: (p: Producto) => void
  onCerrar: () => void
}) {
  return (
    <aside className={`panel-detalles lazo-ignorar ${arrastre ? 'soltar-aqui' : ''}`}
      aria-label="Detalles del producto" {...(p ? propsSoltar(p) : {})}>
      <div className="cabecera-panel">
        <strong>Detalles</strong>
        <button type="button" className="enlace" onClick={onCerrar} aria-label="Cerrar el panel de detalles">✕</button>
      </div>
      {p === null ? (
        <div className="vacio">
          {seleccionados > 1
            ? `${num(seleccionados)} productos seleccionados`
            : 'Selecciona un producto para ver su ficha aquí.'}
        </div>
      ) : (
        <>
          <Imagen sha={p.portada_sha} variante="web_800" titulo={p.nombre} />
          <h3>{p.nombre}</h3>
          <dl>
            <dt>Referencia</dt><dd className="sku">{p.sku || '—'}</dd>
            <dt>Marca</dt><dd>{p.marca || '—'}</dd>
            <dt>Categoría</dt><dd>{p.categoria || '—'}</dd>
            <dt>Precio</dt><dd><PrecioDe p={p} /></dd>
            <dt>Stock</dt><dd>{num(p.stock)} unidades</dd>
            <dt>EAN</dt><dd className="sku">{p.barcode || '—'}</dd>
            <dt>Publicación</dt>
            <dd>
              {(p.publicado ?? []).length === 0
                ? <span className="pastilla dudosa">Sin publicar</span>
                : (
                  <ul className="lista-canales">
                    {(p.publicado ?? []).map((c) => (
                      <li key={c.canal}>
                        <span className={`pastilla ${c.estado === 'published' ? 'ok' : c.estado === 'error' ? 'bloqueante' : 'aviso'}`}>
                          {NOMBRE_CANAL[c.canal] ?? c.canal}
                        </span>
                        <span className="tenue mini-texto"> {ESTADO_CANAL[c.estado] ?? c.estado}</span>
                      </li>
                    ))}
                  </ul>
                )}
            </dd>
            <dt>Estado</dt>
            <dd>
              {p.problemas.length === 0
                ? <span className="pastilla ok">Listo</span>
                : (
                  <ul className="lista-motivos">
                    {p.problemas.map((m) => (
                      <li key={m}>
                        <span className={`pastilla ${bloqueante(m) ? 'bloqueante' : 'aviso'}`}>{motivo(m)}</span>
                        {p.detalles?.[m] && <div className="tenue mini-texto">{p.detalles[m]}</div>}
                      </li>
                    ))}
                  </ul>
                )}
            </dd>
          </dl>
          {p.excluido && <span className="pastilla neutra">Excluido del catálogo</span>}
          <div className="vista-acciones">
            <button onClick={() => onVer(p.id)}>Ver en canales</button>
            <button onClick={() => onEditar(p)} title="Editar precio, marca y descripción">Editar</button>
          </div>
          <div className="tenue mini-texto">
            Arrastra fotos desde tu equipo aquí o sobre la fila para añadirlas al producto.
          </div>
        </>
      )}
    </aside>
  )
}

// Piezas que comparten lista, mosaico e iconos. La tabla de «detalles»
// conserva su marcado propio: es la vista de siempre y no se toca.
function PrecioDe({ p }: { p: Producto }) {
  if (p.precio !== null) return <strong>{money(p.precio)}</strong>
  if (p.precio_sugerido !== null) {
    return (
      <span className="tenue" title="Sugerencia según tarifas de Odoo; asígnalo al editar">
        ({money(p.precio_sugerido)}) sugerido
      </span>
    )
  }
  return <span className="tenue">Sin precio</span>
}

// Dónde vive la ficha. Con `resumen`, una sola pastilla que cuenta canales,
// para los sitios donde no cabe una por canal.
function Publicacion({ p, resumen = false }: { p: Producto; resumen?: boolean }) {
  const canales = p.publicado ?? []
  if (canales.length === 0) return <span className="pastilla dudosa">Sin publicar</span>
  if (resumen) {
    const conError = canales.some((c) => c.estado === 'error')
    return (
      <span className={`pastilla ${conError ? 'bloqueante' : 'ok'}`}
        title={canales.map((c) => `${NOMBRE_CANAL[c.canal] ?? c.canal}: ${ESTADO_CANAL[c.estado] ?? c.estado}`).join(' · ')}>
        {canales.length === 1 ? (NOMBRE_CANAL[canales[0].canal] ?? canales[0].canal) : `${canales.length} canales`}
      </span>
    )
  }
  return (
    <>
      {canales.map((c) => (
        <span key={c.canal}
          className={`pastilla ${c.estado === 'published' ? 'ok' : c.estado === 'error' ? 'bloqueante' : 'aviso'}`}
          title={ESTADO_CANAL[c.estado] ?? c.estado}>
          {NOMBRE_CANAL[c.canal] ?? c.canal}
        </span>
      ))}
    </>
  )
}

// Los problemas de la ficha. Con `resumen`, una pastilla que los cuenta y
// los enumera en el title, que es lo que cabe en una línea o bajo un icono.
function Estado({ p, resumen = false }: { p: Producto; resumen?: boolean }) {
  if (p.problemas.length === 0) return <span className="pastilla ok">Listo</span>
  if (resumen) {
    const grave = p.problemas.some(bloqueante)
    return (
      <span className={`pastilla ${grave ? 'bloqueante' : 'aviso'}`}
        title={p.problemas.map((m) => p.detalles?.[m] || motivo(m)).join(' · ')}>
        {p.problemas.length === 1 ? motivo(p.problemas[0]) : `${p.problemas.length} problemas`}
      </span>
    )
  }
  return (
    <>
      {p.problemas.map((m) => (
        // El detalle va en el title: es lo que explica el aviso, y sin él
        // «la portada no cuadra» no se puede ni juzgar ni resolver.
        <span key={m} title={p.detalles?.[m] || motivo(m)}
          className={`pastilla ${bloqueante(m) ? 'bloqueante' : 'aviso'}`}>
          {motivo(m)}
        </span>
      ))}
    </>
  )
}

// DialogoDespublicar deja elegir de qué canales se quita la ficha.
//
// Antes la acción alcanzaba siempre a todos los canales activos, que es justo
// lo que no se quiere cuando un producto va bien en uno y mal en otro. Y el
// aviso explica la diferencia con «retirar de la venta», porque la palabra
// «despublicar» no la deja clara por sí sola.
function DialogoDespublicar({ cuantos, onCerrar, onConfirmar }: {
  cuantos: number
  onCerrar: () => void
  onConfirmar: (cuentas: CuentaCanal[]) => void
}) {
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [elegidas, setElegidas] = useState<Set<number>>(new Set())
  const [cargando, setCargando] = useState(true)

  useEffect(() => {
    api.cuentas()
      .then((cs) => setCuentas(cs.filter((c) => c.activa)))
      .catch(() => setCuentas([]))
      .finally(() => setCargando(false))
  }, [])

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  const alternar = (id: number) => {
    const s = new Set(elegidas)
    s.has(id) ? s.delete(id) : s.add(id)
    setElegidas(s)
  }

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Despublicar {num(cuantos)} productos</h2>
            <div className="sub">Elige de qué canales se quita la ficha</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        <div className="cuerpo">
          {cargando && <div className="vacio">Cargando canales…</div>}
          {!cargando && cuentas.length === 0 && (
            <div className="vacio">No hay ninguna cuenta de canal activa.</div>
          )}
          {cuentas.map((c) => (
            <label key={c.id} className="casilla fila-cuenta">
              <input type="checkbox" checked={elegidas.has(c.id)}
                onChange={() => alternar(c.id)} />
              <span className="expande">{NOMBRE_CANAL[c.canal] ?? c.canal}</span>
            </label>
          ))}
          {cuentas.length > 1 && (
            <button className="enlace"
              onClick={() => setElegidas(new Set(cuentas.map((c) => c.id)))}>
              Marcar todos
            </button>
          )}
        </div>

        <div className="aviso-caja">
          <strong>El producto no se borra.</strong> Viene de Odoo y sigue en Integra,
          listo para volver a publicarse. Lo que se pierde es lo que vivía en el canal:
          el historial, las preguntas, las reseñas y la posición en el buscador, y la
          dirección de la ficha deja de existir.
        </div>

        <div className="nota-previa">
          Si solo quieres que deje de venderse un tiempo, usa <strong>Retirar</strong> en
          Publicación: eso la pausa y se puede reabrir con su historial entero. En
          MercadoLibre y en Falabella no hay despublicado real, así que la ficha queda
          cerrada para siempre.
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar}>Cancelar</button>
          <button className="primario" disabled={elegidas.size === 0}
            onClick={() => onConfirmar(cuentas.filter((c) => elegidas.has(c.id)))}>
            Despublicar de {num(elegidas.size)} canales
          </button>
        </footer>
      </div>
    </div>
  )
}
