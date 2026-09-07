import { useState, type ReactNode } from 'react'
import type { AppId, Fondo, Preferencias } from './tipos'
import { APPS, ORDEN_APPS } from './apps'
import { FONDOS, cssFondo, usePreferencias } from './preferencias'
import { useSesion } from './sesion'
import { useSistema } from './sistema'

// App de Configuración del escritorio. Cada cambio se guarda al instante con
// `poner`: no hay botón de guardar porque no hay nada que pueda salir mal al
// guardar una preferencia, y así lo que se toca se ve en el acto.

type Seccion = 'apariencia' | 'escritorio' | 'integra' | 'acerca'

const SECCIONES: { id: Seccion; nombre: string }[] = [
  { id: 'apariencia', nombre: 'Apariencia' },
  { id: 'escritorio', nombre: 'Escritorio' },
  { id: 'integra', nombre: 'Integra' },
  { id: 'acerca', nombre: 'Acerca de' },
]

// Versión que inyecta Vite al compilar; en desarrollo no hay. Se accede sin
// tipar `import.meta.env` porque el proyecto no incluye vite/client en types.
const VERSION: string =
  (import.meta as unknown as { env?: Record<string, string | undefined> }).env?.VITE_VERSION ?? 'desarrollo'

const ATAJOS: { teclas: string[]; que: string }[] = [
  { teclas: ['Meta'], que: 'Abrir el menú de inicio' },
  { teclas: ['Ctrl', 'Espacio'], que: 'Abrir el menú de inicio' },
  { teclas: ['Alt', 'Tab'], que: 'Cambiar de ventana' },
  { teclas: ['Ctrl', 'Alt', 'W'], que: 'Cerrar la ventana' },
  { teclas: ['Ctrl', 'Alt', 'M'], que: 'Minimizar la ventana' },
  { teclas: ['Ctrl', 'Alt', '←'], que: 'Ajustar a la izquierda' },
  { teclas: ['Ctrl', 'Alt', '→'], que: 'Ajustar a la derecha' },
  { teclas: ['Ctrl', 'Alt', '↑'], que: 'Maximizar' },
  { teclas: ['Ctrl', 'Alt', 'D'], que: 'Minimizar todas (ver el escritorio)' },
  { teclas: ['?'], que: 'Ayuda' },
]

export function Configuracion() {
  const [seccion, setSeccion] = useState<Seccion>('apariencia')
  return (
    <div className="configuracion">
      <nav className="config-nav" aria-label="Secciones de configuración">
        {SECCIONES.map(s => (
          <button
            key={s.id} type="button"
            className={seccion === s.id ? 'activo' : ''}
            aria-current={seccion === s.id ? 'page' : undefined}
            onClick={() => setSeccion(s.id)}
          >{s.nombre}</button>
        ))}
      </nav>
      <div className="config-seccion">
        {seccion === 'apariencia' && <Apariencia />}
        {seccion === 'escritorio' && <Escritorio />}
        {seccion === 'integra' && <Integra />}
        {seccion === 'acerca' && <Acerca />}
      </div>
    </div>
  )
}

function Grupo({ titulo, nota, children }: { titulo: string; nota?: string; children: ReactNode }) {
  return (
    <section className="config-grupo">
      <h3 className="config-titulo">{titulo}</h3>
      {nota && <p className="config-nota">{nota}</p>}
      {children}
    </section>
  )
}

// Botonera de opciones excluyentes (tema, tamaño de texto, barra).
function Segmentado<T extends string>({ valor, opciones, onCambiar, etiqueta }: {
  valor: T; opciones: { id: T; nombre: string }[]; onCambiar: (v: T) => void; etiqueta: string
}) {
  return (
    <div className="segmentado" role="radiogroup" aria-label={etiqueta}>
      {opciones.map(o => (
        <button
          key={o.id} type="button" role="radio" aria-checked={valor === o.id}
          className={valor === o.id ? 'activo' : ''}
          onClick={() => onCambiar(o.id)}
        >{o.nombre}</button>
      ))}
    </div>
  )
}

// ---------------------------------------------------------- apariencia

function Apariencia() {
  const { prefs, poner } = usePreferencias()
  const fondo = prefs.fondo
  // Los selectores de color recuerdan lo último elegido aunque el fondo activo
  // sea otro, para que volver al liso o al degradado no empiece de cero.
  const [liso, setLiso] = useState(fondo.tipo === 'color' ? fondo.color : '#1e293b')
  const [desde, setDesde] = useState(fondo.tipo === 'degradado' ? fondo.desde : '#1e3a8a')
  const [hasta, setHasta] = useState(fondo.tipo === 'degradado' ? fondo.hasta : '#0e7490')

  const ponerFondo = (f: Fondo) => poner({ fondo: f })
  const temas: { id: Preferencias['tema']; nombre: string; nota: string }[] = [
    { id: 'claro', nombre: 'Claro', nota: 'Fondos blancos, texto oscuro' },
    { id: 'oscuro', nombre: 'Oscuro', nota: 'Gris azulado, descansa la vista' },
    { id: 'sistema', nombre: 'Sistema', nota: 'Sigue al sistema operativo' },
  ]

  return (
    <>
      <Grupo titulo="Tema">
        <div className="muestras-tema" role="radiogroup" aria-label="Tema">
          {temas.map(t => (
            <button
              key={t.id} type="button" role="radio" aria-checked={prefs.tema === t.id}
              className={`muestra-tema${prefs.tema === t.id ? ' activo' : ''}`}
              data-muestra={t.id}
              onClick={() => poner({ tema: t.id })}
            >
              <span className="muestra-pantalla" aria-hidden="true">
                <span className="muestra-ventana" />
                <span className="muestra-barra" />
              </span>
              <span className="muestra-nombre">{t.nombre}</span>
              <span className="muestra-nota">{t.nota}</span>
            </button>
          ))}
        </div>
      </Grupo>

      <Grupo titulo="Fondo del escritorio">
        <div className="fondos" role="radiogroup" aria-label="Fondos incluidos">
          {FONDOS.map(f => {
            const activo = fondo.tipo === 'preset' && fondo.id === f.id
            return (
              <button
                key={f.id} type="button" role="radio" aria-checked={activo}
                className={`fondo-muestra${activo ? ' activo' : ''}`}
                onClick={() => ponerFondo({ tipo: 'preset', id: f.id })}
              >
                <span className="fondo-vista" style={{ background: f.css }} aria-hidden="true" />
                <span className="fondo-nombre">{f.nombre}</span>
              </button>
            )
          })}
        </div>
        <div className="fondos-personalizados">
          <label className={`fondo-personalizado${fondo.tipo === 'color' ? ' activo' : ''}`}>
            <span className="fondo-vista" style={{ background: liso }} aria-hidden="true" />
            <span className="fondo-nombre">Color liso</span>
            <input
              type="color" value={liso} aria-label="Color liso"
              onChange={e => { setLiso(e.target.value); ponerFondo({ tipo: 'color', color: e.target.value }) }}
              onClick={() => { if (fondo.tipo !== 'color') ponerFondo({ tipo: 'color', color: liso }) }}
            />
          </label>
          <div className={`fondo-personalizado${fondo.tipo === 'degradado' ? ' activo' : ''}`}>
            <button
              type="button" className="fondo-vista" aria-label="Usar degradado personalizado"
              style={{ background: cssFondo({ tipo: 'degradado', desde, hasta }) }}
              onClick={() => ponerFondo({ tipo: 'degradado', desde, hasta })}
            />
            <span className="fondo-nombre">Degradado</span>
            <span className="fondo-colores">
              <input
                type="color" value={desde} aria-label="Color inicial del degradado"
                onChange={e => { setDesde(e.target.value); ponerFondo({ tipo: 'degradado', desde: e.target.value, hasta }) }}
              />
              <input
                type="color" value={hasta} aria-label="Color final del degradado"
                onChange={e => { setHasta(e.target.value); ponerFondo({ tipo: 'degradado', desde, hasta: e.target.value }) }}
              />
            </span>
          </div>
        </div>
      </Grupo>

      <Grupo titulo="Tamaño del texto" nota="«Grande» sube toda la interfaz de 14 a 17 px.">
        <Segmentado
          etiqueta="Tamaño del texto"
          valor={prefs.tamanoTexto}
          opciones={[{ id: 'normal', nombre: 'Normal' }, { id: 'grande', nombre: 'Grande' }]}
          onCambiar={v => poner({ tamanoTexto: v })}
        />
      </Grupo>

      <Grupo titulo="Barra de tareas">
        <Segmentado
          etiqueta="Posición de los iconos de la barra"
          valor={prefs.barraCentrada ? 'centro' : 'izquierda'}
          opciones={[{ id: 'centro', nombre: 'Centrada' }, { id: 'izquierda', nombre: 'A la izquierda' }]}
          onCambiar={v => poner({ barraCentrada: v === 'centro' })}
        />
      </Grupo>
    </>
  )
}

// ---------------------------------------------------------- escritorio

// Lista de apps con casilla y orden. Las marcadas van primero, en su orden;
// las demás debajo, en el orden del registro, listas para marcarse.
function ListaApps({ etiqueta, seleccion, candidatas, onCambiar }: {
  etiqueta: string; seleccion: AppId[]; candidatas: AppId[]; onCambiar: (s: AppId[]) => void
}) {
  const filas = [...seleccion.filter(id => candidatas.includes(id)), ...candidatas.filter(id => !seleccion.includes(id))]
  const mover = (id: AppId, paso: -1 | 1) => {
    const i = seleccion.indexOf(id), j = i + paso
    if (i < 0 || j < 0 || j >= seleccion.length) return
    const s = [...seleccion]
    ;[s[i], s[j]] = [s[j], s[i]]
    onCambiar(s)
  }
  return (
    <ul className="lista-apps" aria-label={etiqueta}>
      {filas.map(id => {
        const app = APPS[id]
        const marcada = seleccion.includes(id)
        const pos = seleccion.indexOf(id)
        return (
          <li key={id} className={`fila-app${marcada ? ' marcada' : ''}`}>
            <label>
              <input
                type="checkbox" checked={marcada}
                onChange={e => onCambiar(e.target.checked ? [...seleccion, id] : seleccion.filter(x => x !== id))}
              />
              <span className="app-icono" style={{ background: app.color }} aria-hidden="true">{app.icono}</span>
              <span className="app-nombre">{app.nombre}</span>
            </label>
            {marcada && (
              <span className="fila-app-orden">
                <button type="button" aria-label={`Subir ${app.nombre}`} disabled={pos === 0} onClick={() => mover(id, -1)}>↑</button>
                <button type="button" aria-label={`Bajar ${app.nombre}`} disabled={pos === seleccion.length - 1} onClick={() => mover(id, 1)}>↓</button>
              </span>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function Escritorio() {
  const { prefs, poner } = usePreferencias()
  const { usuario } = useSesion()
  const admin = usuario.role === 'admin'
  // Solo apps de una instancia (secciones, configuración, ayuda): un diálogo
  // no tiene sentido como icono porque necesita props para abrirse.
  const candidatas = ORDEN_APPS.filter(id => APPS[id]?.unica && (!APPS[id].soloAdmin || admin))
  const widgets: { id: keyof Preferencias['widgets']; nombre: string; nota: string }[] = [
    { id: 'atencion', nombre: 'Atención', nota: 'Lo que pide una mano: productos sin foto, sin precio…' },
    { id: 'pedidos', nombre: 'Pedidos', nota: 'Recibidos, en Odoo y fallidos de hoy' },
    { id: 'actividad', nombre: 'Actividad', nota: 'Trabajos en marcha y lo último que pasó' },
  ]
  return (
    <>
      <Grupo titulo="Iconos del escritorio" nota="Marca las apps que quieres ver en el escritorio y ordénalas con las flechas.">
        <ListaApps etiqueta="Iconos del escritorio" seleccion={prefs.escritorio} candidatas={candidatas} onCambiar={s => poner({ escritorio: s })} />
      </Grupo>
      <Grupo titulo="Ancladas en la barra" nota="Las apps ancladas están siempre en la barra de tareas, abiertas o no.">
        <ListaApps etiqueta="Ancladas en la barra" seleccion={prefs.ancladas} candidatas={candidatas} onCambiar={s => poner({ ancladas: s })} />
      </Grupo>
      <Grupo titulo="Widgets">
        <ul className="lista-apps">
          {widgets.map(w => (
            <li key={w.id} className="fila-app">
              <label>
                <input
                  type="checkbox" checked={prefs.widgets[w.id]}
                  onChange={e => poner({ widgets: { ...prefs.widgets, [w.id]: e.target.checked } })}
                />
                <span className="app-nombre">{w.nombre}<small>{w.nota}</small></span>
              </label>
            </li>
          ))}
        </ul>
      </Grupo>
    </>
  )
}

// ------------------------------------------------------------- integra

function Integra() {
  const sistema = useSistema()
  const { usuario } = useSesion()
  const admin = usuario.role === 'admin'
  const accesos: { app: AppId; nombre: string; nota: string }[] = [
    { app: 'integraciones', nombre: 'Odoo', nota: 'Conexión con Odoo: URL, base de datos, credenciales y prueba.' },
    { app: 'canales', nombre: 'Canales', nota: 'Cuentas de MercadoLibre, Falabella, WooCommerce, Shopify… y sus bodegas.' },
    { app: 'usuarios', nombre: 'Usuarios', nota: 'Quién entra en Integra y con qué permisos.' },
    { app: 'avisos', nombre: 'Avisos', nota: 'Qué se avisa, con qué severidad y por dónde.' },
    { app: 'automatizacion', nombre: 'Automatización', nota: 'Reglas que corren solas: sincronización, precios, publicación.' },
  ]
  const visibles = accesos.filter(a => APPS[a.app] && (!APPS[a.app].soloAdmin || admin))
  return (
    <Grupo titulo="Ajustes de Integra" nota="La configuración del negocio vive en sus propias apps; desde aquí se abren.">
      <div className="accesos">
        {visibles.map(a => {
          const def = APPS[a.app]
          return (
            <button key={a.app} type="button" className="acceso" onClick={() => sistema.abrir(a.app)}>
              <span className="app-icono grande" style={{ background: def.color }} aria-hidden="true">{def.icono}</span>
              <span className="acceso-texto">
                <strong>{a.nombre}</strong>
                <span>{a.nota || def.descripcion}</span>
              </span>
            </button>
          )
        })}
      </div>
      {visibles.length < accesos.length && (
        <p className="config-nota">Algunos ajustes solo los ve un administrador.</p>
      )}
    </Grupo>
  )
}

// ----------------------------------------------------------- acerca de

function Acerca() {
  return (
    <>
      <Grupo titulo="Integra">
        <dl className="acerca">
          <dt>Interfaz</dt><dd>{VERSION}</dd>
          <dt>Qué es</dt><dd>Puente entre Odoo y los marketplaces: catálogo, fotos, publicación y pedidos en un solo sitio.</dd>
        </dl>
      </Grupo>
      <Grupo titulo="Atajos de teclado">
        <table className="atajos">
          <tbody>
            {ATAJOS.map((a, i) => (
              <tr key={i}>
                <td className="atajo-teclas">
                  {a.teclas.map((t, j) => (
                    <span key={j}>{j > 0 && <span className="atajo-mas">+</span>}<kbd>{t}</kbd></span>
                  ))}
                </td>
                <td>{a.que}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Grupo>
    </>
  )
}
