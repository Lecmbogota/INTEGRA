import { useCallback, useEffect, useState } from 'react'
import { api, fecha, num, type CuentaCanal, type ResumenPublicacion } from './api'

const NOMBRES: Record<string, string> = {
  mercadolibre: 'MercadoLibre', falabella: 'Falabella',
  woocommerce: 'WooCommerce', shopify: 'Shopify',
}

// Retirar del canal lo que dejó de ser mercancía no es un envío más: es la
// diferencia entre que alguien compre un producto que la empresa ya no tiene y
// que no pueda. Solo se nombra cuando ocurre, para no ensuciar el caso normal.
function retiradas(p: { pausar: number; reanudar: number }): string {
  const partes: string[] = []
  if (p.pausar > 0) partes.push(`${num(p.pausar)} publicaciones a pausar`)
  if (p.reanudar > 0) partes.push(`${num(p.reanudar)} a reabrir`)
  return partes.length > 0 ? `, ${partes.join(' y ')}` : ''
}

// Estado de lo publicado en cada canal y disparador de la planificación.
export function Publicacion() {
  const [filas, setFilas] = useState<ResumenPublicacion[]>([])
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [cargado, setCargado] = useState(false)
  const [refrescando, setRefrescando] = useState(false)
  const [plan, setPlan] = useState<string | null>(null)
  const [ocupado, setOcupado] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    setRefrescando(true)
    return Promise.all([api.publicaciones(), api.cuentas()])
      .then(([p, c]) => { setFilas(p); setCuentas(c); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => { setCargado(true); setRefrescando(false) })
  }, [])
  useEffect(() => { void cargar() }, [cargar])

  // Integra publica la ficha en borrador a propósito: en WooCommerce y en
  // Shopify, que la vea el comprador es decisión de una persona. Este es el
  // sitio donde se toma, y sin él había que entrar al canal producto a
  // producto.
  async function activar(c: CuentaCanal, encender: boolean) {
    const nombre = NOMBRES[c.canal] ?? c.canal
    if (!window.confirm(encender
      ? `Se pondrán a la venta en ${nombre} todas las fichas que Integra tiene publicadas allí. ¿Continuar?`
      : `Se retirarán de la venta en ${nombre} todas las fichas publicadas. Dejan de verse y de venderse hasta que vuelvas a activarlas. ¿Continuar?`)) return

    setOcupado(c.id)
    setError(null)
    setPlan(null)
    try {
      const r = await api.activarPublicaciones(c.id, encender)
      setPlan(r.encoladas === 0
        ? `${nombre}: no había ninguna ficha que ${encender ? 'activar' : 'retirar'}.`
        : `${nombre}: ${num(r.encoladas)} fichas encoladas para ${encender ? 'ponerse a la venta' : 'retirarse'}. El worker las procesa en segundo plano.`)
    } catch (e) {
      setError(`No se pudo ${encender ? 'activar' : 'desactivar'} ${nombre}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupado(null)
    }
  }

  async function planificar(c: CuentaCanal) {
    const nombre = NOMBRES[c.canal] ?? c.canal
    // Planificar encola envíos reales al canal. Con la conexión sin probar,
    // lo más probable es que fallen todos, así que se avisa antes.
    if (c.probada_ok !== true) {
      const aviso = c.probada_ok === false
        ? `La última prueba de conexión de ${nombre} falló. Si planificas ahora, los envíos encolados fallarán uno a uno. ¿Continuar igualmente?`
        : `La conexión de ${nombre} nunca se probó. Los envíos que se encolen pueden fallar todos. ¿Continuar igualmente?`
      if (!window.confirm(aviso)) return
    }
    setOcupado(c.id)
    setError(null)
    setPlan(null)
    try {
      const p = await api.planificar(c.id)
      const total = p.publicar + p.precio + p.stock + p.pausar + p.reanudar
      // Lo retenido por coste se nombra siempre: sin esto, un catálogo entero
      // frenado por precios a pérdida se anunciaba como «ya está todo al día».
      const retenidos = p.bajo_costo > 0
        ? ` ${num(p.bajo_costo)} no salen porque su precio no cubre el coste.`
        : ''
      setPlan(total === 0
        ? `${nombre}: nada que enviar. ${num(p.sin_cambios)} productos ya están al día${p.no_listos > 0 ? `, ${num(p.no_listos)} aún no cumplen los requisitos` : ''}.${retenidos}`
        : `${nombre}: ${num(total)} envíos encolados — ${num(p.publicar)} publicaciones, ${num(p.precio)} precios, ${num(p.stock)} stock${retiradas(p)}. El worker los procesa en segundo plano.${retenidos}`)
      void cargar()
    } catch (e) {
      setError(`No se pudo planificar los envíos de ${nombre}: ${e instanceof Error ? e.message : String(e)}`)
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
        <div className="acciones-cabecera">
          <button onClick={() => void cargar()} disabled={refrescando}>
            {refrescando ? 'Actualizando…' : 'Actualizar'}
          </button>
        </div>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}
      {plan && <div className="nota-previa">{plan}</div>}

      <section className="panel">
        <h2>Estado por canal</h2>
        <div className="cuerpo">
          {!cargado && <div className="vacio">Cargando canales…</div>}
          {cargado && cuentas.length === 0 && (
            <div className="vacio">
              No hay ninguna cuenta conectada. Ve a <strong>Canales</strong> para conectar la primera.
            </div>
          )}
          {cuentas.map((c) => {
            const r = filas.find((f) => f.cuenta_id === c.id)
            const nombre = NOMBRES[c.canal] ?? c.canal
            // Un contenedor de pastillas vacío deja un hueco de más en la fila.
            const hayPastillas = c.probada_ok !== true ||
              (r !== undefined && (r.con_error > 0 || r.pendientes_cola > 0))
            return (
              <div key={c.id} className="fila-cuenta fila-apilable">
                <div className="crece">
                  <div className="recorta">
                    {nombre}
                    {c.nombre && <span className="tenue"> · {c.nombre}</span>}
                  </div>
                  <div className="tenue mini-texto">
                    {r
                      ? `${num(r.activas)} publicados`
                      : 'sin publicaciones todavía'}
                    {r?.ultima_publicacion && ` · última: ${fecha(r.ultima_publicacion)}`}
                  </div>
                  {/* El «title» de la pastilla no existe con el dedo: si la
                      cuenta no está probada hay que decirlo en texto. */}
                  {c.probada_ok === false && (
                    <div className="tenue mini-texto">
                      La prueba de conexión falló{c.probada_msg ? `: ${c.probada_msg}` : ''}. Arréglala en «Canales» antes de publicar.
                    </div>
                  )}
                  {c.probada_ok === null && (
                    <div className="tenue mini-texto">
                      Esta conexión nunca se probó. Ve a «Canales» y pruébala antes de publicar.
                    </div>
                  )}
                </div>
                {/* Lo que falló es lo primero que se busca desde el móvil, así
                    que sale como pastilla y no enterrado en la línea de texto. */}
                {hayPastillas && <div className="etiquetas">
                  {r && r.con_error > 0 && (
                    <span className="pastilla bloqueante">{num(r.con_error)} con error</span>
                  )}
                  {r && r.pendientes_cola > 0 && (
                    <span className="pastilla aviso">{num(r.pendientes_cola)} en cola</span>
                  )}
                  {c.probada_ok === false && <span className="pastilla bloqueante">conexión falló</span>}
                  {c.probada_ok === null && <span className="pastilla aviso">sin verificar</span>}
                </div>}
                <div className="grupo-acciones">
                  <button onClick={() => void planificar(c)} disabled={ocupado === c.id}>
                    {ocupado === c.id ? 'Calculando…' : 'Planificar envíos'}
                  </button>
                  <button onClick={() => void activar(c, true)} disabled={ocupado === c.id}
                    title="Pone a la venta en el canal las fichas ya publicadas">
                    Poner a la venta
                  </button>
                  <button onClick={() => void activar(c, false)} disabled={ocupado === c.id}
                    title="Retira de la venta las fichas publicadas, sin borrarlas">
                    Retirar
                  </button>
                </div>
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

      <div className="nota-previa">
        Una ficha recién publicada queda <strong>en borrador</strong> en el canal: existe
        con su precio, su stock y sus fotos, pero el comprador todavía no la ve. Eso es a
        propósito —ponerla a la venta es una decisión, no un efecto secundario de
        sincronizar— y es lo que hace <strong>Poner a la venta</strong>. Lo que retiró el
        propio canal no se reabre desde aquí: detrás suele haber una infracción.
      </div>
    </>
  )
}
