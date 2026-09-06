import { useCallback, useEffect, useState } from 'react'
import { api, fecha, money, num, type CuentaCanal, type Orden, type ResumenOrdenes } from './api'

const ESTADOS: Record<string, { texto: string; clase: string }> = {
  received: { texto: 'Recibido', clase: 'aviso' },
  mapped: { texto: 'Mapeado', clase: 'aviso' },
  created_in_odoo: { texto: 'En Odoo', clase: 'ok' },
  failed: { texto: 'Falló', clase: 'bloqueante' },
  ignored: { texto: 'Ignorado', clase: 'dudosa' },
}

// Pedidos que llegaron de los canales y su estado camino a Odoo.
export function Pedidos() {
  const [ordenes, setOrdenes] = useState<Orden[]>([])
  const [resumen, setResumen] = useState<ResumenOrdenes | null>(null)
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [trayendo, setTrayendo] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [abierta, setAbierta] = useState<number | null>(null)

  const cargar = useCallback(() => {
    Promise.all([api.ordenes(50), api.resumenOrdenes(), api.cuentas()])
      .then(([o, r, c]) => { setOrdenes(o); setResumen(r); setCuentas(c); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function traer() {
    if (cuentas.length === 0) {
      setError('Conecta al menos una cuenta de canal antes de traer pedidos.')
      return
    }
    setTrayendo(true)
    setError(null)
    try {
      await Promise.all(cuentas.map((c) => api.ingerirOrdenes(c.id)))
      // La ingesta corre en el worker; se refresca al cabo de unos segundos.
      setTimeout(() => { cargar(); setTrayendo(false) }, 6000)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setTrayendo(false)
    }
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Pedidos</h1>
          <div className="sub">Lo que se vendió en los canales y su camino hacia Odoo</div>
        </div>
        <button className="primario" onClick={() => void traer()} disabled={trayendo}>
          {trayendo ? 'Trayendo…' : 'Traer pedidos nuevos'}
        </button>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}

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
        <div className="tabla-envoltorio">
          <table>
            <thead>
              <tr>
                <th>Pedido</th><th>Canal</th><th>Comprador</th>
                <th className="num">Total</th><th>Fecha</th><th>Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {ordenes.map((o) => {
                const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
                return (
                  <>
                    <tr key={o.id} className="clicable"
                      onClick={() => setAbierta(abierta === o.id ? null : o.id)}>
                      <td className="sku">{o.numero || o.external_id}</td>
                      <td>{o.canal}</td>
                      <td>{o.comprador || <span className="tenue">—</span>}</td>
                      <td className="num"><strong>{money(o.total)}</strong></td>
                      <td className="tenue">{fecha(o.fecha_pedido)}</td>
                      <td><span className={`pastilla ${e.clase}`}>{e.texto}</span></td>
                      <td className="tenue">{abierta === o.id ? '▾' : '▸'}</td>
                    </tr>
                    {abierta === o.id && (
                      <tr key={`${o.id}-det`}>
                        <td colSpan={7} className="detalle-pedido">
                          {o.error && <div className="aviso-caja">{o.error}</div>}
                          {o.odoo_pedido_id && (
                            <div className="tenue mini-texto">
                              Pedido de venta en Odoo: #{o.odoo_pedido_id}
                            </div>
                          )}
                          <table className="tabla-lineas">
                            <thead>
                              <tr><th>SKU</th><th>Producto</th><th className="num">Cant.</th><th className="num">Precio</th></tr>
                            </thead>
                            <tbody>
                              {(o.lineas ?? []).map((l) => (
                                <tr key={l.id}>
                                  <td className="sku">
                                    {l.sku}
                                    {l.variante_id === null &&
                                      <span className="pastilla bloqueante" title="Este SKU no existe en el catálogo de Integra">sin mapear</span>}
                                  </td>
                                  <td>{l.titulo}</td>
                                  <td className="num">{num(l.cantidad)}</td>
                                  <td className="num">{money(l.precio_unitario)}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </td>
                      </tr>
                    )}
                  </>
                )
              })}
            </tbody>
          </table>
          {ordenes.length === 0 && (
            <div className="vacio">
              Todavía no llegó ningún pedido. Conecta las cuentas de canal y pulsa «Traer pedidos nuevos».
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
