import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { api, fecha, money, num, type CuentaCanal, type Orden, type ResumenOrdenes } from './api'

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
  const [refrescando, setRefrescando] = useState(false)
  const [cargado, setCargado] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [nota, setNota] = useState<string | null>(null)
  const [filtro, setFiltro] = useState('')
  const [abierta, setAbierta] = useState<number | null>(null)
  const temporizador = useRef<number | null>(null)

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

  const visibles = filtro === '' ? ordenes : ordenes.filter((o) => o.estado === filtro)

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Pedidos</h1>
          <div className="sub">Lo que se vendió en los canales y su camino hacia Odoo</div>
        </div>
        <div className="acciones-cabecera">
          <button onClick={() => void cargar()} disabled={refrescando}>
            {refrescando ? 'Actualizando…' : 'Actualizar'}
          </button>
          <button className="primario" onClick={() => void traer()} disabled={trayendo}>
            {trayendo ? 'Trayendo…' : 'Traer pedidos nuevos'}
          </button>
        </div>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}
      {nota && <div className="nota-previa">{nota}</div>}

      {resumen && (
        <div className="tarjetas">
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

      <section className="panel">
        <h2>Últimos pedidos</h2>
        <div className="cuerpo">
          {/* Con los fallidos contados en la tarjeta de arriba pero repartidos
              entre cincuenta filas, encontrarlos en el móvil era imposible. */}
          <div className="filtros">
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
          </div>
        </div>
        <div className="tabla-envoltorio">
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
                const lineas = o.lineas ?? []
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
                        <td colSpan={7} className="detalle-pedido">
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
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
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

function Tarjeta(props: { etiqueta: string; valor: string; pie?: string; tono?: 'ok' | 'error' }) {
  return (
    <div className="tarjeta">
      <div className="etiqueta">{props.etiqueta}</div>
      <div className={`valor ${props.tono ?? ''}`}>{props.valor}</div>
      {props.pie && <div className="pie">{props.pie}</div>}
    </div>
  )
}
