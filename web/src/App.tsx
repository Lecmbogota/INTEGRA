import { useCallback, useEffect, useMemo, useState } from 'react'
import { alCaducarSesion, api } from './api'
import { Login, borrarSesion, leerSesion, type Sesion } from './Login'
import { ProveedorSesion } from './escritorio/sesion'
import { sonidoCierre } from './escritorio/sonido'
import { Dialogos } from './escritorio/Dialogos'
import { ALTO_BARRA, ProveedorSistema, useEvento, useSistema } from './escritorio/sistema'
import { ProveedorDatos } from './escritorio/datos'
import { ProveedorPreferencias } from './escritorio/preferencias'
import { Emergentes, ProveedorNotificaciones } from './escritorio/Notificaciones'
import { APPS, ContextoAyuda } from './escritorio/apps'
import { Ventana } from './escritorio/Ventana'
import { Escritorio } from './escritorio/Escritorio'
import { BarraTareas } from './escritorio/BarraTareas'
import type { AppId } from './escritorio/tipos'
import { Guia } from './Guia'
import { GENERAL, GUIAS_POR_SECCION, RECORRIDOS, type Recorrido } from './guias'
import type { Progreso } from './Ayuda'
import type { Seccion } from './Sidebar'

// Integra como escritorio.
//
// La interfaz es un sistema operativo pequeño: un fondo con iconos y
// widgets, una barra de tareas, un menú de inicio, notificaciones, y cada
// pantalla —y cada diálogo— es una app que se abre en su ventana. Las
// pantallas son las mismas de antes: lo que cambia es la casa en la que
// viven. App solo monta los proveedores y pinta las ventanas; el resto lo
// hacen las piezas de escritorio/.

export default function App() {
  const [sesion, setSesion] = useState<Sesion | null>(leerSesion)

  // El servidor manda: si rechaza el token, se cae la sesión aunque el
  // navegador siga guardándola.
  useEffect(() => {
    alCaducarSesion(() => { borrarSesion(); setSesion(null) })
  }, [])

  // Salir tiene que cerrar la sesión en el servidor, no solo olvidarla aquí:
  // el login deja una cookie que el servidor acepta como credencial durante
  // 24 h, así que borrando solo el navegador quedaba viva.
  const salir = useCallback(async () => {
    try {
      await api.cerrarSesion()
    } catch {
      // Si el servidor no contesta se sale igual: dejar al usuario dentro
      // porque falló la red sería peor. La cookie caduca sola.
    }
    borrarSesion()
    setSesion(null)
  }, [])

  // Recién entrado, el escritorio aparece con un fundido que continúa el de
  // la pantalla de bloqueo; después ya no hay animación.
  const [recienEntrado, setRecienEntrado] = useState(false)
  const entrar = useCallback((s: Sesion) => {
    setSesion(s)
    setRecienEntrado(true)
    window.setTimeout(() => setRecienEntrado(false), 1000)
  }, [])

  if (!sesion) {
    return <Login onEntrar={entrar} />
  }
  return (
    <ProveedorSesion valor={{ usuario: sesion.usuario, salir: () => { sonidoCierre(); void salir() } }}>
      <ProveedorPreferencias>
        <ProveedorSistema>
          <ProveedorDatos>
            <ProveedorNotificaciones>
              <Pantalla sesion={sesion} entrando={recienEntrado} />
            </ProveedorNotificaciones>
          </ProveedorDatos>
        </ProveedorSistema>
      </ProveedorPreferencias>
    </ProveedorSesion>
  )
}

// Las apps de sección coinciden con las secciones de la guía.
const SECCIONES = new Set<string>(Object.keys(GUIAS_POR_SECCION))

function Pantalla({ sesion, entrando }: { sesion: Sesion; entrando: boolean }) {
  const sis = useSistema()

  // La «sección» actual para la guía: la app de la ventana activa, si es una
  // pantalla (los diálogos tienen su propio «?»).
  const seccion = useMemo<Seccion | undefined>(() => {
    const v = sis.ventanas.find((x) => x.id === sis.activa)
    return v && SECCIONES.has(v.app) ? (v.app as Seccion) : undefined
  }, [sis.ventanas, sis.activa])

  // Ir a una sección = abrir (o enfocar) su app.
  const irA = useCallback((s: Seccion) => { sis.abrir(s as AppId) }, [sis])

  // Recorridos guiados. El progreso de cada uno —por dónde se iba, si se
  // terminó— se guarda por usuario en este navegador. El general salta solo
  // la primera vez; después todo se abre desde Ayuda o desde el «?».
  const claveProgreso = `integra.guia.v2.${sesion.usuario.id}`
  const [progreso, setProgreso] = useState<Progreso>(() => {
    try {
      const crudo = localStorage.getItem(claveProgreso)
      if (crudo) return JSON.parse(crudo) as Progreso
      if (localStorage.getItem(`integra.guia.v1.${sesion.usuario.id}`) === 'vista') {
        return { [GENERAL.clave]: { paso: 0, total: GENERAL.pasos.length, terminado: true, cuando: new Date().toISOString() } }
      }
    } catch { /* sin almacenamiento */ }
    return {}
  })
  const [activo, setActivo] = useState<{ rec: Recorrido; inicio: number } | null>(() =>
    progreso[GENERAL.clave] ? null : { rec: GENERAL, inicio: 0 })
  const iniciar = useCallback((rec: Recorrido, inicio: number) => setActivo({ rec, inicio }), [])
  const anotarProgreso = useCallback((rec: Recorrido, paso: number, terminado: boolean) => {
    setProgreso((prev) => {
      const antes = prev[rec.clave]
      const sig: Progreso = {
        ...prev,
        [rec.clave]: {
          paso: terminado ? 0 : paso, total: rec.pasos.length,
          terminado: terminado || (antes?.terminado ?? false), cuando: new Date().toISOString(),
        },
      }
      try { localStorage.setItem(claveProgreso, JSON.stringify(sig)) } catch { /* sin almacenamiento */ }
      return sig
    })
  }, [claveProgreso])
  const ayudaDeAqui = useCallback(() => {
    const g = seccion ? GUIAS_POR_SECCION[seccion] : undefined
    if (!g) { sis.abrir('ayuda'); return }
    const p = progreso[g.clave]
    setActivo({ rec: g, inicio: p && !p.terminado ? p.paso : 0 })
  }, [seccion, progreso, sis])
  const contextoAyuda = useMemo(() => ({ progreso, iniciar }), [progreso, iniciar])

  // Tecla «?» fuera de un campo: la ayuda de la ventana activa. Y el menú de
  // inicio pide abrir un recorrido concreto con un evento del navegador.
  useEffect(() => {
    const tecla = (e: KeyboardEvent) => {
      if (e.key !== '?' || activo) return
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
      e.preventDefault()
      ayudaDeAqui()
    }
    const pedido = (e: Event) => {
      const d = (e as CustomEvent<{ clave: string; paso?: number }>).detail
      const rec = RECORRIDOS.find((r) => r.clave === d?.clave)
      if (rec) setActivo({ rec, inicio: d.paso ?? 0 })
    }
    window.addEventListener('keydown', tecla)
    window.addEventListener('integra:guia', pedido)
    return () => {
      window.removeEventListener('keydown', tecla)
      window.removeEventListener('integra:guia', pedido)
    }
  }, [activo, ayudaDeAqui])

  // La vista previa pide corregir algo: se abre el editor del producto en la
  // pestaña que toca, o el mapeo de categorías del canal.
  useEvento('ir-a-arreglar', (e) => {
    if (e.nombre !== 'ir-a-arreglar') return
    const d = e.destino
    if (d.tipo === 'editar') sis.abrir('editar', { varianteId: d.varianteId, sku: d.sku, pestana: d.pestana })
    else sis.abrir('categorias', { canal: d.canal })
  })

  // Al entrar por primera vez no hay ventanas: se abre el panel para que el
  // escritorio no esté vacío. Después, se restaura lo que había.
  useEffect(() => {
    if (sis.ventanas.length === 0 && !sessionStorage.getItem('integra.escritorio.arrancado')) {
      sessionStorage.setItem('integra.escritorio.arrancado', '1')
      sis.abrir('panel')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <ContextoAyuda.Provider value={contextoAyuda}>
      <div className={`pantalla ${entrando ? 'entrando' : ''}`} style={{ '--alto-barra': `${ALTO_BARRA}px` } as React.CSSProperties}>
        <Escritorio />
        <div className="ventanas">
          {sis.ventanas.map((v) => {
            const app = APPS[v.app]
            if (!app) return null
            return (
              <Ventana key={v.id} ventana={v}>
                {app.render({ ventanaId: v.id, props: v.props, cerrar: () => sis.cerrar(v.id) })}
              </Ventana>
            )
          })}
        </div>
        <BarraTareas />
        <Emergentes />
        <Dialogos />
        {activo && (
          <Guia pasos={activo.rec.pasos} nombre={activo.rec.nombre} inicio={activo.inicio}
            seccion={seccion} irA={irA}
            onProgreso={(paso, terminado) => anotarProgreso(activo.rec, paso, terminado)}
            onCerrar={() => setActivo(null)} />
        )}
      </div>
    </ContextoAyuda.Provider>
  )
}
