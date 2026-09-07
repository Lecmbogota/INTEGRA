import { useCallback, useEffect, useState } from 'react'
import {
  alCaducarSesion, api, fecha, motivo, num,
  type Atencion, type BusquedaMasiva, type Categoria, type Marca, type Resumen,
  type ResumenOrdenes, type StockAlmacen,
} from './api'
import { Login, borrarSesion, leerSesion, type Sesion } from './Login'
import { Preview, type DestinoFaltante } from './Preview'
import { Mapeos } from './Mapeos'
import { Canales } from './Canales'
import { Cuentas } from './Cuentas'
import { Catalogo, type AbrirEditor } from './Catalogo'
import { Pedidos } from './Pedidos'
import { Publicacion } from './Publicacion'
import { Prioridad } from './Prioridad'
import { Atributos } from './Atributos'
import { Actividad } from './Actividad'
import { Automatizacion } from './Automatizacion'
import { Integraciones } from './Integraciones'
import { Usuarios } from './Usuarios'
import { Mediateca } from './Mediateca'
import { Avisos } from './Avisos'
import { Sidebar, type Seccion } from './Sidebar'

export default function App() {
  const [sesion, setSesion] = useState<Sesion | null>(leerSesion)

  // El servidor manda: si rechaza el token, se cae la sesión aunque el
  // navegador siga guardándola.
  useEffect(() => {
    alCaducarSesion(() => { borrarSesion(); setSesion(null) })
  }, [])

  // Salir tiene que cerrar la sesión en el servidor, no solo olvidarla aquí:
  // el login deja una cookie que el servidor acepta como credencial durante
  // 24 h, así que borrando solo el navegador quedaba viva. En un equipo
  // compartido bastaba con pedir /api/productos desde la barra de
  // direcciones para seguir viendo el catálogo del usuario anterior.
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

  if (!sesion) {
    return <Login onEntrar={setSesion} />
  }
  return <Aplicacion sesion={sesion} onSalir={salir} />
}

function Aplicacion({ sesion, onSalir }: { sesion: Sesion; onSalir: () => void }) {
  const [seccion, setSeccion] = useState<Seccion>('panel')
  // Lo que Integra tiene en marcha ahora mismo. Vive aquí y no dentro de la
  // pantalla porque el número va en el menú: es lo que contesta «pulsé un
  // botón, ¿está pasando algo?» sin tener que entrar a mirar. Cada 15 s basta;
  // el detalle, con refresco rápido, está en la pantalla de Actividad.
  const [tareasActivas, setTareasActivas] = useState(0)
  useEffect(() => {
    let vivo = true
    const mirar = () => {
      api.actividad(1)
        .then((a) => { if (vivo) setTareasActivas(a.activas) })
        .catch(() => {})
    }
    mirar()
    const t = window.setInterval(mirar, 15000)
    return () => { vivo = false; window.clearInterval(t) }
  }, [])
  const [menuAbierto, setMenuAbierto] = useState(false)
  // Estables porque el cajón las usa como dependencia de su efecto de foco:
  // recrearlas en cada render lo montaría y desmontaría sin parar.
  const cerrarMenu = useCallback(() => setMenuAbierto(false), [])
  // Elegir sección cierra el cajón: en el móvil tapa justo lo que se acaba
  // de pedir ver.
  const irA = useCallback((s: Seccion) => { setSeccion(s); setMenuAbierto(false) }, [])
  const [resumen, setResumen] = useState<Resumen | null>(null)
  const [marcas, setMarcas] = useState<Marca[]>([])
  const [categorias, setCategorias] = useState<Categoria[]>([])
  const [atencion, setAtencion] = useState<Atencion[]>([])
  const [stock, setStock] = useState<StockAlmacen[]>([])
  const [pedidos, setPedidos] = useState<ResumenOrdenes | null>(null)
  const [alertas, setAlertas] = useState(0)

  const [error, setError] = useState<string | null>(null)
  const [sincronizando, setSincronizando] = useState(false)
  const [preview, setPreview] = useState<number | null>(null)
  // Lo que pidió la vista previa al pulsar un faltante: abrir tal producto en
  // tal pestaña del editor, o ver el mapeo de categorías de tal canal.
  const [abrirEditor, setAbrirEditor] = useState<AbrirEditor | null>(null)
  const [canalMapeo, setCanalMapeo] = useState<string | undefined>(undefined)

  const irAArreglar = useCallback((d: DestinoFaltante) => {
    setPreview(null)
    if (d.tipo === 'editar') {
      setAbrirEditor({ id: d.varianteId, sku: d.sku, pestana: d.pestana })
      irA('catalogo')
    } else {
      setCanalMapeo(d.canal)
      irA('categorias')
    }
  }, [irA])
  const [masivo, setMasivo] = useState<BusquedaMasiva | null>(null)

  const cargarPanel = useCallback(async () => {
    try {
      const [r, m, cats, a, s, p, al] = await Promise.all([
        api.resumen(), api.marcas(), api.categorias().catch(() => []),
        api.atencion(), api.stock(),
        api.resumenOrdenes().catch(() => null),
        api.alertas().catch(() => []),
      ])
      setResumen(r); setMarcas(m); setCategorias(cats); setAtencion(a); setStock(s); setPedidos(p)
      setAlertas(al.length)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'no se pudo contactar con la API')
    }
  }, [])

  useEffect(() => { void cargarPanel() }, [cargarPanel])

  // Al abrir la página se consulta si quedó un barrido de imágenes en marcha.
  useEffect(() => {
    api.estadoBusquedaMasiva()
      .then((m) => setMasivo(m.total > 0 ? m : null))
      .catch(() => { /* sin API no hay barrido que mostrar */ })
  }, [])

  // Mientras el barrido corre en el servidor, se sondea su progreso.
  useEffect(() => {
    if (!masivo?.en_curso) return
    const t = setInterval(() => {
      api.estadoBusquedaMasiva().then((m) => {
        setMasivo(m)
        if (!m.en_curso) void cargarPanel()
      }).catch(() => { /* la API puede estar reiniciándose; se reintenta */ })
    }, 4000)
    return () => clearInterval(t)
  }, [masivo?.en_curso, cargarPanel])

  async function lanzarMasivo() {
    try {
      await api.buscarImagenesMasivo()
      const m = await api.estadoBusquedaMasiva()
      setMasivo(m.total > 0 ? m : null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function sincronizar() {
    setSincronizando(true)
    try {
      await api.sincronizar()
      // La sincronización corre en segundo plano; se refresca al cabo de un
      // rato para que se vean los datos nuevos.
      setTimeout(() => { void cargarPanel(); setSincronizando(false) }, 12000)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setSincronizando(false)
    }
  }

  const maxAtencion = Math.max(1, ...atencion.map((a) => a.cantidad))
  const maxStock = Math.max(1, ...stock.map((s) => s.unidades))

  return (
    <div className="app">
      {/* Solo visible por debajo de 1024px, donde el menú está escondido:
          sin esta barra no habría forma de llegar a las secciones. */}
      <header className="barra-movil">
        <button className="boton-menu" onClick={() => setMenuAbierto(true)}
          aria-label="Abrir menú" aria-expanded={menuAbierto} aria-controls="menu-lateral">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="1.8" strokeLinecap="round">
            <path d="M4 7h16" /><path d="M4 12h16" /><path d="M4 17h16" />
          </svg>
        </button>
        <span className="barra-movil-nombre">integra</span>
      </header>

      <Sidebar actual={seccion} onIr={irA}
        abierto={menuAbierto} onCerrar={cerrarMenu}
        avisos={resumen?.en_atencion ?? 0}
        tareasActivas={tareasActivas}
        pedidosPendientes={pedidos ? pedidos.recibidos + pedidos.fallidos : 0}
        alertas={alertas}
        usuario={sesion.usuario} onSalir={onSalir} />

      <main className="contenido">
        {error && <div className="aviso-caja">Error: {error}</div>}

        {seccion === 'panel' && (
          <>
            <header className="principal">
              <div>
                <h1 className="titulo-seccion">Panel</h1>
                <div className="sub">
                  Catálogo Odoo → canales de venta
                  {resumen && <> · última sincronización: {fecha(resumen.ultima_sincronizacion)}</>}
                </div>
              </div>
              <button className="primario" onClick={sincronizar} disabled={sincronizando}>
                {sincronizando ? 'Sincronizando…' : 'Sincronizar ahora'}
              </button>
            </header>

            {resumen && (
              <div className="tarjetas">
                <Tarjeta etiqueta="Productos" valor={num(resumen.productos)} pie={`${num(resumen.variantes)} variantes`} />
                <Tarjeta etiqueta="Marcas" valor={num(resumen.marcas)} pie="normalizadas" />
                <Tarjeta etiqueta="Con precio" valor={num(resumen.con_precio)} pie={`de ${num(resumen.variantes)}`} />
                <Tarjeta etiqueta="Con existencias" valor={num(resumen.con_stock)}
                  pie={`${num(resumen.stock_total)} unidades`} />
                <Tarjeta etiqueta="Publicables hoy" valor={num(resumen.publicables)}
                  tono={resumen.publicables > 0 ? 'ok' : 'error'} destacada
                  pie="cumplen todos los requisitos" />
                <Tarjeta etiqueta="Requieren atención" valor={num(resumen.en_atencion)}
                  tono="error" pie="avisos abiertos" />
                {/* Los excluidos se muestran para que se vea que existen y no
                    parezca que Integra perdió productos por el camino. */}
                <Tarjeta etiqueta="Fuera del catálogo" valor={num(resumen.excluidos)}
                  pie="gastos, activos fijos, servicios" />
              </div>
            )}

            <div className="rejilla">
              <section className="panel">
                <h2>Qué bloquea la publicación</h2>
                <div className="cuerpo">
                  {atencion.length === 0 && <div className="vacio">Nada pendiente.</div>}
                  {atencion.map((a) => (
                    <div className="barra-fila" key={a.motivo}>
                      <div className="nombre">{motivo(a.motivo)}</div>
                      <div className="pista">
                        <div className={`relleno ${a.severidad === 'blocking' ? 'error' : 'aviso'}`}
                          style={{ width: `${(a.cantidad / maxAtencion) * 100}%` }} />
                      </div>
                      <div className="cifra">{num(a.cantidad)}</div>
                    </div>
                  ))}
                </div>
              </section>

              <section className="panel">
                <h2>Existencias por bodega</h2>
                <div className="cuerpo">
                  {stock.length === 0 && <div className="vacio">Sin datos de stock.</div>}
                  {stock.map((s) => (
                    <div className="barra-fila" key={s.codigo}>
                      <div className="nombre" title={s.nombre}>{s.nombre}</div>
                      <div className="pista">
                        <div className="relleno"
                          style={{ width: `${Math.max(0, (s.unidades / maxStock) * 100)}%` }} />
                      </div>
                      <div className="cifra">{num(s.unidades)}</div>
                    </div>
                  ))}
                </div>
              </section>
            </div>

            <div className="rejilla">
              <Prioridad onVer={(id) => setPreview(id)} />
              <Canales />
            </div>
          </>
        )}

        {seccion === 'catalogo' && (
          <Catalogo marcas={marcas} categorias={categorias} onVer={(id) => setPreview(id)} onCambio={() => void cargarPanel()}
            abrir={abrirEditor} onAbierto={() => setAbrirEditor(null)} />
        )}

        {seccion === 'mediateca' && (
          <>
            <header className="principal">
              <div>
                <h1 className="titulo-seccion">Imágenes</h1>
                <div className="sub">Banco propio de Integra — las de Odoo son miniaturas que ningún canal acepta</div>
              </div>
              <button onClick={() => void lanzarMasivo()} disabled={masivo?.en_curso ?? false}
                title="Busca en internet por SKU las fotos de los productos que aún no tienen una imagen apta para los cuatro canales.">
                {masivo?.en_curso ? 'Buscando…' : 'Buscar imágenes faltantes'}
              </button>
            </header>

            {masivo && (
              <div className="nota-previa barrido">
                {masivo.en_curso ? (
                  <>
                    <strong>Buscando imágenes:</strong> {num(masivo.procesados)}/{num(masivo.total)} productos
                    · {num(masivo.fotos_agregadas)} fotos descargadas
                    {masivo.fallos > 0 && <> · {num(masivo.fallos)} búsquedas fallidas</>}
                    {masivo.ultimo && <span className="tenue"> · último: {masivo.ultimo}</span>}
                    <span className="pista-barrido">
                      <span className="relleno" style={{ width: `${(masivo.procesados / Math.max(1, masivo.total)) * 100}%` }} />
                    </span>
                  </>
                ) : (
                  <><strong>Búsqueda {masivo.mensaje.startsWith('abortada') ? 'abortada' : 'terminada'}:</strong> {masivo.mensaje}</>
                )}
              </div>
            )}

            <Mediateca />
          </>
        )}

        {seccion === 'publicacion' && <Publicacion />}
        {seccion === 'pedidos' && <Pedidos />}

        {seccion === 'categorias' && (
          <>
            <header className="principal">
              <div>
                <h1 className="titulo-seccion">Categorías</h1>
                <div className="sub">Equivalencia entre el árbol de Odoo y el de cada canal</div>
              </div>
            </header>
            <Mapeos canalInicial={canalMapeo} />
          </>
        )}

        {seccion === 'usuarios' && <Usuarios />}
        {seccion === 'avisos' && <Avisos />}
        {seccion === 'atributos' && <Atributos />}
        {seccion === 'actividad' && <Actividad />}
        {seccion === 'automatizacion' && <Automatizacion />}

        {seccion === 'integraciones' && (
          <>
            <header className="principal">
              <div>
                <h1 className="titulo-seccion">Odoo</h1>
                <div className="sub">Instancias conectadas como fuente del catálogo (SKU, nombre y stock)</div>
              </div>
            </header>
            <div className="rejilla-1">
              <Integraciones />
            </div>
          </>
        )}

        {seccion === 'canales' && (
          <>
            <header className="principal">
              <div>
                <h1 className="titulo-seccion">Canales</h1>
                <div className="sub">Cuentas conectadas y comisiones que compensa el precio publicado</div>
              </div>
            </header>
            <div className="rejilla-1">
              <Cuentas />
              <Canales />
            </div>
          </>
        )}
      </main>

      {preview !== null && (
        <Preview varianteId={preview} onCerrar={() => setPreview(null)} onIr={irAArreglar} />
      )}
    </div>
  )
}

function Tarjeta(props: {
  etiqueta: string; valor: string; pie?: string
  tono?: 'ok' | 'error'; destacada?: boolean
}) {
  return (
    <div className={`tarjeta ${props.destacada ? 'destacada' : ''}`}>
      <div className="etiqueta">{props.etiqueta}</div>
      <div className={`valor ${props.tono ?? ''}`}>{props.valor}</div>
      {props.pie && <div className="pie">{props.pie}</div>}
    </div>
  )
}
