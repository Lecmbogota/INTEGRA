import { useCallback, useEffect, useState } from 'react'
import { api, money, num, type Mapeo } from './api'

export function Mapeos() {
  const [filas, setFilas] = useState<Mapeo[]>([])
  const [error, setError] = useState<string | null>(null)
  const [soloDudosos, setSoloDudosos] = useState(false)

  const cargar = useCallback(() => {
    api.mapeos('mercadolibre')
      .then(setFilas)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  useEffect(() => { cargar() }, [cargar])

  async function confirmar(id: number) {
    try {
      await api.confirmarMapeo(id)
      // Se recarga en vez de mutar en local: así el contador de confirmados
      // y el orden reflejan siempre lo que hay en la base.
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const dudoso = (m: Mapeo) => (m.confianza ?? 0) < 0.5
  const visibles = soloDudosos ? filas.filter(dudoso) : filas

  const confirmados = filas.filter((m) => m.confirmado).length
  const desbloqueado = filas
    .filter((m) => m.confirmado)
    .reduce((s, m) => s + m.valor_inventario, 0)

  return (
    <section className="panel">
      <h2>Mapeo de categorías — MercadoLibre</h2>
      <div className="cuerpo">
        <div className="nota-previa">
          Sugerencias del predictor público de MercadoLibre. <strong>Ninguna se usa
          para publicar hasta que la confirmes.</strong> Las marcadas con ⚠ no comparten
          vocabulario con la categoría de Odoo, así que probablemente estén mal.
        </div>
        <div className="filtros">
          <span className="tenue">
            {confirmados} de {filas.length} confirmadas · {money(desbloqueado)} desbloqueados
          </span>
          <label className="casilla">
            <input type="checkbox" checked={soloDudosos}
              onChange={(e) => setSoloDudosos(e.target.checked)} />
            Solo dudosas
          </label>
        </div>
      </div>

      {error && <div className="aviso-caja">Error: {error}</div>}

      <div className="tabla-envoltorio">
        <table>
          <thead>
            <tr>
              <th>Categoría en Odoo</th>
              <th>Categoría en MercadoLibre</th>
              <th className="num">Productos</th>
              <th className="num">Inventario</th>
              <th>Atributos deducidos</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {visibles.map((m) => (
              <tr key={m.id}>
                <td>
                  {dudoso(m) && !m.confirmado && <span title="poca coherencia con el origen">⚠ </span>}
                  {m.categoria_odoo}
                </td>
                <td>
                  <code>{m.categoria_canal_id}</code>{' '}
                  <span className={dudoso(m) ? 'tenue' : ''}>{m.categoria_canal_nombre}</span>
                </td>
                <td className="num">{num(m.productos)}</td>
                <td className="num">{money(m.valor_inventario)}</td>
                <td>
                  <div className="etiquetas">
                    {(m.atributos_sugeridos ?? []).map((a) => (
                      <span key={a.id} className="pastilla ok" title={a.id}>
                        {a.name}: {a.value_name}
                      </span>
                    ))}
                  </div>
                </td>
                <td>
                  {m.confirmado
                    ? <span className="pastilla ok">Confirmada</span>
                    : <button onClick={() => confirmar(m.id)}>Confirmar</button>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {visibles.length === 0 && (
          <div className="vacio">
            {filas.length === 0
              ? 'Sin sugerencias todavía. Ejecuta: integra sugerir-categorias'
              : 'Ninguna coincide con el filtro.'}
          </div>
        )}
      </div>
    </section>
  )
}
