import { useCallback, useEffect, useState } from 'react'
import { api, fecha, money, type CuentaCanal, type Oferta } from './api'

const ESTADOS: Record<string, { texto: string; clase: string }> = {
  vigente: { texto: 'Vigente ahora', clase: 'ok' },
  programada: { texto: 'Programada', clase: 'aviso' },
  terminada: { texto: 'Terminada', clase: 'neutra' },
  cancelada: { texto: 'Cancelada', clase: 'neutra' },
}

// Promociones de un producto: precio rebajado con fecha de inicio y fin.
//
// Son POR CANAL porque las comisiones y la competencia difieren: puede tener
// sentido rebajar en MercadoLibre y no en la tienda propia.
export function Promocion({ varianteId, precioBase }: {
  varianteId: number
  precioBase: number | null
}) {
  const [ofertas, setOfertas] = useState<Oferta[]>([])
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [error, setError] = useState<string | null>(null)
  const [guardando, setGuardando] = useState(false)

  // Valores por defecto: empieza ahora, sin fecha de fin.
  const [cuenta, setCuenta] = useState<number | null>(null)
  const [precio, setPrecio] = useState('')
  const [inicia, setInicia] = useState(() => paraInput(new Date()))
  const [termina, setTermina] = useState('')

  const cargar = useCallback(() => {
    Promise.all([api.ofertasDe(varianteId), api.cuentas()])
      .then(([o, c]) => {
        setOfertas(o)
        setCuentas(c)
        if (c.length > 0 && cuenta === null) setCuenta(c[0].id)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    // cuenta se omite a propósito: solo se usa para elegir el valor inicial.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [varianteId])
  useEffect(() => { cargar() }, [cargar])

  async function crear() {
    if (cuenta === null) { setError('Conecta primero una cuenta de canal.'); return }
    const p = Number(precio.trim().replace(',', '.'))
    if (!Number.isFinite(p) || p <= 0) { setError('El precio de promoción debe ser mayor que cero.'); return }
    if (precioBase !== null && p >= precioBase) {
      setError(`La promoción (${money(p)}) no es menor que el precio normal (${money(precioBase)}).`)
      return
    }
    if (termina && new Date(termina) <= new Date(inicia)) {
      setError('La fecha de fin tiene que ser posterior a la de inicio.')
      return
    }

    setGuardando(true)
    setError(null)
    try {
      await api.crearOferta(varianteId, cuenta, p,
        new Date(inicia).toISOString(),
        termina ? new Date(termina).toISOString() : null)
      setPrecio('')
      setTermina('')
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setGuardando(false)
    }
  }

  async function cancelar(id: number) {
    try {
      await api.cancelarOferta(id)
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const descuento = (() => {
    const p = Number(precio.trim().replace(',', '.'))
    if (!precioBase || !Number.isFinite(p) || p <= 0 || p >= precioBase) return null
    return Math.round(((precioBase - p) / precioBase) * 100)
  })()

  return (
    <>
      <div className="nota-previa">
        Las promociones son <strong>por canal</strong> y tienen vigencia propia.
        WooCommerce y Falabella las programan de forma nativa; en Shopify y
        MercadoLibre, Integra aplica el precio al empezar y lo revierte al terminar.
      </div>

      {error && <div className="aviso-caja">{error}</div>}

      <div className="form-edicion">
        <label>
          <span>Canal</span>
          <select value={cuenta ?? ''} onChange={(e) => setCuenta(Number(e.target.value))}>
            {cuentas.length === 0 && <option value="">— sin cuentas conectadas —</option>}
            {cuentas.map((c) => (
              <option key={c.id} value={c.id}>{c.canal_nombre} · {c.nombre}</option>
            ))}
          </select>
        </label>

        <label>
          <span>Precio de promoción</span>
          <input inputMode="decimal" value={precio} placeholder={precioBase ? String(precioBase) : ''}
            onChange={(e) => setPrecio(e.target.value)} />
          <small className="tenue">
            {precioBase !== null
              ? <>Precio normal: {money(precioBase)}{descuento !== null && ` · ${descuento} % de descuento`}</>
              : 'Este producto aún no tiene precio normal'}
          </small>
        </label>

        <label>
          <span>Empieza</span>
          <input type="datetime-local" value={inicia} onChange={(e) => setInicia(e.target.value)} />
        </label>

        <label>
          <span>Termina (vacío = sin fin)</span>
          <input type="datetime-local" value={termina} onChange={(e) => setTermina(e.target.value)} />
        </label>

        <div className="ancha">
          <button className="primario" onClick={() => void crear()}
            disabled={guardando || cuentas.length === 0}>
            {guardando ? 'Creando…' : 'Crear promoción'}
          </button>
        </div>
      </div>

      {ofertas.length > 0 && (
        <table className="tabla-lineas tabla-ofertas">
          <thead>
            <tr><th>Canal</th><th className="num">Precio</th><th>Vigencia</th><th>Estado</th><th></th></tr>
          </thead>
          <tbody>
            {ofertas.map((o) => {
              const e = ESTADOS[o.estado] ?? { texto: o.estado, clase: 'aviso' }
              return (
                <tr key={o.id}>
                  <td>{o.canal}</td>
                  <td className="num"><strong>{money(o.precio)}</strong></td>
                  <td className="tenue mini-texto">
                    {fecha(o.inicia)}
                    {o.termina ? ` → ${fecha(o.termina)}` : ' → sin fin'}
                  </td>
                  <td><span className={`pastilla ${e.clase}`}>{e.texto}</span></td>
                  <td>
                    {(o.estado === 'vigente' || o.estado === 'programada') && (
                      <button onClick={() => void cancelar(o.id)}>Cancelar</button>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}

      {ofertas.length === 0 && (
        <div className="vacio">Este producto no tiene promociones.</div>
      )}
    </>
  )
}

// paraInput formatea una fecha para <input type="datetime-local">, que exige
// hora LOCAL sin zona — convertirla a ISO aquí la desplazaría al huso UTC.
function paraInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
