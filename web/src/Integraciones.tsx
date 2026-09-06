import { useCallback, useEffect, useState } from 'react'
import { api, fecha, num, type ConexionOdoo, type DatosIntegracion } from './api'

// Conexiones a Odoo. La API key viaja una sola vez al guardar (se cifra en el
// servidor) y nunca vuelve a la interfaz. Solo una conexión está activa:
// Integra sincroniza contra un único Odoo a la vez.

export function Integraciones() {
  const [conexiones, setConexiones] = useState<ConexionOdoo[]>([])
  const [cargando, setCargando] = useState(true)
  const [editando, setEditando] = useState<ConexionOdoo | 'nueva' | null>(null)
  // Qué fila está ocupada y en qué: el nombre de la acción se usa para decir
  // en pantalla qué se está haciendo, que no siempre es probar la conexión.
  const [operando, setOperando] = useState<{ id: number; que: string } | null>(null)
  const [resultado, setResultado] = useState<{ id: number; ok: boolean; mensaje: string } | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    api.integraciones()
      .then(setConexiones)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function probar(id: number) {
    setOperando({ id, que: 'Probando la conexión…' })
    setError(null)
    // El resultado anterior se borra antes de pedir el nuevo: si no, mientras
    // se comprueba sigue en pantalla el «✓ conecta» de la vez pasada.
    setResultado(null)
    try {
      const r = await api.probarIntegracion(id)
      setResultado({ id, ...r })
    } catch (e) {
      setError(`No se pudo probar la conexión: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOperando(null)
    }
  }

  async function activar(c: ConexionOdoo) {
    // Activar cambia de qué Odoo sale el catálogo entero de aquí en adelante;
    // no borra nada, pero la siguiente sincronización ya lee de otro sitio.
    const actual = conexiones.find((x) => x.activa)
    if (!window.confirm(
      `Integra pasará a sincronizar contra ${c.nombre} (${c.base_url}, base ${c.database})`
      + `${actual ? `, en lugar de ${actual.nombre}` : ''}. ¿Continuar?`)) return
    setOperando({ id: c.id, que: 'Activando…' })
    setError(null)
    try {
      await api.activarIntegracion(c.id)
      cargar()
    } catch (e) {
      setError(`No se pudo activar ${c.nombre}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOperando(null)
    }
  }

  function borrar(c: ConexionOdoo) {
    const aviso = c.productos > 0
      ? `Se borrará la conexión ${c.nombre} Y SUS ${num(c.productos)} PRODUCTOS sincronizados. El trabajo de Integra (precios, imágenes) que cuelga de ella quedará huérfano. ¿Continuar?`
      : `Se borrará la conexión ${c.nombre}. ¿Continuar?`
    if (!window.confirm(aviso)) return
    setOperando({ id: c.id, que: 'Borrando…' })
    setError(null)
    api.borrarIntegracion(c.id)
      .then(() => cargar())
      .catch((e) => setError(`No se pudo borrar ${c.nombre}: ${e instanceof Error ? e.message : String(e)}`))
      .finally(() => setOperando(null))
  }

  return (
    <section className="panel">
      <h2>Conexiones a Odoo</h2>
      <div className="cuerpo">
        {error && <div className="aviso-caja">{error}</div>}
        {cargando && <div className="vacio">Cargando conexiones…</div>}
        {!cargando && conexiones.length === 0 && (
          <div className="vacio">Sin conexiones. Conecta la instancia de Odoo de la que sale el catálogo.</div>
        )}
        {conexiones.map((c) => {
          const prueba = resultado?.id === c.id ? resultado : null
          return (
            <div key={c.id} className="fila-cuenta fila-apilable">
              <div className="expande-recorta">
                <div>{c.nombre}</div>
                {/* Una URL es una sola palabra sin espacios: sin recortarla,
                    en el móvil empuja la fila y desplaza la página entera. */}
                <div className="tenue mini-texto recorta"
                  title={`${c.base_url} · ${c.database} · usuario ${c.usuario}`}>
                  {c.base_url} · {c.database} · usuario {c.usuario}
                </div>
                <div className="tenue mini-texto">
                  {num(c.productos)} productos · {num(c.con_precio)} con precio · {num(c.con_imagen)} con imagen
                  {' · última sincronización: '}{fecha(c.ultima_sync)}
                </div>
                {operando?.id === c.id && <div className="tenue mini-texto">{operando.que}</div>}
                {prueba && (
                  <div className={`mini-texto ${prueba.ok ? '' : 'aviso-caja'}`}>
                    {prueba.ok ? `✓ ${prueba.mensaje}` : `✗ ${prueba.mensaje}`}
                  </div>
                )}
              </div>
              <div className="grupo-acciones">
                {c.activa
                  ? <span className="pastilla ok">Activa</span>
                  : <span className="pastilla">Inactiva</span>}
                <button onClick={() => void probar(c.id)} disabled={operando?.id === c.id}>
                  Probar
                </button>
                {!c.activa && (
                  <button onClick={() => void activar(c)} disabled={operando?.id === c.id}
                    title="La credencial se comprueba antes de activar; si falla, no se cambia nada.">
                    Activar
                  </button>
                )}
                <button onClick={() => setEditando(c)} disabled={operando?.id === c.id}>Editar</button>
                {!c.activa && (
                  <button onClick={() => borrar(c)} disabled={operando?.id === c.id}>Borrar</button>
                )}
              </div>
            </div>
          )
        })}
      </div>

      <div className="cuerpo" style={{ paddingTop: 0 }}>
        <div className="pila apretada">
          <div className="grupo-acciones">
            <button className="primario" onClick={() => setEditando('nueva')}>Conectar otra instancia</button>
          </div>
          <div className="tenue mini-texto">
            De Odoo solo se leen SKU, nombre y stock por almacén. Precios, marcas, descripciones e
            imágenes son propiedad de Integra y el sync nunca los toca. La conexión nueva pasa a ser la activa.
          </div>
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
  // La API key se escribe una sola vez y no se puede volver a leer: sin poder
  // mirarla, un dedazo solo aparece como «credenciales inválidas» de Odoo.
  const [verClave, setVerClave] = useState(false)
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
    // El servidor responde con el error de Odoo («database not found»), que no
    // dice cuál de los cuatro campos quedó vacío. Se comprueba aquí primero.
    const faltan: string[] = []
    if (valores.url.trim() === '') faltan.push('URL')
    if (valores.database.trim() === '') faltan.push('Base de datos')
    if (valores.usuario.trim() === '') faltan.push('Usuario')
    if (!conexion && valores.api_key.trim() === '') faltan.push('API key')
    if (faltan.length > 0) {
      setError(`Faltan datos obligatorios: ${faltan.join(', ')}.`)
      return
    }

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
            <span>Nombre (opcional)</span>
            <input type="text" value={valores.nombre} onChange={poner('nombre')}
              placeholder="Odoo MDV" autoComplete="off" />
            <small className="tenue">Por defecto, host y base.</small>
          </label>
          <label>
            <span>URL</span>
            <input type="url" inputMode="url" value={valores.url} onChange={poner('url')}
              placeholder="https://mi-empresa.odoo.com o http://localhost:8069"
              autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
          </label>
          <label>
            <span>Base de datos</span>
            <input type="text" value={valores.database} onChange={poner('database')}
              placeholder="nombre real de la base, no el subdominio"
              autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
          </label>
          <label>
            <span>Usuario</span>
            <input type="text" value={valores.usuario} onChange={poner('usuario')}
              autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
          </label>
          <label className="ancha">
            <span>
              API key{conexion ? ' (vacía conserva la actual)' : ''}
              {' '}<span className="pastilla neutra">secreto</span>
            </span>
            <input type={verClave ? 'text' : 'password'} value={valores.api_key} onChange={poner('api_key')}
              autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
            <small className="tenue">
              {conexion
                ? 'Solo hace falta si cambió la credencial o la instancia.'
                : 'Mi perfil → Seguridad de la cuenta → Nueva clave de API.'}
            </small>
          </label>
          <label className="ancha casilla">
            <input type="checkbox" checked={verClave}
              onChange={(e) => setVerClave(e.target.checked)} />
            <span>Ver la API key mientras la escribo</span>
          </label>
          <label className="ancha">
            <span>Zona horaria (opcional)</span>
            <input type="text" value={valores.timezone} onChange={poner('timezone')}
              placeholder="America/Bogota"
              autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
            <small className="tenue">Para los horarios programados.</small>
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
