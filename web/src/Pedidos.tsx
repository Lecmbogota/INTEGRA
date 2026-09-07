import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { api, fecha, money, num, type CuentaCanal, type Orden, type ResumenOrdenes } from './api'
import { SelectorVista, useVista } from './Vista'

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

  const visibles = filtro === '' ? ordenes : ordenes.filter((o) => o.estado === filtro)

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
        {(o.estado === 'failed' || o.estado === 'received' || o.estado === 'mapped') && (
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
        <div className="tabla-envoltorio" data-guia="ped-tabla">
          {vista === 'detalles' && (
          <table className="tabla-tarjetas">
            <thead>
              <tr>
                <th>Pedido</th><th>Canal</th><th>Comprador</th>
                <th className="num">Total</th><th>Fecha</th><th>Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {visibles.map((o) => {
                const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
                const desplegada = abierta === o.id
                return (
                  <Fragment key={o.id}>
                    <tr className="clicable"
                      onClick={() => setAbierta(desplegada ? null : o.id)}>
                      <td className="sku titulo-tarjeta">{o.numero || o.external_id}</td>
                      <td data-etiqueta="Canal">{o.canal}</td>
                      <td data-etiqueta="Comprador">{o.comprador || <span className="tenue">—</span>}</td>
                      <td className="num" data-etiqueta="Total"><strong>{money(o.total)}</strong></td>
                      <td className="tenue" data-etiqueta="Fecha">{fecha(o.fecha_pedido)}</td>
                      <td data-etiqueta="Estado"><span className={`pastilla ${e.clase}`}>{e.texto}</span></td>
                      <td className="acciones-fila">
                        {/* La fila entera abre el detalle, pero eso no llega con
                            el teclado: el botón es el que sí es accesible. */}
                        <button className="enlace" aria-expanded={desplegada}
                          onClick={(ev) => { ev.stopPropagation(); setAbierta(desplegada ? null : o.id) }}>
                          {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
                        </button>
                      </td>
                    </tr>
                    {desplegada && (
                      <tr>
                        <td colSpan={7} className="detalle-pedido" data-guia="ped-detalle">
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
                    <div className="fila-lista clicable" onClick={() => setAbierta(desplegada ? null : o.id)}>
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
                          onClick={(ev) => { ev.stopPropagation(); setAbierta(desplegada ? null : o.id) }}>
                          {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
                        </button>
                      </span>
                    </div>
                    {desplegada && <div className="vista-detalle detalle-pedido">{detalleDe(o)}</div>}
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
                    <div className="tarjeta-vista clicable" onClick={() => setAbierta(desplegada ? null : o.id)}>
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
                            onClick={(ev) => { ev.stopPropagation(); setAbierta(desplegada ? null : o.id) }}>
                            {desplegada ? 'Ocultar detalle' : 'Ver detalle'}
                          </button>
                        </div>
                      </div>
                    </div>
                    {desplegada && <div className="vista-detalle detalle-pedido">{detalleDe(o)}</div>}
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
        </div>
      </section>
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
