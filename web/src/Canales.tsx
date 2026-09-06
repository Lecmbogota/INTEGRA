import { useEffect, useState } from 'react'
import { api, money, type Canal } from './api'

// Comisiones por canal: lo que cada plataforma cobra por venta. El precio
// publicado las compensa para que el margen no se regale — el ejemplo de la
// derecha lo muestra con un precio base de $100.000.
export function Canales() {
  const [canales, setCanales] = useState<Canal[]>([])
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

  useEffect(() => { void cargar() }, [])

  async function guardar(codigo: string) {
    const b = borrador[codigo]
    const pct = Number(b.pct.replace(',', '.'))
    const fijo = Number(b.fijo.replace(',', '.'))
    if (!Number.isFinite(pct) || !Number.isFinite(fijo)) {
      setError('Comisión y costo fijo deben ser números.')
      return
    }
    setGuardando(codigo)
    setError(null)
    try {
      await api.editarCanal(codigo, pct, fijo)
      await cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setGuardando(null)
    }
  }

  // Mismo cálculo que hace el servidor, para previsualizar en vivo.
  const ejemplo = (pct: number, fijo: number) => {
    if (pct >= 100) return null
    return Math.ceil(((100000 + fijo) / (1 - pct / 100)) / 100) * 100
  }

  return (
    <section className="panel">
      <h2>Comisiones por canal</h2>
      <div className="cuerpo">
        <div className="nota-previa">
          El precio publicado en cada canal se calcula para que, tras descontar su
          comisión, quede tu precio de Integra. Ejemplo sobre una base de $ 100.000.
        </div>
        {error && <div className="aviso-caja">{error}</div>}
        <table className="tabla-canales">
          <thead>
            <tr><th>Canal</th><th className="num">Comisión %</th><th className="num">Costo fijo</th><th className="num">$100.000 →</th><th></th></tr>
          </thead>
          <tbody>
            {canales.map((c) => {
              const b = borrador[c.codigo] ?? { pct: '0', fijo: '0' }
              const pct = Number(b.pct.replace(',', '.'))
              const fijo = Number(b.fijo.replace(',', '.'))
              const ej = Number.isFinite(pct) && Number.isFinite(fijo) ? ejemplo(pct, fijo) : null
              const cambiado = b.pct !== String(c.comision_pct) || b.fijo !== String(c.costo_fijo)
              return (
                <tr key={c.codigo}>
                  <td>{c.nombre}</td>
                  <td className="num">
                    <input className="mini" inputMode="decimal" value={b.pct}
                      onChange={(e) => setBorrador({ ...borrador, [c.codigo]: { ...b, pct: e.target.value } })} />
                  </td>
                  <td className="num">
                    <input className="mini" inputMode="decimal" value={b.fijo}
                      onChange={(e) => setBorrador({ ...borrador, [c.codigo]: { ...b, fijo: e.target.value } })} />
                  </td>
                  <td className="num tenue">{ej === null ? '—' : money(ej)}</td>
                  <td>
                    {cambiado && (
                      <button onClick={() => void guardar(c.codigo)} disabled={guardando === c.codigo}>
                        {guardando === c.codigo ? 'Guardando…' : 'Guardar'}
                      </button>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </section>
  )
}
