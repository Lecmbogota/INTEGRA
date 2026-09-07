import { useEffect, useState } from 'react'
import { api, num, type FiltroMediateca, type ImagenBanco, type PaginaImagenes } from './api'

// SelectorMediateca: elegir fotos que ya están en el banco para un producto.
//
// Es la tercera forma de darle fotos a un producto, junto a subirlas del PC
// y buscarlas en internet, y la que evita subir dos veces lo mismo: la foto
// del fabricante que ya se usó en otra variante, o la buena que una búsqueda
// dejó huérfana. Se marcan varias y se enlazan de una vez.

const FILTROS: [FiltroMediateca, string][] = [
  ['todas', 'Todas'], ['aptas', 'Aptas'], ['huerfanas', 'Huérfanas'], ['duplicadas', 'Duplicadas'],
]

export function SelectorMediateca({ varianteId, onCerrar, onElegidas }: {
  varianteId: number
  onCerrar: () => void
  onElegidas: (cuantas: number) => void
}) {
  const [filtro, setFiltro] = useState<FiltroMediateca>('todas')
  const [q, setQ] = useState('')
  const [pagina, setPagina] = useState<PaginaImagenes | null>(null)
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [marcadas, setMarcadas] = useState<Set<number>>(new Set())
  const [ocupado, setOcupado] = useState(false)

  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.banco({ filtro, q, orden: 'recientes', limite: 80 })
        .then((p) => { if (vigente) { setPagina(p); setError(null) } })
        .catch((e) => { if (vigente) setError(e instanceof Error ? e.message : String(e)) })
        .finally(() => { if (vigente) setCargando(false) })
    }, 250)
    return () => { vigente = false; clearTimeout(t) }
  }, [filtro, q])

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !ocupado) onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar, ocupado])

  const yaEsta = (i: ImagenBanco) => i.productos.some((p) => p.variante_id === varianteId)

  function alternar(id: number) {
    setMarcadas((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id); else n.add(id)
      return n
    })
  }

  async function anadir() {
    const ids = [...marcadas]
    if (ids.length === 0) return
    setOcupado(true)
    setError(null)
    try {
      const r = await api.asociarVariasDelBanco(ids, varianteId, false)
      onElegidas(r.asociadas)
    } catch (e) {
      setError(`No se pudieron añadir: ${e instanceof Error ? e.message : String(e)}`)
      setOcupado(false)
    }
  }

  const items = pagina?.items ?? []

  return (
    <div className="capa" onClick={() => { if (!ocupado) onCerrar() }}>
      <div className="hoja hoja-ancha" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Elegir fotos de la mediateca</h2>
            <div className="sub">Las que ya están en el banco de Integra: no hace falta volver a subirlas.</div>
          </div>
          <button onClick={onCerrar} disabled={ocupado}>Cerrar ✕</button>
        </header>

        <div className="selector-mediateca">
          <div className="filtros">
            <div className="grupo-badges">
              {FILTROS.map(([id, nombre]) => (
                <button key={id} type="button" className={`badge ${filtro === id ? 'activo' : ''}`}
                  onClick={() => setFiltro(id)}>{nombre}</button>
              ))}
            </div>
            <input type="search" className="crece" autoFocus placeholder="Buscar por SKU o nombre del producto que las usa"
              value={q} onChange={(e) => setQ(e.target.value)} />
          </div>

          {error && <div className="aviso-caja">{error}</div>}
          {cargando && !pagina && <div className="vacio">Cargando el banco…</div>}
          {pagina && items.length === 0 && !cargando && <div className="vacio">Ninguna foto coincide.</div>}

          {items.length > 0 && (
            <div className="rejilla-mini">
              {items.map((i) => {
                const bloqueada = yaEsta(i)
                const marcada = marcadas.has(i.id)
                const pequena = Math.min(i.ancho, i.alto) < 600
                return (
                  <div key={i.id} className={`celda-mini ${marcada ? 'marcada' : ''} ${bloqueada ? 'bloqueada' : ''}`}
                    title={bloqueada ? 'Este producto ya la tiene' : i.productos.map((p) => p.sku || p.nombre).join(', ') || 'Sin producto'}
                    onClick={() => { if (!bloqueada) alternar(i.id) }}>
                    <img src={`/imagenes/${i.sha256}/miniatura_300`} alt="" loading="lazy" />
                    <input type="checkbox" className="marca-sel" checked={marcada} disabled={bloqueada} readOnly />
                    <div className="pie">
                      <span>{i.ancho}×{i.alto}</span>
                      {bloqueada
                        ? <span className="pastilla ok">ya está</span>
                        : i.productos.length === 0
                          ? <span className="pastilla aviso">huérfana</span>
                          : <span className="tenue">{i.productos[0].sku || i.productos[0].nombre}{i.productos.length > 1 ? ` +${i.productos.length - 1}` : ''}</span>}
                      {pequena && <span className="pastilla bloqueante">pequeña</span>}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
          {pagina && pagina.total > items.length && (
            <div className="tenue mini-texto">Se muestran {num(items.length)} de {num(pagina.total)}: busca para afinar.</div>
          )}
        </div>

        <div className="hoja-pie">
          <span className="tenue crece">{marcadas.size === 0 ? 'Marca las fotos que quieras añadir' : `${num(marcadas.size)} marcadas`}</span>
          <button onClick={onCerrar} disabled={ocupado}>Cancelar</button>
          <button className="primario" disabled={marcadas.size === 0 || ocupado} onClick={() => void anadir()}>
            {ocupado ? 'Añadiendo…' : `Añadir ${marcadas.size > 0 ? num(marcadas.size) : ''} al producto`}
          </button>
        </div>
      </div>
    </div>
  )
}
