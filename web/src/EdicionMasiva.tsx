import { useState } from 'react'
import { confirmar } from './escritorio/Dialogos'
import { api, money, num, type OperacionMasiva, type ResultadoMasivo } from './api'
import { Guia } from './Guia'
import { PASOS_EDICION_MASIVA } from './guias/edicionMasiva'

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
  const [ocupado, setOcupado] = useState<'simulando' | 'aplicando' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [ayuda, setAyuda] = useState(false)

  const seleccion = alcance === 'seleccion' ? { ids } : { filtro }
  const cuantos = alcance === 'seleccion' ? ids.length : totalFiltro

  const usaFactor = tipo === 'precio_desde_coste' || tipo === 'precio_ajustar'
  const factorNum = Number(factor.replace(',', '.'))
  const valorNum = Number(valor.replace(',', '.'))

  // Un factor vacío o con letras se convertía en 0 sin decir nada, y un factor
  // 0 pone a cero el precio de cientos de productos. Se bloquea antes.
  const problemaEntrada = usaFactor && !(factorNum > 0)
    ? 'Escribe un factor mayor que cero. Por ejemplo 1.35.'
    : tipo === 'precio_fijo' && !(valorNum > 0)
      ? 'Escribe un precio mayor que cero.'
      : null

  // Se escribe en palabras lo que va a pasar, para el aviso de confirmación y
  // para que el operador lo lea antes de pulsar, no después.
  const QUE_HACE: Record<OperacionMasiva['tipo'], string> = {
    precio_desde_coste: `poner el precio en el coste × ${factor || '?'}`,
    precio_ajustar: `multiplicar el precio actual por ${factor || '?'}`,
    precio_fijo: `poner el precio en ${valorNum > 0 ? money(valorNum) : '?'}`,
    precio_borrar: 'borrar el precio',
    marca: valor.trim() ? `asignar la marca «${valor.trim()}»` : 'quitar la marca',
    excluir: 'excluir del catálogo, con lo que dejan de publicarse',
    incluir: 'devolver al catálogo',
  }

  // Cualquier cambio en los ajustes invalida la simulación: aplicar una previa
  // calculada con otros números sería aplicar algo que nadie vio.
  function ajustar(fn: () => void) {
    fn()
    setPrevia(null)
    setError(null)
  }

  function operacion(simular: boolean): OperacionMasiva {
    return {
      tipo, simular, redondeo,
      factor: factorNum > 0 ? factorNum : 0,
      // La coma decimal solo se normaliza en el precio: una marca puede
      // llevar coma de verdad y cambiársela sería corromper el dato.
      valor: tipo === 'precio_fijo' ? valor.replace(',', '.') : valor,
    }
  }

  async function simular() {
    setOcupado('simulando')
    setError(null)
    try {
      setPrevia(await api.editarMasivo(seleccion, operacion(true)))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setPrevia(null)
    } finally {
      setOcupado(null)
    }
  }

  async function aplicar() {
    if (!previa) return
    const aviso = `Se va a ${QUE_HACE[tipo]} en ${num(previa.afectados)} productos.\n\n` +
      'Esta operación no se puede deshacer. ¿Continuar?'
    if (!(await confirmar(aviso))) return

    setOcupado('aplicando')
    setError(null)
    try {
      await api.editarMasivo(seleccion, operacion(false))
      onAplicado()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setOcupado(null)
    }
  }

  const esPrecio = tipo.startsWith('precio')

  return (
    // Con una operación en curso el fondo no cierra: perder el diálogo a mitad
    // de un guardado deja al operador sin saber si se aplicó o no.
    <div className="capa" onClick={() => { if (ocupado === null) onCerrar() }}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera" data-guia="masa-cabecera">
          <div>
            <h2>Editar en masa</h2>
            <div className="sub">
              {cuantos === 0
                ? 'No hay ningún producto al que aplicar la operación'
                : `${num(cuantos)} productos afectados`}
            </div>
          </div>
          <div className="grupo-acciones">
            <button className="mini" type="button" title="Cómo funciona esta pantalla"
              aria-label="Cómo funciona esta pantalla" onClick={() => setAyuda(true)}>?</button>
            <button onClick={onCerrar} disabled={ocupado !== null}>Cerrar ✕</button>
          </div>
        </header>

        {error && <div className="aviso-caja">No se pudo completar: {error}</div>}

        <div className="form-edicion">
          <label className="ancha" data-guia="masa-alcance">
            <span>A qué productos</span>
            <select value={alcance}
              onChange={(e) => ajustar(() => setAlcance(e.target.value as typeof alcance))}>
              <option value="seleccion" disabled={ids.length === 0}>
                Los {num(ids.length)} seleccionados
              </option>
              <option value="filtro">Todos los {num(totalFiltro)} del filtro actual</option>
            </select>
            {alcance === 'filtro' && totalFiltro > 0 && (
              <small className="tenue">
                Alcanza a los {num(totalFiltro)} productos del filtro, no solo a los de esta página.
              </small>
            )}
          </label>

          <label className="ancha" data-guia="masa-tipo">
            <span>Qué hacer</span>
            <select value={tipo}
              onChange={(e) => ajustar(() => {
                setTipo(e.target.value as typeof tipo)
                // El campo de texto se comparte entre «precio fijo» y «marca»;
                // sin limpiarlo se acaba asignando 119900 como nombre de marca.
                setValor('')
              })}>
              <option value="precio_desde_coste">Precio = coste × factor</option>
              <option value="precio_ajustar">Ajustar el precio actual × factor</option>
              <option value="precio_fijo">Poner un precio fijo</option>
              <option value="precio_borrar">Borrar el precio</option>
              <option value="marca">Asignar marca</option>
              <option value="excluir">Excluir del catálogo</option>
              <option value="incluir">Devolver al catálogo</option>
            </select>
          </label>

          {usaFactor && (
            <label data-guia="masa-factor">
              <span>Factor</span>
              <input inputMode="decimal" value={factor}
                onChange={(e) => ajustar(() => setFactor(e.target.value))} />
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
                onChange={(e) => ajustar(() => setValor(e.target.value))} />
              <small className="tenue">En pesos, sin puntos de miles.</small>
            </label>
          )}

          {tipo === 'marca' && (
            <label>
              <span>Marca</span>
              <input value={valor} placeholder="Bosch"
                onChange={(e) => ajustar(() => setValor(e.target.value))} />
              <small className="tenue">Déjalo vacío para quitarles la marca.</small>
            </label>
          )}

          {esPrecio && tipo !== 'precio_borrar' && (
            <label data-guia="masa-redondeo">
              <span>Redondeo</span>
              <select value={redondeo}
                onChange={(e) => ajustar(() => setRedondeo(Number(e.target.value)))}>
                <option value={900}>Comercial — termina en 900</option>
                <option value={100}>Al centenar</option>
                <option value={1000}>Al millar</option>
                <option value={0}>Sin redondeo</option>
              </select>
            </label>
          )}
        </div>

        {problemaEntrada && <div className="nota-previa">{problemaEntrada}</div>}

        {!previa && !problemaEntrada && cuantos > 0 && (
          <div className="nota-previa">
            Se va a <strong>{QUE_HACE[tipo]}</strong>. Pulsa «Ver qué cambiaría» para
            comprobarlo sobre una muestra antes de guardar nada.
          </div>
        )}

        {previa && (
          <div className="nota-previa">
            <strong>{num(previa.afectados)} productos cambiarían.</strong>
            {previa.omitidos > 0 && (
              <> {num(previa.omitidos)} se omiten: {previa.motivo_omision}.</>
            )}
            {previa.afectados === 0 && (
              <> Con estos ajustes no cambia nada; prueba con otro alcance u otra operación.</>
            )}
            {previa.muestra && previa.muestra.length > 0 && (
              <>
                <table className="tabla-lineas tabla-tarjetas">
                  <thead>
                    <tr><th>SKU</th><th>Producto</th><th className="num">Antes</th><th className="num">Después</th></tr>
                  </thead>
                  <tbody>
                    {previa.muestra.map((m) => (
                      <tr key={m.sku}>
                        <td className="sku" data-etiqueta="SKU">{m.sku}</td>
                        <td className="apilada" data-etiqueta="Producto">
                          {m.nombre.length > 40 ? `${m.nombre.slice(0, 40)}…` : m.nombre}
                        </td>
                        <td className="num tenue" data-etiqueta="Antes">{m.antes === null ? '—' : money(m.antes)}</td>
                        <td className="num" data-etiqueta="Después"><strong>{m.despues === null ? '—' : money(m.despues)}</strong></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {previa.muestra.length < previa.afectados && (
                  <div className="mini-texto">
                    Muestra de {num(previa.muestra.length)} de los {num(previa.afectados)} productos que cambiarían.
                  </div>
                )}
              </>
            )}
          </div>
        )}

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={ocupado !== null}>Cancelar</button>
          <button onClick={() => void simular()} data-guia="masa-simular"
            disabled={ocupado !== null || cuantos === 0 || problemaEntrada !== null}
            title={problemaEntrada ?? ''}>
            {ocupado === 'simulando' ? 'Calculando…' : 'Ver qué cambiaría'}
          </button>
          <button className="primario" onClick={() => void aplicar()} data-guia="masa-aplicar"
            disabled={ocupado !== null || !previa || previa.afectados === 0}
            title={previa ? '' : 'Primero mira qué cambiaría'}>
            {ocupado === 'aplicando'
              ? 'Aplicando…'
              : `Aplicar a ${num(previa?.afectados ?? cuantos)}`}
          </button>
        </footer>

        {ayuda && <Guia pasos={PASOS_EDICION_MASIVA} nombre="Editar en masa" onCerrar={() => setAyuda(false)} />}
      </div>
    </div>
  )
}
