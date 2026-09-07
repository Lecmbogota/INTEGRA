import { useEffect, useState } from 'react'
import { api, money, type Canal } from './api'

// Comisiones por canal: lo que cada plataforma cobra por venta. El precio
// publicado las compensa para que el margen no se regale — el ejemplo de la
// derecha lo muestra con un precio base de $100.000.
export function Canales() {
  const [canales, setCanales] = useState<Canal[]>([])
  const [cargando, setCargando] = useState(true)
  const [borrador, setBorrador] = useState<Record<string, { pct: string; fijo: string }>>({})
  const [guardando, setGuardando] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = () =>
    api.canales().then((cs) => {
      setCanales(cs)
      const b: Record<string, { pct: string; fijo: string }> = {}
      for (const c of cs) b[c.codigo] = { pct: String(c.comision_pct), fijo: String(c.costo_fijo) }
      setBorrador(b)
    }).catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))

  useEffect(() => { void cargar() }, [])

  async function guardar(codigo: string) {
    const b = borrador[codigo]
    const pct = Number(b.pct.replace(',', '.'))
    const fijo = Number(b.fijo.replace(',', '.'))
    // El canal se nombra en el error: con cuatro filas editables a la vez, un
    // «deben ser números» suelto no dice cuál hay que arreglar.
    const nombre = canales.find((c) => c.codigo === codigo)?.nombre ?? codigo
    if (!Number.isFinite(pct) || !Number.isFinite(fijo)) {
      setError(`${nombre}: la comisión y el costo fijo deben ser números.`)
      return
    }
    if (pct < 0 || pct >= 100 || fijo < 0) {
      setError(`${nombre}: la comisión va entre 0 y 99,99 % y el costo fijo no puede ser negativo.`)
      return
    }
    setGuardando(codigo)
    setError(null)
    try {
      await api.editarCanal(codigo, pct, fijo)
      await cargar()
    } catch (e) {
      setError(`No se pudo guardar ${nombre}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setGuardando(null)
    }
  }

  // Devuelve el borrador de un canal al valor guardado. Sin esto, un dedazo en
  // una casilla solo se deshace recargando la página entera.
  function descartar(codigo: string) {
    const c = canales.find((x) => x.codigo === codigo)
    if (!c) return
    setBorrador({ ...borrador, [codigo]: { pct: String(c.comision_pct), fijo: String(c.costo_fijo) } })
  }

  // Mismo cálculo que hace el servidor, para previsualizar en vivo.
  const ejemplo = (pct: number, fijo: number) => {
    if (pct >= 100) return null
    return Math.ceil(((100000 + fijo) / (1 - pct / 100)) / 100) * 100
  }

  return (
    <section className="panel" data-guia="can-comisiones">
      <h2>Comisiones por canal</h2>
      <div className="cuerpo">
        <div className="nota-previa">
          El precio publicado en cada canal se calcula para que, tras descontar su
          comisión, quede tu precio de Integra. Ejemplo sobre una base de $ 100.000.
        </div>
        {error && <div className="aviso-caja">{error}</div>}
        {cargando && <div className="vacio">Cargando comisiones…</div>}
        {!cargando && (
          <div className="tabla-envoltorio">
            <table className="tabla-canales tabla-tarjetas">
              <thead>
                <tr>
                  <th>Canal</th><th className="num">Comisión %</th><th className="num">Costo fijo</th>
                  <th className="num" data-guia="can-ejemplo">$100.000 →</th><th></th>
                </tr>
              </thead>
              <tbody>
                {canales.map((c) => {
                  const b = borrador[c.codigo] ?? { pct: '0', fijo: '0' }
                  const pct = Number(b.pct.replace(',', '.'))
                  const fijo = Number(b.fijo.replace(',', '.'))
                  const numerico = Number.isFinite(pct) && Number.isFinite(fijo)
                  const ej = numerico ? ejemplo(pct, fijo) : null
                  const cambiado = b.pct !== String(c.comision_pct) || b.fijo !== String(c.costo_fijo)
                  return (
                    <tr key={c.codigo}>
                      <td className="titulo-tarjeta">{c.nombre}</td>
                      <td className="num" data-etiqueta="Comisión %">
                        <input className="mini" inputMode="decimal" value={b.pct}
                          aria-label={`Comisión de ${c.nombre} en porcentaje`}
                          onChange={(e) => setBorrador({ ...borrador, [c.codigo]: { ...b, pct: e.target.value } })} />
                      </td>
                      <td className="num" data-etiqueta="Costo fijo">
                        <input className="mini" inputMode="decimal" value={b.fijo}
                          aria-label={`Costo fijo de ${c.nombre}`}
                          onChange={(e) => setBorrador({ ...borrador, [c.codigo]: { ...b, fijo: e.target.value } })} />
                      </td>
                      {/* El ejemplo es el único sitio donde se ve que lo escrito
                          no sirve, así que dice por qué en vez de un guion. */}
                      <td className="num tenue" data-etiqueta="$100.000 →">
                        {!numerico
                          ? <small className="excede">no es número</small>
                          : pct >= 100
                            ? <small className="excede">comisión ≥ 100 %</small>
                            : ej === null ? '—' : money(ej)}
                      </td>
                      {/* Sin cambios la celda queda vacía a propósito: así en el
                          móvil desaparece en vez de dejar un hueco en la tarjeta. */}
                      <td className={cambiado ? 'acciones-fila' : ''}>
                        {cambiado && (
                          <>
                            <button onClick={() => void guardar(c.codigo)} disabled={guardando === c.codigo}>
                              {guardando === c.codigo ? 'Guardando…' : 'Guardar'}
                            </button>
                            <button onClick={() => descartar(c.codigo)} disabled={guardando === c.codigo}>
                              Descartar
                            </button>
                          </>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  )
}
