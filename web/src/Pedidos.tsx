import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, fecha, money, num, type CuentaCanal, type Orden, type ResumenOrdenes } from './api'
import {
  BarraEstado, CabeceraColumnas, SelectorVista,
  useColumnas, useMenuContextual, useSeleccion, useTecladoLista, useVista,
  type Columna, type ColumnaDef, type OpcionMenu,
} from './Vista'

const ESTADOS: Record<string, { texto: string; clase: string }> = {
  received: { texto: 'Recibido', clase: 'aviso' },
  mapped: { texto: 'Mapeado', clase: 'aviso' },
  created_in_odoo: { texto: 'En Odoo', clase: 'ok' },
  failed: { texto: 'Falló', clase: 'bloqueante' },
  ignored: { texto: 'Ignorado', clase: 'dudosa' },
}

// La ingesta la resuelve el worker, no la petición: se refresca al cabo de
// unos segundos porque antes no hay nada nuevo que mostrar.
const ESPERA_INGESTA_MS = 6000

// Columnas de la tabla de detalles (useColumnas): ancho, orden y
// visibilidad se guardan por usuario. Aquí no hay orden del servidor.
const COLUMNAS: ColumnaDef[] = [
  { id: 'pedido', titulo: 'Pedido', ancho: 150 },
  { id: 'canal', titulo: 'Canal', ancho: 120 },
  { id: 'comprador', titulo: 'Comprador', ancho: 220 },
  { id: 'total', titulo: 'Total', ancho: 120, clase: 'num' },
  { id: 'fecha', titulo: 'Fecha', ancho: 150 },
  { id: 'estado', titulo: 'Estado', ancho: 110 },
  { id: 'odoo', titulo: 'Pedido Odoo', ancho: 110, clase: 'num', oculta: true },
  { id: 'acciones', titulo: '', ancho: 130, fija: true, flexible: true },
]
// Funciones estables para los hooks de lista.
const claveDe = (o: Orden) => o.id
const textoDe = (o: Orden) => [o.numero || o.external_id, o.comprador]
// Solo lo que aún no está en Odoo ni canceló el canal se puede reintentar:
// el servidor rechaza el resto, pero no hay por qué ofrecerlo.
const reintentable = (o: Orden) => o.estado === 'failed' || o.estado === 'received' || o.estado === 'mapped'

// Pedidos que llegaron de los canales y su estado camino a Odoo.
export function Pedidos() {
  const [ordenes, setOrdenes] = useState<Orden[]>([])
  const [resumen, setResumen] = useState<ResumenOrdenes | null>(null)
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [trayendo, setTrayendo] = useState(false)
  const [reintentando, setReintentando] = useState<number | null>(null)
  const [refrescando, setRefrescando] = useState(false)
  const [cargado, setCargado] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [nota, setNota] = useState<string | null>(null)
  const [filtro, setFiltro] = useState('')
  const [abierta, setAbierta] = useState<number | null>(null)
  const temporizador = useRef<number | null>(null)
  // Tabla, filas compactas o tarjetas. Sin foto que enseñar, «iconos» no
  // aporta nada aquí y no se ofrece.
  const [vista, setVista] = useVista('pedidos', 'detalles', ['detalles', 'lista', 'mosaico'])
  // Confirmación corta («número copiado») en la barra de estado.
  const [copiado, setCopiado] = useState<string | null>(null)
  useEffect(() => {
    if (copiado === null) return
    const t = window.setTimeout(() => setCopiado(null), 2000)
    return () => window.clearTimeout(t)
  }, [copiado])

  const cargar = useCallback(() => {
    setRefrescando(true)
    return Promise.all([api.ordenes(50), api.resumenOrdenes(), api.cuentas()])
      .then(([o, r, c]) => { setOrdenes(o); setResumen(r); setCuentas(c); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => { setCargado(true); setRefrescando(false) })
  }, [])
  useEffect(() => { void cargar() }, [cargar])

  // Si el operador cambia de pantalla antes de que venza la espera, el
  // refresco diferido no debe seguir vivo.
  useEffect(() => () => {
    if (temporizador.current !== null) window.clearTimeout(temporizador.current)
  }, [])

  async function traer() {
    if (cuentas.length === 0) {
      setError('No hay ninguna cuenta de canal conectada. Ve a «Canales» y conecta al menos una antes de traer pedidos.')
      return
    }
    setTrayendo(true)
    setError(null)
    setNota(null)
    // allSettled y no all: si una cuenta falla hay que decir cuál, no perder
    // de vista las que sí encolaron la ingesta.
    const resultados = await Promise.allSettled(cuentas.map((c) => api.ingerirOrdenes(c.id)))
    const fallidas: string[] = []
    resultados.forEach((r, i) => {
      if (r.status === 'rejected') fallidas.push(cuentas[i].canal_nombre || cuentas[i].nombre)
    })
    const pedidas = cuentas.length - fallidas.length

    if (fallidas.length > 0) {
      setError(`No se pudo pedir los pedidos de ${fallidas.length === 1 ? 'la cuenta' : 'las cuentas'}: ${fallidas.join(', ')}.`)
    }
    if (pedidas === 0) {
      setTrayendo(false)
      return
    }
    setNota(`Ingesta lanzada en ${pedidas} cuenta${pedidas === 1 ? '' : 's'}. La descarga corre en segundo plano: la lista se actualiza sola en unos segundos.`)
    temporizador.current = window.setTimeout(() => {
      temporizador.current = null
      void cargar()
      setTrayendo(false)
    }, ESPERA_INGESTA_MS)
  }

  // Un pedido que falló cinco veces desaparecía de la cola de montaje y no
  // había dónde pulsar: la alerta seguía sonando y el pedido, cobrado, no
  // llegaba a Odoo. El montaje corre en el worker, así que la lista se
  // refresca al cabo de unos segundos, igual que tras traer pedidos.
  async function reintentar(o: Orden) {
    setReintentando(o.id)
    setError(null)
    setNota(null)
    try {
      await api.reintentarOrden(o.id)
      setNota(`El pedido ${o.numero || o.external_id} volvió a la cola de montaje. Se crea en Odoo en segundo plano: la lista se actualiza sola en unos segundos.`)
      temporizador.current = window.setTimeout(() => {
        temporizador.current = null
        void cargar()
      }, ESPERA_INGESTA_MS)
    } catch (e) {
      setError(`No se pudo reintentar el pedido ${o.numero || o.external_id}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setReintentando(null)
    }
  }

  // Despacho: la guía se teclea aquí porque no sale de Odoo (el módulo de
  // transporte no está instalado). Se guarda por pedido para no perderla al
  // desplegar otro.
  const [guias, setGuias] = useState<Record<number, { guia: string; transportadora: string }>>({})
  const [despachando, setDespachando] = useState<number | null>(null)

  async function despachar(o: Orden) {
    const g = guias[o.id] ?? { guia: '', transportadora: '' }
    setDespachando(o.id)
    setError(null)
    setNota(null)
    try {
      const r = await api.despacharPedido(o.id, g)
      setNota(r.aviso ?? `Se avisará a ${o.canal} del despacho del pedido ${o.numero || o.external_id}.`)
      temporizador.current = window.setTimeout(() => {
        temporizador.current = null
        void cargar()
      }, ESPERA_INGESTA_MS)
    } catch (e) {
      setError(`No se pudo despachar el pedido ${o.numero || o.external_id}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setDespachando(null)
    }
  }

  const visibles = useMemo(
    () => filtro === '' ? ordenes : ordenes.filter((o) => o.estado === filtro),
    [ordenes, filtro],
  )

  // Selección de explorador, teclado, menú contextual y columnas: las
  // mismas piezas que Productos (Vista.tsx).
  const seleccion = useSeleccion(visibles, claveDe)
  const menu = useMenuContextual()
  const col = useColumnas('pedidos', COLUMNAS)
  const refLista = useRef<HTMLDivElement>(null)
  const refTabla = useRef<HTMLTableElement>(null)
  const alternarDetalle = (o: Orden) => setAbierta((a) => (a === o.id ? null : o.id))
  const teclado = useTecladoLista(refLista, visibles, {
    clave: claveDe,
    seleccion,
    abrir: alternarDetalle,
    texto: textoDe,
    contextual: (o, e) => menu.abrir(e, opcionesDe(o)),
    copiar: (os) => void copiar(os.map((o) => o.numero || o.external_id).join('\n'), os.length),
    rol: vista === 'detalles' ? 'grid' : 'listbox',
  })
  // Cambiar el filtro de estado limpia la selección: lo marcado dejaría de
  // verse y las acciones sobre ello serían una sorpresa.
  const limpiarSeleccion = seleccion.limpiar
  useEffect(() => { limpiarSeleccion() }, [filtro, limpiarSeleccion])

  async function copiar(texto: string, cuantos: number) {
    try {
      await navigator.clipboard.writeText(texto)
      setCopiado(cuantos === 1 ? 'Número copiado' : `${num(cuantos)} números copiados`)
    } catch {
      setError('El navegador no dejó copiar al portapapeles.')
    }
  }

  function opcionesDe(o: Orden): OpcionMenu[] {
    const ids = seleccion.seleccion.has(o.id) ? seleccion.seleccion : new Set([o.id])
    const elegidos = visibles.filter((x) => ids.has(x.id))
    const varios = elegidos.length > 1
    return [
      { etiqueta: abierta === o.id ? 'Ocultar detalle' : 'Ver detalle', atajo: 'Enter', accion: () => alternarDetalle(o) },
      ...(reintentable(o) ? [{
        etiqueta: 'Reintentar en Odoo',
        deshabilitado: reintentando === o.id,
        accion: () => void reintentar(o),
      }] : []),
      {
        etiqueta: varios ? `Copiar ${num(elegidos.length)} números` : 'Copiar número',
        atajo: 'Ctrl+C',
        separador: true,
        accion: () => void copiar(elegidos.map((x) => x.numero || x.external_id).join('\n'), elegidos.length),
      },
    ]
  }

  // Totales de la barra de estado: de la selección si la hay, si no de lo
  // que se ve con el filtro.
  const totales = useMemo(() => {
    const base = seleccion.seleccion.size > 0 ? visibles.filter((o) => seleccion.seleccion.has(o.id)) : visibles
    return {
      importe: base.reduce((s, o) => s + o.total, 0),
      fallidos: base.filter((o) => o.estado === 'failed').length,
      deSeleccion: seleccion.seleccion.size > 0,
    }
  }, [visibles, seleccion.seleccion])

  const claseFila = (o: Orden) => [
    'clicable',
    teclado.foco === o.id ? 'enfocada' : '',
    seleccion.seleccion.has(o.id) ? 'marcada' : '',
  ].join(' ').trim()
  const TITULO_FILA = 'Doble clic o Enter: ver el detalle · clic derecho: más acciones'

  // Una celda de la tabla según la columna elegida.
  const celda = (c: Columna, o: Orden, desplegada: boolean) => {
    const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
    switch (c.id) {
      case 'pedido': return { clase: 'sku titulo-tarjeta', contenido: o.numero || o.external_id }
      case 'canal': return { etiqueta: 'Canal', contenido: o.canal }
      case 'comprador': return { etiqueta: 'Comprador', contenido: o.comprador || <span className="tenue">—</span> }
      case 'total': return { clase: 'num', etiqueta: 'Total', contenido: <strong>{money(o.total)}</strong> }
      case 'fecha': return { clase: 'tenue', etiqueta: 'Fecha', contenido: fecha(o.fecha_pedido) }
      case 'estado': return { etiqueta: 'Estado', contenido: <span className={`pastilla ${e.clase}`}>{e.texto}</span> }
      case 'odoo': return { clase: 'num', etiqueta: 'Pedido Odoo', contenido: o.odoo_pedido_id ? `#${o.odoo_pedido_id}` : <span className="tenue">—</span> }
      case 'acciones':
        return {
          clase: 'acciones-fila',
          contenido: (
            // La fila entera abre el detalle, pero eso no llega con el
            // teclado en el móvil: el botón es el que sí es accesible.
            <button className="enlace" aria-expanded={desplegada}
              onClick={(ev) => { ev.stopPropagation(); alternarDetalle(o) }}>
              {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
            </button>
          ),
        }
      default: return { contenido: null }
    }
  }

  // El detalle desplegado de un pedido es el mismo en los tres modos: en la
  // tabla va dentro de una fila extra; en lista y mosaico, bajo la fila o la
  // tarjeta. Por eso se pinta desde una sola función.
  const detalleDe = (o: Orden) => {
    const lineas = o.lineas ?? []
    return (
      <>
        {o.error && (
          <div className="aviso-caja">
            {o.error}
            {o.intentos > 0 && ` (${num(o.intentos)} intento${o.intentos === 1 ? '' : 's'})`}
          </div>
        )}
        {o.odoo_pedido_id && (
          <div className="tenue mini-texto">
            Pedido de venta en Odoo: #{o.odoo_pedido_id}
          </div>
        )}
        {(o.envio > 0 || o.impuesto > 0) && (
          <div className="tenue mini-texto">
            Envío {money(o.envio)} · Impuestos {money(o.impuesto)}
          </div>
        )}
        {lineas.length === 0
          ? <div className="tenue mini-texto">Este pedido llegó sin líneas de detalle.</div>
          : (
            <table className="tabla-lineas tabla-tarjetas">
              <thead>
                <tr><th>SKU</th><th>Producto</th><th className="num">Cant.</th><th className="num">Precio</th></tr>
              </thead>
              <tbody>
                {lineas.map((l) => (
                  <tr key={l.id}>
                    <td className="sku ancha">
                      {l.sku}
                      {l.variante_id === null &&
                        <span className="pastilla bloqueante" title="Este SKU no existe en el catálogo de Integra">sin mapear</span>}
                    </td>
                    <td className="apilada" data-etiqueta="Producto">{l.titulo}</td>
                    <td className="num" data-etiqueta="Cant.">{num(l.cantidad)}</td>
                    <td className="num" data-etiqueta="Precio">{money(l.precio_unitario)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        {/* Solo lo que aún no está en Odoo ni canceló el canal:
            el servidor rechaza el resto, pero no hay por qué
            ofrecer un botón que no puede hacer nada. */}
        {reintentable(o) && (
          <div className="grupo-acciones">
            <button className="primario" data-guia="ped-reintentar" onClick={() => void reintentar(o)}
              disabled={reintentando === o.id}>
              {reintentando === o.id ? 'Reintentando…' : 'Reintentar en Odoo'}
            </button>
          </div>
        )}

        {/* Despacho. Solo tiene sentido con el pedido ya en
            Odoo: lo que dispara el aviso al canal es el
            albarán validado allí, no este botón. */}
        {o.estado === 'created_in_odoo' && (
          <div className="bloque-despacho" data-guia="ped-despacho">
            {o.despachado_at ? (
              <div className="fila">
                <span className="pastilla ok">Canal avisado</span>
                <span className="tenue mini-texto">
                  {fecha(o.despachado_at)}
                  {o.guia && ` · guía ${o.guia}`}
                  {o.transportadora && ` · ${o.transportadora}`}
                </span>
              </div>
            ) : (
              <>
                {/* Que el canal no lo sepa no es un detalle:
                    MercadoLibre y Falabella miden el tiempo
                    hasta el despacho y, pasado el plazo,
                    cancelan y bajan la reputación. */}
                <div className="fila">
                  <span className="pastilla aviso">{o.canal} no sabe que salió</span>
                  {o.salida_bodega_at && (
                    <span className="tenue mini-texto">
                      salió de bodega el {fecha(o.salida_bodega_at)}
                    </span>
                  )}
                </div>
                {o.despacho_error && (
                  <div className="mini-texto error" data-guia="ped-despacho-error">
                    Último intento falló: {o.despacho_error}
                  </div>
                )}
                <div className="fila apila-movil" data-guia="ped-guia">
                  <input className="expande" placeholder="Guía (opcional)"
                    aria-label="Número de guía"
                    value={guias[o.id]?.guia ?? o.guia}
                    onChange={(e) => setGuias({
                      ...guias,
                      [o.id]: { guia: e.target.value, transportadora: guias[o.id]?.transportadora ?? o.transportadora },
                    })} />
                  <input className="expande" placeholder="Transportadora (opcional)"
                    aria-label="Transportadora"
                    value={guias[o.id]?.transportadora ?? o.transportadora}
                    onChange={(e) => setGuias({
                      ...guias,
                      [o.id]: { guia: guias[o.id]?.guia ?? o.guia, transportadora: e.target.value },
                    })} />
                  <button className="primario" onClick={() => void despachar(o)}
                    disabled={despachando === o.id}>
                    {despachando === o.id ? 'Avisando…' : 'Avisar del despacho'}
                  </button>
                </div>
                <div className="tenue mini-texto">
                  Sin guía también vale: en Mercado Envíos y en Falabella la
                  logística la pone el canal. El aviso sale en cuanto el albarán
                  esté validado en Odoo.
                </div>
              </>
            )}
          </div>
        )}
      </>
    )
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Pedidos</h1>
          <div className="sub">Lo que se vendió en los canales y su camino hacia Odoo</div>
        </div>
        <div className="acciones-cabecera">
          <button data-guia="ped-actualizar" onClick={() => void cargar()} disabled={refrescando}>
            {refrescando ? 'Actualizando…' : 'Actualizar'}
          </button>
          <button className="primario" data-guia="ped-traer" onClick={() => void traer()} disabled={trayendo}>
            {trayendo ? 'Trayendo…' : 'Traer pedidos nuevos'}
          </button>
        </div>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}
      {nota && <div className="nota-previa">{nota}</div>}

      {resumen && (
        <div className="tarjetas" data-guia="ped-resumen">
          <Tarjeta etiqueta="Pedidos" valor={num(resumen.total)} pie="en total" />
          <Tarjeta etiqueta="Vendido hoy" valor={money(resumen.monto_hoy)} pie="suma del día" />
          <Tarjeta etiqueta="En Odoo" valor={num(resumen.en_odoo)} tono="ok"
            pie="creados como pedido de venta" />
          <Tarjeta etiqueta="Pendientes" valor={num(resumen.recibidos)} pie="sin montar en Odoo" />
          <Tarjeta etiqueta="Fallidos" valor={num(resumen.fallidos)}
            tono={resumen.fallidos > 0 ? 'error' : undefined} pie="requieren atención" />
          <Tarjeta etiqueta="Líneas sin SKU" valor={num(resumen.lineas_sin_mapear)}
            tono={resumen.lineas_sin_mapear > 0 ? 'error' : undefined}
            pie="no existen en el catálogo" />
        </div>
      )}

      <section className="panel" data-guia="pedidos">
        <h2>Últimos pedidos</h2>
        <div className="cuerpo">
          {/* Con los fallidos contados en la tarjeta de arriba pero repartidos
              entre cincuenta filas, encontrarlos en el móvil era imposible. */}
          <div className="filtros" data-guia="ped-filtro">
            <select aria-label="Filtrar los pedidos por estado"
              value={filtro} onChange={(e) => setFiltro(e.target.value)}>
              <option value="">Todos los estados</option>
              {Object.entries(ESTADOS).map(([clave, e]) => (
                <option key={clave} value={clave}>{e.texto}</option>
              ))}
            </select>
            <span className="tenue mini-texto">
              {filtro === ''
                ? `Los ${num(ordenes.length)} pedidos más recientes`
                : `${num(visibles.length)} de ${num(ordenes.length)} pedidos`}
            </span>
            <SelectorVista modo={vista} onCambiar={setVista} admitidos={['detalles', 'lista', 'mosaico']} />
          </div>
        </div>
        <div {...teclado.propsContenedor} className={`tabla-envoltorio explorador vista-${vista}`} data-guia="ped-tabla"
          aria-label="Pedidos" aria-busy={refrescando}
          onMouseDown={seleccion.lazo.onMouseDown}>
          {vista === 'detalles' && (
          <table className="tabla-tarjetas tabla-columnas" ref={refTabla} style={{ minWidth: col.anchoMinimo }}>
            {col.colgroup}
            <CabeceraColumnas col={col} menu={menu} refTabla={refTabla} />
            <tbody>
              {visibles.map((o) => {
                const desplegada = abierta === o.id
                return (
                  <Fragment key={o.id}>
                    <tr {...teclado.propsFila(o)} className={claseFila(o)} title={TITULO_FILA}>
                      {col.columnas.map((c) => {
                        const { clase, etiqueta, contenido } = celda(c, o, desplegada)
                        return <td key={c.id} className={clase} data-etiqueta={etiqueta}>{contenido}</td>
                      })}
                    </tr>
                    {desplegada && (
                      <tr className="lazo-ignorar">
                        <td colSpan={col.columnas.length} className="detalle-pedido" data-guia="ped-detalle">
                          {detalleDe(o)}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
          )}

          {vista === 'lista' && visibles.length > 0 && (
            <div className="vista-lista">
              {visibles.map((o) => {
                const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
                const desplegada = abierta === o.id
                return (
                  <Fragment key={o.id}>
                    <div {...teclado.propsFila(o)} className={`fila-lista ${claseFila(o)}`} title={TITULO_FILA}>
                      <IconoCanal canal={o.canal} />
                      <span className="sku">{o.numero || o.external_id}</span>
                      <span className="principal" title={o.comprador || ''}>
                        {o.comprador || <span className="tenue">—</span>}
                      </span>
                      <span className="dato">{fecha(o.fecha_pedido)}</span>
                      <span className="num"><strong>{money(o.total)}</strong></span>
                      <span className={`pastilla ${e.clase}`}>{e.texto}</span>
                      <span className="vista-acciones">
                        <button className="enlace" aria-expanded={desplegada}
                          onClick={(ev) => { ev.stopPropagation(); alternarDetalle(o) }}>
                          {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
                        </button>
                      </span>
                    </div>
                    {desplegada && <div className="vista-detalle detalle-pedido lazo-ignorar">{detalleDe(o)}</div>}
                  </Fragment>
                )
              })}
            </div>
          )}

          {vista === 'mosaico' && visibles.length > 0 && (
            <div className="vista-mosaico">
              {visibles.map((o) => {
                const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
                const desplegada = abierta === o.id
                return (
                  <Fragment key={o.id}>
                    <div {...teclado.propsFila(o)} className={`tarjeta-vista ${claseFila(o)}`} title={TITULO_FILA}>
                      <div className="cuerpo-tarjeta">
                        <div className="fila">
                          <IconoCanal canal={o.canal} />
                          <div className="crece">
                            <div className="titulo">{o.numero || o.external_id}</div>
                            <div className="sku">{o.canal}</div>
                          </div>
                        </div>
                        <div className="datos">
                          <span className="recorta" title={o.comprador || ''}>{o.comprador || '—'}</span>
                          <span className="num"><strong>{money(o.total)}</strong></span>
                          <span>{fecha(o.fecha_pedido)}</span>
                        </div>
                        <div className="etiquetas"><span className={`pastilla ${e.clase}`}>{e.texto}</span></div>
                        <div className="vista-acciones">
                          <button className="enlace" aria-expanded={desplegada}
                            onClick={(ev) => { ev.stopPropagation(); alternarDetalle(o) }}>
                            {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
                          </button>
                        </div>
                      </div>
                    </div>
                    {desplegada && <div className="vista-detalle detalle-pedido lazo-ignorar">{detalleDe(o)}</div>}
                  </Fragment>
                )
              })}
            </div>
          )}

          {!cargado && <div className="vacio">Cargando pedidos…</div>}
          {cargado && ordenes.length === 0 && (
            <div className="vacio">
              Todavía no llegó ningún pedido. Conecta las cuentas de canal y pulsa «Traer pedidos nuevos».
            </div>
          )}
          {cargado && ordenes.length > 0 && visibles.length === 0 && (
            <div className="vacio">
              Ningún pedido reciente con ese estado.
            </div>
          )}
          {seleccion.lazo.marco}
        </div>

        <BarraEstado total={visibles.length} seleccionados={seleccion.seleccion.size} nombre="pedidos">
          {visibles.length > 0 && (
            <span className="num">
              {totales.deSeleccion ? 'Selección' : 'Total'}: {money(totales.importe)}
              {totales.fallidos > 0 && ` · ${num(totales.fallidos)} fallido${totales.fallidos === 1 ? '' : 's'}`}
            </span>
          )}
          {copiado && <span className="nota-estado">{copiado}</span>}
        </BarraEstado>
      </section>

      {menu.Menu}
    </>
  )
}

// Sin logotipos de los canales en el proyecto, el icono son las dos primeras
// letras del canal; el nombre completo va en el title.
function IconoCanal({ canal }: { canal: string }) {
  return (
    <span className="icono-canal" title={canal} aria-label={canal}>
      {canal.slice(0, 2).toUpperCase()}
    </span>
  )
}

function Tarjeta(props: { etiqueta: string; valor: string; pie?: string; tono?: 'ok' | 'error' }) {
  return (
    <div className="tarjeta">
      <div className="etiqueta">{props.etiqueta}</div>
      <div className={`valor ${props.tono ?? ''}`}>{props.valor}</div>
      {props.pie && <div className="pie">{props.pie}</div>}
    </div>
  )
}
