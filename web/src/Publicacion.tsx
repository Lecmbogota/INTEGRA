import { useCallback, useEffect, useState } from 'react'
import { api, fecha, num, type CuentaCanal, type ResumenPublicacion } from './api'

const NOMBRES: Record<string, string> = {
  mercadolibre: 'MercadoLibre', falabella: 'Falabella',
  woocommerce: 'WooCommerce', shopify: 'Shopify',
}

// Estado de lo publicado en cada canal y disparador de la planificación.
export function Publicacion() {
  const [filas, setFilas] = useState<ResumenPublicacion[]>([])
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [plan, setPlan] = useState<string | null>(null)
  const [ocupado, setOcupado] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    Promise.all([api.publicaciones(), api.cuentas()])
      .then(([p, c]) => { setFilas(p); setCuentas(c) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function planificar(cuentaId: number) {
    setOcupado(cuentaId)
    setError(null)
    setPlan(null)
    try {
      const p = await api.planificar(cuentaId)
      const total = p.publicar + p.precio + p.stock
      setPlan(total === 0
        ? `Nada que enviar: ${p.sin_cambios} productos ya están al día${p.no_listos > 0 ? `, ${p.no_listos} aún no cumplen los requisitos` : ''}.`
        : `${total} envíos encolados — ${p.publicar} publicaciones, ${p.precio} precios, ${p.stock} stock. El worker los procesa en segundo plano.`)
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setOcupado(null)
    }
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Publicación</h1>
          <div className="sub">Qué está publicado en cada canal y qué falta por enviar</div>
        </div>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}
      {plan && <div className="nota-previa">{plan}</div>}

      <section className="panel">
        <h2>Estado por canal</h2>
        <div className="cuerpo">
          {cuentas.length === 0 && (
            <div className="vacio">
              No hay ninguna cuenta conectada. Ve a <strong>Canales</strong> para conectar la primera.
            </div>
          )}
          {cuentas.map((c) => {
            const r = filas.find((f) => f.cuenta_id === c.id)
            return (
              <div key={c.id} className="fila-cuenta">
                <div className="crece">
                  <div>{NOMBRES[c.canal] ?? c.canal}</div>
                  <div className="tenue mini-texto">
                    {r
                      ? `${num(r.activas)} publicados · ${num(r.con_error)} con error · ${num(r.pendientes_cola)} en cola`
                      : 'sin publicaciones todavía'}
                    {r?.ultima_publicacion && ` · última: ${fecha(r.ultima_publicacion)}`}
                  </div>
                </div>
                {c.probada_ok !== true && (
                  <span className="pastilla aviso" title="Prueba la conexión en Canales antes de publicar">
                    sin verificar
                  </span>
                )}
                <button onClick={() => void planificar(c.id)} disabled={ocupado === c.id}>
                  {ocupado === c.id ? 'Calculando…' : 'Planificar envíos'}
                </button>
              </div>
            )
          })}
        </div>
      </section>

      <div className="nota-previa">
        <strong>Cómo funciona:</strong> «Planificar envíos» compara el catálogo con lo ya
        publicado y encola <em>solo lo que cambió</em>, con el stock primero. Nada se envía
        desde aquí: los trabajos los procesa el worker con reintentos.
      </div>
    </>
  )
}
