import { useCallback, useEffect, useState } from 'react'
import { api, money, num, type Mapeo } from './api'

export function Mapeos() {
  const [filas, setFilas] = useState<Mapeo[]>([])
  const [cargando, setCargando] = useState(true)
  const [confirmando, setConfirmando] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [soloDudosos, setSoloDudosos] = useState(false)

  const cargar = useCallback(() => {
    api.mapeos('mercadolibre')
      .then(setFilas)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])

  useEffect(() => { cargar() }, [cargar])

  const dudoso = (m: Mapeo) => (m.confianza ?? 0) < 0.5

  async function confirmar(m: Mapeo) {
    // Solo se pregunta en las dudosas: confirmar es la acción de todos los días
    // y no hay endpoint para deshacerla, así que la pregunta se reserva para
    // donde equivocarse cuesta —publicar el catálogo en otra categoría.
    if (dudoso(m) && !window.confirm(
      `«${m.categoria_odoo}» no comparte vocabulario con «${m.categoria_canal_nombre}», así que la sugerencia probablemente esté mal. ` +
      `Confirmarla hará que ${num(m.productos)} productos se publiquen ahí y no se puede deshacer desde esta pantalla. ¿Continuar?`)) return

    setConfirmando(m.id)
    setError(null)
    try {
      await api.confirmarMapeo(m.id)
      // Se recarga en vez de mutar en local: así el contador de confirmados
      // y el orden reflejan siempre lo que hay en la base.
      cargar()
    } catch (e) {
      setError(`No se pudo confirmar «${m.categoria_odoo}»: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setConfirmando(null)
    }
  }

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
        <table className="tabla-tarjetas">
          <thead>
            <tr>
              <th>Categoría en Odoo</th>
              <th>Categoría en MercadoLibre</th>
              <th className="num">Productos</th>
              <th className="num">Inventario</th>
              <th className="oculto-movil">Atributos deducidos</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {visibles.map((m) => (
              <tr key={m.id}>
                <td className="titulo-tarjeta">
                  {dudoso(m) && !m.confirmado && <span title="poca coherencia con el origen">⚠ </span>}
                  {m.categoria_odoo}
                </td>
                <td className="apilada" data-etiqueta="Categoría en MercadoLibre">
                  <code>{m.categoria_canal_id}</code>{' '}
                  <span className={dudoso(m) ? 'tenue' : ''}>{m.categoria_canal_nombre}</span>
                </td>
                <td className="num" data-etiqueta="Productos">{num(m.productos)}</td>
                <td className="num" data-etiqueta="Inventario">{money(m.valor_inventario)}</td>
                {/* Los atributos deducidos se revisan al completar la ficha, no
                    al mapear: en el móvil solo alargarían la tarjeta. */}
                <td className="oculto-movil">
                  <div className="etiquetas">
                    {(m.atributos_sugeridos ?? []).map((a) => (
                      <span key={a.id} className="pastilla ok" title={a.id}>
                        {a.name}: {a.value_name}
                      </span>
                    ))}
                  </div>
                </td>
                <td className="acciones-fila">
                  {m.confirmado
                    ? <span className="pastilla ok">Confirmada</span>
                    : (
                      <button onClick={() => void confirmar(m)} disabled={confirmando === m.id}>
                        {confirmando === m.id ? 'Confirmando…' : 'Confirmar'}
                      </button>
                    )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {cargando && <div className="vacio">Cargando sugerencias…</div>}
        {!cargando && visibles.length === 0 && (
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
