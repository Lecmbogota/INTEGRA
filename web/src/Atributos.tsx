import { useEffect, useState } from 'react'
import { api, num, type AtributoProducto, type ResumenAtributos } from './api'

// Atributos obligatorios: lo que MercadoLibre y Falabella exigen por
// categoría y sin lo cual rechazan la publicación.
export function Atributos() {
  const [resumenes, setResumenes] = useState<Record<string, ResumenAtributos>>({})
  // Antes un canal que respondía con error se tragaba la excepción y quedaba
  // igual que un canal sin categorías mapeadas: se leía «ve a Categorías» y
  // allí no había nada que arreglar.
  const [fallos, setFallos] = useState<Record<string, string>>({})
  const [cargando, setCargando] = useState(true)

  useEffect(() => {
    Promise.all(CANALES_CON_ATRIBUTOS.map((c) =>
      api.resumenAtributos(c.codigo)
        .then((r) => ({ codigo: c.codigo, resumen: r, error: null as string | null }))
        .catch((e) => ({
          codigo: c.codigo,
          resumen: null,
          error: e instanceof Error ? e.message : String(e),
        }))))
      .then((rs) => {
        const m: Record<string, ResumenAtributos> = {}
        const f: Record<string, string> = {}
        for (const r of rs) {
          if (r.resumen) m[r.codigo] = r.resumen
          if (r.error) f[r.codigo] = r.error
        }
        setResumenes(m)
        setFallos(f)
      })
      .finally(() => setCargando(false))
  }, [])

  return (
    <>
      <header className="principal" data-guia="attr-cabecera">
        <div>
          <h1 className="titulo-seccion">Atributos</h1>
          <div className="sub">
            Lo que cada marketplace exige por categoría — sin esto rechazan la publicación
          </div>
        </div>
      </header>

      {CANALES_CON_ATRIBUTOS.map((c) => {
        const r = resumenes[c.codigo]
        const pct = r && r.con_categoria > 0
          ? Math.round((r.completos / r.con_categoria) * 100) : 0
        return (
          <section key={c.codigo} className="panel" data-guia="attr-resumen">
            <h2>{c.nombre}</h2>
            {/* El recorrido resalta el primer attr-tarjetas que haya: el de
                MercadoLibre si tiene categorías, si no el de Falabella. */}
            <div className="cuerpo" data-guia="attr-tarjetas">
              {cargando ? (
                <div className="vacio">Cargando…</div>
              ) : fallos[c.codigo] ? (
                <div className="aviso-caja">
                  No se pudo leer el resumen de {c.nombre}: {fallos[c.codigo]}
                </div>
              ) : !r || r.con_categoria === 0 ? (
                <div className="vacio">
                  Sin categorías mapeadas a {c.nombre}. Ve a <strong>Categorías</strong> para
                  confirmarlas y luego ejecuta <code>integra atributos</code>.
                </div>
              ) : (
                <div className="tarjetas">
                  <Tarjeta etiqueta="Con categoría" valor={num(r.con_categoria)}
                    pie="productos mapeados" />
                  <Tarjeta etiqueta="Completos" valor={num(r.completos)} tono="ok"
                    destacada pie={`${pct} % — listos para publicar`} />
                  <Tarjeta etiqueta="Incompletos" valor={num(r.faltan_obligatorios)}
                    tono={r.faltan_obligatorios > 0 ? 'error' : undefined}
                    pie="les falta algún obligatorio" />
                </div>
              )}
            </div>
          </section>
        )
      })}

      <div className="nota-previa" data-guia="attr-nota-tiendas">
        <strong>Shopify y WooCommerce no aparecen</strong> porque son tiendas propias:
        no exigen atributos para publicar. Lo que allí se ve como «especificaciones»
        sale del texto del producto.
      </div>

      <div className="nota-previa" data-guia="attr-nota-deduccion">
        Los valores se deducen de lo que Integra ya sabe: la marca, el SKU y las
        especificaciones extraídas del nombre. <strong>Nunca se inventan</strong> — un
        atributo que no se puede deducir queda vacío para que lo escribas tú, porque un
        dato plausible pero falso en una ficha es peor que un hueco.
        Abre cualquier producto desde <strong>Productos</strong> para completarlos.
      </div>

      <div className="nota-previa" data-guia="attr-nota-refrescar">
        Para refrescar los requisitos desde MercadoLibre y volver a deducir, ejecuta:
        <code className="bloque-cmd">integra atributos</code>
      </div>
    </>
  )
}

// Canales que imponen atributos por categoría. Shopify y WooCommerce no
// están porque son tiendas propias: no exigen ninguno para publicar.
const CANALES_CON_ATRIBUTOS = [
  { codigo: 'mercadolibre', nombre: 'MercadoLibre' },
  { codigo: 'falabella', nombre: 'Falabella' },
]

// PanelAtributos se muestra dentro de la vista previa de un producto.
//
// Solo enseña de entrada los obligatorios y los que ya tienen valor: una
// categoría de MercadoLibre puede traer cincuenta atributos opcionales con
// nombres crípticos («AGID», «APGID») y mostrarlos todos no ayuda a nadie.
// El resto se añaden a demanda desde el buscador.
export function PanelAtributos({ varianteId }: { varianteId: number }) {
  const [canal, setCanal] = useState('mercadolibre')
  const [attrs, setAttrs] = useState<AtributoProducto[]>([])
  const [cargado, setCargado] = useState(false)
  const [guardando, setGuardando] = useState<string | null>(null)
  const [borrador, setBorrador] = useState<Record<string, string>>({})
  // Opcionales que el usuario decidió mostrar en esta sesión.
  const [anadidos, setAnadidos] = useState<Set<string>>(new Set())
  const [buscando, setBuscando] = useState(false)
  const [filtro, setFiltro] = useState('')
  const [errorGuardado, setErrorGuardado] = useState<string | null>(null)
  // Un fallo al leer se veía como «sin atributos para este producto», que manda
  // a mapear una categoría que a lo mejor ya estaba bien.
  const [errorCarga, setErrorCarga] = useState<string | null>(null)

  const cargar = () => {
    setCargado(false)
    setErrorCarga(null)
    api.atributosDe(varianteId, canal)
      .then((r) => {
        setAttrs(r.atributos)
        const b: Record<string, string> = {}
        for (const a of r.atributos) b[a.attribute_id] = a.value_name
        setBorrador(b)
        setCargado(true)
      })
      .catch((e) => {
        setAttrs([])
        setErrorCarga(e instanceof Error ? e.message : String(e))
        setCargado(true)
      })
  }
  useEffect(cargar, [varianteId, canal])

  async function guardar(a: AtributoProducto) {
    setGuardando(a.attribute_id)
    setErrorGuardado(null)
    try {
      await api.guardarAtributo(varianteId, a.attribute_id, borrador[a.attribute_id] ?? '', '', canal)
      cargar()
    } catch (e) {
      // Sin esto el fallo no se veía: el valor seguía escrito en pantalla y
      // parecía guardado. Un atributo obligatorio que en realidad falta hace
      // que el canal rechace la publicación entera, y nadie sabría por qué.
      setErrorGuardado(`No se pudo guardar «${a.nombre || a.attribute_id}»: ${
        e instanceof Error ? e.message : String(e)}`)
    } finally {
      setGuardando(null)
    }
  }

  // Visibles: obligatorios, los que ya traen valor, y los añadidos a mano.
  const visibles = attrs.filter(
    (a) => a.obligatorio || a.value_name.trim() !== '' || anadidos.has(a.attribute_id))
  const disponibles = attrs.filter(
    (a) => !a.obligatorio && a.value_name.trim() === '' && !anadidos.has(a.attribute_id))
  const coincidentes = disponibles.filter(
    (a) => a.nombre.toLowerCase().includes(filtro.toLowerCase()))
  const faltan = attrs.filter((a) => a.obligatorio && !a.value_name.trim()).length

  return (
    <div className="competencia" data-guia="attr-panel">
      {/* En el móvil las tres piezas de la cabecera no caben en una línea; con
          apila-movil se ponen una debajo de otra en vez de estrujarse. */}
      <div className="competencia-cabecera fila apila-movil">
        <strong>Atributos del canal</strong>
        <div className="pestanas pestanas-mini expande" data-guia="attr-panel-canal">
          {CANALES_CON_ATRIBUTOS.map((c) => (
            <button key={c.codigo}
              className={`pestana ${canal === c.codigo ? 'activa' : ''}`}
              onClick={() => { setCanal(c.codigo); setAnadidos(new Set()); setFiltro('') }}>
              {c.nombre}
            </button>
          ))}
        </div>
        {attrs.length > 0 && (faltan > 0
          ? <span className="pastilla bloqueante">{faltan} obligatorios sin valor</span>
          : <span className="pastilla ok">Completos</span>)}
      </div>

      {errorGuardado && <div className="aviso-caja">{errorGuardado}</div>}
      {errorCarga && (
        <div className="aviso-caja">No se pudieron leer los atributos: {errorCarga}</div>
      )}

      {!cargado && <div className="vacio">Cargando…</div>}

      {cargado && !errorCarga && attrs.length === 0 && (
        <div className="vacio">
          Sin atributos para este producto en {CANALES_CON_ATRIBUTOS.find((c) => c.codigo === canal)?.nombre}.
          {' '}Falta mapear su categoría, o aún no se han traído los requisitos del canal
          (<code>integra atributos</code>).
        </div>
      )}

      {cargado && attrs.length > 0 && (
        <>
          <table className="tabla-tarjetas">
            <thead>
              <tr><th>Atributo</th><th>Valor</th><th>Origen</th><th></th></tr>
            </thead>
            <tbody>
              {visibles.map((a) => {
                const cambiado = (borrador[a.attribute_id] ?? '') !== a.value_name
                return (
                  <tr key={a.attribute_id}>
                    <td className="titulo-tarjeta">
                      {a.nombre}
                      {a.obligatorio && <span className="obligatorio" title="Obligatorio">*</span>}
                    </td>
                    {/* En la tarjeta del móvil la celda es una caja flexible, así
                        que el campo crece hasta el borde en vez de quedarse en
                        su ancho por defecto junto a la etiqueta. */}
                    <td data-etiqueta="Valor">
                      <input className="expande" value={borrador[a.attribute_id] ?? ''}
                        aria-label={`Valor de ${a.nombre}`}
                        placeholder={a.obligatorio ? 'obligatorio' : 'opcional'}
                        onChange={(e) => setBorrador({ ...borrador, [a.attribute_id]: e.target.value })} />
                    </td>
                    <td className="tenue mini-texto" data-etiqueta="Origen">{ETIQUETA[a.origen] ?? '—'}</td>
                    <td className={cambiado ? 'acciones-fila' : ''}>
                      {cambiado && (
                        <button onClick={() => void guardar(a)} disabled={guardando === a.attribute_id}>
                          {guardando === a.attribute_id ? 'Guardando…' : 'Guardar'}
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>

          {disponibles.length > 0 && !buscando && (
            <button className="enlace anadir-attr" onClick={() => setBuscando(true)}>
              + Añadir otro atributo ({disponibles.length} opcionales disponibles)
            </button>
          )}

          {buscando && (
            <div className="selector-attr">
              <input autoFocus placeholder="Buscar atributo…" value={filtro}
                onChange={(e) => setFiltro(e.target.value)} />
              <div className="lista-attr">
                {coincidentes.slice(0, 30).map((a) => (
                  <button key={a.attribute_id} className="item-attr"
                    onClick={() => {
                      setAnadidos(new Set(anadidos).add(a.attribute_id))
                      setFiltro('')
                      setBuscando(false)
                    }}>
                    {a.nombre}
                  </button>
                ))}
                {coincidentes.length === 0 && (
                  <div className="tenue mini-texto">Ningún atributo coincide.</div>
                )}
              </div>
              <button onClick={() => { setBuscando(false); setFiltro('') }}>Cerrar</button>
            </div>
          )}
        </>
      )}
    </div>
  )
}

const ETIQUETA: Record<string, string> = {
  deducido: 'del nombre', predictor: 'sugerido', manual: 'a mano', defecto: 'por defecto', '': '—',
}

function Tarjeta(props: { etiqueta: string; valor: string; pie?: string; tono?: 'ok' | 'error'; destacada?: boolean }) {
  return (
    <div className={`tarjeta ${props.destacada ? 'destacada' : ''}`}>
      <div className="etiqueta">{props.etiqueta}</div>
      <div className={`valor ${props.tono ?? ''}`}>{props.valor}</div>
      {props.pie && <div className="pie">{props.pie}</div>}
    </div>
  )
}
