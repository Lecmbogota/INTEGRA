import type { ReactNode } from 'react'
import type {
  Atencion, BusquedaMasiva, Categoria, Marca, Resumen, ResumenOrdenes, StockAlmacen, Producto,
} from '../api'
import type { FotoEditable } from '../EditorFoto'
// La pestaña del editor de producto (Datos, Fotos…) se llama igual que la
// pestaña de ventana de aquí abajo; se renombra al importar para que
// `Pestana` en el escritorio signifique siempre pestaña de ventana.
import type { Pestana as PestanaEditor } from '../Editar'

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
export type PropsEditar = { varianteId: number; pestana?: PestanaEditor; sku?: string }
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
  // Termina esta página: si es la base de la ventana la cierra; si se llegó
  // navegando (Productos → Editar), vuelve a la anterior y la olvida.
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

// Una página del historial de una ventana: la app que se ve y con qué se
// abrió. Seleccionar un producto en Productos no abre otra ventana: navega a
// la previa dentro de la misma, y ← vuelve a la lista tal como estaba.
// `clave` es única por página y sirve de key de React: dos ediciones seguidas
// del mismo producto son páginas distintas, con su estado cada una.
export type Pagina = { clave: string; app: AppId; props: Record<string, unknown>; titulo: string }

// Una pestaña de una ventana, como en el explorador de Windows 11: cada una
// lleva su propio historial y su página actual (`historial[indice]`). Una
// ventana tiene siempre al menos una; cerrar la última cierra la ventana.
export type Pestana = { id: string; historial: Pagina[]; indice: number }

export type Ventana = {
  id: string
  // Página actual de la pestaña activa (siempre `pestañaActiva.historial[indice]`,
  // copiada aquí para que todo lo que ya lee `v.app`, `v.props`, `v.titulo`,
  // `v.historial` o `v.indice` siga funcionando sin saber que hay pestañas).
  app: AppId
  titulo: string
  props: Record<string, unknown>
  historial: Pagina[]
  indice: number
  // Las pestañas en su orden y cuál se ve. `historial` e `indice` de arriba
  // son copias de la activa: toda mutación pasa por el gestor, que las
  // mantiene sincronizadas.
  pestanas: Pestana[]
  pestanaActiva: string
  x: number
  y: number
  w: number
  h: number
  estado: EstadoVentana
  // Orden de apilado: mayor = delante.
  z: number
  // Geometría a la que volver al restaurar desde maximizada/ajustada.
  anterior?: { x: number; y: number; w: number; h: number }
}

// Eventos entre ventanas. Quien cambia algo lo emite; quien lo muestra se
// suscribe y recarga. Es lo que sustituye a los callbacks onCambio/onGuardado
// que antes atravesaban App. («Ir a arreglar» desde la previa ya no es un
// evento: la previa navega dentro de su propia ventana con useIr.)
export type Evento =
  | { nombre: 'producto-cambiado'; varianteId?: number }
  | { nombre: 'fotos-cambiadas'; varianteId?: number }
  | { nombre: 'publicaciones-cambiadas' }
  | { nombre: 'pedidos-cambiados' }

export type Sistema = {
  ventanas: Ventana[]
  // Ventana con el foco (la de mayor z que no esté minimizada), o null.
  activa: string | null
  // Abre una app en una ventana nueva. Si es única y ya está abierta (como
  // página actual o como base de una ventana), la enfoca y va a esa página
  // (y restaura si estaba minimizada). Devuelve el id de la ventana.
  abrir: (app: AppId, props?: Record<string, unknown>, opciones?: { titulo?: string }) => string
  cerrar: (id: string) => void
  // Historial de la pestaña activa de la ventana. `navegar` añade una página
  // tras la actual —descartando lo que hubiera «adelante», como un
  // navegador— y la hace actual; `atras`/`adelante` se mueven por él sin
  // perder nada; `irAPagina` salta a la página k (las migas de la barra de
  // dirección). Si se puede ir atrás o adelante se deduce de `indice` y
  // `historial.length`.
  navegar: (id: string, app: AppId, props?: Record<string, unknown>, titulo?: string) => void
  atras: (id: string) => void
  adelante: (id: string) => void
  irAPagina: (id: string, indice: number) => void
  // Una página terminó (el diálogo guardó o se canceló): se quita del
  // historial con las que hubiera después y se vuelve a la anterior. No es
  // `atras`: un editor que ya guardó no debe seguir «adelante» con datos
  // viejos. Se busca por clave en todas las pestañas (la página puede haber
  // quedado en una de fondo). Con la página base de una pestaña no hace
  // nada: esa se cierra con `cerrarPestana`.
  cerrarPagina: (id: string, clave: string) => void
  // Pestañas. `nuevaPestana` abre una al final con la app pedida (por
  // defecto la app base de la ventana: Ctrl+T en Productos da otro
  // Productos) y la activa; `abrirEnPestana` es lo mismo con una página
  // concreta («abrir en pestaña nueva»). `cerrarPestana` con la última
  // pestaña cierra la ventana. `moverPestana` la reordena a la posición
  // pedida (arrastre).
  nuevaPestana: (id: string, app?: AppId, props?: Record<string, unknown>, titulo?: string) => void
  abrirEnPestana: (id: string, app: AppId, props?: Record<string, unknown>, titulo?: string) => void
  cerrarPestana: (id: string, pestanaId: string) => void
  activarPestana: (id: string, pestanaId: string) => void
  moverPestana: (id: string, pestanaId: string, aIndice: number) => void
  enfocar: (id: string) => void
  minimizar: (id: string) => void
  maximizar: (id: string) => void
  restaurar: (id: string) => void
  // Ajusta a la mitad izquierda o derecha del área de trabajo.
  ajustar: (id: string, lado: 'izquierda' | 'derecha') => void
  mover: (id: string, x: number, y: number) => void
  redimensionar: (id: string, geometria: { x: number; y: number; w: number; h: number }) => void
  // Cambia el título de la página actual de la pestaña activa (queda en el
  // historial).
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
  | { tipo: 'imagen'; url: string; nombre?: string }   // una foto (Unsplash o propia)

export type Preferencias = {
  tema: 'claro' | 'oscuro' | 'sistema'
  fondo: Fondo
  // Apps ancladas en la barra, en orden.
  ancladas: AppId[]
  // Iconos del escritorio, en orden de cuadrícula.
  escritorio: AppId[]
  tamanoTexto: 'normal' | 'grande'
  // Barra de tareas: centrada (estilo moderno) o a la izquierda.
  barraCentrada: boolean
  // Dónde está cada cosa en el escritorio: posición libre de cada icono y
  // la lista de widgets con su posición y tamaño. Todo en celdas de la
  // cuadrícula invisible (CELDA px), para que al soltar quede alineado.
  disposicion: Disposicion
}

// ------------------------------------------------------------- escritorio

// Lado de la celda de la cuadrícula invisible del escritorio, en px.
export const CELDA = 24

// Tipos de widget. Cada bloque del Panel es uno, más los del sistema.
export type WidgetTipo =
  | 'cifras'          // las tarjetas del resumen (productos, marcas, con precio, con stock, publicables, atención, excluidos)
  | 'cifra'           // una sola cifra grande, elegible (config.clave)
  | 'bloqueos'        // qué bloquea la publicación
  | 'bodegas'         // existencias por bodega
  | 'prioridad'       // qué publicar primero
  | 'pedidos'         // resumen de pedidos
  | 'actividad'       // trabajos en marcha y última sincronización
  | 'sincronizacion'  // botón Sincronizar ahora + última vez
  | 'reloj'           // hora y fecha grandes
  | 'atajos'          // accesos rápidos a apps elegidas (config.apps)

export type WidgetInstancia = {
  id: string
  tipo: WidgetTipo
  // Posición y tamaño en celdas.
  x: number
  y: number
  w: number
  h: number
  // Ajustes propios del tipo (la cifra que enseña, las apps del atajo…).
  config?: Record<string, unknown>
}

export type Disposicion = {
  // Posición en celdas de cada icono del escritorio. Un icono sin posición
  // se coloca en el primer hueco libre de la cuadrícula.
  iconos: Partial<Record<AppId, { x: number; y: number }>>
  widgets: WidgetInstancia[]
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
