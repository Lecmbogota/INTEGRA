import type { Seccion } from '../Sidebar'
import { PASOS, type Paso } from '../GuiaPasos'
import { PASOS_PANEL } from './panel'
import { PASOS_CATALOGO } from './catalogo'
import { PASOS_EDITAR } from './editar'
import { PASOS_EDICION_MASIVA } from './edicionMasiva'
import { PASOS_PLANTILLA } from './plantilla'
import { PASOS_PREVIEW } from './preview'
import { PASOS_MEDIATECA, PASOS_ASIGNAR } from './mediateca'
import { PASOS_EDITOR_FOTO } from './editorFoto'
import { PASOS_SELECTOR } from './selectorMediateca'
import { PASOS_PUBLICACION } from './publicacion'
import { PASOS_PEDIDOS } from './pedidos'
import { PASOS_ACTIVIDAD } from './actividad'
import { PASOS_AUTOMATIZACION } from './automatizacion'
import { PASOS_INTEGRACIONES } from './integraciones'
import { PASOS_CATEGORIAS } from './categorias'
import { PASOS_ATRIBUTOS } from './atributos'
import { PASOS_CANALES } from './canales'
import { PASOS_USUARIOS } from './usuarios'
import { PASOS_AVISOS } from './avisos'

// Todos los recorridos, en el orden en que se trabaja. `clave` es estable:
// con ella se guarda el progreso de cada uno. Los de sección se abren desde
// el «?» de su pantalla; los de diálogo, desde el «?» del diálogo (aquí se
// pueden leer igual, pero sin nada que resaltar porque el diálogo no está
// abierto).
export type Recorrido = {
  clave: string
  nombre: string
  pasos: Paso[]
  tipo: 'general' | 'seccion' | 'dialogo'
  seccion?: Seccion
  // Qué explica, en una línea, para la lista del centro de ayuda.
  resumen: string
}

export const RECORRIDOS: Recorrido[] = [
  { clave: 'general', nombre: 'Cómo funciona Integra', pasos: PASOS, tipo: 'general',
    resumen: 'El flujo entero de punta a punta: de Odoo a los canales y de vuelta. Empieza aquí.' },
  { clave: 'panel', nombre: 'Panel', pasos: PASOS_PANEL, tipo: 'seccion', seccion: 'panel',
    resumen: 'Las cifras del día, qué bloquea la publicación y qué publicar primero.' },
  { clave: 'catalogo', nombre: 'Productos', pasos: PASOS_CATALOGO, tipo: 'seccion', seccion: 'catalogo',
    resumen: 'Buscar, filtrar, leer las columnas, seleccionar y publicar en lote.' },
  { clave: 'editar', nombre: 'Editar producto', pasos: PASOS_EDITAR, tipo: 'dialogo',
    resumen: 'Cada pestaña y cada campo de la ficha: precio, marca, títulos, envío.' },
  { clave: 'edicion-masiva', nombre: 'Edición en masa', pasos: PASOS_EDICION_MASIVA, tipo: 'dialogo',
    resumen: 'Una operación sobre muchos productos, simulada antes de aplicarse.' },
  { clave: 'plantilla', nombre: 'Actualización por plantilla', pasos: PASOS_PLANTILLA, tipo: 'dialogo',
    resumen: 'Precios y promociones desde una hoja de cálculo, con revisión.' },
  { clave: 'preview', nombre: 'Vista previa', pasos: PASOS_PREVIEW, tipo: 'dialogo',
    resumen: 'Qué se enviará a cada canal, qué le falta y desde dónde se corrige.' },
  { clave: 'mediateca', nombre: 'Imágenes', pasos: PASOS_MEDIATECA, tipo: 'seccion', seccion: 'mediateca',
    resumen: 'Subir en masa, el banco entero, filtros, asignar y borrar.' },
  { clave: 'editor-foto', nombre: 'Editor de fotos', pasos: PASOS_EDITOR_FOTO, tipo: 'dialogo',
    resumen: 'Recortar, quitar el fondo, encajar en cuadrado y exportar como piden los canales.' },
  { clave: 'selector', nombre: 'Elegir de la mediateca', pasos: PASOS_SELECTOR, tipo: 'dialogo',
    resumen: 'Darle a un producto fotos que ya están en el banco.' },
  { clave: 'asignar', nombre: 'Asignar a producto', pasos: PASOS_ASIGNAR, tipo: 'dialogo',
    resumen: 'Enlazar una o varias fotos al producto que se busque.' },
  { clave: 'categorias', nombre: 'Categorías', pasos: PASOS_CATEGORIAS, tipo: 'seccion', seccion: 'categorias',
    resumen: 'El mapeo entre el árbol de Odoo y el de cada canal, y qué desbloquea confirmarlo.' },
  { clave: 'atributos', nombre: 'Atributos', pasos: PASOS_ATRIBUTOS, tipo: 'seccion', seccion: 'atributos',
    resumen: 'Lo que exigen MercadoLibre y Falabella por categoría y de dónde salen los valores.' },
  { clave: 'publicacion', nombre: 'Publicación', pasos: PASOS_PUBLICACION, tipo: 'seccion', seccion: 'publicacion',
    resumen: 'Estado por canal, planificar envíos, poner a la venta, retirar; producto a producto.' },
  { clave: 'pedidos', nombre: 'Pedidos', pasos: PASOS_PEDIDOS, tipo: 'seccion', seccion: 'pedidos',
    resumen: 'Cómo llegan, qué pasa en Odoo, errores, despacho y stock.' },
  { clave: 'actividad', nombre: 'Actividad', pasos: PASOS_ACTIVIDAD, tipo: 'seccion', seccion: 'actividad',
    resumen: 'Qué está en marcha, cancelar y reintentar, y la línea de tiempo.' },
  { clave: 'automatizacion', nombre: 'Automatización', pasos: PASOS_AUTOMATIZACION, tipo: 'seccion', seccion: 'automatizacion',
    resumen: 'Avisos abiertos, qué vigila Integra y las tareas programadas.' },
  { clave: 'integraciones', nombre: 'Odoo', pasos: PASOS_INTEGRACIONES, tipo: 'seccion', seccion: 'integraciones',
    resumen: 'La conexión con Odoo: qué se trae, qué no, probar, cambiar.' },
  { clave: 'canales', nombre: 'Canales', pasos: PASOS_CANALES, tipo: 'seccion', seccion: 'canales',
    resumen: 'Cuentas, credenciales, bodegas, margen mínimo y comisiones.' },
  { clave: 'usuarios', nombre: 'Usuarios', pasos: PASOS_USUARIOS, tipo: 'seccion', seccion: 'usuarios',
    resumen: 'Quién entra, con qué rol, y cómo se da o se quita el acceso.' },
  { clave: 'avisos', nombre: 'Avisos', pasos: PASOS_AVISOS, tipo: 'seccion', seccion: 'avisos',
    resumen: 'A qué correos sale lo que Integra tiene que contar, y desde qué gravedad.' },
]

export const GENERAL = RECORRIDOS[0]

// Qué recorrido explica cada sección del menú (para el botón «?»).
export const GUIAS_POR_SECCION: Partial<Record<Seccion, Recorrido>> = Object.fromEntries(
  RECORRIDOS.filter((r) => r.seccion).map((r) => [r.seccion as Seccion, r]),
) as Partial<Record<Seccion, Recorrido>>

export const TOTAL_PASOS = RECORRIDOS.reduce((n, r) => n + r.pasos.length, 0)
