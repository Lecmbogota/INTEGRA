import { useCallback, useEffect, useState } from 'react'
import { api, money, type EdicionProducto, type Marca, type Producto } from './api'
import { Promocion } from './Promocion'

// Límite de caracteres del título en cada canal. MercadoLibre es el que
// aprieta: 60 caracteres obligan a decidir qué sobra, y el título es lo
// primero (a veces lo único) que lee un comprador.
const LIMITES: Record<string, { nombre: string; max: number }> = {
  mercadolibre: { nombre: 'MercadoLibre', max: 60 },
  falabella: { nombre: 'Falabella', max: 150 },
  woocommerce: { nombre: 'WooCommerce', max: 255 },
  shopify: { nombre: 'Shopify', max: 255 },
}

export type Pestana = 'venta' | 'promocion' | 'titulos' | 'envio' | 'interno'

// Un campo mal escrito vive en una pestaña concreta, y el error se enseñaba
// sin decir en cuál: quien tecleaba mal el peso lo leía desde «Títulos» y no
// encontraba qué corregir. Por eso el fallo viaja con la pestaña que lo tiene.
type Recolectado =
  | { ok: true; campos: EdicionProducto }
  | { ok: false; pestana: Pestana; mensaje: string }

// Editor de los campos propiedad de Integra. De Odoo solo llegan la
// referencia, el nombre y el stock; todo lo demás vive aquí.
export function Editar({ producto, marcas, onCerrar, onGuardado, pestanaInicial }: {
  producto: Producto
  marcas: Marca[]
  onCerrar: () => void
  onGuardado: () => void
  // Quien llega desde «falta el peso» tiene que caer en la casilla del peso,
  // no en la primera pestaña a buscarla.
  pestanaInicial?: Pestana
}) {
  const [pestana, setPestana] = useState<Pestana>(pestanaInicial ?? 'venta')

  const [precio, setPrecio] = useState(producto.precio === null ? '' : String(producto.precio))
  const [marca, setMarca] = useState(producto.marca)
  const [descripcion, setDescripcion] = useState(producto.descripcion)
  const [barcode, setBarcode] = useState(producto.barcode)
  const [peso, setPeso] = useState(producto.peso > 0 ? String(producto.peso) : '')
  const [excluido, setExcluido] = useState(producto.excluido)
  const [condicion, setCondicion] = useState<string>(producto.condicion || 'nuevo')
  const [garantiaMeses, setGarantiaMeses] = useState(
    producto.garantia_meses === null ? '' : String(producto.garantia_meses))
  const [garantiaTipo, setGarantiaTipo] = useState(producto.garantia_tipo || '')
  const [videoURL, setVideoURL] = useState(producto.video_url || '')
  const [nota, setNota] = useState(producto.nota_interna || '')
  const [dim, setDim] = useState({
    largo: producto.largo_cm > 0 ? String(producto.largo_cm) : '',
    ancho: producto.ancho_cm > 0 ? String(producto.ancho_cm) : '',
    alto: producto.alto_cm > 0 ? String(producto.alto_cm) : '',
  })
  const [titulos, setTitulos] = useState<Record<string, string>>(producto.titulos ?? {})

  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Peso volumétrico: es lo que de verdad cobran los canales cuando supera al
  // peso real, y por eso se muestra en vivo mientras se teclean las medidas.
  const volumetrico = (() => {
    const l = Number(dim.largo), a = Number(dim.ancho), h = Number(dim.alto)
    if (!l || !a || !h) return null
    return (l * a * h) / 5000
  })()

  function numero(s: string): number | null {
    const limpio = s.trim().replace(',', '.')
    if (limpio === '') return null
    const n = Number(limpio)
    return Number.isFinite(n) && n >= 0 ? n : NaN
  }

  // recolectar es la única fuente de verdad sobre qué cambió: la usan el
  // guardado y el aviso de salida, así que no pueden contradecirse.
  function recolectar(): Recolectado {
    const campos: EdicionProducto = {}

    if (precio !== (producto.precio === null ? '' : String(producto.precio))) {
      const n = numero(precio)
      if (Number.isNaN(n)) {
        return {
          ok: false, pestana: 'venta',
          mensaje: 'El precio de venta debe ser un número positivo. Déjalo vacío para quitarlo.',
        }
      }
      campos.precio = n
    }
    if (marca !== producto.marca) campos.marca = marca.trim()
    if (descripcion !== producto.descripcion) campos.descripcion = descripcion
    if (barcode !== producto.barcode) campos.barcode = barcode.trim()
    if (excluido !== producto.excluido) campos.excluido = excluido
    if (condicion !== producto.condicion) campos.condicion = condicion
    if (garantiaTipo !== (producto.garantia_tipo || '')) campos.garantia_tipo = garantiaTipo
    if (videoURL !== (producto.video_url || '')) campos.video_url = videoURL.trim()
    if (nota !== (producto.nota_interna || '')) campos.nota_interna = nota

    for (const [clave, etiqueta, actual, previo] of [
      ['peso', 'El peso real', peso, producto.peso],
      ['largo_cm', 'El largo', dim.largo, producto.largo_cm],
      ['ancho_cm', 'El ancho', dim.ancho, producto.ancho_cm],
      ['alto_cm', 'El alto', dim.alto, producto.alto_cm],
    ] as const) {
      if (actual.trim() === (previo > 0 ? String(previo) : '')) continue
      const n = numero(actual)
      if (Number.isNaN(n)) {
        return {
          ok: false, pestana: 'envio',
          mensaje: `${etiqueta} debe ser un número positivo, o quedar vacío.`,
        }
      }
      // Vaciar la casilla es la única forma de borrar una medida ya guardada;
      // antes respondía «debe ser un número» y no había manera de deshacerla.
      ;(campos as Record<string, unknown>)[clave] = n ?? 0
    }

    if (garantiaMeses !== (producto.garantia_meses === null ? '' : String(producto.garantia_meses))) {
      const n = numero(garantiaMeses)
      if (Number.isNaN(n)) {
        return {
          ok: false, pestana: 'venta',
          mensaje: 'La garantía debe ser un número de meses, o quedar vacía.',
        }
      }
      // Antes un valor no numérico se descartaba en silencio y el usuario se
      // iba creyendo que había guardado la garantía.
      campos.garantia_meses = n === null ? 0 : Math.round(n)
    }

    const titulosCambiados: Record<string, string> = {}
    for (const canal of Object.keys(LIMITES)) {
      const nuevo = titulos[canal] ?? ''
      if (nuevo !== (producto.titulos?.[canal] ?? '')) titulosCambiados[canal] = nuevo
    }
    if (Object.keys(titulosCambiados).length > 0) campos.titulos = titulosCambiados

    return { ok: true, campos }
  }

  const recolectado = recolectar()
  // Un valor inválido también cuenta como cambio: alguien escribió algo.
  const sucio = !recolectado.ok || Object.keys(recolectado.campos).length > 0

  // Cerrar tira el formulario entero, y se cierra con Escape o tocando fuera
  // de la hoja —en un móvil eso pasa sin querer—, así que si hay algo escrito
  // se pregunta antes.
  const intentarCerrar = useCallback(() => {
    if (guardando) return
    if (sucio && !window.confirm('Hay cambios sin guardar en esta ficha. ¿Cerrar y descartarlos?')) return
    onCerrar()
  }, [guardando, sucio, onCerrar])

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') intentarCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [intentarCerrar])

  async function guardar() {
    const r = recolectar()
    if (!r.ok) { setPestana(r.pestana); setError(r.mensaje); return }
    if (Object.keys(r.campos).length === 0) { onCerrar(); return }

    setGuardando(true)
    setError(null)
    try {
      await api.editarProducto(producto.id, r.campos)
      onGuardado()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setGuardando(false)
    }
  }

  return (
    <div className="capa" onClick={intentarCerrar}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Editar producto</h2>
            <div className="sub">{producto.sku || '(sin referencia)'} · {producto.nombre}</div>
          </div>
          <button onClick={intentarCerrar} disabled={guardando}>Cerrar ✕</button>
        </header>

        <div className="pestanas">
          {([
            ['venta', 'Venta'], ['promocion', 'Promoción'], ['titulos', 'Títulos'],
            ['envio', 'Envío'], ['interno', 'Interno'],
          ] as const).map(([id, nombre]) => (
            <button key={id} className={`pestana ${pestana === id ? 'activa' : ''}`}
              onClick={() => setPestana(id)}>{nombre}</button>
          ))}
        </div>

        {error && <div className="aviso-caja">No se guardó: {error}</div>}

        {pestana === 'venta' && (
          <div className="form-edicion">
            <label>
              <span>Precio de venta (COP)</span>
              <input inputMode="decimal" placeholder="sin precio"
                value={precio} onChange={(e) => setPrecio(e.target.value)} />
              {producto.precio_sugerido !== null && (
                <small className="tenue">
                  Sugerencia según tarifas de Odoo: {money(producto.precio_sugerido)}{' '}
                  <button className="enlace" type="button"
                    onClick={() => setPrecio(String(producto.precio_sugerido))}>usar</button>
                </small>
              )}
            </label>

            <label>
              <span>Marca</span>
              <input list="marcas-conocidas" placeholder="sin marca"
                value={marca} onChange={(e) => setMarca(e.target.value)} />
              <datalist id="marcas-conocidas">
                {marcas.map((m) => <option key={m.codigo} value={m.nombre} />)}
              </datalist>
            </label>

            <label>
              <span>Condición</span>
              <select value={condicion} onChange={(e) => setCondicion(e.target.value)}>
                <option value="nuevo">Nuevo</option>
                <option value="usado">Usado</option>
                <option value="reacondicionado">Reacondicionado</option>
              </select>
            </label>

            <label>
              <span>Código de barras (EAN)</span>
              <input value={barcode} placeholder="sin código"
                onChange={(e) => setBarcode(e.target.value)} />
              <small className="tenue">Falabella lo exige para publicar</small>
            </label>

            <label>
              <span>Garantía (meses)</span>
              <input inputMode="numeric" placeholder="sin garantía"
                value={garantiaMeses} onChange={(e) => setGarantiaMeses(e.target.value)} />
            </label>

            <label>
              <span>Quién responde</span>
              <select value={garantiaTipo} onChange={(e) => setGarantiaTipo(e.target.value)}>
                <option value="">Sin especificar</option>
                <option value="fabricante">Fabricante</option>
                <option value="vendedor">Vendedor</option>
                <option value="sin_garantia">Sin garantía</option>
              </select>
            </label>

            <label className="ancha">
              <span>Descripción de venta</span>
              <textarea rows={7} placeholder="Descripción que verán los canales…"
                value={descripcion} onChange={(e) => setDescripcion(e.target.value)} />
            </label>

            <label className="casilla ancha">
              <input type="checkbox" checked={excluido}
                onChange={(e) => setExcluido(e.target.checked)} />
              Excluir del catálogo publicable
            </label>
          </div>
        )}

        {pestana === 'promocion' && (
          <>
            {/* El pie dice «Guardar cambios», pero no toca las promociones:
                cada una se crea y se cancela por su cuenta y al instante. */}
            <div className="nota-previa">
              Las promociones se guardan al crearlas, aparte del resto de la ficha.
            </div>
            <Promocion varianteId={producto.id} precioBase={producto.precio} />
          </>
        )}

        {pestana === 'titulos' && (
          <>
            <div className="nota-previa">
              Cada canal tiene su propio límite. El de MercadoLibre son 60 caracteres
              y es el que decide si el comprador te encuentra: pon primero producto,
              marca y modelo. El que dejes vacío se publica con el nombre de Odoo.
            </div>
            <div className="form-edicion">
              {Object.entries(LIMITES).map(([canal, cfg]) => {
                const valor = titulos[canal] ?? ''
                const largo = [...valor].length
                return (
                  <label key={canal} className="ancha">
                    <span>{cfg.nombre}</span>
                    <input value={valor} placeholder={producto.nombre}
                      onChange={(e) => setTitulos({ ...titulos, [canal]: e.target.value })} />
                    <small className={largo > cfg.max ? 'excede' : 'tenue'}>
                      {largo}/{cfg.max} caracteres
                      {largo > cfg.max && ' — se recortará al publicar'}
                    </small>
                  </label>
                )
              })}
            </div>
          </>
        )}

        {pestana === 'envio' && (
          <>
            <div className="nota-previa">
              Los canales cobran el envío por el <strong>peso volumétrico</strong>
              {' '}(largo × ancho × alto ÷ 5000) cuando supera al peso real. Sin medidas,
              el envío sale mal cobrado o el canal bloquea la venta. Deja una casilla
              vacía para borrar la medida guardada.
            </div>
            <div className="form-edicion">
              <label>
                <span>Peso real (kg)</span>
                <input inputMode="decimal" placeholder="sin peso"
                  value={peso} onChange={(e) => setPeso(e.target.value)} />
              </label>
              <label>
                <span>Largo (cm)</span>
                <input inputMode="decimal" placeholder="sin medida" value={dim.largo}
                  onChange={(e) => setDim({ ...dim, largo: e.target.value })} />
              </label>
              <label>
                <span>Ancho (cm)</span>
                <input inputMode="decimal" placeholder="sin medida" value={dim.ancho}
                  onChange={(e) => setDim({ ...dim, ancho: e.target.value })} />
              </label>
              <label>
                <span>Alto (cm)</span>
                <input inputMode="decimal" placeholder="sin medida" value={dim.alto}
                  onChange={(e) => setDim({ ...dim, alto: e.target.value })} />
              </label>
              {volumetrico !== null && (
                <div className="ancha nota-previa">
                  Peso volumétrico: <strong>{volumetrico.toFixed(2)} kg</strong>
                  {Number(peso) > 0 && (
                    <> · el canal cobrará por{' '}
                      <strong>{Math.max(volumetrico, Number(peso)).toFixed(2)} kg</strong></>
                  )}
                </div>
              )}
            </div>
          </>
        )}

        {pestana === 'interno' && (
          <div className="form-edicion">
            <label className="ancha">
              <span>Vídeo (YouTube)</span>
              <input value={videoURL} placeholder="https://youtube.com/watch?v=…"
                onChange={(e) => setVideoURL(e.target.value)} />
              <small className="tenue">MercadoLibre lo admite y mejora la conversión</small>
            </label>
            <label className="ancha">
              <span>Nota interna</span>
              <textarea rows={5} placeholder="Solo para el equipo…"
                value={nota} onChange={(e) => setNota(e.target.value)} />
              <small className="tenue">Nunca se publica en ningún canal</small>
            </label>
          </div>
        )}

        <footer className="hoja-pie">
          <button onClick={intentarCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando || !sucio}>
            {guardando ? 'Guardando…' : sucio ? 'Guardar cambios' : 'Sin cambios que guardar'}
          </button>
        </footer>
      </div>
    </div>
  )
}
