import { useCallback, useEffect, useState } from 'react'
import { api, fecha, type UsuarioCuenta } from './api'

// Qué puede hacer cada rol. Está escrito aquí porque es la pregunta que se
// hace quien va a dar de alta a alguien, y tenerlo solo en el código obliga a
// adivinarlo o a probar con una cuenta de verdad.
const ROLES = [
  {
    valor: 'viewer', nombre: 'Consulta',
    puede: 'Ver el catálogo, los pedidos y los avisos. No cambia nada.',
  },
  {
    valor: 'operator', nombre: 'Operación',
    puede: 'Todo lo anterior, más editar productos y precios, publicar y despachar.',
  },
  {
    valor: 'admin', nombre: 'Administración',
    puede: 'Todo, más usuarios, credenciales de canal, destinos de aviso y auditoría.',
  },
]

const NOMBRE_ROL: Record<string, string> = {
  viewer: 'Consulta', operator: 'Operación', admin: 'Administración',
}

export function Usuarios() {
  const [usuarios, setUsuarios] = useState<UsuarioCuenta[]>([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editando, setEditando] = useState<UsuarioCuenta | 'nuevo' | null>(null)
  const [ocupado, setOcupado] = useState<number | null>(null)

  const cargar = useCallback(() => {
    setError(null)
    api.usuarios()
      .then(setUsuarios)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function alternarAcceso(u: UsuarioCuenta) {
    // Quitar el acceso no borra a la persona: su rastro en la auditoría tiene
    // que seguir ahí, y por eso esto desactiva en vez de eliminar.
    const admins = usuarios.filter((x) => x.role === 'admin' && x.active).length
    if (u.active && u.role === 'admin' && admins === 1) {
      setError('No puedes quitarle el acceso al único administrador activo: nadie podría volver a entrar a gestionar usuarios.')
      return
    }
    if (u.active && !window.confirm(
      `${u.name} dejará de poder entrar. Su rastro en la auditoría se conserva. ¿Continuar?`)) return

    setOcupado(u.id)
    setError(null)
    try {
      await api.editarUsuario(u.id, { name: u.name, role: u.role, active: !u.active })
      cargar()
    } catch (e) {
      setError(`No se pudo cambiar el acceso: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupado(null)
    }
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Usuarios</h1>
          <div className="sub">Quién entra a Integra y qué puede hacer</div>
        </div>
        <button className="primario" onClick={() => setEditando('nuevo')} data-guia="usr-nuevo">Nuevo usuario</button>
      </header>

      {error && <div className="aviso-caja">{error}</div>}

      <section className="panel">
        <h2>Cuentas</h2>
        <div className="cuerpo">
          {cargando && <div className="vacio">Cargando…</div>}
          {!cargando && usuarios.length === 0 && <div className="vacio">Sin usuarios.</div>}

          {usuarios.length > 0 && (
            <div className="tabla-envoltorio">
              <table className="tabla-tarjetas" data-guia="usr-tabla">
                <thead>
                  <tr>
                    <th>Nombre</th><th>Correo</th><th>Rol</th>
                    <th className="oculto-movil">Última entrada</th><th data-guia="usr-estado">Estado</th><th></th>
                  </tr>
                </thead>
                <tbody>
                  {usuarios.map((u) => (
                    <tr key={u.id}>
                      <td className="titulo-tarjeta">{u.name}</td>
                      <td data-etiqueta="Correo">{u.email}</td>
                      <td data-etiqueta="Rol">{NOMBRE_ROL[u.role] ?? u.role}</td>
                      <td className="tenue oculto-movil" data-etiqueta="Última entrada">
                        {u.last_login_at ? fecha(u.last_login_at) : 'nunca'}
                      </td>
                      <td data-etiqueta="Estado">
                        {u.active
                          ? <span className="pastilla ok">Activo</span>
                          : <span className="pastilla dudosa">Sin acceso</span>}
                      </td>
                      <td className="acciones-fila">
                        <button onClick={() => setEditando(u)} data-guia="usr-editar">Editar</button>
                        <button onClick={() => void alternarAcceso(u)} disabled={ocupado === u.id} data-guia="usr-acceso">
                          {u.active ? 'Quitar acceso' : 'Devolver acceso'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </section>

      <section className="panel" data-guia="usr-roles">
        <h2>Qué puede cada rol</h2>
        <div className="cuerpo">
          {ROLES.map((r) => (
            <div key={r.valor} className="fila-cuenta fila-apilable">
              <div className="expande-recorta">
                <strong>{r.nombre}</strong>
                <div className="tenue mini-texto">{r.puede}</div>
              </div>
            </div>
          ))}
        </div>
      </section>

      <div className="nota-previa" data-guia="usr-nota">
        Quitar el acceso no borra a la persona: lo que hizo sigue en la auditoría, que es
        justo para lo que sirve. Para eliminarla del todo hay que usar
        <code className="bloque-cmd">integra borrar-usuario &lt;correo&gt; --confirmar</code>
      </div>

      {editando && (
        <FormularioUsuario
          usuario={editando === 'nuevo' ? null : editando}
          onCerrar={() => setEditando(null)}
          onGuardado={() => { setEditando(null); cargar() }}
        />
      )}
    </>
  )
}

function FormularioUsuario({ usuario, onCerrar, onGuardado }: {
  usuario: UsuarioCuenta | null
  onCerrar: () => void
  onGuardado: () => void
}) {
  const [email, setEmail] = useState(usuario?.email ?? '')
  const [nombre, setNombre] = useState(usuario?.name ?? '')
  const [rol, setRol] = useState(usuario?.role ?? 'operator')
  const [password, setPassword] = useState('')
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  async function guardar() {
    setError(null)
    if (nombre.trim() === '') {
      setError('Ponle un nombre: en la auditoría es lo que identifica quién hizo qué.')
      return
    }
    if (!usuario) {
      if (!email.includes('@')) {
        setError('El correo no parece válido.')
        return
      }
      if (password.length < 8) {
        setError('La contraseña tiene que tener al menos 8 caracteres.')
        return
      }
    } else if (password !== '' && password.length < 8) {
      setError('La contraseña nueva tiene que tener al menos 8 caracteres.')
      return
    }

    setGuardando(true)
    try {
      if (usuario) {
        await api.editarUsuario(usuario.id, {
          name: nombre.trim(), role: rol, active: usuario.active,
          // Vacía significa «no la cambies», no «bórrala».
          ...(password ? { password } : {}),
        })
      } else {
        await api.crearUsuario({
          email: email.trim(), name: nombre.trim(), password, role: rol,
        })
      }
      onGuardado()
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
            <h2>{usuario ? `Editar a ${usuario.name}` : 'Nuevo usuario'}</h2>
            {usuario && <div className="sub">{usuario.email}</div>}
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          {!usuario && (
            <label className="ancha">
              <span>Correo</span>
              <input value={email} onChange={(e) => setEmail(e.target.value)}
                placeholder="persona@mdv.com" autoComplete="off" />
            </label>
          )}
          <label className="ancha">
            <span>Nombre</span>
            <input value={nombre} onChange={(e) => setNombre(e.target.value)} />
          </label>
          <label className="ancha">
            <span>Rol</span>
            <select value={rol} onChange={(e) => setRol(e.target.value as UsuarioCuenta['role'])}>
              {ROLES.map((r) => <option key={r.valor} value={r.valor}>{r.nombre}</option>)}
            </select>
          </label>
          <div className="ancha tenue mini-texto">
            {ROLES.find((r) => r.valor === rol)?.puede}
          </div>
          <label className="ancha">
            <span>Contraseña{usuario && ' (en blanco: no se cambia)'}</span>
            <input type="password" value={password} autoComplete="new-password"
              onChange={(e) => setPassword(e.target.value)} />
          </label>
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando}>
            {guardando ? 'Guardando…' : usuario ? 'Guardar cambios' : 'Crear usuario'}
          </button>
        </footer>
      </div>
    </div>
  )
}
