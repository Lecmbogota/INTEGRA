import { useEffect, useState } from 'react'
import { Marca } from './Logo'

// Sesión guardada en el navegador.
//
// Va en localStorage y no en una cookie de sesión porque la API es un servicio
// aparte: el token viaja en la cabecera Authorization, que es lo que ya espera
// el servidor. Cerrar la pestaña no cierra la sesión; cerrarla explícitamente
// o que caduque el token, sí.
const CLAVE = 'integra_sesion'

export interface Sesion {
  token: string
  usuario: { id: number; email: string; name: string; role: string }
}

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

export function Login({ onEntrar }: { onEntrar: (s: Sesion) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [entrando, setEntrando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => { document.title = 'Integra — Entrar' }, [])

  async function entrar(e: React.FormEvent) {
    e.preventDefault()
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
        setError(r.status === 401
          ? 'Correo o contraseña incorrectos.'
          : `No se pudo entrar (HTTP ${r.status}).`)
        setEntrando(false)
        return
      }
      const s = (await r.json()) as Sesion
      guardarSesion(s)
      onEntrar(s)
    } catch {
      setError('No se pudo contactar con el servidor. ¿Está corriendo integra serve?')
      setEntrando(false)
    }
  }

  return (
    <div className="pantalla-login">
      <form className="caja-login" onSubmit={entrar}>
        <div className="login-marca">
          <Marca alto={40} />
          <div>
            <div className="logo-nombre">integra</div>
            <div className="logo-sub">Logistics &amp; Solutions</div>
          </div>
        </div>

        {error && <div className="aviso-caja">{error}</div>}

        <label>
          <span>Correo</span>
          <input type="email" autoFocus autoComplete="username"
            value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>

        <label>
          <span>Contraseña</span>
          <input type="password" autoComplete="current-password"
            value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>

        <button className="primario" type="submit" disabled={entrando}>
          {entrando ? 'Entrando…' : 'Entrar'}
        </button>

        <div className="tenue mini-texto login-pie">
          ¿Sin usuario todavía? Créalo desde la terminal:
          <code className="bloque-cmd">integra crear-usuario correo nombre contraseña admin</code>
        </div>
      </form>
    </div>
  )
}
