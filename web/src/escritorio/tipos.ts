import type { ReactNode } from 'react'
import type {
  Atencion, BusquedaMasiva, Categoria, Marca, Resumen, ResumenOrdenes, StockAlmacen, Producto,
} from '../api'
import type { FotoEditable } from '../EditorFoto'
import type { Pestana } from '../Editar'

// Contrato del escritorio: lo que cada pieza ofrece y lo que espera de las
// demás. Las piezas viven en ficheros distintos y las escriben personas
// distintas; este fichero es el único sitio donde se acuerdan las formas.
//
// Piezas:
//   sistema.tsx        gestor de ventanas (useSistema) + bus de eventos
//   Ventana.tsx        marco de una ventana: barra de título, mover, redimensionar
//   apps.tsx           registro de apps: cada pantalla y cada diálogo como app
//   datos.tsx          datos globales que antes vivían en App (useDatos)
//   preferencias.tsx   tema, fondo, apps ancladas… (usePreferencias)
//   Notificaciones.tsx centro de notificaciones y avisos emergentes (useNotificaciones)
//   Escritorio.tsx     fondo, iconos, widgets, menú contextual
//   BarraTareas.tsx    barra inferior: inicio, ancladas, ventanas, bandeja, reloj
//   Inicio.tsx         menú de inicio con buscador global
//   Configuracion.tsx  app de configuración del escritorio

// ------------------------------------------------------------------ apps

// Las apps de sección (una instancia) y los diálogos (varias instancias,
// cada una con sus props). Los identificadores de sección coinciden con
// `Seccion` de Sidebar.tsx para que la guía siga funcionando.
export type AppId =
  | 'panel' | 'catalogo' | 'mediateca' | 'publicacion' | 'pedidos'
  | 'actividad' | 'automatizacion' | 'integraciones' | 'categorias' | 'atributos'
  | 'canales' | 'usuarios' | 'avisos'
  | 'configuracion' | 'ayuda'
  | 'editar' | 'preview' | 'editor-foto' | 'plantilla' | 'edicion-masiva' | 'selector-mediateca' | 'asignar-foto'

// Props que recibe cada diálogo cuando se abre como ventana. Los callbacks no
// viajan en props (una ventana es independiente de quien la abrió): los
// resultados se comunican por el bus de eventos (ver Evento).
// `sku` porque no hay GET de producto por id: el editor lo localiza buscando
// por referencia en la lista y quedándose con la variante pedida.
export type PropsEditar = { varianteId: number; pestana?: Pestana; sku?: string }
export type PropsPreview = { varianteId: number }
export type PropsEditorFoto = { foto: FotoEditable }
export type PropsPlantilla = Record<string, never>
export type PropsEdicionMasiva = { seleccion: { ids?: number[]; filtro?: Record<string, unknown> }; cuantos: number }
export type PropsSelectorMediateca = { varianteId: number }
export type PropsAsignarFoto = { imagenIds: number[] }

export type PropsVentana = {
  // Identificador de la ventana que contiene la app.
  ventanaId: string
  // Props con las que se abrió (vacío en las apps de sección).
  props: Record<string, unknown>
  // Cierra esta ventana.
  cerrar: () => void
}

export type AppDef = {
  id: AppId
  nombre: string
  // Una línea: para el menú de inicio, el buscador y la configuración.
  descripcion: string
  // Icono SVG de 24×24 con `currentColor`; el fondo lo pone `color`.
  icono: ReactNode
  // Color de fondo del icono (hex).
  color: string
  // Tamaño inicial y mínimo de la ventana, en px.
  tamano: { w: number; h: number }
  minimo: { w: number; h: number }
  // Una sola instancia (secciones) o varias (diálogos).
  unica: boolean
  soloAdmin?: boolean
  // Aparece en el escritorio / en la barra por defecto. Los diálogos no.
  enEscritorio: boolean
  anclada: boolean
  // Contenido de la ventana.
  render: (p: PropsVentana) => ReactNode
}

// ------------------------------------------------------------- ventanas

export type EstadoVentana = 'normal' | 'minimizada' | 'maximizada' | 'izquierda' | 'derecha'

export type Ventana = {
  id: string
  app: AppId
  titulo: string
  x: number
  y: number
  w: number
  h: number
  estado: EstadoVentana
  // Orden de apilado: mayor = delante.
  z: number
  props: Record<string, unknown>
  // Geometría a la que volver al restaurar desde maximizada/ajustada.
  anterior?: { x: number; y: number; w: number; h: number }
}

// Eventos entre ventanas. Quien cambia algo lo emite; quien lo muestra se
// suscribe y recarga. Es lo que sustituye a los callbacks onCambio/onGuardado
// que antes atravesaban App.
export type Evento =
  | { nombre: 'producto-cambiado'; varianteId?: number }
  | { nombre: 'fotos-cambiadas'; varianteId?: number }
  | { nombre: 'publicaciones-cambiadas' }
  | { nombre: 'pedidos-cambiados' }
  | { nombre: 'ir-a-arreglar'; destino: { tipo: 'editar'; varianteId: number; sku: string; pestana: Pestana } | { tipo: 'categorias'; canal: string } }

export type Sistema = {
  ventanas: Ventana[]
  // Ventana con el foco (la de mayor z que no esté minimizada), o null.
  activa: string | null
  // Abre una app. Si es única y ya está abierta, la enfoca (y restaura si
  // estaba minimizada). Devuelve el id de la ventana.
  abrir: (app: AppId, props?: Record<string, unknown>, opciones?: { titulo?: string }) => string
  cerrar: (id: string) => void
  enfocar: (id: string) => void
  minimizar: (id: string) => void
  maximizar: (id: string) => void
  restaurar: (id: string) => void
  // Ajusta a la mitad izquierda o derecha del área de trabajo.
  ajustar: (id: string, lado: 'izquierda' | 'derecha') => void
  mover: (id: string, x: number, y: number) => void
  redimensionar: (id: string, geometria: { x: number; y: number; w: number; h: number }) => void
  retitular: (id: string, titulo: string) => void
  minimizarTodas: () => void
  // Bus de eventos.
  emitir: (e: Evento) => void
  suscribir: (nombre: Evento['nombre'], cb: (e: Evento) => void) => () => void
  // Área de trabajo disponible (la pantalla menos la barra de tareas).
  area: { w: number; h: number }
  // Verdadero por debajo de 768 px: las ventanas van a pantalla completa.
  movil: boolean
}

// ---------------------------------------------------------- preferencias

export type Fondo =
  | { tipo: 'preset'; id: string }      // uno de los fondos incluidos
  | { tipo: 'color'; color: string }    // color liso
  | { tipo: 'degradado'; desde: string; hasta: string }

export type Preferencias = {
  tema: 'claro' | 'oscuro' | 'sistema'
  fondo: Fondo
  // Apps ancladas en la barra, en orden.
  ancladas: AppId[]
  // Iconos del escritorio, en orden de cuadrícula.
  escritorio: AppId[]
  tamanoTexto: 'normal' | 'grande'
  // Widgets del escritorio visibles.
  widgets: { atencion: boolean; pedidos: boolean; actividad: boolean }
  // Barra de tareas: centrada (estilo moderno) o a la izquierda.
  barraCentrada: boolean
}

// ------------------------------------------------------------------ datos

export type Datos = {
  resumen: Resumen | null
  marcas: Marca[]
  categorias: Categoria[]
  atencion: Atencion[]
  stock: StockAlmacen[]
  pedidos: ResumenOrdenes | null
  // Avisos abiertos (los que cuenta la insignia).
  alertas: number
  // Trabajos en marcha ahora mismo (se sondea cada 15 s).
  tareasActivas: number
  masivo: BusquedaMasiva | null
  error: string | null
  recargar: () => Promise<void>
  sincronizar: () => Promise<void>
  sincronizando: boolean
  lanzarMasivo: () => Promise<void>
}

// --------------------------------------------------------- notificaciones

export type Notificacion = {
  id: string
  tipo: 'aviso' | 'pedido' | 'tarea' | 'sistema'
  titulo: string
  texto?: string
  cuando: string
  severidad: 'info' | 'warning' | 'error' | 'critical'
  // App que se abre al pulsarla.
  app?: AppId
  leida: boolean
}

export type Notificaciones = {
  lista: Notificacion[]
  noLeidas: number
  marcarLeidas: () => void
  quitar: (id: string) => void
  vaciar: () => void
  // Añade una notificación (y la enseña emergente si `emergente`).
  notificar: (n: Omit<Notificacion, 'id' | 'cuando' | 'leida'>, emergente?: boolean) => void
}

// ------------------------------------------------------------------ sesión

export type Usuario = { id: number; email: string; name: string; role: string }
export type SesionActiva = { usuario: Usuario; salir: () => void }

// Datos de un producto tal y como los necesita el editor cuando se abre como
// ventana: el adaptador los pide por id.
export type ProductoCargado = Producto
