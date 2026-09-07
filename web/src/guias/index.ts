import type { Seccion } from '../Sidebar'
import type { Paso } from '../GuiaPasos'
import { PASOS_PANEL } from './panel'
import { PASOS_CATALOGO } from './catalogo'
import { PASOS_MEDIATECA } from './mediateca'
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

// Qué recorrido explica cada sección del menú. Los recorridos de los
// diálogos (editar producto, vista previa, editor de fotos, plantilla…) no
// están aquí: cada diálogo abre el suyo desde su propio botón de ayuda.
export type Recorrido = { nombre: string; pasos: Paso[] }

export const GUIAS_POR_SECCION: Partial<Record<Seccion, Recorrido>> = {
  panel: { nombre: 'Panel', pasos: PASOS_PANEL },
  catalogo: { nombre: 'Productos', pasos: PASOS_CATALOGO },
  mediateca: { nombre: 'Imágenes', pasos: PASOS_MEDIATECA },
  publicacion: { nombre: 'Publicación', pasos: PASOS_PUBLICACION },
  pedidos: { nombre: 'Pedidos', pasos: PASOS_PEDIDOS },
  actividad: { nombre: 'Actividad', pasos: PASOS_ACTIVIDAD },
  automatizacion: { nombre: 'Automatización', pasos: PASOS_AUTOMATIZACION },
  integraciones: { nombre: 'Odoo', pasos: PASOS_INTEGRACIONES },
  categorias: { nombre: 'Categorías', pasos: PASOS_CATEGORIAS },
  atributos: { nombre: 'Atributos', pasos: PASOS_ATRIBUTOS },
  canales: { nombre: 'Canales', pasos: PASOS_CANALES },
  usuarios: { nombre: 'Usuarios', pasos: PASOS_USUARIOS },
  avisos: { nombre: 'Avisos', pasos: PASOS_AVISOS },
}
