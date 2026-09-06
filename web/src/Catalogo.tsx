import { useEffect, useState } from 'react'
import { api, money, motivo, num, type Categoria, type Marca, type PaginaProductos, type Producto } from './api'
import { Editar } from './Editar'
import { EdicionMasiva } from './EdicionMasiva'
import { PlantillaMasiva } from './PlantillaMasiva'

const POR_PAGINA = 50
// Motivos que impiden publicar. Un producto con cualquiera de ellos no sale al
// canal, así que la pantalla los marca distinto de los que solo empeoran la
// ficha.
const BLOQUEANTES = new Set([
  'missing_sku', 'duplicate_sku', 'missing_description', 'missing_price', 'price_below_cost',
])
const bloqueante = (m: string) => BLOQUEANTES.has(m)

export function Catalogo({ marcas, categorias = [], onVer, onCambio }: {
  marcas: Marca[]
  categorias?: Categoria[]
  onVer: (varianteId: number) => void
  onCambio: () => void
}) {
  const [pagina, setPagina] = useState<PaginaProductos | null>(null)
  const [busqueda, setBusqueda] = useState('')
  const [marca, setMarca] = useState('')
  const [categoria, setCategoria] = useState('')
  const [soloProblemas, setSoloProblemas] = useState(false)
  const [verExcluidos, setVerExcluidos] = useState(false)
  const [sinPrecio, setSinPrecio] = useState(false)
  const [offset, setOffset] = useState(0)
  const [editando, setEditando] = useState<Producto | null>(null)
  const [version, setVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [cargando, setCargando] = useState(true)
  const [marcados, setMarcados] = useState<Set<number>>(new Set())
  const [masiva, setMasiva] = useState(false)
  const [plantilla, setPlantilla] = useState(false)

  const filtroActual = {
    q: busqueda, marca, categoria, problemas: soloProblemas,
    excluidos: verExcluidos, sin_precio: sinPrecio,
  }
  const hayFiltro = !!(busqueda || marca || categoria || soloProblemas || verExcluidos || sinPrecio)
  const alternar = (id: number) => {
    const s = new Set(marcados)
    s.has(id) ? s.delete(id) : s.add(id)
    setMarcados(s)
  }
  const items = pagina?.items ?? []
  // `every` sobre una lista vacía da true, y eso dejaría la casilla de
  // «seleccionar todo» marcada en una página sin productos.
  const paginaEntera = items.length > 0 && items.every((p) => marcados.has(p.id))
  const alternarPagina = () => {
    const s = new Set(marcados)
    for (const p of items) paginaEntera ? s.delete(p.id) : s.add(p.id)
    setMarcados(s)
  }

  // La búsqueda se retrasa 300 ms para no lanzar una consulta por tecla.
  // `vigente` descarta la respuesta de una consulta que ya quedó atrás: al
  // teclear rápido hay varias en vuelo y no siempre vuelven en orden, así que
  // sin esto la lista puede quedarse mostrando el resultado de un texto viejo.
  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.productos({
        q: busqueda, marca, categoria, problemas: soloProblemas,
        excluidos: verExcluidos, sin_precio: sinPrecio,
        limite: POR_PAGINA, offset,
      })
        .then((p) => {
          if (!vigente) return
          setPagina(p)
          // Un error de hace dos búsquedas no debe seguir en pantalla cuando
          // la siguiente ya trajo datos buenos.
          setError(null)
        })
        .catch((e) => {
          if (!vigente) return
          setError(e instanceof Error ? e.message : String(e))
        })
        .finally(() => { if (vigente) setCargando(false) })
    }, 300)
    return () => { vigente = false; clearTimeout(t) }
  }, [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio, offset, version])

  // Al cambiar un filtro se vuelve a la primera página: quedarse en la página 7
  // de un resultado que ahora tiene 2 muestra una tabla vacía sin explicación.
  // Cambiar de filtro limpia la selección: aplicar una operación a productos
  // que ya no se ven en pantalla sería una sorpresa desagradable.
  useEffect(() => { setOffset(0); setMarcados(new Set()) },
    [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio])

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Productos</h1>
          <div className="sub">
            {verExcluidos
              ? 'Fuera del catálogo — gastos, activos fijos y servicios'
              : 'Precio, marca, descripción e imágenes son de Integra; Odoo solo aporta SKU, nombre y stock'}
          </div>
        </div>
      </header>

      {error && (
        <div className="aviso-caja">
          <div className="fila">
            <span className="expande-recorta">No se pudo cargar la lista: {error}</span>
            <button onClick={() => setVersion((v) => v + 1)}>Reintentar</button>
          </div>
        </div>
      )}

      <section className="panel">
        <div className="cuerpo">
          <div className="filtros">
            <input className="crece" placeholder="Buscar por nombre o referencia…"
              value={busqueda} onChange={(e) => setBusqueda(e.target.value)} />
            <select value={marca} onChange={(e) => setMarca(e.target.value)}>
              <option value="">Todas las marcas</option>
              {marcas.filter((m) => m.cantidad > 0).map((m) => (
                <option key={m.codigo} value={m.codigo}>{m.nombre} ({m.cantidad})</option>
              ))}
            </select>
            <select value={categoria} onChange={(e) => setCategoria(e.target.value)}>
              <option value="">Todas las categorías</option>
              {categorias.filter((c) => c.cantidad > 0).map((c) => (
                <option key={c.nombre} value={c.nombre}>{c.nombre} ({c.cantidad})</option>
              ))}
            </select>
            <label className="casilla">
              <input type="checkbox" checked={soloProblemas}
                onChange={(e) => setSoloProblemas(e.target.checked)} />
              Solo con problemas
            </label>
            <label className="casilla">
              <input type="checkbox" checked={sinPrecio}
                onChange={(e) => setSinPrecio(e.target.checked)} />
              Sin precio
            </label>
            <label className="casilla">
              <input type="checkbox" checked={verExcluidos}
                onChange={(e) => setVerExcluidos(e.target.checked)} />
              Ver excluidos
            </label>
          </div>
        </div>

        {(marcados.size > 0 || (pagina?.total ?? 0) > 0) && (
          <div className="barra-masiva">
            <span className="crece">
              {marcados.size > 0
                ? `${num(marcados.size)} seleccionados`
                : cargando
                  ? 'Buscando…'
                  : `${num(pagina?.total ?? 0)} productos en el filtro actual`}
            </span>
            {/* En el móvil no hay cabecera de tabla, así que la casilla de
                «seleccionar toda la página» desaparece; este botón la sustituye. */}
            {items.length > 0 && (
              <button className="solo-movil" onClick={alternarPagina}>
                {paginaEntera ? 'Quitar esta página' : 'Marcar esta página'}
              </button>
            )}
            {marcados.size > 0 && (
              <button onClick={() => setMarcados(new Set())}>Limpiar selección</button>
            )}
            <button onClick={() => setPlantilla(true)}
              title="Descarga el catálogo como hoja de Excel, edítalo y súbelo para actualizar precios y promociones de muchos productos a la vez.">
              Actualizar por plantilla
            </button>
            <button className="primario" onClick={() => setMasiva(true)}>
              Editar en masa
            </button>
          </div>
        )}

        <div className="tabla-envoltorio">
          <table className="tabla-tarjetas">
            <thead>
              <tr>
                <th className="col-check">
                  <input type="checkbox" checked={paginaEntera}
                    title="Seleccionar los de esta página"
                    onChange={alternarPagina} />
                </th>
                <th className="oculto-movil">Referencia</th><th>Producto</th>
                <th className="oculto-movil">Marca</th>
                <th className="num">Precio</th><th className="num">Stock</th>
                <th>Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {pagina?.items.map((p) => (
                <tr key={p.id} className={`clicable ${marcados.has(p.id) ? 'marcada' : ''}`}
                  onClick={() => onVer(p.id)}
                  title="Ver cómo quedaría en cada canal">
                  <td className="col-check" onClick={(e) => e.stopPropagation()}>
                    <label className="casilla">
                      <input type="checkbox" checked={marcados.has(p.id)}
                        onChange={() => alternar(p.id)} />
                      {/* En tarjeta la casilla queda suelta sin nada que la
                          nombre; en la tabla el encabezado ya lo dice. */}
                      <span className="solo-movil mini-texto tenue">Seleccionar</span>
                    </label>
                  </td>
                  <td className="sku oculto-movil">{p.sku || <span className="tenue">—</span>}</td>
                  <td className="titulo-tarjeta">
                    <div>{p.nombre}</div>
                    {p.categoria && <div className="categoria">{p.categoria}</div>}
                    {/* La columna Referencia se esconde en el móvil, pero saber
                        qué SKU es sigue siendo lo primero que se busca. */}
                    <div className="solo-movil mini-texto tenue">
                      Ref. {p.sku || '—'}{p.marca ? ` · ${p.marca}` : ''}
                    </div>
                  </td>
                  <td className="oculto-movil">{p.marca || <span className="tenue">—</span>}</td>
                  <td className="num" data-etiqueta="Precio">
                    {p.precio !== null
                      ? <strong>{money(p.precio)}</strong>
                      : p.precio_sugerido !== null
                        ? <span className="tenue" title="Sugerencia según tarifas de Odoo; asígnalo al editar">
                            ({money(p.precio_sugerido)}) sugerido
                          </span>
                        : <span className="tenue">Sin precio</span>}
                  </td>
                  <td className="num" data-etiqueta="Stock">{num(p.stock)}</td>
                  <td className="apilada" data-etiqueta="Estado">
                    <div className="etiquetas">
                      {p.problemas.length === 0
                        ? <span className="pastilla ok">Listo</span>
                        : p.problemas.map((m) => (
                          <span key={m} className={`pastilla ${bloqueante(m) ? 'bloqueante' : 'aviso'}`}>
                            {motivo(m)}
                          </span>
                        ))}
                    </div>
                  </td>
                  <td className="acciones-fila">
                    {/* Tocar la tarjeta abre la vista previa, pero eso no se ve;
                        en el móvil el botón lo hace explícito. */}
                    <button className="solo-movil"
                      onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>
                      Ver en canales
                    </button>
                    <button onClick={(e) => { e.stopPropagation(); setEditando(p) }}
                      title="Editar precio, marca y descripción">
                      Editar
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {pagina === null && cargando && !error && (
            <div className="vacio">Cargando productos…</div>
          )}
          {pagina && pagina.items.length === 0 && !cargando && (
            <div className="vacio">
              {hayFiltro
                ? 'Ningún producto coincide con el filtro. Prueba a quitar alguna condición.'
                : 'Todavía no hay productos. Sincroniza con Odoo para traer el catálogo.'}
            </div>
          )}
        </div>

        {pagina && pagina.total > 0 && (
          <div className="paginacion">
            <button disabled={offset === 0 || cargando}
              onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))}>
              ← Anterior
            </button>
            <span className="tenue">
              {pagina.total === 0 ? 0 : offset + 1}–{Math.min(offset + POR_PAGINA, pagina.total)} de {num(pagina.total)}
            </span>
            <button disabled={offset + POR_PAGINA >= pagina.total || cargando}
              onClick={() => setOffset(offset + POR_PAGINA)}>
              Siguiente →
            </button>
          </div>
        )}
      </section>

      {plantilla && (
        <PlantillaMasiva filtro={filtroActual} total={pagina?.total ?? 0}
          onCerrar={() => setPlantilla(false)}
          onAplicado={() => {
            // No se cierra el diálogo al aplicar: el resumen de lo que cambió
            // es lo que el operador necesita leer justo después.
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {masiva && (
        <EdicionMasiva ids={[...marcados]} filtro={filtroActual}
          totalFiltro={pagina?.total ?? 0}
          onCerrar={() => setMasiva(false)}
          onAplicado={() => {
            setMasiva(false)
            setMarcados(new Set())
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {editando !== null && (
        <Editar producto={editando} marcas={marcas}
          onCerrar={() => setEditando(null)}
          onGuardado={() => {
            setEditando(null)
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}
    </>
  )
}
