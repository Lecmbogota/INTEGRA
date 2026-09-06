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
      setError(e instanceof Error ? e.message : String(e))
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
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setOcupado(null)
    }
  }

  async function aplicar() {
    if (!archivo) return
    setOcupado('aplicar')
    setError(null)
    try {
      const r = await api.cargarPlantilla(archivo, true)
      setPrevia(r)
      if (r.aplicado) onAplicado()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setOcupado(null)
    }
  }

  const problemas = previa?.problemas ?? []
  const puedeAplicar = !!previa && problemas.length === 0 &&
    (previa.precios > 0 || previa.promociones > 0) && !previa.aplicado

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-plantilla" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Actualización masiva por plantilla</h2>
            <div className="sub">Precios y promociones de muchos productos a la vez, desde Excel</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">{error}</div>}

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
              onChange={(e) => { const f = e.target.files?.[0]; if (f) void revisar(f) }} />
            <button onClick={() => entrada.current?.click()} disabled={ocupado !== null}>
              {ocupado === 'revision' ? 'Revisando…' : archivo ? `Cambiar archivo (${archivo.name})` : 'Elegir archivo…'}
            </button>
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
                    <table className="tabla-lineas">
                      <thead><tr><th>Fila</th><th>SKU</th><th>Qué pasa</th></tr></thead>
                      <tbody>
                        {problemas.slice(0, 200).map((p, i) => (
                          <tr key={i}>
                            <td className="num">{p.fila}</td>
                            <td className="sku">{p.sku || '—'}</td>
                            <td>{p.mensaje}</td>
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
                  <table className="tabla-lineas">
                    <thead>
                      <tr>
                        <th>SKU</th><th>Producto</th><th className="num">Precio</th>
                        <th>Promoción</th><th>Vigencia</th>
                      </tr>
                    </thead>
                    <tbody>
                      {previa.cambios.slice(0, 200).map((c) => (
                        <tr key={c.fila}>
                          <td className="sku">{c.sku}</td>
                          <td className="mini-texto">{c.nombre}</td>
                          <td className="num">
                            {c.precio_despues !== null ? (
                              <>
                                <span className="tachado">{money(c.precio_antes)}</span>{' '}
                                <strong>{money(c.precio_despues)}</strong>
                              </>
                            ) : <span className="tenue">sin cambio</span>}
                          </td>
                          <td>
                            {c.promo_precio !== null
                              ? <>{money(c.promo_precio)} <span className="tenue">en {c.promo_canal}</span></>
                              : <span className="tenue">—</span>}
                          </td>
                          <td className="tenue mini-texto">
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
          <button onClick={onCerrar}>{previa?.aplicado ? 'Cerrar' : 'Cancelar'}</button>
          {puedeAplicar && (
            <button className="primario" onClick={() => void aplicar()} disabled={ocupado !== null}>
              {ocupado === 'aplicar'
                ? 'Aplicando…'
                : `Aplicar ${num(previa!.precios + previa!.promociones)} cambios`}
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
