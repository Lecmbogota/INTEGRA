import { useCallback, useEffect, useState } from 'react'
import { api, fecha, num, type ConexionOdoo, type DatosIntegracion } from './api'

// Conexiones a Odoo. La API key viaja una sola vez al guardar (se cifra en el
// servidor) y nunca vuelve a la interfaz. Solo una conexión está activa:
// Integra sincroniza contra un único Odoo a la vez.

export function Integraciones() {
  const [conexiones, setConexiones] = useState<ConexionOdoo[]>([])
  const [editando, setEditando] = useState<ConexionOdoo | 'nueva' | null>(null)
  const [operando, setOperando] = useState<number | null>(null)
  const [resultado, setResultado] = useState<{ id: number; ok: boolean; mensaje: string } | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    api.integraciones().then(setConexiones).catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function probar(id: number) {
    setOperando(id)
    setError(null)
    try {
      const r = await api.probarIntegracion(id)
      setResultado({ id, ...r })
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setOperando(null)
    }
  }

  async function activar(id: number) {
    setOperando(id)
    setError(null)
    try {
      await api.activarIntegracion(id)
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setOperando(null)
    }
  }

  function borrar(c: ConexionOdoo) {
    const aviso = c.productos > 0
      ? `Se borrará la conexión ${c.nombre} Y SUS ${num(c.productos)} PRODUCTOS sincronizados. El trabajo de Integra (precios, imágenes) que cuelga de ella quedará huérfano. ¿Continuar?`
      : `Se borrará la conexión ${c.nombre}. ¿Continuar?`
    if (!window.confirm(aviso)) return
    setOperando(c.id)
    setError(null)
    api.borrarIntegracion(c.id)
      .then(() => cargar())
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setOperando(null))
  }

  return (
    <section className="panel">
      <h2>Conexiones a Odoo</h2>
      <div className="cuerpo">
        {error && <div className="aviso-caja">{error}</div>}
        {conexiones.length === 0 && (
          <div className="vacio">Sin conexiones. Conecta la instancia de Odoo de la que sale el catálogo.</div>
        )}
        {conexiones.map((c) => {
          const prueba = resultado?.id === c.id ? resultado : null
          return (
            <div key={c.id} className="fila-cuenta">
              <div className="crece">
                <div>{c.nombre}</div>
                <div className="tenue mini-texto">
                  {c.base_url} · {c.database} · usuario {c.usuario}
                </div>
                <div className="tenue mini-texto">
                  {num(c.productos)} productos · {num(c.con_precio)} con precio · {num(c.con_imagen)} con imagen
                  {' · última sincronización: '}{fecha(c.ultima_sync)}
                </div>
                {prueba && (
                  <div className={`mini-texto ${prueba.ok ? '' : 'aviso-caja'}`}>
                    {prueba.ok ? `✓ ${prueba.mensaje}` : `✗ ${prueba.mensaje}`}
                  </div>
                )}
              </div>
              {c.activa
                ? <span className="pastilla ok">Activa</span>
                : <span className="pastilla">Inactiva</span>}
              <button onClick={() => void probar(c.id)} disabled={operando === c.id}>
                Probar
              </button>
              {!c.activa && (
                <button onClick={() => void activar(c.id)} disabled={operando === c.id}
                  title="La credencial se comprueba antes de activar; si falla, no se cambia nada.">
                  Activar
                </button>
              )}
              <button onClick={() => setEditando(c)} disabled={operando === c.id}>Editar</button>
              {!c.activa && (
                <button onClick={() => borrar(c)} disabled={operando === c.id}>Borrar</button>
              )}
            </div>
          )
        })}
      </div>

      <div className="cuerpo" style={{ paddingTop: 0 }}>
        <button className="primario" onClick={() => setEditando('nueva')}>Conectar otra instancia</button>
        <div className="tenue mini-texto" style={{ marginTop: 8 }}>
          De Odoo solo se leen SKU, nombre y stock por almacén. Precios, marcas, descripciones e
          imágenes son propiedad de Integra y el sync nunca los toca. La conexión nueva pasa a ser la activa.
        </div>
      </div>

      {editando && (
        <FormularioIntegracion
          conexion={editando === 'nueva' ? null : editando}
          onCerrar={() => setEditando(null)}
          onGuardada={() => { setEditando(null); setResultado(null); cargar() }} />
      )}
    </section>
  )
}

function FormularioIntegracion({ conexion, onCerrar, onGuardada }: {
  conexion: ConexionOdoo | null
  onCerrar: () => void
  onGuardada: () => void
}) {
  const [valores, setValores] = useState({
    nombre: conexion?.nombre ?? '',
    url: conexion?.base_url ?? '',
    database: conexion?.database ?? '',
    usuario: conexion?.usuario ?? '',
    api_key: '',
    timezone: conexion?.timezone ?? '',
  })
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  function poner(clave: keyof typeof valores) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setValores({ ...valores, [clave]: e.target.value })
  }

  async function guardar() {
    setGuardando(true)
    setError(null)
    try {
      if (conexion) {
        const datos: Partial<DatosIntegracion> = {
          nombre: valores.nombre, url: valores.url, database: valores.database,
          usuario: valores.usuario, timezone: valores.timezone,
        }
        if (valores.api_key.trim() !== '') datos.api_key = valores.api_key
        await api.editarIntegracion(conexion.id, datos)
      } else {
        await api.crearIntegracion({ ...valores })
      }
      onGuardada()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setGuardando(false)
    }
  }

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>{conexion ? `Editar ${conexion.nombre}` : 'Conectar Odoo'}</h2>
            <div className="sub">
              La credencial se comprueba contra Odoo antes de guardar y se cifra; no vuelve a mostrarse.
            </div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">{error}</div>}

        <div className="form-edicion">
          <label>
            <span>Nombre</span>
            <input type="text" value={valores.nombre} onChange={poner('nombre')}
              placeholder="Odoo MDV" autoComplete="off" />
            <small className="tenue">Opcional; por defecto, host y base.</small>
          </label>
          <label>
            <span>URL</span>
            <input type="text" value={valores.url} onChange={poner('url')}
              placeholder="https://mi-empresa.odoo.com o http://localhost:8069" autoComplete="off" />
          </label>
          <label>
            <span>Base de datos</span>
            <input type="text" value={valores.database} onChange={poner('database')}
              placeholder="nombre real de la base, no el subdominio" autoComplete="off" />
          </label>
          <label>
            <span>Usuario</span>
            <input type="text" value={valores.usuario} onChange={poner('usuario')}
              autoComplete="off" />
          </label>
          <label>
            <span>API key{conexion ? ' (vacía conserva la actual)' : ''}</span>
            <input type="password" value={valores.api_key} onChange={poner('api_key')}
              autoComplete="off" />
            <small className="tenue">
              {conexion
                ? 'Solo hace falta si cambió la credencial o la instancia.'
                : 'Mi perfil → Seguridad de la cuenta → Nueva clave de API.'}
            </small>
          </label>
          <label>
            <span>Zona horaria</span>
            <input type="text" value={valores.timezone} onChange={poner('timezone')}
              placeholder="America/Bogota" autoComplete="off" />
            <small className="tenue">Opcional; para los horarios programados.</small>
          </label>
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando}>
            {guardando ? 'Comprobando y guardando…' : 'Comprobar y guardar'}
          </button>
        </footer>
      </div>
    </div>
  )
}
