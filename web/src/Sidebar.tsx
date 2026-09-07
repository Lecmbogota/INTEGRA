import { useEffect, useRef } from 'react'
import { Marca } from './Logo'

// Las secciones siguen el ciclo real del negocio: primero el catálogo que se
// va a vender, luego lo que hace falta para publicarlo, después la venta y,
// al final, la configuración que rara vez se toca.
export type Seccion =
  | 'panel' | 'catalogo' | 'imagenes' | 'mediateca'
  | 'publicacion' | 'categorias' | 'atributos' | 'canales' | 'integraciones'
  | 'pedidos' | 'actividad' | 'automatizacion' | 'usuarios' | 'avisos'

type Item = { id: Seccion; nombre: string; icono: JSX.Element; insignia?: number }

export function Sidebar({ actual, onIr, avisos, tareasActivas, pedidosPendientes, alertas, usuario, onSalir, abierto, onCerrar, onGuia }: {
  actual: Seccion
  onIr: (s: Seccion) => void
  // Abre el recorrido guiado. Va en el menú para que se encuentre siempre.
  onGuia?: () => void
  avisos: number
  tareasActivas: number
  pedidosPendientes: number
  alertas: number
  usuario: { name: string; email: string; role: string }
  onSalir: () => void
  /* En escritorio el menú es fijo y estas dos no hacen nada; por debajo de
     1024px es un cajón que tapa el contenido, y entonces sí. */
  abierto: boolean
  onCerrar: () => void
}) {
  const navRef = useRef<HTMLElement | null>(null)

  // Abierto, el cajón tapa la pantalla: se comporta como un diálogo o el
  // teclado se pierde detrás de él. Atrapa el foco, cierra con Escape y
  // congela el fondo. Si la ventana se ensancha hasta escritorio el cajón
  // deja de existir como tal, así que se cierra: si no, quedaría el velo
  // encima de un menú que ya es fijo.
  useEffect(() => {
    const nodo = navRef.current
    if (!abierto || !nodo) return

    const activoAlAbrir = document.activeElement
    const previo = activoAlAbrir instanceof HTMLElement && activoAlAbrir !== document.body
      ? activoAlAbrir
      : null
    document.body.classList.add('menu-abierto')

    // Se recalcula en cada pulsación porque la lista de secciones cambia
    // según el rol y el pie de sesión puede aparecer o no.
    const enfocables = () =>
      Array.from(nodo.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'))
        .filter((el) => !el.hasAttribute('disabled'))

    enfocables()[0]?.focus()

    function alPulsar(ev: KeyboardEvent) {
      if (ev.key === 'Escape') {
        ev.preventDefault()
        onCerrar()
        return
      }
      if (ev.key !== 'Tab') return
      const lista = enfocables()
      if (lista.length === 0) return
      const primero = lista[0]
      const ultimo = lista[lista.length - 1]
      const activo = document.activeElement
      if (!nodo || !nodo.contains(activo)) {
        ev.preventDefault()
        primero.focus()
      } else if (ev.shiftKey && activo === primero) {
        ev.preventDefault()
        ultimo.focus()
      } else if (!ev.shiftKey && activo === ultimo) {
        ev.preventDefault()
        primero.focus()
      }
    }
    document.addEventListener('keydown', alPulsar)

    // Misma consulta, literalmente, que la del CSS: con «min-width: 1024px»
    // los dos coincidían en 1024 y el cajón se cerraba solo justo en el ancho
    // en que aún es un cajón.
    const estrecho = window.matchMedia('(max-width: 1024px)')
    const alEnsanchar = () => { if (!estrecho.matches) onCerrar() }
    estrecho.addEventListener('change', alEnsanchar)

    return () => {
      document.removeEventListener('keydown', alPulsar)
      estrecho.removeEventListener('change', alEnsanchar)
      document.body.classList.remove('menu-abierto')
      // Devolver el foco a donde estaba: quien navega con teclado tiene que
      // volver a su sitio, no al principio del documento. Si no había foco
      // previo —el cajón se abrió con el dedo— se deja en el botón que lo
      // abre, que es lo único visible de la navegación.
      const destino = previo ?? document.querySelector<HTMLElement>('.boton-menu')
      destino?.focus()
    }
  }, [abierto, onCerrar])

  const grupos: { titulo: string; items: Item[] }[] = [
    {
      titulo: 'Catálogo',
      items: [
        { id: 'panel', nombre: 'Panel', icono: <IconoPanel /> },
        { id: 'catalogo', nombre: 'Productos', icono: <IconoCaja />, insignia: avisos },
        { id: 'mediateca', nombre: 'Imágenes', icono: <IconoImagen /> },
      ],
    },
    {
      titulo: 'Venta',
      items: [
        { id: 'publicacion', nombre: 'Publicación', icono: <IconoSubir /> },
        { id: 'pedidos', nombre: 'Pedidos', icono: <IconoCarrito />, insignia: pedidosPendientes },
      ],
    },
    {
      titulo: 'Operación',
      items: [
        // La insignia cuenta lo que está en marcha AHORA. Es el número que
        // contesta «pulsé un botón, ¿está pasando algo?», que es justo lo que
        // no se podía saber.
        { id: 'actividad', nombre: 'Actividad', icono: <IconoPulso />, insignia: tareasActivas },
        { id: 'automatizacion', nombre: 'Automatización', icono: <IconoReloj />, insignia: alertas },
      ],
    },
    {
      titulo: 'Configuración',
      items: [
        { id: 'integraciones', nombre: 'Odoo', icono: <IconoBaseDatos /> },
        { id: 'categorias', nombre: 'Categorías', icono: <IconoArbol /> },
        { id: 'atributos', nombre: 'Atributos', icono: <IconoEtiqueta /> },
        { id: 'canales', nombre: 'Canales', icono: <IconoEnchufe /> },
        // Quien no es administrador no ve estas dos: no puede usarlas —la API
        // las cierra a admin— y ofrecerlas seria prometer algo que devuelve un
        // rechazo.
        ...(usuario.role === 'admin'
          ? [
              { id: 'usuarios' as Seccion, nombre: 'Usuarios', icono: <IconoUsuarios /> },
              { id: 'avisos' as Seccion, nombre: 'Avisos', icono: <IconoCampana /> },
            ]
          : []),
      ],
    },
  ]

  return (
    <>
      {/* Pulsar fuera cierra. Es decorativo para el lector de pantalla: la
          misma acción está en el botón de cerrar y en la tecla Escape. */}
      <div className={`velo-menu ${abierto ? 'visible' : ''}`} onClick={onCerrar} aria-hidden="true" />

      <nav id="menu-lateral" ref={navRef} aria-label="Secciones"
        className={`sidebar ${abierto ? 'abierto' : ''}`}>
        <button className="cerrar-menu" onClick={onCerrar} aria-label="Cerrar menú">
          <svg {...props}><path d="M18 6 6 18" /><path d="m6 6 12 12" /></svg>
        </button>

        <div className="sidebar-marca">
          <Marca alto={30} />
          <div>
            <div className="sidebar-nombre">integra</div>
            <div className="sidebar-sub">Logistics &amp; Solutions</div>
          </div>
        </div>

        {grupos.map((g) => (
          <div key={g.titulo} className="sidebar-grupo">
            <div className="sidebar-titulo">{g.titulo}</div>
            {g.items.map((it) => (
              <button key={it.id} data-guia={`menu-${it.id}`}
                className={`sidebar-item ${actual === it.id ? 'activo' : ''}`}
                aria-current={actual === it.id ? 'page' : undefined}
                onClick={() => onIr(it.id)}>
                <span className="sidebar-icono">{it.icono}</span>
                <span className="crece">{it.nombre}</span>
                {it.insignia !== undefined && it.insignia > 0 && (
                  <span className="sidebar-insignia">{it.insignia > 999 ? '999+' : it.insignia}</span>
                )}
              </button>
            ))}
          </div>
        ))}

        {onGuia && (
          <button className="sidebar-item sidebar-guia" data-guia="ver-guia" onClick={onGuia}
            title="Recorrido de dos minutos por cómo funciona Integra">
            <span className="sidebar-icono">
              <svg {...props}><circle cx="12" cy="12" r="10" /><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3" /><path d="M12 17h.01" /></svg>
            </span>
            <span className="crece">Ver guía</span>
          </button>
        )}

        {/* La sesión va abajo del todo: se consulta poco y se cierra menos. */}
        <div className="sidebar-sesion">
          <div className="crece">
            <div className="sidebar-usuario" title={usuario.email}>{usuario.name}</div>
            <div className="sidebar-rol">{ROLES[usuario.role] ?? usuario.role}</div>
          </div>
          <button className="salir" onClick={onSalir} title="Cerrar sesión" aria-label="Cerrar sesión">
            <svg {...props}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5" /><path d="M21 12H9" /></svg>
          </button>
        </div>
      </nav>
    </>
  )
}

const ROLES: Record<string, string> = {
  admin: 'Administrador', operator: 'Operador', viewer: 'Solo lectura',
}

/* Iconos de trazo en rejilla de 24px, en un solo estilo. */
const props = {
  width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none',
  stroke: 'currentColor', strokeWidth: 1.8,
  strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const,
}

const IconoPanel = () => (
  <svg {...props}><rect x="3" y="3" width="7" height="9" rx="1" /><rect x="14" y="3" width="7" height="5" rx="1" /><rect x="14" y="12" width="7" height="9" rx="1" /><rect x="3" y="16" width="7" height="5" rx="1" /></svg>
)
const IconoCaja = () => (
  <svg {...props}><path d="M21 8v8a2 2 0 0 1-1 1.7l-7 4a2 2 0 0 1-2 0l-7-4A2 2 0 0 1 3 16V8a2 2 0 0 1 1-1.7l7-4a2 2 0 0 1 2 0l7 4A2 2 0 0 1 21 8z" /><path d="m3.3 7 8.7 5 8.7-5" /><path d="M12 22V12" /></svg>
)
const IconoImagen = () => (
  <svg {...props}><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="9" cy="9" r="2" /><path d="m21 15-4.6-4.6a2 2 0 0 0-2.8 0L3 21" /></svg>
)
const IconoSubir = () => (
  <svg {...props}><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="m7 9 5-5 5 5" /><path d="M12 4v12" /></svg>
)
const IconoCarrito = () => (
  <svg {...props}><circle cx="9" cy="20" r="1.4" /><circle cx="18" cy="20" r="1.4" /><path d="M2 3h2.5l2.4 12.1a2 2 0 0 0 2 1.6h8.7a2 2 0 0 0 2-1.6L21 7H5.6" /></svg>
)
const IconoArbol = () => (
  <svg {...props}><rect x="9" y="3" width="10" height="5" rx="1" /><rect x="12" y="16" width="10" height="5" rx="1" /><path d="M5 3v13a2 2 0 0 0 2 2h5" /><path d="M9 5.5H5" /></svg>
)
const IconoReloj = () => (
  <svg {...props}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.2 1.9" /></svg>
)
const IconoEtiqueta = () => (
  <svg {...props}><path d="M12.6 2.7 21 11a2 2 0 0 1 0 2.8l-7.2 7.2a2 2 0 0 1-2.8 0L2.7 12.6A2 2 0 0 1 2 11V4a2 2 0 0 1 2-2h7a2 2 0 0 1 1.6.7z" /><circle cx="7" cy="7" r="1.2" /></svg>
)
const IconoEnchufe = () => (
  <svg {...props}><path d="M9 2v6" /><path d="M15 2v6" /><path d="M6 8h12v3a6 6 0 0 1-6 6 6 6 0 0 1-6-6z" /><path d="M12 17v5" /></svg>
)
const IconoUsuarios = () => (
  <svg {...props}><circle cx="9" cy="8" r="3.2" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0" /><path d="M16.5 5.4a3.2 3.2 0 0 1 0 5.2" /><path d="M18 14.4a6.5 6.5 0 0 1 3.5 5.6" /></svg>
)
const IconoCampana = () => (
  <svg {...props}><path d="M18 9a6 6 0 1 0-12 0c0 5-2 6-2 6h16s-2-1-2-6" /><path d="M10.3 20a2 2 0 0 0 3.4 0" /></svg>
)
const IconoBaseDatos = () => (
  <svg {...props}><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" /><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></svg>
)

function IconoPulso() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor"
      strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 12h4l3 8 4-16 3 8h4" />
    </svg>
  )
}
