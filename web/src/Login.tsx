import { useEffect, useMemo, useRef, useState } from 'react'
import { FOTOS, cssFondo, urlFoto } from './escritorio/preferencias'
import { sonidoInicio } from './escritorio/sonido'
import type { Preferencias } from './escritorio/tipos'

// Sesión guardada en el navegador.
//
// Va en localStorage y no en una cookie de sesión porque la API es un servicio
// aparte: el token viaja en la cabecera Authorization, que es lo que ya espera
// el servidor. Cerrar la pestaña no cierra la sesión; cerrarla explícitamente
// o que caduque el token, sí.
const CLAVE = 'integra_sesion'
// Quién entró la última vez en este navegador: la pantalla de bloqueo lo
// enseña con su nombre y su inicial, como un sistema operativo, y toma su
// fondo de escritorio. No guarda nada secreto.
const CLAVE_ULTIMO = 'integra.ultimo_usuario'

export interface Sesion {
  token: string
  usuario: { id: number; email: string; name: string; role: string }
}

type Ultimo = { id: number; email: string; name: string }

export function leerSesion(): Sesion | null {
  try {
    const crudo = localStorage.getItem(CLAVE)
    return crudo ? (JSON.parse(crudo) as Sesion) : null
  } catch {
    return null
  }
}

export function guardarSesion(s: Sesion) {
  localStorage.setItem(CLAVE, JSON.stringify(s))
}

export function borrarSesion() {
  localStorage.removeItem(CLAVE)
}

function leerUltimo(): Ultimo | null {
  try {
    const crudo = localStorage.getItem(CLAVE_ULTIMO)
    return crudo ? (JSON.parse(crudo) as Ultimo) : null
  } catch {
    return null
  }
}

// El fondo de la pantalla de bloqueo es el del escritorio del último usuario,
// leído de sus preferencias guardadas; si no hay, la primera foto incluida.
function fondoDeBloqueo(ultimo: Ultimo | null): string {
  try {
    if (ultimo) {
      const crudo = localStorage.getItem(`integra.escritorio.prefs.${ultimo.id}`)
      if (crudo) {
        const prefs = JSON.parse(crudo) as Partial<Preferencias>
        if (prefs.fondo) return cssFondo(prefs.fondo)
      }
    }
  } catch { /* preferencias ilegibles: se usa la foto por defecto */ }
  return `#1e293b url("${urlFoto(FOTOS[0].id, 1920)}") center / cover no-repeat`
}

function inicial(nombre: string, email: string): string {
  const base = (nombre || email).trim()
  return base ? base[0].toUpperCase() : '?'
}

// Pantalla de bloqueo. Primero se dice quién eres, después la contraseña,
// como en cualquier sistema: el nombre y la inicial del último usuario ya
// están puestos, así que lo normal es solo escribir la contraseña. Al entrar,
// suena la firma de Integra y la pantalla se funde con el escritorio.
export function Login({ onEntrar }: { onEntrar: (s: Sesion) => void }) {
  const ultimo = useMemo(leerUltimo, [])
  const [paso, setPaso] = useState<'usuario' | 'clave'>(ultimo ? 'clave' : 'usuario')
  const [email, setEmail] = useState(ultimo?.email ?? '')
  const [nombre, setNombre] = useState(ultimo?.name ?? '')
  const [password, setPassword] = useState('')
  const [entrando, setEntrando] = useState(false)
  const [saliendo, setSaliendo] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [ahora, setAhora] = useState(() => new Date())
  const claveRef = useRef<HTMLInputElement>(null)
  const fondo = useMemo(() => fondoDeBloqueo(ultimo), [ultimo])

  useEffect(() => { document.title = 'Integra' }, [])
  useEffect(() => {
    const t = window.setInterval(() => setAhora(new Date()), 15000)
    return () => window.clearInterval(t)
  }, [])
  useEffect(() => { if (paso === 'clave') claveRef.current?.focus() }, [paso])

  function seguir(e: React.FormEvent) {
    e.preventDefault()
    if (!email.trim()) return
    setError(null)
    setPaso('clave')
  }

  function otroUsuario() {
    setPaso('usuario')
    setNombre('')
    setPassword('')
    setError(null)
  }

  async function entrar(e: React.FormEvent) {
    e.preventDefault()
    if (!password) return
    setEntrando(true)
    setError(null)
    try {
      const r = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: email.trim(), password }),
      })
      if (!r.ok) {
        // El servidor no distingue entre correo inexistente y contraseña
        // equivocada, y está bien: decirlo ayudaría a adivinar usuarios.
        setError(r.status === 401 ? 'La contraseña no es correcta.' : `No se pudo entrar (HTTP ${r.status}).`)
        setEntrando(false)
        setPassword('')
        claveRef.current?.focus()
        return
      }
      const s = (await r.json()) as Sesion
      guardarSesion(s)
      try {
        localStorage.setItem(CLAVE_ULTIMO, JSON.stringify({ id: s.usuario.id, email: s.usuario.email, name: s.usuario.name } satisfies Ultimo))
      } catch { /* sin almacenamiento */ }
      // La firma sonora y el fundido salen a la vez; el escritorio aparece
      // cuando la pantalla de bloqueo ya se fue.
      sonidoInicio()
      setNombre(s.usuario.name)
      setSaliendo(true)
      window.setTimeout(() => onEntrar(s), 750)
    } catch {
      setError('No se pudo contactar con el servidor. ¿Está corriendo integra serve?')
      setEntrando(false)
    }
  }

  const hora = ahora.toLocaleTimeString('es-CO', { hour: '2-digit', minute: '2-digit', hour12: false })
  const fecha = ahora.toLocaleDateString('es-CO', { weekday: 'long', day: 'numeric', month: 'long' })

  return (
    <div className={`pantalla-bloqueo ${saliendo ? 'saliendo' : ''}`}>
      <div className="bloqueo-fondo" style={{ background: fondo }} aria-hidden="true" />
      <div className="bloqueo-velo" aria-hidden="true" />

      <div className="bloqueo-reloj" aria-hidden="true">
        <div className="bloqueo-hora">{hora}</div>
        <div className="bloqueo-fecha">{fecha}</div>
      </div>

      <div className="bloqueo-centro">
        <div className="bloqueo-avatar" aria-hidden="true">
          {paso === 'clave' ? inicial(nombre, email) : <img src="/marca.png" alt="" />}
        </div>

        {paso === 'usuario' && (
          <form className="bloqueo-form" onSubmit={seguir}>
            <div className="bloqueo-nombre">Integra</div>
            <div className="bloqueo-sub">¿Quién eres?</div>
            {error && <div className="bloqueo-error">{error}</div>}
            <div className="bloqueo-campo">
              <input type="email" autoFocus autoComplete="username" placeholder="Correo"
                value={email} onChange={(e) => setEmail(e.target.value)} required aria-label="Correo" />
              <button type="submit" aria-label="Siguiente" title="Siguiente">→</button>
            </div>
          </form>
        )}

        {paso === 'clave' && (
          <form className="bloqueo-form" onSubmit={entrar}>
            <div className="bloqueo-nombre">{nombre || email}</div>
            {nombre && <div className="bloqueo-sub">{email}</div>}
            {error && <div className="bloqueo-error">{error}</div>}
            <div className="bloqueo-campo">
              <input ref={claveRef} type="password" autoComplete="current-password" placeholder="Contraseña"
                value={password} onChange={(e) => setPassword(e.target.value)} required aria-label="Contraseña"
                disabled={entrando} />
              <button type="submit" aria-label="Entrar" title="Entrar" disabled={entrando}>
                {entrando ? <span className="bloqueo-espera" aria-hidden="true" /> : '→'}
              </button>
            </div>
            <button type="button" className="bloqueo-enlace" onClick={otroUsuario} disabled={entrando}>
              {ultimo ? 'No soy yo' : 'Cambiar de usuario'}
            </button>
          </form>
        )}
      </div>

      <div className="bloqueo-pie">
        <img src="/logo.png" alt="Integra Platform" className="bloqueo-logo" />
        {paso === 'usuario' && (
          <div className="bloqueo-nota">
            ¿Sin usuario todavía? Créalo desde la terminal: <code>integra crear-usuario correo nombre contraseña admin</code>
          </div>
        )}
      </div>
    </div>
  )
}
