import { useEffect, useState } from 'react'
import { api, num, type FiltroMediateca, type PaginaImagenes } from './api'

// El banco de imágenes visto entero, no producto a producto.
//
// Las fotos son lo único de Integra que ocupa disco de verdad y que ningún
// canal perdona: una ficha sin foto no vende y una foto pequeña la rechaza
// MercadoLibre en el alta. Desde cada producto se ve una; aquí se ven todas,
// con las preguntas que importan sobre el conjunto: qué productos no pueden
// publicarse por falta de foto, qué fotos no sirven en ningún canal, y cuánto
// disco ocupa lo que ya no usa nadie.

const FILTROS: [FiltroMediateca, string, string][] = [
  ['todas', 'Todas', ''],
  ['aptas', 'Aptas', 'Lado menor de 600 px o más: valen en los cuatro canales'],
  ['pequenas', 'Pequeñas', 'Por debajo de 600 px: MercadoLibre las rechaza'],
  ['huerfanas', 'Huérfanas', 'No las usa ningún producto: ocupan disco sin publicarse'],
  ['duplicadas', 'Duplicadas', 'Mismo tamaño y dimensiones que otra: casi siempre la misma foto dos veces'],
]

const POR_PAGINA = 60

function tamano(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

export function Mediateca({ onVer }: { onVer?: (varianteId: number) => void }) {
  const [filtro, setFiltro] = useState<FiltroMediateca>('todas')
  const [busqueda, setBusqueda] = useState('')
  const [offset, setOffset] = useState(0)
  const [pagina, setPagina] = useState<PaginaImagenes | null>(null)
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [borrando, setBorrando] = useState<number | null>(null)
  const [version, setVersion] = useState(0)

  // La búsqueda se retrasa 300 ms para no consultar por tecla, y `vigente`
  // descarta la respuesta de una consulta que ya quedó atrás.
  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.banco({ filtro, q: busqueda, limite: POR_PAGINA, offset })
        .then((p) => { if (vigente) { setPagina(p); setError(null) } })
        .catch((e) => { if (vigente) setError(e instanceof Error ? e.message : String(e)) })
        .finally(() => { if (vigente) setCargando(false) })
    }, 300)
    return () => { vigente = false; clearTimeout(t) }
  }, [filtro, busqueda, offset, version])

  function cambiarFiltro(f: FiltroMediateca) { setFiltro(f); setOffset(0) }

  async function borrar(id: number) {
    if (!window.confirm('Se borra del disco. No la usa ningún producto, así que ninguna ficha se queda sin foto. ¿Continuar?')) return
    setBorrando(id)
    setError(null)
    try {
      await api.borrarDelBanco(id)
      setVersion((v) => v + 1)
    } catch (e) {
      setError(`No se pudo borrar: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setBorrando(null)
    }
  }

  const total = pagina?.total ?? 0
  const desde = total === 0 ? 0 : offset + 1
  const hasta = Math.min(offset + POR_PAGINA, total)

  return (
    <>
      {pagina && (
        <div className="tarjetas">
          <Cifra etiqueta="Productos sin foto" valor={num(pagina.productos_sin_foto)}
            pie="No pueden publicarse en ningún canal" tono={pagina.productos_sin_foto > 0 ? 'error' : 'ok'} />
          <Cifra etiqueta="Fotos pequeñas" valor={num(pagina.pequenas)}
            pie="Por debajo de 600 px: MercadoLibre las rechaza" tono={pagina.pequenas > 0 ? 'error' : 'ok'} />
          <Cifra etiqueta="Disco en huérfanas" valor={tamano(pagina.bytes_huerfanos)}
            pie="Fotos que no usa ningún producto" />
        </div>
      )}

      <section className="panel">
        <h2>Banco de imágenes{pagina ? ` · ${num(total)}` : ''}</h2>
        <div className="cuerpo">
          <div className="filtros">
            <div className="grupo-badges">
              {FILTROS.map(([id, nombre, pista]) => (
                <button key={id} type="button" className={`badge ${filtro === id ? 'activo' : ''}`}
                  title={pista} onClick={() => cambiarFiltro(id)}>{nombre}</button>
              ))}
            </div>
            <input type="search" className="crece" placeholder="Buscar por SKU o nombre del producto"
              value={busqueda} onChange={(e) => { setBusqueda(e.target.value); setOffset(0) }} />
          </div>

          {error && <div className="aviso-caja">{error}</div>}
          {cargando && !pagina && <div className="vacio">Cargando el banco…</div>}
          {pagina && pagina.items.length === 0 && !cargando && (
            <div className="vacio">
              {filtro === 'todas' && !busqueda
                ? 'El banco está vacío: sube fotos desde cualquier producto o usa «Buscar imágenes faltantes».'
                : 'Ninguna imagen coincide con el filtro.'}
            </div>
          )}

          {pagina && pagina.items.length > 0 && (
            <div className="galeria">
              {pagina.items.map((i) => {
                const huerfana = i.productos.length === 0
                const pequena = Math.min(i.ancho, i.alto) < 600
                return (
                  <figure key={i.id} className={huerfana ? 'huerfana' : ''}>
                    <img src={`/imagenes/${i.sha256}/miniatura_300`} alt="" loading="lazy" />
                    <figcaption>
                      <span className="dim">
                        {i.ancho}×{i.alto} · {tamano(i.bytes)} · {i.formato}
                        {pequena && <span className="pastilla bloqueante" style={{ marginLeft: 6 }}>pequeña</span>}
                      </span>
                      {/* Cada producto que la usa es un enlace a su vista previa,
                          que es donde se sube, se quita o se elige portada. */}
                      <div className="etiquetas">
                        {huerfana
                          ? <span className="pastilla aviso">Sin producto</span>
                          : i.productos.map((p) => (
                            <button key={p.variante_id} type="button" className="enlace"
                              title={`${p.nombre}${p.principal ? ' · portada' : ''}`}
                              onClick={() => onVer?.(p.variante_id)}>
                              {p.principal ? '★ ' : ''}{p.sku || p.nombre}
                            </button>
                          ))}
                      </div>
                      {huerfana && (
                        <div className="acciones">
                          <button onClick={() => void borrar(i.id)} disabled={borrando === i.id}>
                            {borrando === i.id ? 'Borrando…' : 'Borrar del disco'}
                          </button>
                        </div>
                      )}
                    </figcaption>
                  </figure>
                )
              })}
            </div>
          )}

          {pagina && total > POR_PAGINA && (
            <div className="paginacion">
              <button onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))} disabled={offset === 0 || cargando}>
                Anterior
              </button>
              <span className="tenue">{num(desde)}–{num(hasta)} de {num(total)}</span>
              <button onClick={() => setOffset(offset + POR_PAGINA)} disabled={hasta >= total || cargando}>
                Siguiente
              </button>
            </div>
          )}
        </div>
      </section>

      <div className="nota-previa">
        Las fotos se suben, se quitan y se eligen como portada desde cada producto:
        pulsa su referencia aquí o ábrelo desde <strong>Productos</strong>. Aquí solo
        se borran las que no usa nadie. «Buscar imágenes faltantes» recorre el
        catálogo entero buscando en internet por SKU.
      </div>
    </>
  )
}

function Cifra({ etiqueta, valor, pie, tono }: {
  etiqueta: string; valor: string; pie?: string; tono?: 'ok' | 'error'
}) {
  return (
    <div className="tarjeta">
      <div className="etiqueta">{etiqueta}</div>
      <div className={`valor ${tono ?? ''}`}>{valor}</div>
      {pie && <div className="pie">{pie}</div>}
    </div>
  )
}
