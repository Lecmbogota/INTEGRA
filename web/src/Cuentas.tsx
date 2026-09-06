import { useCallback, useEffect, useState } from 'react'
import { api, fecha, type CredencialesCanal, type CuentaCanal } from './api'

// Conexión de las cuentas de canal. Las credenciales viajan una sola vez al
// guardar (se cifran en el servidor) y nunca vuelven a la interfaz: aquí solo
// se ve si la cuenta existe y si la última prueba conectó.

const CANALES: { codigo: string; nombre: string; campos: { clave: keyof CredencialesCanal; etiqueta: string; ayuda?: string; secreto?: boolean }[] }[] = [
  {
    codigo: 'mercadolibre', nombre: 'MercadoLibre',
    campos: [
      { clave: 'app_id', etiqueta: 'App ID', ayuda: 'developers.mercadolibre.com.co → Mis aplicaciones' },
      { clave: 'app_secret', etiqueta: 'App Secret', secreto: true },
      { clave: 'refresh_token', etiqueta: 'Refresh token', secreto: true, ayuda: 'del flujo OAuth; se renueva solo en cada uso' },
      { clave: 'access_token', etiqueta: 'Access token (opcional)', secreto: true, ayuda: 'si pegas uno vigente, sirve sin refresh' },
    ],
  },
  {
    codigo: 'falabella', nombre: 'Falabella Seller Center',
    campos: [
      { clave: 'user_id', etiqueta: 'User ID', ayuda: 'el correo del usuario API del Seller Center' },
      { clave: 'api_key', etiqueta: 'API Key', secreto: true },
    ],
  },
  {
    codigo: 'woocommerce', nombre: 'WooCommerce',
    campos: [
      { clave: 'url', etiqueta: 'URL de la tienda', ayuda: 'https://mitienda.com' },
      { clave: 'consumer_key', etiqueta: 'Consumer key', secreto: true },
      { clave: 'consumer_secret', etiqueta: 'Consumer secret', secreto: true },
    ],
  },
  {
    codigo: 'shopify', nombre: 'Shopify',
    campos: [
      { clave: 'tienda', etiqueta: 'Tienda', ayuda: 'mitienda.myshopify.com' },
      { clave: 'token', etiqueta: 'Admin API token', secreto: true, ayuda: 'app personalizada → shpat_…' },
    ],
  },
]

export function Cuentas() {
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [abriendo, setAbriendo] = useState<string | null>(null)
  const [probando, setProbando] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    api.cuentas().then(setCuentas).catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function probar(id: number) {
    setProbando(id)
    setError(null)
    try {
      await api.probarCuenta(id)
      cargar()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setProbando(null)
    }
  }

  return (
    <section className="panel">
      <h2>Cuentas de canal</h2>
      <div className="cuerpo">
        {error && <div className="aviso-caja">{error}</div>}
        {CANALES.map((c) => {
          const cuenta = cuentas.find((x) => x.canal === c.codigo)
          return (
            <div key={c.codigo} className="fila-cuenta">
              <div className="crece">
                <div>{c.nombre}</div>
                <div className="tenue mini-texto">
                  {!cuenta
                    ? 'sin conectar'
                    : cuenta.probada_ok === null
                      ? 'credenciales guardadas, sin probar'
                      : cuenta.probada_ok
                        ? `✓ ${cuenta.probada_msg} · ${fecha(cuenta.probada_at)}`
                        : `✗ ${cuenta.probada_msg}`}
                </div>
              </div>
              {cuenta && cuenta.probada_ok === true && <span className="pastilla ok">Conectado</span>}
              {cuenta && cuenta.probada_ok === false && <span className="pastilla bloqueante">Falla</span>}
              {cuenta && (
                <button onClick={() => void probar(cuenta.id)} disabled={probando === cuenta.id}>
                  {probando === cuenta.id ? 'Probando…' : 'Probar'}
                </button>
              )}
              <button onClick={() => setAbriendo(c.codigo)}>
                {cuenta ? 'Reemplazar' : 'Conectar'}
              </button>
            </div>
          )
        })}
      </div>

      {abriendo && (
        <FormularioCuenta def={CANALES.find((c) => c.codigo === abriendo)!}
          onCerrar={() => setAbriendo(null)}
          onGuardada={() => { setAbriendo(null); cargar() }} />
      )}
    </section>
  )
}

function FormularioCuenta({ def, onCerrar, onGuardada }: {
  def: (typeof CANALES)[number]
  onCerrar: () => void
  onGuardada: () => void
}) {
  const [valores, setValores] = useState<CredencialesCanal>({})
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  async function guardar() {
    setGuardando(true)
    setError(null)
    try {
      const r = await api.guardarCuenta(def.codigo, `Cuenta ${def.nombre}`, valores)
      // Se prueba de inmediato: guardar sin saber si conecta deja a ciegas.
      await api.probarCuenta(r.id).catch(() => { /* el resultado queda anotado */ })
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
            <h2>Conectar {def.nombre}</h2>
            <div className="sub">La credencial se cifra al guardar y no vuelve a mostrarse.</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          {def.campos.map((campo) => (
            <label key={campo.clave} className={def.campos.length <= 2 ? 'ancha' : ''}>
              <span>{campo.etiqueta}</span>
              <input type={campo.secreto ? 'password' : 'text'}
                autoComplete="off"
                value={(valores[campo.clave] as string) ?? ''}
                onChange={(e) => setValores({ ...valores, [campo.clave]: e.target.value })} />
              {campo.ayuda && <small className="tenue">{campo.ayuda}</small>}
            </label>
          ))}
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando}>
            {guardando ? 'Guardando y probando…' : 'Guardar y probar'}
          </button>
        </footer>
      </div>
    </div>
  )
}
