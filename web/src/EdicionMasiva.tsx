import { useState } from 'react'
import { api, money, type OperacionMasiva, type ResultadoMasivo } from './api'

// Edición masiva. Siempre simula antes de aplicar: en una operación que toca
// cientos de productos, ver el efecto antes de causarlo no es un lujo.
export function EdicionMasiva({ ids, filtro, totalFiltro, onCerrar, onAplicado }: {
  ids: number[]
  filtro: Record<string, unknown>
  totalFiltro: number
  onCerrar: () => void
  onAplicado: () => void
}) {
  const [alcance, setAlcance] = useState<'seleccion' | 'filtro'>(
    ids.length > 0 ? 'seleccion' : 'filtro')
  const [tipo, setTipo] = useState<OperacionMasiva['tipo']>('precio_desde_coste')
  const [factor, setFactor] = useState('1.35')
  const [valor, setValor] = useState('')
  const [redondeo, setRedondeo] = useState(900)
  const [previa, setPrevia] = useState<ResultadoMasivo | null>(null)
  const [ocupado, setOcupado] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const seleccion = alcance === 'seleccion' ? { ids } : { filtro }
  const cuantos = alcance === 'seleccion' ? ids.length : totalFiltro

  function operacion(simular: boolean): OperacionMasiva {
    return {
      tipo, simular, redondeo,
      factor: Number(factor.replace(',', '.')) || 0,
      valor,
    }
  }

  async function simular() {
    setOcupado(true)
    setError(null)
    try {
      setPrevia(await api.editarMasivo(seleccion, operacion(true)))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setPrevia(null)
    } finally {
      setOcupado(false)
    }
  }

  async function aplicar() {
    setOcupado(true)
    setError(null)
    try {
      await api.editarMasivo(seleccion, operacion(false))
      onAplicado()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setOcupado(false)
    }
  }

  const esPrecio = tipo.startsWith('precio')

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Editar en masa</h2>
            <div className="sub">{cuantos} productos afectados</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          <label className="ancha">
            <span>A qué productos</span>
            <select value={alcance} onChange={(e) => { setAlcance(e.target.value as typeof alcance); setPrevia(null) }}>
              <option value="seleccion" disabled={ids.length === 0}>
                Los {ids.length} seleccionados
              </option>
              <option value="filtro">Todos los {totalFiltro} del filtro actual</option>
            </select>
          </label>

          <label className="ancha">
            <span>Qué hacer</span>
            <select value={tipo} onChange={(e) => { setTipo(e.target.value as typeof tipo); setPrevia(null) }}>
              <option value="precio_desde_coste">Precio = coste × factor</option>
              <option value="precio_ajustar">Ajustar el precio actual × factor</option>
              <option value="precio_fijo">Poner un precio fijo</option>
              <option value="precio_borrar">Borrar el precio</option>
              <option value="marca">Asignar marca</option>
              <option value="excluir">Excluir del catálogo</option>
              <option value="incluir">Devolver al catálogo</option>
            </select>
          </label>

          {(tipo === 'precio_desde_coste' || tipo === 'precio_ajustar') && (
            <label>
              <span>Factor</span>
              <input inputMode="decimal" value={factor}
                onChange={(e) => { setFactor(e.target.value); setPrevia(null) }} />
              <small className="tenue">
                {tipo === 'precio_desde_coste'
                  ? '1.35 = 35 % sobre el coste'
                  : '1.10 sube un 10 %, 0.90 baja un 10 %'}
              </small>
            </label>
          )}

          {tipo === 'precio_fijo' && (
            <label>
              <span>Precio</span>
              <input inputMode="decimal" value={valor} placeholder="119900"
                onChange={(e) => { setValor(e.target.value); setPrevia(null) }} />
            </label>
          )}

          {tipo === 'marca' && (
            <label>
              <span>Marca (vacío = quitar)</span>
              <input value={valor} onChange={(e) => setValor(e.target.value)} />
            </label>
          )}

          {esPrecio && tipo !== 'precio_borrar' && (
            <label>
              <span>Redondeo</span>
              <select value={redondeo} onChange={(e) => { setRedondeo(Number(e.target.value)); setPrevia(null) }}>
                <option value={900}>Comercial — termina en 900</option>
                <option value={100}>Al centenar</option>
                <option value={1000}>Al millar</option>
                <option value={0}>Sin redondeo</option>
              </select>
            </label>
          )}
        </div>

        {previa && (
          <div className="nota-previa">
            <strong>{previa.afectados} productos cambiarían.</strong>
            {previa.omitidos > 0 && (
              <> {previa.omitidos} se omiten: {previa.motivo_omision}.</>
            )}
            {previa.muestra && previa.muestra.length > 0 && (
              <table className="tabla-lineas">
                <thead>
                  <tr><th>SKU</th><th>Producto</th><th className="num">Antes</th><th className="num">Después</th></tr>
                </thead>
                <tbody>
                  {previa.muestra.map((m) => (
                    <tr key={m.sku}>
                      <td className="sku">{m.sku}</td>
                      <td>{m.nombre.slice(0, 40)}</td>
                      <td className="num tenue">{m.antes === null ? '—' : money(m.antes)}</td>
                      <td className="num"><strong>{m.despues === null ? '—' : money(m.despues)}</strong></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={ocupado}>Cancelar</button>
          <button onClick={() => void simular()} disabled={ocupado || cuantos === 0}>
            {ocupado ? '…' : 'Ver qué cambiaría'}
          </button>
          <button className="primario" onClick={() => void aplicar()}
            disabled={ocupado || !previa || previa.afectados === 0}
            title={!previa ? 'Primero mira qué cambiaría' : ''}>
            Aplicar a {previa?.afectados ?? cuantos}
          </button>
        </footer>
      </div>
    </div>
  )
}
