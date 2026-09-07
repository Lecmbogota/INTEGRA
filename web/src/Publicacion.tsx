import { useCallback, useEffect, useState } from 'react'
import { api, fecha, num, type CuentaCanal, type ResumenPublicacion, type EstadoDeProducto, type Situacion } from './api'

// Cómo se nombra y se pinta cada situación. «Sin publicar» no es un problema
// —es el estado natural de un producto nuevo— y por eso no sale en rojo; lo
// que sí lo es, «no se puede publicar», sí.
const ETIQUETA: Record<string, string> = {
  nuevo: 'Sin publicar',
  al_dia: 'Al día',
  pendiente: 'Cambios sin enviar',
  pausado: 'Retirado por nosotros',
  retirado: 'Lo retiró el canal',
  no_publicable: 'No se puede publicar',
}

const TONO: Record<string, string> = {
  nuevo: 'dudosa',
  al_dia: 'ok',
  pendiente: 'aviso',
  pausado: 'dudosa',
  retirado: 'bloqueante',
  no_publicable: 'bloqueante',
}

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

  // El estado producto a producto. Sin esto, entre Productos —que lista el
  // catálogo de Odoo— y el resumen de arriba —que cuenta fichas por canal— no
  // había manera de saber qué es un producto concreto: nunca publicado,
  // publicado y al día, o publicado con un cambio sin enviar.
  const [estados, setEstados] = useState<EstadoDeProducto[]>([])
  const [situacion, setSituacion] = useState<Situacion | ''>('')
  const [buscando, setBuscando] = useState('')
  const [cargandoEstado, setCargandoEstado] = useState(true)

  useEffect(() => {
    setCargandoEstado(true)
    api.estadoPublicaciones({ situacion: situacion || undefined, q: buscando || undefined })
      .then((r) => setEstados(r.items))
      .catch(() => setEstados([]))
      .finally(() => setCargandoEstado(false))
  }, [situacion, buscando, plan])

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
          <button data-guia="pub-actualizar" onClick={() => void cargar()} disabled={refrescando}>
            {refrescando ? 'Actualizando…' : 'Actualizar'}
          </button>
        </div>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}
      {plan && <div className="nota-previa">{plan}</div>}

      <section className="panel" data-guia="publicacion">
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
                  <div className="tenue mini-texto" data-guia="pub-cifras">
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
                {hayPastillas && <div className="etiquetas" data-guia="pub-pastillas">
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
                  <button data-guia="pub-planificar" onClick={() => void planificar(c)} disabled={ocupado === c.id}>
                    {ocupado === c.id ? 'Calculando…' : 'Planificar envíos'}
                  </button>
                  <button data-guia="pub-activar" onClick={() => void activar(c, true)} disabled={ocupado === c.id}
                    title="Pone a la venta en el canal las fichas ya publicadas">
                    Poner a la venta
                  </button>
                  <button data-guia="pub-retirar" onClick={() => void activar(c, false)} disabled={ocupado === c.id}
                    title="Retira de la venta las fichas publicadas, sin borrarlas">
                    Retirar
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      </section>

      <section className="panel" data-guia="pub-producto">
        <h2>Producto a producto</h2>
        <div className="cuerpo">
          <div className="filtros">
            <input className="crece" data-guia="pub-buscar" placeholder="Buscar por SKU o nombre…"
              value={buscando} onChange={(e) => setBuscando(e.target.value)} />
            <select aria-label="Filtrar por situación" data-guia="pub-situacion" value={situacion}
              onChange={(e) => setSituacion(e.target.value as Situacion | '')}>
              <option value="">Todas las situaciones</option>
              <option value="nuevo">Sin publicar todavía</option>
              <option value="pendiente">Con cambios sin enviar</option>
              <option value="al_dia">Publicados y al día</option>
              <option value="no_publicable">No se pueden publicar</option>
              <option value="pausado">Retirados por nosotros</option>
              <option value="retirado">Retirados por el canal</option>
            </select>
          </div>

          {cargandoEstado && <div className="vacio">Cargando…</div>}
          {!cargandoEstado && estados.length === 0 && (
            <div className="vacio">Ningún producto en esa situación.</div>
          )}

          {estados.length > 0 && (
            <div className="tabla-envoltorio" data-guia="pub-tabla">
              <table className="tabla-tarjetas">
                <thead>
                  <tr><th>Producto</th><th>Canal</th><th>Situación</th><th>Detalle</th></tr>
                </thead>
                <tbody>
                  {estados.map((e) => e.canales.map((c, i) => (
                    <tr key={`${e.variante_id}-${c.cuenta_id}`}>
                      {/* El SKU solo en la primera fila del producto: repetirlo
                          en cada canal hace la tabla ilegible de un vistazo. */}
                      <td className="titulo-tarjeta">
                        {i === 0 ? <><span className="sku">{e.sku}</span> {e.titulo}</> : ''}
                      </td>
                      <td data-etiqueta="Canal">{NOMBRES[c.canal] ?? c.canal}</td>
                      <td data-etiqueta="Situación">
                        <span className={`pastilla ${TONO[c.situacion] ?? 'aviso'}`}>
                          {ETIQUETA[c.situacion] ?? c.situacion}
                        </span>
                      </td>
                      <td className="apilada tenue mini-texto" data-etiqueta="Detalle">
                        {c.cambios && c.cambios.length > 0 && `falta enviar: ${c.cambios.join(', ')}`}
                        {c.falta && c.falta.length > 0 && (
                          <div className={c.situacion === 'no_publicable' ? 'error' : ''}>
                            {c.falta.join(' · ')}
                          </div>
                        )}
                      </td>
                    </tr>
                  )))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </section>

      <div className="nota-previa" data-guia="pub-nota-planificar">
        <strong>Cómo funciona:</strong> «Planificar envíos» compara el catálogo con lo ya
        publicado y encola <em>solo lo que cambió</em>, con el stock primero. Nada se envía
        desde aquí: los trabajos los procesa el worker con reintentos.
      </div>

      <div className="nota-previa" data-guia="pub-nota-borrador">
        Una ficha recién publicada queda <strong>en borrador</strong> en el canal: existe
        con su precio, su stock y sus fotos, pero el comprador todavía no la ve. Eso es a
        propósito —ponerla a la venta es una decisión, no un efecto secundario de
        sincronizar— y es lo que hace <strong>Poner a la venta</strong>. Lo que retiró el
        propio canal no se reabre desde aquí: detrás suele haber una infracción.
      </div>
    </>
  )
}
