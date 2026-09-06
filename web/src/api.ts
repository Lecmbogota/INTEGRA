// Cliente de la API de Integra.

export interface Resumen {
  productos: number
  variantes: number
  marcas: number
  con_sku: number
  con_precio: number
  con_stock: number
  con_descripcion: number
  publicables: number
  en_atencion: number
  ultima_sincronizacion: string | null
  stock_total: number
  excluidos: number
}

export interface Producto {
  id: number
  sku: string
  nombre: string
  marca: string
  categoria: string
  precio: number | null
  precio_sugerido: number | null
  barcode: string
  peso: number
  descripcion: string
  excluido: boolean
  stock: number
  problemas: string[]
  titulos: Record<string, string>
  largo_cm: number
  ancho_cm: number
  alto_cm: number
  condicion: 'nuevo' | 'usado' | 'reacondicionado'
  garantia_meses: number | null
  garantia_tipo: string
  video_url: string
  nota_interna: string
}

// EdicionProducto son los campos propiedad de Integra. Solo se envían las
// claves presentes: {precio: null} borra el precio, omitirlo lo deja como está.
export interface EdicionProducto {
  precio?: number | null
  marca?: string
  descripcion?: string
  barcode?: string
  peso?: number
  excluido?: boolean
  largo_cm?: number
  ancho_cm?: number
  alto_cm?: number
  condicion?: string
  garantia_meses?: number
  garantia_tipo?: string
  video_url?: string
  nota_interna?: string
  titulos?: Record<string, string>
}

export interface PaginaProductos {
  total: number
  limite: number
  offset: number
  items: Producto[]
}

export interface Marca {
  codigo: string
  nombre: string
  cantidad: number
  alias: number
}

export interface Categoria {
  nombre: string
  cantidad: number
}

export interface Atencion {
  motivo: string
  severidad: string
  cantidad: number
}

export interface StockAlmacen {
  codigo: string
  nombre: string
  unidades: number
  variantes: number
}

export interface Faltante {
  campo: string
  motivo: string
  severidad: 'bloquea' | 'advierte'
}

export interface Proyeccion {
  canal: string
  metodo: string
  endpoint: string
  payload: Record<string, unknown>
  titulo: string
  titulo_limite: number
  precio: number
  precio_tachado: number
  stock: number
  faltantes: Faltante[] | null
  notas: string[] | null
  imagen?: string
}

export interface PreviewRespuesta {
  producto: {
    variante_id: number
    sku: string
    nombre_odoo: string
    marca: string
    categoria_odoo: string
    confianza: string
    avisos: string[]
  }
  proyecciones: Proyeccion[]
}

export interface Mapeo {
  id: number
  canal: string
  categoria_odoo: string
  categoria_canal_id: string
  categoria_canal_nombre: string
  origen: string
  confianza: number | null
  confirmado: boolean
  productos: number
  valor_inventario: number
  atributos_sugeridos: { id: string; name: string; value_name: string }[] | null
}

export interface OperacionMasiva {
  tipo: 'precio_desde_coste' | 'precio_fijo' | 'precio_ajustar' | 'precio_borrar'
      | 'marca' | 'excluir' | 'incluir'
  factor?: number
  valor?: string
  redondeo?: number
  simular?: boolean
}

export interface ResultadoMasivo {
  afectados: number
  omitidos: number
  motivo_omision: string
  muestra: { sku: string; nombre: string; antes: number | null; despues: number | null }[] | null
  simulado: boolean
}

// Filtros del catálogo, tal como los manda la pantalla de Productos. Se
// reutilizan para acotar la plantilla masiva a lo que se está viendo.
export interface FiltroCatalogo {
  q?: string
  marca?: string
  categoria?: string
  problemas?: boolean
  excluidos?: boolean
  sin_precio?: boolean
}

export interface ProblemaPlantilla {
  fila: number
  sku: string
  mensaje: string
}

export interface CambioPlantilla {
  fila: number
  sku: string
  nombre: string
  precio_antes: number | null
  precio_despues: number | null
  promo_canal: string
  promo_precio: number | null
  promo_inicia: string | null
  promo_termina: string | null
}

export interface ResultadoPlantilla {
  aplicado: boolean
  cambios: CambioPlantilla[]
  problemas: ProblemaPlantilla[]
  filas_leidas: number
  precios: number
  promociones: number
}

export interface Oferta {
  id: number
  variante_id: number
  cuenta_id: number
  canal: string
  precio: number
  inicia: string
  termina: string | null
  activa: boolean
  estado: 'programada' | 'vigente' | 'terminada' | 'cancelada'
  aplicada_at: string | null
}

export interface Horario {
  id: number
  nombre: string
  cuenta_id: number | null
  canal: string
  hora: string
  zona: string
  dias: number[]
  alcance: 'full' | 'price' | 'stock'
  activo: boolean
  ultima_ejecucion: string | null
  proxima_ejecucion: string | null
}

export interface Alerta {
  id: number
  tipo: string
  severidad: 'info' | 'warning' | 'error' | 'critical'
  canal: string
  mensaje: string
  creada_at: string
  vista_at: string | null
}

export interface AtributoProducto {
  attribute_id: string
  nombre: string
  value_id: string
  value_name: string
  origen: '' | 'deducido' | 'predictor' | 'manual' | 'defecto'
  obligatorio: boolean
}

export interface AtributosRespuesta {
  canal: string
  atributos: AtributoProducto[]
  faltantes: string[] | null
}

export interface ResumenAtributos {
  canal: string
  con_categoria: number
  completos: number
  faltan_obligatorios: number
}

export interface LineaOrden {
  id: number
  sku: string
  titulo: string
  cantidad: number
  precio_unitario: number
  total: number
  variante_id: number | null
}

export interface Orden {
  id: number
  cuenta_id: number
  canal: string
  external_id: string
  numero: string
  estado_canal: string
  fecha_pedido: string
  moneda: string
  total: number
  envio: number
  impuesto: number
  comprador: string
  estado: 'received' | 'mapped' | 'created_in_odoo' | 'failed' | 'ignored'
  odoo_pedido_id: number | null
  error: string
  intentos: number
  sincronizada_at: string | null
  lineas: LineaOrden[] | null
}

export interface ResumenOrdenes {
  total: number
  recibidos: number
  en_odoo: number
  fallidos: number
  lineas_sin_mapear: number
  monto_hoy: number
}

export interface ResumenPublicacion {
  canal: string
  cuenta_id: number
  activas: number
  con_error: number
  pendientes_cola: number
  ultima_publicacion: string | null
}

export interface CuentaCanal {
  id: number
  canal: string
  canal_nombre: string
  nombre: string
  activa: boolean
  ultimo_sync: string | null
  probada_at: string | null
  probada_ok: boolean | null
  probada_msg: string
  // Bodegas asignadas. Con cero la cuenta no publica nada.
  bodegas: number
}

// BodegaCuenta es una bodega de Odoo vista desde una cuenta: si la alimenta
// o no, y cuánto stock tiene para saber qué se está eligiendo.
export interface BodegaCuenta {
  id: number
  odoo_id: number
  codigo: string
  nombre: string
  conexion: string
  unidades: number
  asignada: boolean
}


// ---------------------------------------------------------------- usuarios

export interface UsuarioCuenta {
  id: number
  email: string
  name: string
  role: 'admin' | 'operator' | 'viewer'
  active: boolean
  last_login_at: string | null
  created_at: string
}

export interface RegistroAuditoria {
  id: number
  usuario: string
  accion: string
  entidad: string
  entidad_id: string
  ip: string
  creado_at: string
  before?: unknown
  after?: unknown
}

// --------------------------------------------------------------- mediateca

export interface ImagenBanco {
  id: number
  sha256: string
  ancho: number
  alto: number
  bytes: number
  formato: string
  // Productos a los que está vinculada. Vacío = huérfana: ocupa disco y no
  // la publica nadie.
  productos: { variante_id: number; sku: string; nombre: string; principal: boolean }[]
  verificacion: '' | 'ok' | 'dudosa' | 'sin_verificar'
}

export interface PaginaImagenes {
  total: number
  limite: number
  offset: number
  items: ImagenBanco[]
  // Los tres números que mueven decisiones, calculados sobre TODO el banco y
  // no sobre la página: cuántos productos no pueden publicarse por falta de
  // foto, cuántas fotos son demasiado pequeñas para los canales, y cuánto
  // disco ocupa lo que no pertenece a ningún producto.
  productos_sin_foto: number
  pequenas: number
  bytes_huerfanos: number
}

export type FiltroMediateca = 'todas' | 'aptas' | 'pequenas' | 'huerfanas' | 'duplicadas' | 'dudosas'

// ----------------------------------------------------- destinos de avisos

export interface DestinoAviso {
  id: number
  tipo: 'email'
  nombre: string
  min_severidad: 'info' | 'warning' | 'error' | 'critical'
  activo: boolean
  ultimo_envio: string | null
  ultimo_error: string
  // La configuración nunca vuelve del servidor: lleva la contraseña SMTP.
  // Solo se manda al guardar.
  destinatarios: string[]
}

export interface ConfigSMTP {
  host: string
  puerto: number
  usuario: string
  password: string
  remitente: string
  destinatarios: string[]
  sin_tls?: boolean
}

// Los campos usados dependen del canal; el resto se omite.
export interface CredencialesCanal {
  tienda?: string
  token?: string
  url?: string
  consumer_key?: string
  consumer_secret?: string
  user_id?: string
  api_key?: string
  app_id?: string
  app_secret?: string
  access_token?: string
  refresh_token?: string
  // Valida que un aviso entrante venga de verdad del canal. Sin guardarlo, el
  // endpoint de webhooks rechaza todos los avisos legitimos.
  webhook_secret?: string
  url_seguimiento?: string
}

export interface Canal {
  codigo: string
  nombre: string
  comision_pct: number
  costo_fijo: number
}

export interface ConexionOdoo {
  id: number
  nombre: string
  base_url: string
  database: string
  usuario: string
  timezone: string
  activa: boolean
  ultima_sync: string | null
  productos: number
  con_precio: number
  con_imagen: number
}

// La API key viaja una sola vez al guardar (se cifra en el servidor) y nunca
// vuelve a la interfaz. Vacía u omitida en la edición: se conserva la actual.
export interface DatosIntegracion {
  nombre?: string
  url: string
  database: string
  usuario: string
  api_key?: string
  timezone?: string
}

export interface FilaPrioridad {
  variante_id: number
  sku: string
  nombre: string
  marca: string
  precio: number | null
  stock: number
  valor_stock: number
  listo: boolean
  bloqueos: number
}

export interface Competidor {
  titulo: string
  precio: number
  moneda: string
  permalink: string
  vendedor: string
  condicion: string
  envio_gratis: boolean
}

export interface Competencia {
  consulta: string
  precio_propio: number
  items: Competidor[]
}

export interface BusquedaMasiva {
  en_curso: boolean
  total: number
  procesados: number
  productos_con_foto: number
  fotos_agregadas: number
  fallos: number
  ultimo: string
  mensaje: string
}

export interface BusquedaImagenes {
  consulta: string
  encontradas: number
  agregadas: { id: number; sha256: string; ancho: number; alto: number; fuente: string }[]
  descartadas: number
}

export interface ImagenProducto {
  id: number
  sha256: string
  formato: string
  ancho: number
  alto: number
  bytes: number
  origen: string
  posicion: number
  principal: boolean
  verificacion: '' | 'corresponde' | 'dudosa' | 'no_corresponde'
  verificacion_nota: string
  url: string
  url_publicable: string
  publicable: Record<string, boolean> | null
  problemas: Record<string, { canal: string; motivo: string; bloquea: boolean }[]> | null
}

// alCaducarSesion lo instala App para reaccionar cuando el servidor rechaza
// el token: caducó o lo revocaron, y hay que volver a la pantalla de entrada.
let alSalirSesion: (() => void) | null = null
export function alCaducarSesion(fn: () => void) { alSalirSesion = fn }

function tokenGuardado(): string {
  try {
    const crudo = localStorage.getItem('integra_sesion')
    return crudo ? (JSON.parse(crudo).token as string) : ''
  } catch {
    return ''
  }
}

async function pedir<T>(ruta: string, init?: RequestInit): Promise<T> {
  // El token se lee en cada llamada y no se cachea, para que cerrar sesión
  // surta efecto de inmediato.
  const token = tokenGuardado()
  const cabeceras = new Headers(init?.headers)
  if (token) cabeceras.set('Authorization', `Bearer ${token}`)

  const r = await fetch(ruta, { ...init, headers: cabeceras })
  if (!r.ok) {
    if (r.status === 401) {
      // Sesión inválida: se avisa a la aplicación para que muestre el login
      // en vez de llenar la pantalla de errores sin explicación.
      alSalirSesion?.()
    }
    // El backend responde con {error: "..."}; si no, se usa el estado HTTP.
    let detalle = `HTTP ${r.status}`
    try {
      const cuerpo = await r.json()
      if (cuerpo?.error) detalle = cuerpo.error
    } catch {
      /* respuesta sin JSON: se queda el estado */
    }
    throw new Error(detalle)
  }
  return r.json() as Promise<T>
}

export const api = {
  resumen: () => pedir<Resumen>('/api/resumen'),
  marcas: () => pedir<Marca[]>('/api/marcas'),
  categorias: () => pedir<Categoria[]>('/api/categorias'),
  atencion: () => pedir<Atencion[]>('/api/atencion'),
  stock: () => pedir<StockAlmacen[]>('/api/stock'),

  productos: (p: { q?: string; marca?: string; categoria?: string; problemas?: boolean; excluidos?: boolean; sin_precio?: boolean; limite?: number; offset?: number }) => {
    const qs = new URLSearchParams()
    if (p.q) qs.set('q', p.q)
    if (p.marca) qs.set('marca', p.marca)
    if (p.categoria) qs.set('categoria', p.categoria)
    if (p.problemas) qs.set('problemas', '1')
    if (p.excluidos) qs.set('excluidos', '1')
    if (p.sin_precio) qs.set('sin_precio', '1')
    qs.set('limite', String(p.limite ?? 50))
    qs.set('offset', String(p.offset ?? 0))
    return pedir<PaginaProductos>(`/api/productos?${qs}`)
  },

  // Cierra la sesión en el servidor. Sin esto, la cookie que dejó el login
  // sigue siendo una credencial válida hasta 24 h después.
  cerrarSesion: () => pedir<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),

  // ---- usuarios y auditoría (solo administradores) ----
  usuarios: () => pedir<UsuarioCuenta[]>('/api/usuarios'),

  crearUsuario: (u: { email: string; name: string; password: string; role: string }) =>
    pedir<{ ok: boolean; id: number }>('/api/usuarios', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(u),
    }),

  editarUsuario: (id: number, campos: { name?: string; role?: string; active?: boolean; password?: string }) =>
    pedir<{ ok: boolean }>(`/api/usuarios/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(campos),
    }),

  auditoria: (p?: { entidad?: string; limite?: number }) => {
    const q = new URLSearchParams()
    if (p?.entidad) q.set('entity', p.entidad)
    if (p?.limite) q.set('limite', String(p.limite))
    return pedir<{ items: RegistroAuditoria[] }>(`/api/auditoria?${q}`)
  },

  // ---- mediateca ----
  banco: (p?: { filtro?: FiltroMediateca; q?: string; limite?: number; offset?: number }) => {
    const q = new URLSearchParams()
    if (p?.filtro && p.filtro !== 'todas') q.set('filtro', p.filtro)
    if (p?.q) q.set('q', p.q)
    q.set('limite', String(p?.limite ?? 60))
    q.set('offset', String(p?.offset ?? 0))
    return pedir<PaginaImagenes>(`/api/imagenes?${q}`)
  },

  borrarDelBanco: (id: number) =>
    pedir<{ estado: string }>(`/api/imagenes/${id}`, { method: 'DELETE' }),

  // ---- destinos de avisos ----
  destinosAviso: () => pedir<DestinoAviso[]>('/api/avisos/destinos'),

  guardarDestinoAviso: (d: { id?: number; nombre: string; min_severidad: string; activo: boolean; config: ConfigSMTP }) =>
    pedir<{ ok: boolean; id: number }>('/api/avisos/destinos', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(d),
    }),

  borrarDestinoAviso: (id: number) =>
    pedir<{ estado: string }>(`/api/avisos/destinos/${id}`, { method: 'DELETE' }),

  // Manda un correo de prueba: una configuración SMTP que nadie ha probado no
  // se descubre rota hasta el día que hay una alerta de verdad.
  probarDestinoAviso: (id: number) =>
    pedir<{ ok: boolean; mensaje: string }>(`/api/avisos/destinos/${id}/probar`, { method: 'POST' }),

  // ---- despacho ----
  despacharPedido: (ordenID: number, envio: { guia: string; transportadora: string }) =>
    pedir<{ estado: string; aviso?: string }>(`/api/ordenes/${ordenID}/despachar`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(envio),
    }),

  sincronizar: () => pedir<{ estado: string }>('/api/sincronizar', { method: 'POST' }),

  editarProducto: (varianteId: number, campos: EdicionProducto) =>
    pedir<{ estado: string }>(`/api/productos/${varianteId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(campos),
    }),

  preview: (varianteId: number) => pedir<PreviewRespuesta>(`/api/preview/${varianteId}`),

  imagenes: (varianteId: number) =>
    pedir<ImagenProducto[]>(`/api/productos/${varianteId}/imagenes`),

  // La subida va como multipart: no se pone Content-Type a mano porque el
  // navegador tiene que añadir el boundary.
  subirImagen: (varianteId: number, archivo: File) => {
    const fd = new FormData()
    fd.append('archivo', archivo)
    return pedir<{ id: number }>(`/api/productos/${varianteId}/imagenes`, {
      method: 'POST', body: fd,
    })
  },

  // Busca fotos en internet por SKU y descarga varias candidatas al banco.
  // Puede tardar: hay una búsqueda y varias descargas del lado del servidor.
  buscarImagenes: (varianteId: number, max = 5) =>
    pedir<BusquedaImagenes>(`/api/productos/${varianteId}/imagenes/buscar`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ max }),
    }),

  // Lanza el barrido de imágenes para todos los productos publicables sin
  // foto. Corre en el servidor; el progreso se consulta con estadoBusquedaMasiva.
  buscarImagenesMasivo: () =>
    pedir<{ estado: string; productos?: number }>('/api/imagenes/buscar-masivo', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    }),

  estadoBusquedaMasiva: () => pedir<BusquedaMasiva>('/api/imagenes/buscar-masivo'),

  quitarImagen: (varianteId: number, imagenId: number) =>
    pedir<{ estado: string }>(`/api/productos/${varianteId}/imagenes/${imagenId}`, {
      method: 'DELETE',
    }),

  imagenPrincipal: (varianteId: number, imagenId: number) =>
    pedir<{ estado: string }>(`/api/productos/${varianteId}/imagenes/${imagenId}/principal`, {
      method: 'POST',
    }),

  editarMasivo: (
    seleccion: { ids?: number[]; filtro?: Record<string, unknown> },
    operacion: OperacionMasiva,
  ) =>
    pedir<ResultadoMasivo>('/api/productos/masivo', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...seleccion, operacion }),
    }),

  // La descarga no puede pasar por `pedir`: el cuerpo es un .xlsx binario, y
  // el token va en una cabecera, así que tampoco vale abrir la URL sin más.
  descargarPlantilla: async (filtro: FiltroCatalogo) => {
    // Mismos nombres que /api/productos: la plantilla tiene que traer lo mismo
    // que la lista que el operador está mirando.
    const p = new URLSearchParams()
    if (filtro.q) p.set('q', filtro.q)
    if (filtro.marca) p.set('marca', filtro.marca)
    if (filtro.categoria) p.set('categoria', filtro.categoria)
    if (filtro.problemas) p.set('problemas', '1')
    if (filtro.excluidos) p.set('excluidos', '1')
    if (filtro.sin_precio) p.set('sin_precio', '1')

    const cab = new Headers()
    const t = tokenGuardado()
    if (t) cab.set('Authorization', `Bearer ${t}`)

    const r = await fetch(`/api/plantillas/precios?${p}`, { headers: cab })
    if (!r.ok) {
      if (r.status === 401) alSalirSesion?.()
      throw new Error(`no se pudo generar la plantilla (HTTP ${r.status})`)
    }
    const blob = await r.blob()
    const nombre = nombreDeCabecera(r.headers.get('Content-Disposition'))
      ?? 'integra-precios.xlsx'

    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = nombre
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  },

  cargarPlantilla: (archivo: File, aplicar: boolean) => {
    const datos = new FormData()
    datos.append('archivo', archivo)
    // Sin Content-Type explícito: el navegador tiene que ponerlo él para
    // incluir el `boundary` del multipart, y fijarlo a mano lo rompe.
    return pedir<ResultadoPlantilla>(
      `/api/plantillas/precios?aplicar=${aplicar}`,
      { method: 'POST', body: datos })
  },

  ofertasDe: (varianteId: number) => pedir<Oferta[]>(`/api/variantes/${varianteId}/ofertas`),
  ofertasVigentes: () => pedir<Oferta[]>('/api/ofertas'),
  // Las fechas viajan en ISO 8601 con zona; el servidor las guarda como
  // timestamptz, así que una promoción «desde las 00:00 en Bogotá» se
  // interpreta bien aunque el servidor esté en otro huso.
  crearOferta: (varianteId: number, cuentaId: number, precio: number, inicia: string, termina: string | null) =>
    pedir<{ id: number }>(`/api/variantes/${varianteId}/ofertas`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        channel_account_id: cuentaId,
        offer_price: precio,
        starts_at: inicia,
        ...(termina ? { ends_at: termina } : {}),
      }),
    }),
  cancelarOferta: (id: number) =>
    pedir<{ estado: string }>(`/api/ofertas/${id}`, { method: 'DELETE' }),

  horarios: () => pedir<Horario[]>('/api/horarios'),
  guardarHorario: (h: Partial<Horario>) =>
    pedir<{ estado: string; id: number }>('/api/horarios', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(h),
    }),
  borrarHorario: (id: number) =>
    pedir<{ estado: string }>(`/api/horarios/${id}`, { method: 'DELETE' }),

  alertas: (todas = false) => pedir<Alerta[]>(`/api/alertas${todas ? '?todas=1' : ''}`),
  reconocerAlerta: (id: number) =>
    pedir<{ estado: string }>(`/api/alertas/${id}/vista`, { method: 'POST' }),

  resumenAtributos: (canal = 'mercadolibre') =>
    pedir<ResumenAtributos>(`/api/atributos/resumen?canal=${canal}`),
  atributosDe: (varianteId: number, canal = 'mercadolibre') =>
    pedir<AtributosRespuesta>(`/api/productos/${varianteId}/atributos?canal=${canal}`),
  guardarAtributo: (varianteId: number, attrId: string, value_name: string, value_id = '', canal = 'mercadolibre') =>
    pedir<{ estado: string }>(`/api/productos/${varianteId}/atributos/${attrId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ canal, value_id, value_name }),
    }),

  ordenes: (limite = 50) => pedir<Orden[]>(`/api/ordenes?limite=${limite}`),
  resumenOrdenes: () => pedir<ResumenOrdenes>('/api/ordenes/resumen'),
  ingerirOrdenes: (cuentaId: number) =>
    pedir<{ estado: string }>(`/api/cuentas/${cuentaId}/ingerir-ordenes`, { method: 'POST' }),
  // Devuelve a la cola de montaje un pedido que agotó sus intentos. Cuando no
  // procede (ya está en Odoo, lo canceló el canal, le falta un SKU) el
  // servidor responde 409 con el motivo, que `pedir` convierte en el error.
  reintentarOrden: (ordenId: number) =>
    pedir<{ estado: string; numero: string }>(`/api/ordenes/${ordenId}/reintentar`, { method: 'POST' }),

  publicaciones: () => pedir<ResumenPublicacion[]>('/api/publicaciones'),
  planificar: (cuentaId: number) =>
    pedir<{ publicar: number; precio: number; stock: number; sin_cambios: number; no_listos: number }>(
      `/api/cuentas/${cuentaId}/planificar`, { method: 'POST' }),

  cuentas: () => pedir<CuentaCanal[]>('/api/cuentas'),
  guardarCuenta: (canal: string, nombre: string, credenciales: CredencialesCanal) =>
    pedir<{ estado: string; id: number }>(`/api/cuentas/${canal}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ nombre, credenciales }),
    }),
  probarCuenta: (id: number) =>
    pedir<{ ok: boolean; mensaje: string }>(`/api/cuentas/${id}/probar`, { method: 'POST' }),
  bodegasCuenta: (id: number) => pedir<BodegaCuenta[]>(`/api/cuentas/${id}/bodegas`),
  // Reemplaza la asignación entera: se manda la lista completa de marcadas.
  asignarBodegasCuenta: (id: number, bodegas: number[]) =>
    pedir<{ estado: string }>(`/api/cuentas/${id}/bodegas`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bodegas }),
    }),

  integraciones: () => pedir<ConexionOdoo[]>('/api/integraciones'),
  crearIntegracion: (datos: DatosIntegracion) =>
    pedir<{ estado: string; id: number; uid: number }>('/api/integraciones', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(datos),
    }),
  editarIntegracion: (id: number, datos: Partial<DatosIntegracion>) =>
    pedir<{ estado: string }>(`/api/integraciones/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(datos),
    }),
  probarIntegracion: (id: number) =>
    pedir<{ ok: boolean; mensaje: string }>(`/api/integraciones/${id}/probar`, { method: 'POST' }),
  activarIntegracion: (id: number) =>
    pedir<{ estado: string }>(`/api/integraciones/${id}/activar`, { method: 'POST' }),
  borrarIntegracion: (id: number) =>
    pedir<{ estado: string; productos_borrados: number }>(`/api/integraciones/${id}`, {
      method: 'DELETE',
    }),

  canales: () => pedir<Canal[]>('/api/canales'),
  editarCanal: (codigo: string, comision_pct: number, costo_fijo: number) =>
    pedir<{ estado: string }>(`/api/canales/${codigo}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ comision_pct, costo_fijo }),
    }),

  prioridad: (limite = 10) => pedir<FilaPrioridad[]>(`/api/prioridad?limite=${limite}`),

  competencia: (varianteId: number) =>
    pedir<Competencia>(`/api/productos/${varianteId}/competencia`),

  mapeos: (canal: string) => pedir<Mapeo[]>(`/api/mapeos?canal=${encodeURIComponent(canal)}`),
  confirmarMapeo: (id: number) =>
    pedir<{ estado: string }>(`/api/mapeos/${id}/confirmar`, { method: 'POST' }),
}

// --- formato ---

const cop = new Intl.NumberFormat('es-CO', {
  style: 'currency',
  currency: 'COP',
  maximumFractionDigits: 0,
})

// nombreDeCabecera saca el filename de un Content-Disposition. Si el servidor
// cambia el nombre (lleva la fecha), el archivo descargado lo respeta.
function nombreDeCabecera(cd: string | null): string | null {
  if (!cd) return null
  const m = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(cd)
  return m ? decodeURIComponent(m[1]) : null
}

export const money = (n: number | null) => (n === null ? '—' : cop.format(n))
export const num = (n: number) => new Intl.NumberFormat('es-CO').format(Math.round(n))

export function fecha(iso: string | null): string {
  if (!iso) return 'nunca'
  return new Date(iso).toLocaleString('es-CO', {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

// Los motivos de la cola de atención se guardan en inglés porque son claves
// estables del dominio; aquí se traducen para mostrarlos.
export const MOTIVOS: Record<string, string> = {
  missing_sku: 'Sin referencia interna',
  duplicate_sku: 'Referencia repetida en otro producto',
  missing_description: 'Sin descripción',
  missing_price: 'Sin precio asignado',
  title_too_long: 'Título demasiado largo',
  missing_brand: 'Sin marca',
  no_stock: 'Sin existencias',
  image_mismatch: 'Portada dudosa',
  sku_renombrado: 'SKU renombrado en Odoo',
}

export const motivo = (k: string) => MOTIVOS[k] ?? k
