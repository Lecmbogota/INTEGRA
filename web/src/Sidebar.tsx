import { Marca } from './Logo'

// Las secciones siguen el ciclo real del negocio: primero el catálogo que se
// va a vender, luego lo que hace falta para publicarlo, después la venta y,
// al final, la configuración que rara vez se toca.
export type Seccion =
  | 'panel' | 'catalogo' | 'imagenes' | 'mediateca'
  | 'publicacion' | 'categorias' | 'atributos' | 'canales' | 'integraciones'
  | 'pedidos' | 'automatizacion' | 'usuarios' | 'avisos'

type Item = { id: Seccion; nombre: string; icono: JSX.Element; insignia?: number }

export function Sidebar({ actual, onIr, avisos, pedidosPendientes, alertas, usuario, onSalir }: {
  actual: Seccion
  onIr: (s: Seccion) => void
  avisos: number
  pedidosPendientes: number
  alertas: number
  usuario: { name: string; email: string; role: string }
  onSalir: () => void
}) {
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
    <nav className="sidebar">
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
            <button key={it.id}
              className={`sidebar-item ${actual === it.id ? 'activo' : ''}`}
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

      {/* La sesión va abajo del todo: se consulta poco y se cierra menos. */}
      <div className="sidebar-sesion">
        <div className="crece">
          <div className="sidebar-usuario" title={usuario.email}>{usuario.name}</div>
          <div className="sidebar-rol">{ROLES[usuario.role] ?? usuario.role}</div>
        </div>
        <button className="salir" onClick={onSalir} title="Cerrar sesión">
          <svg {...props}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5" /><path d="M21 12H9" /></svg>
        </button>
      </div>
    </nav>
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
