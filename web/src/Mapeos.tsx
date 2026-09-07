import { useCallback, useEffect, useState } from 'react'
import { confirmar as preguntar } from './escritorio/Dialogos'
import { api, money, num, type Mapeo } from './api'
import { SelectorVista, useVista } from './Vista'

const NOMBRES: Record<string, string> = {
  mercadolibre: 'MercadoLibre',
  falabella: 'Falabella',
}

// La pantalla enseñaba solo MercadoLibre aunque la API sirve el mapeo de
// cualquier canal, y Falabella también exige categoría: quien llega desde
// «hace falta mapear la categoría al árbol de Falabella» tiene que ver ese
// árbol y no el de otro canal.
export function Mapeos({ canalInicial }: { canalInicial?: string } = {}) {
  const [canal, setCanal] = useState(canalInicial ?? 'mercadolibre')
  useEffect(() => { if (canalInicial) setCanal(canalInicial) }, [canalInicial])
  const [filas, setFilas] = useState<Mapeo[]>([])
  const [cargando, setCargando] = useState(true)
  const [confirmando, setConfirmando] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [soloDudosos, setSoloDudosos] = useState(false)
  const [vista, setVista] = useVista('mapeos', 'detalles', ['detalles', 'lista'])

  const cargar = useCallback(() => {
    setCargando(true)
    api.mapeos(canal)
      .then(setFilas)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [canal])

  useEffect(() => { cargar() }, [cargar])

  const dudoso = (m: Mapeo) => (m.confianza ?? 0) < 0.5

  async function confirmar(m: Mapeo) {
    // Solo se pregunta en las dudosas: confirmar es la acción de todos los días
    // y no hay endpoint para deshacerla, así que la pregunta se reserva para
    // donde equivocarse cuesta —publicar el catálogo en otra categoría.
    if (dudoso(m) && !(await preguntar(
      `«${m.categoria_odoo}» no comparte vocabulario con «${m.categoria_canal_nombre}», así que la sugerencia probablemente esté mal. ` +
      `Confirmarla hará que ${num(m.productos)} productos se publiquen ahí y no se puede deshacer desde esta pantalla. ¿Continuar?`))) return

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
    <section className="panel" data-guia="mapeos">
      <h2>Mapeo de categorías — {NOMBRES[canal] ?? canal}</h2>
      <div className="cuerpo">
        {/* Cada canal tiene su propio árbol y su propia forma de llenar esta
            tabla; el texto lo dice para que nadie busque en Falabella un
            predictor que solo existe en MercadoLibre. */}
        {canal === 'mercadolibre' ? (
          <div className="nota-previa" data-guia="catg-nota">
            Cada categoría de Odoo necesita una equivalente en MercadoLibre para poder
            publicar. Las filas son sugerencias del predictor público de MercadoLibre
            (<code>integra sugerir-categorias</code>). <strong>Ninguna se usa para
            publicar hasta que la confirmes.</strong> Las marcadas con ⚠ no comparten
            vocabulario con la categoría de Odoo, así que probablemente estén mal.
          </div>
        ) : (
          <div className="nota-previa" data-guia="catg-nota">
            Falabella también exige una categoría de su árbol por cada categoría de Odoo,
            pero <strong>Integra todavía no tiene forma de proponerla ni de asignarla</strong>:
            no hay predictor como el de MercadoLibre ni pantalla para elegirla a mano.
            Mientras tanto, todo producto queda bloqueado para Falabella por
            «PrimaryCategory».
          </div>
        )}
        <div className="filtros">
          <div className="grupo-badges" data-guia="catg-canal">
            {Object.entries(NOMBRES).map(([id, nombre]) => (
              <button key={id} type="button" className={`badge ${canal === id ? 'activo' : ''}`}
                onClick={() => setCanal(id)}>{nombre}</button>
            ))}
          </div>
          <span className="tenue" data-guia="catg-resumen">
            {confirmados} de {filas.length} confirmadas · {money(desbloqueado)} desbloqueados
          </span>
          <label className="casilla" data-guia="catg-dudosas">
            <input type="checkbox" checked={soloDudosos}
              onChange={(e) => setSoloDudosos(e.target.checked)} />
            Solo dudosas
          </label>
          <SelectorVista modo={vista} onCambiar={setVista} admitidos={['detalles', 'lista']} />
        </div>
      </div>

      {error && <div className="aviso-caja">Error: {error}</div>}

      <div className="tabla-envoltorio">
        {vista === 'detalles' && (
        <table className="tabla-tarjetas" data-guia="catg-tabla">
          <thead>
            <tr>
              <th>Categoría en Odoo</th>
              <th>Categoría en {NOMBRES[canal] ?? canal}</th>
              <th className="num">Productos</th>
              <th className="num" data-guia="catg-inventario">Inventario</th>
              <th className="oculto-movil" data-guia="catg-atributos">Atributos deducidos</th>
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
                <td className="apilada" data-etiqueta={`Categoría en ${NOMBRES[canal] ?? canal}`}>
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
                      <button onClick={() => void confirmar(m)} disabled={confirmando === m.id} data-guia="catg-confirmar">
                        {confirmando === m.id ? 'Confirmando…' : 'Confirmar'}
                      </button>
                    )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        )}
        {vista === 'lista' && visibles.length > 0 && (
          <div className="vista-lista">
            {visibles.map((m) => (
              <div key={m.id} className="fila-lista">
                <span className="principal" title={m.categoria_odoo}>
                  {dudoso(m) && !m.confirmado && <span title="poca coherencia con el origen">⚠ </span>}
                  {m.categoria_odoo}
                </span>
                <span className="dato">→</span>
                <span className={`principal ${dudoso(m) ? 'tenue' : ''}`} title={`${m.categoria_canal_id} · ${m.categoria_canal_nombre}`}>
                  {m.categoria_canal_nombre}
                </span>
                <span className="num">{num(m.productos)} prod.</span>
                <span className="num">{money(m.valor_inventario)}</span>
                <span className="vista-acciones">
                  {m.confirmado
                    ? <span className="pastilla ok">Confirmada</span>
                    : (
                      <button onClick={() => void confirmar(m)} disabled={confirmando === m.id}>
                        {confirmando === m.id ? 'Confirmando…' : 'Confirmar'}
                      </button>
                    )}
                </span>
              </div>
            ))}
          </div>
        )}
        {cargando && <div className="vacio">Cargando sugerencias…</div>}
        {!cargando && visibles.length === 0 && (
          <div className="vacio">
            {filas.length === 0
              ? `Sin mapeos de ${NOMBRES[canal] ?? canal} todavía.${canal === 'mercadolibre' ? ' Ejecuta: integra sugerir-categorias' : ''}`
              : 'Ninguna coincide con el filtro.'}
          </div>
        )}
      </div>
    </section>
  )
}
