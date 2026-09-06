import { useRef, useState } from 'react'
import { api, money, num, type FiltroCatalogo, type ResultadoPlantilla } from './api'

// Actualización masiva de precios y promociones por hoja de cálculo.
//
// El flujo tiene tres pasos a propósito: descargar, subir para VER qué va a
// pasar, y solo entonces confirmar. Una plantilla puede cambiar el precio de
// cientos de productos y no hay forma de deshacer eso a mano, así que el paso
// intermedio no es una cortesía: es lo que hace la herramienta usable.
export function PlantillaMasiva({ filtro, total, onCerrar, onAplicado }: {
  filtro: FiltroCatalogo
  total: number
  onCerrar: () => void
  onAplicado: () => void
}) {
  const [archivo, setArchivo] = useState<File | null>(null)
  const [previa, setPrevia] = useState<ResultadoPlantilla | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [ocupado, setOcupado] = useState<'descarga' | 'revision' | 'aplicar' | null>(null)
  const entrada = useRef<HTMLInputElement>(null)

  const hayFiltro = !!(filtro.q || filtro.marca || filtro.categoria ||
    filtro.problemas || filtro.excluidos || filtro.sin_precio)

  async function descargar() {
    setOcupado('descarga')
    setError(null)
    try {
      await api.descargarPlantilla(filtro)
    } catch (e) {
      setError(`No se pudo generar la plantilla: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupado(null)
    }
  }

  async function revisar(f: File) {
    setArchivo(f)
    setPrevia(null)
    setError(null)
    setOcupado('revision')
    try {
      setPrevia(await api.cargarPlantilla(f, false))
    } catch (e) {
      setError(`No se pudo leer el archivo: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupado(null)
    }
  }

  async function aplicar() {
    if (!archivo || !previa) return
    const aviso = `Se van a guardar ${num(previa.precios)} precios` +
      (previa.promociones > 0 ? ` y ${num(previa.promociones)} promociones` : '') +
      '.\n\nEsta operación no se puede deshacer. ¿Continuar?'
    if (!window.confirm(aviso)) return

    setOcupado('aplicar')
    setError(null)
    try {
      const r = await api.cargarPlantilla(archivo, true)
      setPrevia(r)
      if (r.aplicado) onAplicado()
      // El servidor puede aceptar el archivo y aun así no aplicar nada; sin
      // este aviso el operador se queda mirando una pantalla que no cambió.
      else setError(r.problemas.length > 0
        ? 'El archivo tiene errores, así que no se guardó nada. Están listados abajo.'
        : 'No se guardó ningún cambio. Revisa el archivo y vuelve a intentarlo.')
    } catch (e) {
      setError(`No se pudieron aplicar los cambios: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupado(null)
    }
  }

  // Cerrar con un archivo ya revisado y sin aplicar tira a la basura el trabajo
  // de revisión; con una carga en curso, además, deja la duda de si se guardó.
  function cerrar() {
    if (ocupado !== null) return
    if (previa && !previa.aplicado &&
      !window.confirm('Se perderá la revisión del archivo y habrá que volver a subirlo. ¿Cerrar?')) return
    onCerrar()
  }

  const problemas = previa?.problemas ?? []
  const puedeAplicar = !!previa && problemas.length === 0 &&
    (previa.precios > 0 || previa.promociones > 0) && !previa.aplicado

  return (
    <div className="capa" onClick={cerrar}>
      <div className="hoja hoja-plantilla" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Actualización masiva por plantilla</h2>
            <div className="sub">Precios y promociones de muchos productos a la vez, desde Excel</div>
          </div>
          <button onClick={cerrar} disabled={ocupado !== null}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">{error}</div>}

        <p className="solo-movil mini-texto tenue">
          Este flujo pide editar una hoja de Excel: se hace mucho mejor desde un
          computador. Desde aquí puedes revisar y confirmar lo que ya preparaste.
        </p>

        <ol className="pasos">
          <li className={previa ? 'hecho' : 'activo'}>
            <div className="paso-titulo">1. Descarga la plantilla</div>
            <p className="tenue">
              Trae {num(total)} productos {hayFiltro ? 'del filtro que tienes puesto' : 'del catálogo'},
              con su precio actual y su promoción vigente ya rellenados.
              Edita solo las columnas de encabezado verde.
            </p>
            <button onClick={() => void descargar()} disabled={ocupado !== null}>
              {ocupado === 'descarga' ? 'Preparando…' : 'Descargar plantilla (.xlsx)'}
            </button>
          </li>

          <li className={previa ? 'hecho' : archivo ? 'activo' : ''}>
            <div className="paso-titulo">2. Sube el archivo editado</div>
            <p className="tenue">
              Integra lo revisa y te enseña qué va a cambiar. Todavía no se guarda nada.
            </p>
            <input ref={entrada} type="file" accept=".xlsx" style={{ display: 'none' }}
              onChange={(e) => {
                const f = e.target.files?.[0]
                // Se limpia el input para que volver a elegir el MISMO archivo
                // (corregido fuera) dispare igual el onChange.
                e.target.value = ''
                if (f) void revisar(f)
              }} />
            <button onClick={() => entrada.current?.click()} disabled={ocupado !== null}>
              {ocupado === 'revision' ? 'Revisando…' : archivo ? 'Cambiar archivo' : 'Elegir archivo…'}
            </button>
            {archivo && (
              <div className="mini-texto tenue">Archivo: {archivo.name}</div>
            )}
          </li>

          {previa && (
            <li className={previa.aplicado ? 'hecho' : 'activo'}>
              <div className="paso-titulo">3. Revisa y confirma</div>

              <div className="resumen-plantilla">
                <Cifra etiqueta="Filas con cambios" valor={previa.cambios.length} />
                <Cifra etiqueta="Precios" valor={previa.precios} />
                <Cifra etiqueta="Promociones" valor={previa.promociones} />
                <Cifra etiqueta="Errores" valor={problemas.length}
                  tono={problemas.length > 0 ? 'error' : undefined} />
              </div>

              {previa.aplicado && (
                <div className="nota-previa ok-caja">
                  <strong>Aplicado.</strong> Se actualizaron {num(previa.precios)} precios
                  {previa.promociones > 0 && <> y se programaron {num(previa.promociones)} promociones</>}.
                  {previa.promociones > 0 && ' Integra las aplicará en el canal cuando llegue su hora de inicio.'}
                </div>
              )}

              {problemas.length > 0 && (
                <>
                  <div className="nota-previa aviso-previa">
                    No se aplica nada mientras haya errores: corrige el archivo y vuelve a subirlo.
                    Se muestran las filas tal como están numeradas en Excel.
                  </div>
                  <div className="tabla-envoltorio corta">
                    <table className="tabla-lineas tabla-tarjetas">
                      <thead><tr><th>Fila</th><th>SKU</th><th>Qué pasa</th></tr></thead>
                      <tbody>
                        {problemas.slice(0, 200).map((p, i) => (
                          <tr key={i}>
                            <td className="num" data-etiqueta="Fila">{p.fila}</td>
                            <td className="sku" data-etiqueta="SKU">{p.sku || '—'}</td>
                            <td className="apilada" data-etiqueta="Qué pasa">{p.mensaje}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {problemas.length > 200 && (
                      <div className="tenue mini-texto pie-tabla">
                        …y {num(problemas.length - 200)} errores más.
                      </div>
                    )}
                  </div>
                </>
              )}

              {problemas.length === 0 && previa.cambios.length > 0 && (
                <div className="tabla-envoltorio corta">
                  <table className="tabla-lineas tabla-tarjetas">
                    <thead>
                      <tr>
                        <th>SKU</th><th>Producto</th><th className="num">Precio</th>
                        <th>Promoción</th><th className="oculto-movil">Vigencia</th>
                      </tr>
                    </thead>
                    <tbody>
                      {previa.cambios.slice(0, 200).map((c) => (
                        <tr key={c.fila}>
                          <td className="sku" data-etiqueta="SKU">{c.sku}</td>
                          <td className="mini-texto apilada" data-etiqueta="Producto">{c.nombre}</td>
                          {/* En tarjeta cada celda es una fila de dos columnas:
                              el contenido va envuelto en un solo elemento para
                              que no se reparta en trozos sueltos. */}
                          <td className="num" data-etiqueta="Precio">
                            {c.precio_despues !== null ? (
                              <div>
                                <span className="tachado">{money(c.precio_antes)}</span>{' '}
                                <strong>{money(c.precio_despues)}</strong>
                              </div>
                            ) : <span className="tenue">sin cambio</span>}
                          </td>
                          <td data-etiqueta="Promoción">
                            {c.promo_precio !== null
                              ? <div>
                                  {money(c.promo_precio)} <span className="tenue">en {c.promo_canal}</span>
                                  {/* La vigencia se esconde como columna en el
                                      móvil, pero una promoción sin fechas no se
                                      puede revisar: viaja junto al precio. */}
                                  <div className="solo-movil mini-texto tenue">
                                    {c.promo_inicia ? corta(c.promo_inicia) : 'ahora'} → {c.promo_termina ? corta(c.promo_termina) : 'sin fin'}
                                  </div>
                                </div>
                              : <span className="tenue">—</span>}
                          </td>
                          <td className="tenue mini-texto oculto-movil">
                            {c.promo_precio === null ? '—'
                              : `${c.promo_inicia ? corta(c.promo_inicia) : 'ahora'} → ${c.promo_termina ? corta(c.promo_termina) : 'sin fin'}`}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {previa.cambios.length > 200 && (
                    <div className="tenue mini-texto pie-tabla">
                      …y {num(previa.cambios.length - 200)} filas más, que también se aplicarán.
                    </div>
                  )}
                </div>
              )}

              {previa.cambios.length === 0 && problemas.length === 0 && (
                <div className="vacio">
                  El archivo no trae ningún cambio: todas las casillas editables están vacías.
                </div>
              )}
            </li>
          )}
        </ol>

        <footer className="hoja-pie">
          <button onClick={cerrar} disabled={ocupado !== null}>
            {previa?.aplicado ? 'Cerrar' : 'Cancelar'}
          </button>
          {puedeAplicar && previa && (
            <button className="primario" onClick={() => void aplicar()} disabled={ocupado !== null}>
              {ocupado === 'aplicar'
                ? 'Aplicando…'
                : `Aplicar ${num(previa.precios + previa.promociones)} cambios`}
            </button>
          )}
        </footer>
      </div>
    </div>
  )
}

function Cifra({ etiqueta, valor, tono }: { etiqueta: string; valor: number; tono?: 'error' }) {
  return (
    <div className="cifra-plantilla">
      <div className="etiqueta">{etiqueta}</div>
      <div className={`valor ${tono ?? ''}`}>{num(valor)}</div>
    </div>
  )
}

// En una tabla de doscientas filas, la fecha larga con año y segundos deja de
// ser información y pasa a ser ruido.
function corta(iso: string): string {
  return new Date(iso).toLocaleString('es-CO', {
    day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit',
  })
}
