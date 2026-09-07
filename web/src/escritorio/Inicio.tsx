import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as KeyboardEventReact, type MouseEvent as MouseEventReact, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { AppId } from './tipos'
import { useSistema } from './sistema'
import { usePreferencias } from './preferencias'
import { APPS } from './apps'
import { useSesion } from './sesion'
import { api, type Producto } from '../api'
import { RECORRIDOS } from '../guias'
import { MenuContextual, useCerrarFuera, nombreRol, type OpcionMenu } from './Escritorio'

// El menú de inicio: buscador global (apps, productos, temas de la guía),
// apps ancladas, todas las apps y el pie con el usuario. Se pinta por portal
// al body por lo mismo que el menú contextual: el cristal de la barra
// (backdrop-filter) atraparía un `position: fixed` dentro de ella.

const RETRASO_BUSQUEDA = 250 // ms sin teclear antes de preguntar al servidor
const MAX_GUIA = 6

// Sin acentos ni mayúsculas, para que «categoria» encuentre «Categorías».
function normalizar(s: string): string {
  // \p{M}: las marcas diacríticas que NFD separa de la letra base.
  return s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
}

type Grupo = 'apps' | 'productos' | 'guia'

type Resultado = {
  clave: string
  grupo: Grupo
  icono: ReactNode
  color?: string
  titulo: string
  detalle?: string
  accion: () => void
}

const TITULOS: Record<Grupo, string> = { apps: 'Apps', productos: 'Productos', guia: 'Ayuda y guías' }

const ICONO_PRODUCTO = (
  <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <path d="M12 3 4 7v10l8 4 8-4V7z" /><path d="M4 7l8 4 8-4M12 11v10" />
  </svg>
)
const ICONO_GUIA = (
  <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <circle cx="12" cy="12" r="9" /><path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.7.3-1 .9-1 1.7M12 17h.01" />
  </svg>
)
const ICONO_LUPA = (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />
  </svg>
)

// Abre la app de ayuda y le pide que enseñe un recorrido concreto. No hay
// contrato para pasarle props a `ayuda`, así que se avisa por un evento
// global; Ayuda (o quien la envuelva) puede escucharlo. Va en un setTimeout
// para que la ventana ya esté montada y suscrita cuando llegue el evento.
function abrirGuia(sistema: ReturnType<typeof useSistema>, clave: string, paso: number) {
  sistema.abrir('ayuda')
  setTimeout(() => {
    window.dispatchEvent(new CustomEvent('integra:guia', { detail: { clave, paso } }))
  }, 0)
}

export function Inicio({ abierto, onCerrar }: { abierto: boolean; onCerrar: () => void }) {
  const sistema = useSistema()
  const { prefs, poner } = usePreferencias()
  const { usuario, salir } = useSesion()
  const esAdmin = usuario.role === 'admin'

  const [q, setQ] = useState('')
  const [productos, setProductos] = useState<Producto[]>([])
  const [buscando, setBuscando] = useState(false)
  const [sel, setSel] = useState(0)
  const [menu, setMenu] = useState<{ app: AppId; x: number; y: number } | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const listaRef = useRef<HTMLDivElement>(null)

  useCerrarFuera(panel, abierto, onCerrar, '[data-abre-inicio], .menu-contextual')

  // Al abrir: limpio y con el foco en el buscador (autofoco).
  useEffect(() => {
    if (!abierto) return
    setQ('')
    setProductos([])
    setSel(0)
    setMenu(null)
    const id = requestAnimationFrame(() => input.current?.focus())
    return () => cancelAnimationFrame(id)
  }, [abierto])

  // Productos: con retraso para no pedir al servidor en cada tecla.
  useEffect(() => {
    const texto = q.trim()
    if (!abierto || texto.length < 2) { setProductos([]); setBuscando(false); return }
    let vivo = true
    setBuscando(true)
    const t = setTimeout(() => {
      api.productos({ q: texto, limite: 6 })
        .then((p) => { if (vivo) setProductos(p.items) })
        .catch(() => { if (vivo) setProductos([]) })
        .finally(() => { if (vivo) setBuscando(false) })
    }, RETRASO_BUSQUEDA)
    return () => { vivo = false; clearTimeout(t) }
  }, [q, abierto])

  const visible = useCallback((id: AppId) => APPS[id] != null && (!APPS[id].soloAdmin || esAdmin), [esAdmin])
  // Solo las apps de una instancia (secciones) se pueden abrir sin props;
  // los diálogos (editar, preview…) necesitan un producto y no van aquí.
  const todas = useMemo(
    () => Object.values(APPS)
      .filter((a) => a.unica && visible(a.id))
      .sort((a, b) => a.nombre.localeCompare(b.nombre, 'es')),
    [visible],
  )
  const ancladas = useMemo(() => {
    const lista = prefs.ancladas.filter(visible)
    // Si no hay nada anclado, se recomiendan las que el registro marca.
    return lista.length > 0 ? lista : todas.filter((a) => a.anclada).map((a) => a.id)
  }, [prefs.ancladas, todas, visible])

  const elegirApp = useCallback((app: AppId) => { onCerrar(); sistema.abrir(app) }, [onCerrar, sistema])

  const resultados = useMemo<Resultado[]>(() => {
    const texto = normalizar(q.trim())
    if (!texto) return []
    const r: Resultado[] = []
    for (const a of todas) {
      if (normalizar(a.nombre).includes(texto) || normalizar(a.descripcion).includes(texto)) {
        r.push({ clave: `app-${a.id}`, grupo: 'apps', icono: a.icono, color: a.color, titulo: a.nombre, detalle: a.descripcion, accion: () => elegirApp(a.id) })
      }
    }
    for (const p of productos) {
      r.push({
        clave: `prod-${p.id}`, grupo: 'productos', icono: ICONO_PRODUCTO, titulo: p.nombre,
        detalle: [p.sku, p.marca].filter(Boolean).join(' · '),
        accion: () => { onCerrar(); sistema.abrir('preview', { varianteId: p.id }) },
      })
    }
    let n = 0
    for (const rec of RECORRIDOS) {
      if (n >= MAX_GUIA) break
      if (normalizar(rec.nombre).includes(texto) || normalizar(rec.resumen).includes(texto)) {
        r.push({ clave: `guia-${rec.clave}`, grupo: 'guia', icono: ICONO_GUIA, titulo: rec.nombre, detalle: rec.resumen, accion: () => { onCerrar(); abrirGuia(sistema, rec.clave, 0) } })
        n++
      }
      rec.pasos.forEach((paso, i) => {
        if (n >= MAX_GUIA) return
        if (normalizar(paso.titulo).includes(texto)) {
          r.push({ clave: `guia-${rec.clave}-${i}`, grupo: 'guia', icono: ICONO_GUIA, titulo: paso.titulo, detalle: `${rec.nombre} · paso ${i + 1}`, accion: () => { onCerrar(); abrirGuia(sistema, rec.clave, i) } })
          n++
        }
      })
    }
    return r
  }, [q, productos, todas, elegirApp, onCerrar, sistema])

  // Si cambian los resultados, la selección no puede apuntar fuera de ellos.
  useEffect(() => { setSel((s) => Math.min(s, Math.max(0, resultados.length - 1))) }, [resultados.length])
  useEffect(() => {
    listaRef.current?.querySelector<HTMLElement>(`[data-idx="${sel}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  const teclas = (e: KeyboardEventReact<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSel((s) => Math.min(s + 1, Math.max(0, resultados.length - 1))) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)) }
    else if (e.key === 'Enter') {
      const r = resultados[sel]
      if (r) { e.preventDefault(); r.accion() }
    }
    // Escape lo gestiona useCerrarFuera.
  }

  const contextual = (app: AppId, e: MouseEventReact<HTMLElement>) => {
    e.preventDefault()
    setMenu({ app, x: e.clientX, y: e.clientY })
  }
  const opciones = (app: AppId): OpcionMenu[] => {
    const anclada = prefs.ancladas.includes(app)
    const enEscritorio = prefs.escritorio.includes(app)
    return [
      { etiqueta: 'Abrir', accion: () => elegirApp(app) },
      { separador: true },
      {
        etiqueta: anclada ? 'Desanclar de la barra' : 'Anclar a la barra',
        accion: () => poner({ ancladas: anclada ? prefs.ancladas.filter((a) => a !== app) : [...prefs.ancladas, app] }),
      },
      {
        etiqueta: enEscritorio ? 'Quitar del escritorio' : 'Añadir al escritorio',
        accion: () => poner({ escritorio: enEscritorio ? prefs.escritorio.filter((a) => a !== app) : [...prefs.escritorio, app] }),
      },
    ]
  }

  if (!abierto) return null

  const hayBusqueda = q.trim().length > 0
  const grupos = (['apps', 'productos', 'guia'] as Grupo[]).filter((g) => resultados.some((r) => r.grupo === g))
  // Índice global de cada resultado para las flechas, calculado por grupo.
  let idx = -1

  const clases = ['inicio']
  if (prefs.barraCentrada) clases.push('centrada')
  if (sistema.movil) clases.push('movil')

  return createPortal(
    <div ref={panel} className={clases.join(' ')} role="dialog" aria-label="Inicio" aria-modal={sistema.movil}>
      <div className="inicio-buscador">
        {ICONO_LUPA}
        <input
          ref={input}
          type="search"
          value={q}
          placeholder="Buscar apps, productos y ayuda"
          aria-label="Buscar"
          autoComplete="off"
          spellCheck={false}
          role="combobox"
          aria-expanded={hayBusqueda}
          aria-controls="inicio-resultados"
          aria-activedescendant={hayBusqueda && resultados[sel] ? `inicio-r-${sel}` : undefined}
          onChange={(e) => { setQ(e.target.value); setSel(0) }}
          onKeyDown={teclas}
        />
        {buscando && <span className="inicio-buscando" aria-live="polite">Buscando…</span>}
      </div>

      {hayBusqueda ? (
        <div ref={listaRef} id="inicio-resultados" className="inicio-resultados" role="listbox">
          {grupos.length === 0 && !buscando && (
            <div className="inicio-vacio">Nada que coincida con «{q.trim()}»</div>
          )}
          {grupos.map((g) => (
            <section key={g} className="inicio-grupo">
              <h3>{TITULOS[g]}</h3>
              {resultados.filter((r) => r.grupo === g).map((r) => {
                idx++
                const i = idx
                return (
                  <button
                    key={r.clave}
                    type="button"
                    id={`inicio-r-${i}`}
                    data-idx={i}
                    role="option"
                    aria-selected={i === sel}
                    className={`inicio-resultado${i === sel ? ' seleccionado' : ''}`}
                    onMouseEnter={() => setSel(i)}
                    onClick={r.accion}
                  >
                    <span className={`inicio-resultado-icono${r.color ? '' : ' neutro'}`} style={r.color ? { background: r.color } : undefined} aria-hidden>{r.icono}</span>
                    <span className="inicio-resultado-texto">
                      <span className="inicio-resultado-titulo">{r.titulo}</span>
                      {r.detalle && <span className="inicio-resultado-detalle">{r.detalle}</span>}
                    </span>
                  </button>
                )
              })}
            </section>
          ))}
        </div>
      ) : (
        <div className="inicio-cuerpo">
          {ancladas.length > 0 && (
            <section className="inicio-seccion">
              <h3>Ancladas</h3>
              <div className="inicio-apps">
                {ancladas.map((id) => {
                  const a = APPS[id]
                  return (
                    <button key={id} type="button" className="inicio-app" data-guia={`menu-${id}`}
                      title={a.descripcion} onClick={() => elegirApp(id)} onContextMenu={(e) => contextual(id, e)}>
                      <span className="inicio-app-icono" style={{ background: a.color }} aria-hidden>{a.icono}</span>
                      <span className="inicio-app-nombre">{a.nombre}</span>
                    </button>
                  )
                })}
              </div>
            </section>
          )}
          <section className="inicio-seccion">
            <h3>Todas las apps</h3>
            <div className="inicio-lista">
              {todas.map((a) => (
                <button key={a.id} type="button" className="inicio-fila" data-guia={`menu-${a.id}`}
                  onClick={() => elegirApp(a.id)} onContextMenu={(e) => contextual(a.id, e)}>
                  <span className="inicio-fila-icono" style={{ background: a.color }} aria-hidden>{a.icono}</span>
                  <span className="inicio-fila-texto">
                    <span className="inicio-fila-nombre">{a.nombre}</span>
                    <span className="inicio-fila-desc">{a.descripcion}</span>
                  </span>
                </button>
              ))}
            </div>
          </section>
        </div>
      )}

      <div className="inicio-pie">
        <div className="inicio-usuario">
          <span className="avatar" aria-hidden>{usuario.name.trim().charAt(0).toUpperCase() || '?'}</span>
          <span className="inicio-usuario-texto">
            <span className="inicio-usuario-nombre">{usuario.name}</span>
            <span className="inicio-usuario-rol">{nombreRol(usuario.role)}</span>
          </span>
        </div>
        <div className="inicio-pie-botones">
          <button type="button" onClick={() => elegirApp('configuracion')}>Configuración</button>
          <button type="button" data-guia="ver-guia" onClick={() => elegirApp('ayuda')}>Ayuda</button>
          <button type="button" className="peligro" onClick={() => { onCerrar(); salir() }}>Cerrar sesión</button>
        </div>
      </div>

      {menu && <MenuContextual x={menu.x} y={menu.y} opciones={opciones(menu.app)} onCerrar={() => setMenu(null)} />}
    </div>,
    document.body,
  )
}
