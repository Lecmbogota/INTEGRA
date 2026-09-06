import { useEffect, useState } from 'react'
import { api, money, motivo, num, type Categoria, type Marca, type PaginaProductos, type Producto } from './api'
import { Editar } from './Editar'
import { EdicionMasiva } from './EdicionMasiva'
import { PlantillaMasiva } from './PlantillaMasiva'

const POR_PAGINA = 50
const BLOQUEANTES = new Set(['missing_sku', 'missing_description', 'missing_price'])
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
  const [marcados, setMarcados] = useState<Set<number>>(new Set())
  const [masiva, setMasiva] = useState(false)
  const [plantilla, setPlantilla] = useState(false)

  const filtroActual = {
    q: busqueda, marca, categoria, problemas: soloProblemas,
    excluidos: verExcluidos, sin_precio: sinPrecio,
  }
  const alternar = (id: number) => {
    const s = new Set(marcados)
    s.has(id) ? s.delete(id) : s.add(id)
    setMarcados(s)
  }
  const todosEnPagina = (pagina?.items ?? []).every((p) => marcados.has(p.id))

  // La búsqueda se retrasa 300 ms para no lanzar una consulta por tecla.
  useEffect(() => {
    const t = setTimeout(() => {
      api.productos({
        q: busqueda, marca, categoria, problemas: soloProblemas,
        excluidos: verExcluidos, sin_precio: sinPrecio,
        limite: POR_PAGINA, offset,
      })
        .then(setPagina)
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    }, 300)
    return () => clearTimeout(t)
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

      {error && <div className="aviso-caja">Error: {error}</div>}

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
                ? `${marcados.size} seleccionados`
                : `${pagina?.total ?? 0} productos en el filtro actual`}
            </span>
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
          <table>
            <thead>
              <tr>
                <th className="col-check">
                  <input type="checkbox" checked={todosEnPagina && (pagina?.items.length ?? 0) > 0}
                    title="Seleccionar los de esta página"
                    onChange={() => {
                      const s = new Set(marcados)
                      for (const p of pagina?.items ?? []) {
                        todosEnPagina ? s.delete(p.id) : s.add(p.id)
                      }
                      setMarcados(s)
                    }} />
                </th>
                <th>Referencia</th><th>Producto</th><th>Marca</th>
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
                    <input type="checkbox" checked={marcados.has(p.id)}
                      onChange={() => alternar(p.id)} />
                  </td>
                  <td className="sku">{p.sku || <span className="tenue">—</span>}</td>
                  <td>
                    <div>{p.nombre}</div>
                    {p.categoria && <div className="categoria">{p.categoria}</div>}
                  </td>
                  <td>{p.marca || <span className="tenue">—</span>}</td>
                  <td className="num">
                    {p.precio !== null
                      ? <strong>{money(p.precio)}</strong>
                      : p.precio_sugerido !== null
                        ? <span className="tenue" title="Sugerencia según tarifas de Odoo; asígnalo al editar">
                            ({money(p.precio_sugerido)})
                          </span>
                        : <span className="tenue">—</span>}
                  </td>
                  <td className="num">{num(p.stock)}</td>
                  <td>
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
                  <td>
                    <button onClick={(e) => { e.stopPropagation(); setEditando(p) }}
                      title="Editar precio, marca y descripción">
                      Editar
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {pagina && pagina.items.length === 0 && (
            <div className="vacio">Ningún producto coincide con el filtro.</div>
          )}
        </div>

        {pagina && (
          <div className="paginacion">
            <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))}>
              ← Anterior
            </button>
            <span className="tenue">
              {pagina.total === 0 ? 0 : offset + 1}–{Math.min(offset + POR_PAGINA, pagina.total)} de {num(pagina.total)}
            </span>
            <button disabled={offset + POR_PAGINA >= pagina.total}
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
