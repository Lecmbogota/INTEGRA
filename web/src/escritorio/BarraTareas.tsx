import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as MouseEventReact } from 'react'
import { createPortal } from 'react-dom'
import type { AppId, Ventana } from './tipos'
import { useSistema } from './sistema'
import { usePreferencias } from './preferencias'
import { APPS } from './apps'
import { useDatos } from './datos'
import { useNotificaciones, CentroNotificaciones } from './Notificaciones'
import { useSesion } from './sesion'
import { Marca } from '../Logo'
import { Inicio } from './Inicio'
import { MenuContextual, useCerrarFuera, nombreRol, type OpcionMenu } from './Escritorio'

// La barra de tareas: inicio, buscador, apps ancladas y ventanas abiertas,
// bandeja (notificaciones, tareas, reloj, usuario). Es el único sitio que
// sabe qué ventana estuvo activa por última vez, para restaurarla cuando
// todas están minimizadas y se pulsa un hueco de la barra.

const FMT_HORA = new Intl.DateTimeFormat('es-CO', { hour: 'numeric', minute: '2-digit' })
const FMT_FECHA = new Intl.DateTimeFormat('es-CO', { weekday: 'short', day: 'numeric', month: 'short' })
const FMT_LARGA = new Intl.DateTimeFormat('es-CO', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })

// ¿El foco está en algo donde se escribe? Los atajos globales no deben
// robarle teclas a un input… salvo al buscador del propio menú de inicio,
// para que Ctrl+Espacio también lo cierre.
function escribiendo(): boolean {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  if (el.closest('.inicio')) return false
  return el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable
}

function iniciales(nombre: string): string {
  const partes = nombre.trim().split(/\s+/).filter(Boolean)
  if (partes.length === 0) return '?'
  return (partes[0][0] + (partes.length > 1 ? partes[partes.length - 1][0] : '')).toUpperCase()
}

const ICONO_BUSCAR = (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />
  </svg>
)
const ICONO_CAMPANA = (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15z" /><path d="M10 20a2 2 0 0 0 4 0" />
  </svg>
)

export function BarraTareas() {
  const sistema = useSistema()
  const { prefs, poner } = usePreferencias()
  const datos = useDatos()
  const notis = useNotificaciones()
  const { usuario, salir } = useSesion()
  const esAdmin = usuario.role === 'admin'

  const [inicio, setInicio] = useState(false)
  const [centro, setCentro] = useState(false)
  const [menuUsuario, setMenuUsuario] = useState(false)
  const [menuApp, setMenuApp] = useState<{ app: AppId; x: number; y: number } | null>(null)
  const [lista, setLista] = useState<{ app: AppId; x: number } | null>(null)

  // Última ventana activa: para restaurarla desde un hueco de la barra.
  const ultimaActiva = useRef<string | null>(null)
  useEffect(() => { if (sistema.activa) ultimaActiva.current = sistema.activa }, [sistema.activa])

  // --- reloj: cada 30 s basta para que el minuto nunca vaya con más de medio
  // minuto de retraso sin despertar al navegador cada segundo.
  const [ahora, setAhora] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setAhora(new Date()), 30_000)
    return () => clearInterval(t)
  }, [])

  // --- atajo global: Meta (Windows/⌘) sola o Ctrl+Espacio abre/cierra el
  // inicio. Meta se resuelve en keyup solo si no se pulsó otra tecla mientras
  // estaba bajada, para no dispararse con Win+D, ⌘+C, etc.
  useEffect(() => {
    let metaSola = false
    const abajo = (e: KeyboardEvent) => {
      if (e.key === 'Meta') { metaSola = !escribiendo(); return }
      metaSola = false
      if (e.ctrlKey && e.code === 'Space' && !escribiendo()) {
        e.preventDefault()
        setInicio((v) => !v)
      }
    }
    const arriba = (e: KeyboardEvent) => {
      if (e.key !== 'Meta') return
      if (metaSola) { e.preventDefault(); setInicio((v) => !v) }
      metaSola = false
    }
    window.addEventListener('keydown', abajo)
    window.addEventListener('keyup', arriba)
    return () => {
      window.removeEventListener('keydown', abajo)
      window.removeEventListener('keyup', arriba)
    }
  }, [])

  // --- qué botones enseñar: ancladas (en su orden) + apps con ventana que no
  // están ancladas (en orden de apertura).
  const visible = useCallback((id: AppId) => APPS[id] != null && (!APPS[id].soloAdmin || esAdmin), [esAdmin])
  const ancladas = useMemo(() => prefs.ancladas.filter(visible), [prefs.ancladas, visible])
  const botones = useMemo(() => {
    const extra: AppId[] = []
    for (const v of sistema.ventanas) {
      if (!ancladas.includes(v.app) && !extra.includes(v.app) && visible(v.app)) extra.push(v.app)
    }
    return [...ancladas, ...extra]
  }, [ancladas, sistema.ventanas, visible])

  const ventanasDe = (app: AppId): Ventana[] => sistema.ventanas.filter((v) => v.app === app)

  const traer = (v: Ventana) => {
    if (v.estado === 'minimizada') sistema.restaurar(v.id)
    sistema.enfocar(v.id)
  }

  const pulsarApp = (app: AppId, e: MouseEventReact<HTMLButtonElement>) => {
    const vs = ventanasDe(app)
    if (vs.length <= 1) setLista(null)
    if (vs.length === 0) { sistema.abrir(app); return }
    if (vs.length > 1) {
      // Varias ventanas (diálogos): una lista pequeña para elegir.
      const r = e.currentTarget.getBoundingClientRect()
      setLista(lista?.app === app ? null : { app, x: r.left + r.width / 2 })
      return
    }
    const v = vs[0]
    if (v.estado === 'minimizada') traer(v)
    else if (sistema.activa === v.id) sistema.minimizar(v.id)
    else sistema.enfocar(v.id)
  }

  const contextualApp = (app: AppId, e: MouseEventReact<HTMLButtonElement>) => {
    e.preventDefault()
    setMenuApp({ app, x: e.clientX, y: e.clientY })
  }

  const insignia = (app: AppId): number => {
    switch (app) {
      case 'catalogo': return datos.resumen?.en_atencion ?? 0
      case 'pedidos': return (datos.pedidos?.recibidos ?? 0) + (datos.pedidos?.fallidos ?? 0)
      case 'actividad': return datos.tareasActivas
      case 'automatizacion': return datos.alertas
      default: return 0
    }
  }

  // Clic en un hueco: minimiza todas; si ya lo están, restaura la última activa.
  const hueco = (e: MouseEventReact<HTMLElement>) => {
    const t = e.target as HTMLElement
    if (t !== e.currentTarget && !t.classList.contains('barra-hueco')) return
    if (sistema.ventanas.length === 0) return
    const todasMin = sistema.ventanas.every((v) => v.estado === 'minimizada')
    if (!todasMin) { sistema.minimizarTodas(); return }
    const ultima = sistema.ventanas.find((v) => v.id === ultimaActiva.current)
      ?? sistema.ventanas.reduce((m, v) => (v.z > m.z ? v : m))
    traer(ultima)
  }

  const refUsuario = useRef<HTMLDivElement>(null)
  useCerrarFuera(refUsuario, menuUsuario, () => setMenuUsuario(false), '[data-abre-usuario]')
  const refLista = useRef<HTMLDivElement>(null)
  useCerrarFuera(refLista, lista != null, () => setLista(null), '.barra-app')

  const abrirDesdeMenu = (app: AppId) => { setMenuUsuario(false); sistema.abrir(app) }

  const opcionesApp = (app: AppId): OpcionMenu[] => {
    const vs = ventanasDe(app)
    const anclada = prefs.ancladas.includes(app)
    return [
      { etiqueta: APPS[app].nombre, deshabilitado: vs.length > 0 && APPS[app].unica, accion: () => sistema.abrir(app) },
      { separador: true },
      {
        etiqueta: anclada ? 'Desanclar de la barra' : 'Anclar a la barra',
        accion: () => poner({ ancladas: anclada ? prefs.ancladas.filter((a) => a !== app) : [...prefs.ancladas, app] }),
      },
      {
        etiqueta: vs.length > 1 ? `Cerrar ${vs.length} ventanas` : 'Cerrar ventana',
        deshabilitado: vs.length === 0,
        accion: () => { for (const v of vs) sistema.cerrar(v.id) },
      },
    ]
  }

  const clases = ['barra-tareas']
  if (prefs.barraCentrada) clases.push('centrada')
  if (sistema.movil) clases.push('movil')

  const tareas = datos.tareasActivas

  return (
    <>
      <nav className={clases.join(' ')} aria-label="Barra de tareas" onClick={hueco}>
        <div className="barra-hueco" />
        <div className="barra-centro">
          <button
            type="button"
            className={`barra-boton barra-inicio${inicio ? ' activo' : ''}`}
            data-guia="inicio"
            data-abre-inicio
            aria-label="Inicio"
            aria-expanded={inicio}
            title="Inicio (Win / ⌘ o Ctrl+Espacio)"
            onClick={() => setInicio((v) => !v)}
          >
            <Marca alto={22} />
          </button>
          <button
            type="button"
            className="barra-boton barra-buscar"
            data-abre-inicio
            aria-label="Buscar"
            title="Buscar apps, productos y ayuda"
            onClick={() => setInicio(true)}
          >
            {ICONO_BUSCAR}
            <span className="barra-buscar-texto">Buscar</span>
          </button>

          {botones.map((app) => {
            const def = APPS[app]
            const vs = ventanasDe(app)
            const abierta = vs.length > 0
            const activa = vs.some((v) => v.id === sistema.activa)
            const n = insignia(app)
            const cls = ['barra-app']
            if (abierta) cls.push('abierta')
            if (activa) cls.push('activa')
            if (vs.length > 1) cls.push('varias')
            return (
              <button
                key={app}
                type="button"
                className={cls.join(' ')}
                data-guia={prefs.ancladas.includes(app) ? `menu-${app}` : undefined}
                aria-label={def.nombre}
                aria-pressed={activa}
                title={vs.length > 1 ? `${def.nombre} (${vs.length} ventanas)` : def.nombre}
                onClick={(e) => pulsarApp(app, e)}
                onContextMenu={(e) => contextualApp(app, e)}
              >
                <span className="barra-app-icono" style={{ background: def.color }} aria-hidden>{def.icono}</span>
                {n > 0 && <span className="barra-insignia" aria-label={`${n} pendientes`}>{n > 99 ? '99+' : n}</span>}
                <span className="barra-app-marca" aria-hidden />
              </button>
            )
          })}
        </div>
        <div className="barra-hueco" />

        <div className="bandeja">
          {tareas > 0 && (
            <span className="bandeja-tareas" title={`${tareas} ${tareas === 1 ? 'tarea en marcha' : 'tareas en marcha'}`}
              role="status" aria-label={`${tareas} tareas en marcha`}>
              <span className="punto-animado" aria-hidden />
              {!sistema.movil && <span>{tareas}</span>}
            </span>
          )}
          <button
            type="button"
            className={`barra-boton bandeja-campana${centro ? ' activo' : ''}`}
            data-guia="notificaciones"
            aria-label={notis.noLeidas > 0 ? `Notificaciones, ${notis.noLeidas} sin leer` : 'Notificaciones'}
            aria-expanded={centro}
            title="Notificaciones"
            onClick={() => setCentro((v) => !v)}
          >
            {ICONO_CAMPANA}
            {notis.noLeidas > 0 && <span className="barra-insignia">{notis.noLeidas > 99 ? '99+' : notis.noLeidas}</span>}
          </button>
          <button type="button" className="reloj" title={FMT_LARGA.format(ahora)} aria-label={`Hora: ${FMT_HORA.format(ahora)}, ${FMT_LARGA.format(ahora)}`}>
            <span className="reloj-hora">{FMT_HORA.format(ahora)}</span>
            {!sistema.movil && <span className="reloj-fecha">{FMT_FECHA.format(ahora)}</span>}
          </button>
          <button
            type="button"
            className={`barra-boton bandeja-usuario${menuUsuario ? ' activo' : ''}`}
            data-abre-usuario
            aria-label={`Cuenta: ${usuario.name}`}
            aria-expanded={menuUsuario}
            title={usuario.name}
            onClick={() => setMenuUsuario((v) => !v)}
          >
            <span className="avatar" aria-hidden>{iniciales(usuario.name)}</span>
            {!sistema.movil && <span className="bandeja-nombre">{usuario.name.split(' ')[0]}</span>}
          </button>
        </div>
      </nav>

      {menuUsuario && createPortal(
        <div ref={refUsuario} className="menu-usuario" role="menu">
          <div className="menu-usuario-cabecera">
            <span className="avatar grande" aria-hidden>{iniciales(usuario.name)}</span>
            <div>
              <div className="menu-usuario-nombre">{usuario.name}</div>
              <div className="menu-usuario-correo">{usuario.email}</div>
              <div className="menu-usuario-rol">{nombreRol(usuario.role)}</div>
            </div>
          </div>
          <div className="menu-separador" role="separator" />
          <button type="button" role="menuitem" onClick={() => abrirDesdeMenu('configuracion')}>Configuración</button>
          <button type="button" role="menuitem" onClick={() => abrirDesdeMenu('ayuda')}>Ayuda</button>
          <div className="menu-separador" role="separator" />
          <button type="button" role="menuitem" className="peligro" onClick={() => { setMenuUsuario(false); salir() }}>Cerrar sesión</button>
        </div>,
        document.body,
      )}

      {lista && createPortal(
        <div ref={refLista} className="barra-lista" role="menu" style={{ left: lista.x }}>
          {ventanasDe(lista.app).map((v) => (
            <button key={v.id} type="button" role="menuitem"
              className={v.id === sistema.activa ? 'activa' : undefined}
              onClick={() => { setLista(null); traer(v) }}>
              <span className="barra-lista-titulo">{v.titulo}</span>
              <span className="barra-lista-cerrar" role="button" aria-label="Cerrar" title="Cerrar"
                onClick={(e) => { e.stopPropagation(); sistema.cerrar(v.id); if (ventanasDe(lista.app).length <= 2) setLista(null) }}>×</span>
            </button>
          ))}
        </div>,
        document.body,
      )}

      {menuApp && (
        <MenuContextual x={menuApp.x} y={menuApp.y} opciones={opcionesApp(menuApp.app)} onCerrar={() => setMenuApp(null)} />
      )}

      <CentroNotificaciones abierto={centro} onCerrar={() => setCentro(false)} />
      <Inicio abierto={inicio} onCerrar={() => setInicio(false)} />
    </>
  )
}
