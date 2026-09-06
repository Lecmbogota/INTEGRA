import { useCallback, useEffect, useState } from 'react'
import { api, fecha, type CredencialesCanal, type CuentaCanal } from './api'

// Conexión de las cuentas de canal. Las credenciales viajan una sola vez al
// guardar (se cifran en el servidor) y nunca vuelven a la interfaz: aquí solo
// se ve si la cuenta existe y si la última prueba conectó.

type Campo = {
  clave: keyof CredencialesCanal
  etiqueta: string
  ayuda?: string
  secreto?: boolean
  // Marcado a mano y no deducido del texto de la etiqueta: de esto depende que
  // se deje guardar una cuenta a medias, y adivinarlo por «(opcional)» se
  // rompe en cuanto alguien reescribe una etiqueta.
  opcional?: boolean
}

const CANALES: { codigo: string; nombre: string; campos: Campo[] }[] = [
  {
    codigo: 'mercadolibre', nombre: 'MercadoLibre',
    campos: [
      { clave: 'app_id', etiqueta: 'App ID', ayuda: 'developers.mercadolibre.com.co → Mis aplicaciones' },
      { clave: 'app_secret', etiqueta: 'App Secret', secreto: true },
      { clave: 'refresh_token', etiqueta: 'Refresh token', secreto: true, ayuda: 'del flujo OAuth; se renueva solo en cada uso' },
      { clave: 'access_token', etiqueta: 'Access token', secreto: true, opcional: true, ayuda: 'si pegas uno vigente, sirve sin refresh' },
      { clave: 'url_seguimiento', etiqueta: 'URL de rastreo', opcional: true, ayuda: 'plantilla de tu transportadora con {guia}; solo para envíos por tu cuenta' },
    ],
  },
  {
    codigo: 'falabella', nombre: 'Falabella Seller Center',
    campos: [
      { clave: 'user_id', etiqueta: 'User ID', ayuda: 'el correo del usuario API del Seller Center' },
      { clave: 'api_key', etiqueta: 'API Key', secreto: true },
      { clave: 'webhook_secret', etiqueta: 'Token de webhook', secreto: true, ayuda: 'lo eliges tú: va en la URL de callback que registres (?token=…)' },
    ],
  },
  {
    codigo: 'woocommerce', nombre: 'WooCommerce',
    campos: [
      { clave: 'url', etiqueta: 'URL de la tienda', ayuda: 'https://mitienda.com' },
      { clave: 'consumer_key', etiqueta: 'Consumer key', secreto: true },
      { clave: 'consumer_secret', etiqueta: 'Consumer secret', secreto: true },
      { clave: 'webhook_secret', etiqueta: 'Secreto del webhook', secreto: true, opcional: true, ayuda: 'si lo dejas vacío, WooCommerce firma con el consumer secret' },
    ],
  },
  {
    codigo: 'shopify', nombre: 'Shopify',
    campos: [
      { clave: 'tienda', etiqueta: 'Tienda', ayuda: 'mitienda.myshopify.com' },
      { clave: 'token', etiqueta: 'Admin API token', secreto: true, ayuda: 'app personalizada → shpat_…' },
      { clave: 'webhook_secret', etiqueta: 'Client secret', secreto: true, ayuda: 'firma los webhooks; NO es el token shpat_, está en los datos de la app' },
    ],
  },
]

export function Cuentas() {
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [cargando, setCargando] = useState(true)
  const [abriendo, setAbriendo] = useState<string | null>(null)
  const [probando, setProbando] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    api.cuentas()
      .then(setCuentas)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function probar(id: number) {
    setProbando(id)
    setError(null)
    try {
      await api.probarCuenta(id)
      cargar()
    } catch (e) {
      setError(`No se pudo probar la cuenta: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setProbando(null)
    }
  }

  return (
    <section className="panel">
      <h2>Cuentas de canal</h2>
      <div className="cuerpo">
        {error && <div className="aviso-caja">{error}</div>}
        {cargando && <div className="vacio">Cargando cuentas…</div>}
        {!cargando && CANALES.map((c) => {
          const cuenta = cuentas.find((x) => x.canal === c.codigo)
          return (
            <div key={c.codigo}>
            <div className="fila-cuenta fila-apilable">
              <div className="expande-recorta">
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
              {/* Estado y botones viajan juntos: al apilarse la fila en el
                  móvil quedan en una línea bajo el nombre, no en tres. */}
              <div className="grupo-acciones">
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
            </div>
            {cuenta && <SueloDeCosto cuenta={cuenta} onGuardado={cargar} />}
            </div>
          )
        })}
      </div>

      {abriendo && (
        <FormularioCuenta def={CANALES.find((c) => c.codigo === abriendo)!}
          yaConectada={cuentas.some((x) => x.canal === abriendo)}
          onCerrar={() => setAbriendo(null)}
          onGuardada={() => { setAbriendo(null); cargar() }} />
      )}
    </section>
  )
}

// SueloDeCosto es el margen mínimo por debajo del cual esta cuenta no publica.
//
// Sin esto, el suelo solo existía si alguien había creado una regla de canal y
// le había puesto margen mínimo: el resto del catálogo salía a la venta sin
// una sola comprobación contra el coste. El valor por defecto (0 % y frenar)
// es «que al menos cubra el coste», que es lo único que se puede dar por
// decidido sin preguntar.
function SueloDeCosto({ cuenta, onGuardado }: { cuenta: CuentaCanal; onGuardado: () => void }) {
  const [margen, setMargen] = useState(String(cuenta.min_margen_pct))
  const [bloquear, setBloquear] = useState(cuenta.bloquear_bajo_costo)
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Los valores guardados mandan cuando la lista se recarga: si no, tras
  // guardar se seguiría viendo el borrador aunque el servidor hubiera
  // redondeado o rechazado algo.
  useEffect(() => {
    setMargen(String(cuenta.min_margen_pct))
    setBloquear(cuenta.bloquear_bajo_costo)
  }, [cuenta.min_margen_pct, cuenta.bloquear_bajo_costo])

  const sucio = String(cuenta.min_margen_pct) !== margen || cuenta.bloquear_bajo_costo !== bloquear

  async function guardar() {
    const pct = Number(margen.replace(',', '.'))
    if (!Number.isFinite(pct) || pct < 0) {
      setError('El margen mínimo debe ser un número positivo.')
      return
    }
    setGuardando(true)
    setError(null)
    try {
      await api.guardarSueloCosto(cuenta.id, pct, bloquear)
      onGuardado()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setGuardando(false)
    }
  }

  return (
    <div className="fila-cuenta fila-apilable">
      <div className="expande-recorta tenue mini-texto">
        Margen mínimo sobre el coste. Por debajo, el producto entra en la cola de
        atención y sale un aviso.
      </div>
      <div className="grupo-acciones">
        <input value={margen} onChange={(e) => setMargen(e.target.value)}
          inputMode="decimal" size={4} aria-label="Margen mínimo en por ciento" />
        <span className="tenue mini-texto">%</span>
        <label className="tenue mini-texto">
          <input type="checkbox" checked={bloquear}
            onChange={(e) => setBloquear(e.target.checked)} /> No publicar
        </label>
        <button onClick={() => void guardar()} disabled={guardando || !sucio}>
          {guardando ? 'Guardando…' : 'Guardar'}
        </button>
      </div>
      {error && <div className="aviso-caja">{error}</div>}
    </div>
  )
}

function FormularioCuenta({ def, yaConectada, onCerrar, onGuardada }: {
  def: (typeof CANALES)[number]
  yaConectada: boolean
  onCerrar: () => void
  onGuardada: () => void
}) {
  const [valores, setValores] = useState<CredencialesCanal>({})
  const [guardando, setGuardando] = useState(false)
  // Un solo interruptor para todos los secretos: pegar un token a ciegas y
  // enterarse del dedazo cuando el canal rechaza la conexión es el error de
  // configuración más común, y en el móvil ni se ve lo que se pegó.
  const [verSecretos, setVerSecretos] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  async function guardar() {
    const faltan = def.campos
      .filter((c) => !c.opcional && ((valores[c.clave] as string) ?? '').trim() === '')
      .map((c) => c.etiqueta)
    if (faltan.length > 0) {
      // Antes se guardaba la cuenta a medias: el servidor la aceptaba y la
      // prueba fallaba con un mensaje del canal que no decía qué faltaba.
      setError(`Faltan datos obligatorios: ${faltan.join(', ')}.`)
      return
    }
    // Guardar sobre una cuenta que ya existe borra la credencial anterior y no
    // hay forma de recuperarla: la que había nunca llegó a mostrarse.
    if (yaConectada && !window.confirm(
      `Se reemplazarán las credenciales de ${def.nombre}. Las actuales se borran y no se pueden recuperar. ¿Continuar?`)) return

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
            <h2>{yaConectada ? `Reemplazar ${def.nombre}` : `Conectar ${def.nombre}`}</h2>
            <div className="sub">La credencial se cifra al guardar y no vuelve a mostrarse.</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          {def.campos.map((campo) => (
            <label key={campo.clave} className={def.campos.length <= 2 ? 'ancha' : ''}>
              <span>
                {campo.etiqueta}
                {campo.opcional && ' (opcional)'}
                {/* Marca visible de qué se cifra: entre seis campos iguales, los
                    puntitos del input no bastan para distinguirlo. */}
                {campo.secreto && <> <span className="pastilla neutra">secreto</span></>}
              </span>
              {/* El teclado del móvil pone mayúscula inicial y corrige: sobre un
                  token eso lo invalida sin que se vea. */}
              <input type={campo.secreto && !verSecretos ? 'password' : 'text'}
                autoComplete="off" autoCapitalize="off" autoCorrect="off" spellCheck={false}
                value={(valores[campo.clave] as string) ?? ''}
                onChange={(e) => setValores({ ...valores, [campo.clave]: e.target.value })} />
              {campo.ayuda && <small className="tenue">{campo.ayuda}</small>}
            </label>
          ))}
          {def.campos.some((c) => c.secreto) && (
            <label className="ancha casilla">
              <input type="checkbox" checked={verSecretos}
                onChange={(e) => setVerSecretos(e.target.checked)} />
              <span>Ver lo que escribo en los campos secretos</span>
            </label>
          )}
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
