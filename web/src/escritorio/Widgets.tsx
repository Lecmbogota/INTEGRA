import { useEffect, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { motivo, num } from '../api'
import { Prioridad } from '../Prioridad'
import { CELDA, type AppId, type Datos, type Disposicion, type Sistema, type WidgetInstancia, type WidgetTipo } from './tipos'
import { APPS } from './apps'
import { usePreferencias } from './preferencias'
import { useSistema } from './sistema'

// Los widgets del escritorio: qué tipos hay, cómo se pintan, sus tamaños y
// la galería para añadirlos. Aquí también vive la aritmética de la
// cuadrícula invisible (celdas, huecos libres, solapes) porque la usan el
// escritorio al arrastrar y la galería al colocar uno nuevo.

// ------------------------------------------------------------ cuadrícula

export type Rect = { x: number; y: number; w: number; h: number }

// Margen del lienzo respecto al borde de la pantalla, en px.
export const MARGEN = 16
// Lado de un icono del escritorio, en celdas (96 px).
export const ICONO = 4

// Cuántas celdas caben en el área de trabajo (la pantalla menos la barra).
export function dimensionesLienzo(area: { w: number; h: number }): { cols: number; filas: number } {
  return {
    cols: Math.max(ICONO, Math.floor((area.w - MARGEN * 2) / CELDA)),
    filas: Math.max(ICONO, Math.floor((area.h - MARGEN * 2) / CELDA)),
  }
}

function seSolapan(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

export function libre(r: Rect, ocupados: Rect[]): boolean {
  return !ocupados.some((o) => seSolapan(r, o))
}

// Mete un rectángulo dentro del lienzo sin cambiar su tamaño (salvo que sea
// más grande que el lienzo entero, caso de una pantalla pequeña).
export function encajar(r: Rect, cols: number, filas: number): Rect {
  const w = Math.min(r.w, cols)
  const h = Math.min(r.h, filas)
  return {
    x: Math.max(0, Math.min(r.x, cols - w)),
    y: Math.max(0, Math.min(r.y, filas - h)),
    w, h,
  }
}

// Primer hueco libre recorriendo columnas de izquierda a derecha (dentro de
// cada columna, de arriba abajo), como coloca un escritorio de verdad.
// `paso` es el salto entre candidatos: ICONO para que los iconos queden en
// columnas limpias, 1 para los widgets.
export function primerHueco(w: number, h: number, ocupados: Rect[], cols: number, filas: number, paso = 1): Rect | null {
  for (let x = 0; x + w <= cols; x += paso) {
    for (let y = 0; y + h <= filas; y += paso) {
      const r = { x, y, w, h }
      if (libre(r, ocupados)) return r
    }
  }
  return null
}

// Hueco libre más cercano a donde se soltó algo. Si el sitio está libre es
// ese mismo; si no, se mira cada posición del lienzo y se elige la libre a
// menor distancia (el lienzo tiene unas 3.000 celdas: es instantáneo).
export function huecoMasCercano(deseado: Rect, ocupados: Rect[], cols: number, filas: number): Rect {
  const r = encajar(deseado, cols, filas)
  if (libre(r, ocupados)) return r
  let mejor: Rect | null = null
  let distancia = Infinity
  for (let x = 0; x + r.w <= cols; x++) {
    for (let y = 0; y + r.h <= filas; y++) {
      const d = (x - r.x) ** 2 + (y - r.y) ** 2
      if (d >= distancia) continue
      const c = { x, y, w: r.w, h: r.h }
      if (libre(c, ocupados)) { mejor = c; distancia = d }
    }
  }
  // Sin sitio libre en todo el lienzo: se queda donde cayó, encima de lo que
  // haya. Mejor eso que perderlo.
  return mejor ?? r
}

// Crea una instancia nueva de un tipo en el primer hueco libre del lienzo
// (o, si está lleno, arriba a la izquierda) y la añade a la disposición.
export function anadirWidget(tipo: WidgetTipo, disp: Disposicion, cols: number, filas: number, config?: Record<string, unknown>): Disposicion {
  const def = WIDGETS[tipo]
  const ocupados: Rect[] = [
    ...disp.widgets.map((w) => encajar(w, cols, filas)),
    ...Object.values(disp.iconos).filter((p): p is { x: number; y: number } => !!p).map((p) => ({ ...p, w: ICONO, h: ICONO })),
  ]
  const sitio = primerHueco(def.inicial.w, def.inicial.h, ocupados, cols, filas)
    ?? encajar({ x: 0, y: 0, ...def.inicial }, cols, filas)
  const inst: WidgetInstancia = {
    id: `${tipo}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`,
    tipo, ...sitio,
    ...(config ? { config } : {}),
  }
  return { ...disp, widgets: [...disp.widgets, inst] }
}

// ------------------------------------------------------------ utilidades

// «hace 5 min», «hace 2 h», «ayer»… para la última sincronización. Lo
// reexporta Escritorio.tsx (de donde lo importan la barra y el inicio).
export function hace(iso: string | null | undefined): string {
  if (!iso) return 'nunca'
  const ms = Date.now() - new Date(iso).getTime()
  if (!Number.isFinite(ms)) return 'nunca'
  const min = Math.round(ms / 60000)
  if (min < 1) return 'ahora mismo'
  if (min < 60) return `hace ${min} min`
  const h = Math.round(min / 60)
  if (h < 24) return `hace ${h} h`
  const d = Math.round(h / 24)
  return d === 1 ? 'ayer' : `hace ${d} días`
}

// ------------------------------------------------------------ contexto

// Lo que un widget necesita del exterior. Los datos vienen de useDatos y las
// acciones abren apps; `esAdmin` filtra las apps que puede enseñar `atajos`.
export type ContextoWidget = {
  datos: Datos
  abrir: Sistema['abrir']
  movil: boolean
  esAdmin: boolean
}

export type Tamano = { w: number; h: number }
export type TamanoRapido = 'pequeno' | 'mediano' | 'grande' | 'ancho'
export const TAMANOS_RAPIDOS: { id: TamanoRapido; nombre: string }[] = [
  { id: 'pequeno', nombre: 'Pequeño' },
  { id: 'mediano', nombre: 'Mediano' },
  { id: 'grande', nombre: 'Grande' },
  { id: 'ancho', nombre: 'Ancho' },
]

export type DefWidget = {
  nombre: string
  descripcion: string
  // Todo en celdas.
  minimo: Tamano
  inicial: Tamano
  tamanos: Record<TamanoRapido, Tamano>
  render: (inst: WidgetInstancia, ctx: ContextoWidget) => ReactNode
  // Formulario de ajustes propio del tipo (solo `cifra` y `atajos`). Recibe
  // la instancia y una función para guardar su `config`.
  ajustes?: (inst: WidgetInstancia, cambiar: (config: Record<string, unknown>) => void, ctx: ContextoWidget) => ReactNode
  // Título que enseña la cabecera (puede depender de la config: la cifra).
  titulo?: (inst: WidgetInstancia) => string
}

// ---------------------------------------------------------------- cifras

export type ClaveCifra =
  | 'productos' | 'marcas' | 'con_precio' | 'con_stock' | 'publicables'
  | 'en_atencion' | 'excluidos' | 'pedidos_recibidos' | 'tareas'

// Las siete tarjetas del Panel más dos del sistema. `tono` colorea el valor
// como en el Panel: publicables en verde si hay, atención en rojo.
export const CIFRAS: Record<ClaveCifra, {
  nombre: string
  app: AppId
  valor: (d: Datos) => number | null
  pie: (d: Datos) => string
  tono?: (n: number) => 'ok' | 'error' | undefined
}> = {
  productos: { nombre: 'Productos', app: 'catalogo', valor: (d) => d.resumen?.productos ?? null, pie: (d) => `${num(d.resumen?.variantes ?? 0)} variantes` },
  marcas: { nombre: 'Marcas', app: 'catalogo', valor: (d) => d.resumen?.marcas ?? null, pie: () => 'normalizadas' },
  con_precio: { nombre: 'Con precio', app: 'catalogo', valor: (d) => d.resumen?.con_precio ?? null, pie: (d) => `de ${num(d.resumen?.variantes ?? 0)}` },
  con_stock: { nombre: 'Con existencias', app: 'catalogo', valor: (d) => d.resumen?.con_stock ?? null, pie: (d) => `${num(d.resumen?.stock_total ?? 0)} unidades` },
  publicables: { nombre: 'Publicables hoy', app: 'publicacion', valor: (d) => d.resumen?.publicables ?? null, pie: () => 'cumplen todos los requisitos', tono: (n) => (n > 0 ? 'ok' : 'error') },
  en_atencion: { nombre: 'Requieren atención', app: 'catalogo', valor: (d) => d.resumen?.en_atencion ?? null, pie: () => 'avisos abiertos', tono: () => 'error' },
  // Los excluidos se enseñan para que se vea que existen y no parezca que
  // Integra perdió productos por el camino.
  excluidos: { nombre: 'Fuera del catálogo', app: 'catalogo', valor: (d) => d.resumen?.excluidos ?? null, pie: () => 'gastos, activos fijos, servicios' },
  pedidos_recibidos: { nombre: 'Pedidos recibidos', app: 'pedidos', valor: (d) => d.pedidos?.recibidos ?? null, pie: (d) => `${num(d.pedidos?.fallidos ?? 0)} fallidos` },
  tareas: { nombre: 'Tareas en marcha', app: 'actividad', valor: (d) => d.tareasActivas, pie: (d) => `sincronización ${hace(d.resumen?.ultima_sincronizacion)}` },
}
const ORDEN_CIFRAS: ClaveCifra[] = ['productos', 'marcas', 'con_precio', 'con_stock', 'publicables', 'en_atencion', 'excluidos']

function claveDe(inst: WidgetInstancia): ClaveCifra {
  const c = inst.config?.clave
  return typeof c === 'string' && c in CIFRAS ? (c as ClaveCifra) : 'publicables'
}

// Una tarjeta de cifra. Es un botón: pulsarla abre la app relacionada.
function TarjetaCifra({ clave, ctx, grande }: { clave: ClaveCifra; ctx: ContextoWidget; grande?: boolean }) {
  const c = CIFRAS[clave]
  const n = c.valor(ctx.datos)
  const tono = n != null ? c.tono?.(n) : undefined
  return (
    <button type="button" className={`widget-tarjeta${grande ? ' grande' : ''}`} onClick={() => ctx.abrir(c.app)}
      title={`Abrir ${APPS[c.app].nombre}`}>
      <span className="widget-etiqueta">{c.nombre}</span>
      <span className={`widget-valor${tono ? ` ${tono}` : ''}`}>{n == null ? '—' : num(n)}</span>
      <span className="widget-pie">{n == null ? 'sin datos todavía' : c.pie(ctx.datos)}</span>
    </button>
  )
}

function WidgetCifras({ ctx }: { ctx: ContextoWidget }) {
  return (
    <div className="widget-tarjetas">
      {ORDEN_CIFRAS.map((k) => <TarjetaCifra key={k} clave={k} ctx={ctx} />)}
    </div>
  )
}

function AjustesCifra({ inst, cambiar }: { inst: WidgetInstancia; cambiar: (c: Record<string, unknown>) => void }) {
  return (
    <label className="widget-ajuste">
      <span>Cifra que enseña</span>
      <select value={claveDe(inst)} onChange={(e) => cambiar({ ...inst.config, clave: e.target.value })}>
        {(Object.keys(CIFRAS) as ClaveCifra[]).map((k) => <option key={k} value={k}>{CIFRAS[k].nombre}</option>)}
      </select>
    </label>
  )
}

// ------------------------------------------------------- barras (Panel)

function Barras({ filas, vacio, onAbrir }: {
  filas: { clave: string; nombre: string; valor: number; tono?: 'aviso' | 'error' }[]
  vacio: string
  onAbrir: () => void
}) {
  const max = Math.max(1, ...filas.map((f) => f.valor))
  if (filas.length === 0) return <div className="widget-vacio">{vacio}</div>
  return (
    <div className="widget-barras" role="button" tabIndex={0} onClick={onAbrir}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onAbrir() } }}>
      {filas.map((f) => (
        <div className="widget-barra" key={f.clave}>
          <span className="widget-barra-nombre" title={f.nombre}>{f.nombre}</span>
          <span className="widget-barra-pista">
            <span className={`widget-barra-relleno${f.tono ? ` ${f.tono}` : ''}`} style={{ width: `${Math.max(0, (f.valor / max) * 100)}%` }} />
          </span>
          <span className="widget-barra-cifra">{num(f.valor)}</span>
        </div>
      ))}
    </div>
  )
}

// ------------------------------------------------------------- pedidos

function WidgetPedidos({ ctx }: { ctx: ContextoWidget }) {
  const p = ctx.datos.pedidos
  const abrir = () => ctx.abrir('pedidos')
  if (!p) return <div className="widget-vacio">Sin datos todavía</div>
  return (
    <div className="widget-tres" role="button" tabIndex={0} onClick={abrir}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); abrir() } }}>
      <div><strong>{num(p.recibidos)}</strong><span>recibidos</span></div>
      <div className={p.fallidos > 0 ? 'mal' : undefined}><strong>{num(p.fallidos)}</strong><span>fallidos</span></div>
      {/* «en Odoo» = ya creados allí (en_odoo en la API). */}
      <div className="bien"><strong>{num(p.en_odoo)}</strong><span>en Odoo</span></div>
    </div>
  )
}

// ----------------------------------------------------------- actividad

function WidgetActividad({ ctx }: { ctx: ContextoWidget }) {
  const n = ctx.datos.tareasActivas
  const abrir = () => ctx.abrir('actividad')
  return (
    <div className="widget-marcha" role="button" tabIndex={0} onClick={abrir}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); abrir() } }}>
      <div className="widget-cifra">
        {n > 0 && <span className="punto-animado" aria-hidden />}
        <strong className={n > 0 ? 'en-marcha' : undefined}>{n}</strong>
        <span>{n === 1 ? 'tarea en marcha' : 'tareas en marcha'}</span>
      </div>
      <div className="widget-nota">Última sincronización: <b>{hace(ctx.datos.resumen?.ultima_sincronizacion)}</b></div>
    </div>
  )
}

// ------------------------------------------------------- sincronización

function WidgetSincronizacion({ ctx }: { ctx: ContextoWidget }) {
  const { sincronizar, sincronizando, resumen } = ctx.datos
  return (
    <div className="widget-sincro">
      <button type="button" className="primario" disabled={sincronizando} onClick={() => void sincronizar()}>
        {sincronizando ? 'Sincronizando…' : 'Sincronizar ahora'}
      </button>
      <div className="widget-nota">Última vez: <b>{hace(resumen?.ultima_sincronizacion)}</b></div>
    </div>
  )
}

// --------------------------------------------------------------- reloj

function WidgetReloj() {
  const [ahora, setAhora] = useState(() => new Date())
  // Cada 30 s basta: el reloj no enseña segundos.
  useEffect(() => {
    const t = setInterval(() => setAhora(new Date()), 30000)
    return () => clearInterval(t)
  }, [])
  const hora = ahora.toLocaleTimeString('es-CO', { hour: '2-digit', minute: '2-digit' })
  const fechaLarga = ahora.toLocaleDateString('es-CO', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
  return (
    <div className="widget-reloj-cuerpo">
      <div className="widget-reloj-hora">{hora}</div>
      <div className="widget-reloj-fecha">{fechaLarga}</div>
    </div>
  )
}

// -------------------------------------------------------------- atajos

const ATAJOS_DEFECTO: AppId[] = ['catalogo', 'mediateca', 'publicacion', 'pedidos']

function appsDe(inst: WidgetInstancia, esAdmin: boolean): AppId[] {
  const c = inst.config?.apps
  const lista = Array.isArray(c) ? (c as AppId[]) : ATAJOS_DEFECTO
  return lista.filter((id) => APPS[id]?.unica && (!APPS[id].soloAdmin || esAdmin))
}

function WidgetAtajos({ inst, ctx }: { inst: WidgetInstancia; ctx: ContextoWidget }) {
  const apps = appsDe(inst, ctx.esAdmin)
  if (apps.length === 0) return <div className="widget-vacio">Elige apps en los ajustes (…)</div>
  return (
    <div className="widget-atajos-lista">
      {apps.map((id) => (
        <button key={id} type="button" className="widget-atajo" title={APPS[id].descripcion} onClick={() => ctx.abrir(id)}>
          <span className="widget-atajo-icono" style={{ background: APPS[id].color }} aria-hidden>{APPS[id].icono}</span>
          <span className="widget-atajo-nombre">{APPS[id].nombre}</span>
        </button>
      ))}
    </div>
  )
}

function AjustesAtajos({ inst, cambiar, ctx }: { inst: WidgetInstancia; cambiar: (c: Record<string, unknown>) => void; ctx: ContextoWidget }) {
  const actuales = appsDe(inst, ctx.esAdmin)
  // Solo apps de una instancia: un diálogo necesita props para abrirse.
  const candidatas = (Object.keys(APPS) as AppId[]).filter((id) => APPS[id].unica && (!APPS[id].soloAdmin || ctx.esAdmin))
  const alternar = (id: AppId) => {
    const s = new Set(actuales)
    if (s.has(id)) s.delete(id); else s.add(id)
    cambiar({ ...inst.config, apps: candidatas.filter((c) => s.has(c)) })
  }
  return (
    <div className="widget-ajuste-lista">
      {candidatas.map((id) => (
        <label key={id}>
          <input type="checkbox" checked={actuales.includes(id)} onChange={() => alternar(id)} />
          <span className="widget-atajo-icono mini" style={{ background: APPS[id].color }} aria-hidden>{APPS[id].icono}</span>
          {APPS[id].nombre}
        </label>
      ))}
    </div>
  )
}

// ------------------------------------------------------------ catálogo

export const WIDGETS: Record<WidgetTipo, DefWidget> = {
  cifras: {
    nombre: 'Cifras',
    descripcion: 'Las siete tarjetas del resumen: productos, marcas, precio, existencias, publicables, atención y excluidos.',
    minimo: { w: 14, h: 6 }, inicial: { w: 29, h: 7 },
    tamanos: { pequeno: { w: 14, h: 12 }, mediano: { w: 29, h: 7 }, grande: { w: 36, h: 10 }, ancho: { w: 46, h: 5 } },
    render: (_i, ctx) => <WidgetCifras ctx={ctx} />,
  },
  cifra: {
    nombre: 'Una cifra',
    descripcion: 'Un solo número grande, a elegir. Pulsarlo abre la app relacionada.',
    minimo: { w: 6, h: 4 }, inicial: { w: 8, h: 5 },
    tamanos: { pequeno: { w: 6, h: 4 }, mediano: { w: 8, h: 5 }, grande: { w: 12, h: 8 }, ancho: { w: 16, h: 5 } },
    render: (inst, ctx) => <TarjetaCifra clave={claveDe(inst)} ctx={ctx} grande />,
    ajustes: (inst, cambiar) => <AjustesCifra inst={inst} cambiar={cambiar} />,
    titulo: (inst) => CIFRAS[claveDe(inst)].nombre,
  },
  bloqueos: {
    nombre: 'Qué bloquea la publicación',
    descripcion: 'Motivos por los que hay productos sin publicar, de mayor a menor.',
    minimo: { w: 12, h: 6 }, inicial: { w: 15, h: 12 },
    tamanos: { pequeno: { w: 12, h: 8 }, mediano: { w: 15, h: 12 }, grande: { w: 20, h: 16 }, ancho: { w: 30, h: 8 } },
    render: (_i, ctx) => (
      <Barras vacio="Nada pendiente." onAbrir={() => ctx.abrir('catalogo')}
        filas={ctx.datos.atencion.map((a) => ({
          clave: a.motivo, nombre: motivo(a.motivo), valor: a.cantidad,
          tono: a.severidad === 'blocking' ? 'error' : 'aviso',
        }))} />
    ),
  },
  bodegas: {
    nombre: 'Existencias por bodega',
    descripcion: 'Unidades en cada bodega de Odoo.',
    minimo: { w: 12, h: 5 }, inicial: { w: 16, h: 6 },
    tamanos: { pequeno: { w: 12, h: 6 }, mediano: { w: 16, h: 6 }, grande: { w: 20, h: 12 }, ancho: { w: 30, h: 6 } },
    render: (_i, ctx) => (
      <Barras vacio="Sin datos de stock." onAbrir={() => ctx.abrir('catalogo')}
        filas={ctx.datos.stock.map((s) => ({ clave: s.codigo, nombre: s.nombre, valor: s.unidades }))} />
    ),
  },
  prioridad: {
    nombre: 'Qué publicar primero',
    descripcion: 'Los productos listos ordenados por valor de inventario; una fila abre su vista previa.',
    minimo: { w: 20, h: 10 }, inicial: { w: 46, h: 20 },
    tamanos: { pequeno: { w: 20, h: 12 }, mediano: { w: 30, h: 16 }, grande: { w: 46, h: 20 }, ancho: { w: 60, h: 14 } },
    render: (_i, ctx) => (
      <div className="widget-prioridad">
        <Prioridad onVer={(id) => ctx.abrir('preview', { varianteId: id })} />
      </div>
    ),
  },
  pedidos: {
    nombre: 'Pedidos',
    descripcion: 'Recibidos, fallidos y ya creados en Odoo.',
    minimo: { w: 10, h: 5 }, inicial: { w: 13, h: 6 },
    tamanos: { pequeno: { w: 10, h: 5 }, mediano: { w: 13, h: 6 }, grande: { w: 16, h: 9 }, ancho: { w: 24, h: 5 } },
    render: (_i, ctx) => <WidgetPedidos ctx={ctx} />,
  },
  actividad: {
    nombre: 'Actividad',
    descripcion: 'Tareas en marcha y cuándo fue la última sincronización.',
    minimo: { w: 10, h: 4 }, inicial: { w: 13, h: 5 },
    tamanos: { pequeno: { w: 10, h: 4 }, mediano: { w: 13, h: 5 }, grande: { w: 16, h: 8 }, ancho: { w: 24, h: 4 } },
    render: (_i, ctx) => <WidgetActividad ctx={ctx} />,
  },
  sincronizacion: {
    nombre: 'Sincronización',
    descripcion: 'El botón «Sincronizar ahora» a mano, con la última vez.',
    minimo: { w: 10, h: 4 }, inicial: { w: 16, h: 5 },
    tamanos: { pequeno: { w: 10, h: 4 }, mediano: { w: 16, h: 5 }, grande: { w: 16, h: 8 }, ancho: { w: 24, h: 4 } },
    render: (_i, ctx) => <WidgetSincronizacion ctx={ctx} />,
  },
  reloj: {
    nombre: 'Reloj',
    descripcion: 'La hora en grande y la fecha completa.',
    minimo: { w: 10, h: 5 }, inicial: { w: 16, h: 7 },
    tamanos: { pequeno: { w: 10, h: 5 }, mediano: { w: 16, h: 7 }, grande: { w: 20, h: 10 }, ancho: { w: 28, h: 6 } },
    render: () => <WidgetReloj />,
  },
  atajos: {
    nombre: 'Atajos',
    descripcion: 'Accesos rápidos a las apps que elijas.',
    minimo: { w: 8, h: 4 }, inicial: { w: 16, h: 5 },
    tamanos: { pequeno: { w: 8, h: 8 }, mediano: { w: 16, h: 5 }, grande: { w: 16, h: 9 }, ancho: { w: 24, h: 4 } },
    render: (inst, ctx) => <WidgetAtajos inst={inst} ctx={ctx} />,
    ajustes: (inst, cambiar, ctx) => <AjustesAtajos inst={inst} cambiar={cambiar} ctx={ctx} />,
  },
}

export const TIPOS_WIDGET = Object.keys(WIDGETS) as WidgetTipo[]

// Título de la cabecera de una instancia.
export function tituloWidget(inst: WidgetInstancia): string {
  const def = WIDGETS[inst.tipo]
  return def.titulo?.(inst) ?? def.nombre
}

// ------------------------------------------------------------- galería

// Miniaturas esquemáticas: formas en `currentColor` que sugieren cada tipo
// sin datos. Van en SVG de 64×40 para que escalen sin pesar nada.
function Miniatura({ tipo }: { tipo: WidgetTipo }) {
  const r = (x: number, y: number, w: number, h: number, o = 1) => <rect x={x} y={y} width={w} height={h} rx="2" opacity={o} />
  let cuerpo: ReactNode
  switch (tipo) {
    case 'cifras': cuerpo = <>{r(4, 4, 17, 14)}{r(24, 4, 17, 14)}{r(44, 4, 16, 14)}{r(4, 22, 17, 14)}{r(24, 22, 17, 14)}{r(44, 22, 16, 14)}</>; break
    case 'cifra': cuerpo = <>{r(6, 6, 20, 4, .5)}<text x="6" y="33" fontSize="22" fontWeight="700" fill="currentColor" stroke="none">42</text></>; break
    case 'bloqueos': cuerpo = <>{r(4, 6, 48, 6)}{r(4, 17, 34, 6, .7)}{r(4, 28, 20, 6, .45)}</>; break
    case 'bodegas': cuerpo = <>{r(4, 6, 40, 6)}{r(4, 17, 52, 6, .7)}{r(4, 28, 28, 6, .45)}</>; break
    case 'prioridad': cuerpo = <>{[0, 1, 2].map((i) => <g key={i}><circle cx="9" cy={9 + i * 12} r="4" />{r(18, 6 + i * 12, 40, 6, .6)}</g>)}</>; break
    case 'pedidos': cuerpo = <>{[0, 1, 2].map((i) => <g key={i}>{r(6 + i * 19, 8, 14, 14)}{r(8 + i * 19, 27, 10, 4, .5)}</g>)}</>; break
    case 'actividad': cuerpo = <><circle cx="12" cy="16" r="6" />{r(24, 12, 30, 8, .7)}{r(6, 30, 50, 4, .4)}</>; break
    case 'sincronizacion': cuerpo = <><rect x="8" y="10" width="48" height="14" rx="7" />{r(14, 30, 36, 4, .4)}</>; break
    case 'reloj': cuerpo = <><text x="32" y="26" fontSize="18" fontWeight="700" textAnchor="middle" fill="currentColor" stroke="none">12:30</text>{r(16, 31, 32, 4, .4)}</>; break
    case 'atajos': cuerpo = <>{[0, 1, 2, 3].map((i) => r(5 + i * 14, 12, 11, 11))}{[0, 1, 2, 3].map((i) => r(6 + i * 14, 27, 9, 3, .4))}</>; break
  }
  return <svg viewBox="0 0 64 40" className="galeria-miniatura" aria-hidden fill="currentColor">{cuerpo}</svg>
}

// Panel de cristal con todos los tipos y un botón «Añadir» por cada uno. Lo
// abren el menú contextual del fondo, el botón «+» del escritorio y la
// configuración; por eso va por portal al body y no depende de dónde esté.
// Cada widget se añade en el primer hueco libre con su tamaño inicial; se
// pueden repetir (varias cifras distintas, por ejemplo).
export function GaleriaWidgets({ abierta, onCerrar }: { abierta: boolean; onCerrar: () => void }) {
  const { prefs, poner } = usePreferencias()
  const sistema = useSistema()
  const [anadido, setAnadido] = useState<WidgetTipo | null>(null)

  useEffect(() => {
    if (!abierta) return
    const tecla = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    document.addEventListener('keydown', tecla)
    return () => document.removeEventListener('keydown', tecla)
  }, [abierta, onCerrar])

  if (!abierta) return null
  const { cols, filas } = dimensionesLienzo(sistema.area)
  const anadir = (tipo: WidgetTipo) => {
    poner({ disposicion: anadirWidget(tipo, prefs.disposicion, cols, filas) })
    // Un «Añadido» breve en el botón para que se note que pasó algo aunque
    // la galería tape el widget nuevo.
    setAnadido(tipo)
    setTimeout(() => setAnadido((a) => (a === tipo ? null : a)), 1200)
  }
  const cuantos = (tipo: WidgetTipo) => prefs.disposicion.widgets.filter((w) => w.tipo === tipo).length
  // Lo que ya está en el escritorio no se vuelve a ofrecer: la galería es
  // «qué me falta por poner», no un catálogo. Cada widget se ajusta desde
  // su propio menú «…» una vez colocado.
  const disponibles = TIPOS_WIDGET.filter((tipo) => cuantos(tipo) === 0)

  return createPortal(
    <div className="galeria-fondo" onPointerDown={(e) => { if (e.target === e.currentTarget) onCerrar() }}>
      <div className={`galeria-widgets${sistema.movil ? ' movil' : ''}`} role="dialog" aria-label="Añadir widget">
        <div className="galeria-cabecera">
          <h2>Widgets</h2>
          <button type="button" className="galeria-cerrar" aria-label="Cerrar" onClick={onCerrar}>×</button>
        </div>
        <div className="galeria-lista">
          {disponibles.length === 0 && (
            <div className="galeria-vacia">Todos los widgets están ya en el escritorio. Para volver a colocar uno, quítalo desde su menú «…».</div>
          )}
          {disponibles.map((tipo) => {
            const def = WIDGETS[tipo]
            return (
              <div key={tipo} className="galeria-item">
                <Miniatura tipo={tipo} />
                <div className="galeria-texto">
                  <div className="galeria-nombre">{def.nombre}</div>
                  <div className="galeria-desc">{def.descripcion}</div>
                </div>
                <button type="button" className="galeria-anadir" onClick={() => anadir(tipo)}>
                  {anadido === tipo ? 'Añadido ✓' : 'Añadir'}
                </button>
              </div>
            )
          })}
        </div>
      </div>
    </div>,
    document.body,
  )
}
