import { useCallback, useEffect, useState } from 'react'
import { confirmar } from './escritorio/Dialogos'
import { api, fecha, type ConfigSMTP, type DestinoAviso } from './api'
import { SelectorVista, useVista } from './Vista'

const SEVERIDADES = [
  { valor: 'critical', nombre: 'Solo lo crítico', pie: 'canal caído, credencial vencida' },
  { valor: 'error', nombre: 'Errores y peor', pie: 'lo recomendado' },
  { valor: 'warning', nombre: 'Avisos y peor', pie: 'incluye publicados sin stock' },
  { valor: 'info', nombre: 'Todo', pie: 'llena el buzón; entrena a ignorarlos' },
]

const NOMBRE_SEVERIDAD: Record<string, string> = {
  critical: 'Solo lo crítico', error: 'Errores y peor',
  warning: 'Avisos y peor', info: 'Todo',
}

// Destinos de aviso: a dónde sale lo que Integra tiene que contar.
//
// Mientras no haya ninguno, todo el sistema de alertas es una tabla que nadie
// lee: el planificador levanta el aviso, lo guarda y ahí se queda. Por eso la
// pantalla no empieza vacía y en silencio, sino diciéndolo.
export function Avisos() {
  const [destinos, setDestinos] = useState<DestinoAviso[]>([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editando, setEditando] = useState<DestinoAviso | 'nuevo' | null>(null)
  const [probando, setProbando] = useState<number | null>(null)
  // El resultado de la prueba se enseña junto a su destino, no en una alerta
  // global: con varios configurados hay que saber cuál respondió.
  const [resultado, setResultado] = useState<Record<number, { ok: boolean; mensaje: string }>>({})
  // «Detalles» son las filas de siempre, con destinatarios, último envío y
  // el resultado de la prueba; «lista» es una línea por destino.
  const [vista, setVista] = useVista('avisos', 'detalles', ['detalles', 'lista'])

  const cargar = useCallback(() => {
    setError(null)
    api.destinosAviso()
      .then(setDestinos)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function probar(d: DestinoAviso) {
    setProbando(d.id)
    try {
      const r = await api.probarDestinoAviso(d.id)
      setResultado({ ...resultado, [d.id]: r })
      cargar()
    } catch (e) {
      setResultado({
        ...resultado,
        [d.id]: { ok: false, mensaje: e instanceof Error ? e.message : String(e) },
      })
    } finally {
      setProbando(null)
    }
  }

  async function borrar(d: DestinoAviso) {
    // Borrar el último destino devuelve a Integra al silencio, y eso no se
    // nota hasta que hace falta un aviso.
    const ultimo = destinos.length === 1
    if (!(await confirmar(ultimo
      ? `«${d.nombre}» es el único destino configurado. Si lo borras, los avisos de Integra dejarán de salir a ninguna parte. ¿Continuar?`
      : `Se borrará el destino «${d.nombre}». ¿Continuar?`))) return
    try {
      await api.borrarDestinoAviso(d.id)
      cargar()
    } catch (e) {
      setError(`No se pudo borrar: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const ningunoActivo = !cargando && destinos.filter((d) => d.activo).length === 0

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Avisos</h1>
          <div className="sub">A dónde sale lo que Integra tiene que contar</div>
        </div>
        <button className="primario" onClick={() => setEditando('nuevo')} data-guia="avi-nuevo">Nuevo destino</button>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}

      {ningunoActivo && (
        <div className="aviso-caja" data-guia="avi-alerta">
          <strong>Ahora mismo los avisos no le llegan a nadie.</strong> Integra detecta
          canales caídos, credenciales vencidas y pedidos que no se pudieron montar en
          Odoo, y los guarda para que alguien entre a mirarlos. Con un destino
          configurado, además, los manda por correo.
        </div>
      )}

      <section className="panel" data-guia="avi-destinos">
        <h2>Destinos por correo</h2>
        <div className="cuerpo">
          <div className="filtros">
            <SelectorVista modo={vista} onCambiar={setVista} admitidos={['detalles', 'lista']} />
          </div>
          {cargando && <div className="vacio">Cargando…</div>}
          {!cargando && destinos.length === 0 && (
            <div className="vacio">Sin destinos. Crea uno para que los avisos salgan por correo.</div>
          )}
          {vista === 'lista' && destinos.length > 0 && (
            <div className="vista-lista">
              {destinos.map((d) => {
                const res = resultado[d.id]
                return (
                  <div key={d.id} className="fila-lista">
                    <span className="principal" title={d.destinatarios.join(', ')}>
                      <strong>{d.nombre}</strong>
                      <span className="tenue"> · {d.destinatarios.length > 0 ? d.destinatarios.join(', ') : 'sin destinatarios'}</span>
                    </span>
                    <span className="dato">{NOMBRE_SEVERIDAD[d.min_severidad] ?? d.min_severidad}</span>
                    {d.ultimo_envio && <span className="dato">último: {fecha(d.ultimo_envio)}</span>}
                    {d.activo
                      ? <span className="pastilla ok">Activo</span>
                      : <span className="pastilla dudosa">Pausado</span>}
                    {/* Un fallo no puede quedarse escondido por compacta que
                        sea la fila: sale como pastilla con el motivo en el title. */}
                    {d.ultimo_error && <span className="pastilla bloqueante" title={d.ultimo_error}>falló</span>}
                    {res && <span className={`pastilla ${res.ok ? 'ok' : 'bloqueante'}`} title={res.mensaje}>{res.ok ? 'prueba ok' : 'prueba falló'}</span>}
                    <span className="vista-acciones">
                      <button onClick={() => void probar(d)} disabled={probando === d.id} title="Manda un correo de prueba ahora mismo">
                        {probando === d.id ? 'Enviando…' : 'Probar'}
                      </button>
                      <button onClick={() => setEditando(d)}>Editar</button>
                      <button onClick={() => void borrar(d)}>Borrar</button>
                    </span>
                  </div>
                )
              })}
            </div>
          )}
          {vista === 'detalles' && destinos.map((d) => {
            const res = resultado[d.id]
            return (
              <div key={d.id} className="fila-cuenta fila-apilable">
                <div className="expande-recorta">
                  <div className="fila">
                    <strong className="expande">{d.nombre}</strong>
                    {d.activo
                      ? <span className="pastilla ok" data-guia="avi-estado">Activo</span>
                      : <span className="pastilla dudosa" data-guia="avi-estado">Pausado</span>}
                  </div>
                  <div className="tenue mini-texto" data-guia="avi-resumen">
                    {d.destinatarios.length > 0 ? d.destinatarios.join(', ') : 'sin destinatarios'}
                    {' · '}{NOMBRE_SEVERIDAD[d.min_severidad] ?? d.min_severidad}
                    {d.ultimo_envio && ` · último envío: ${fecha(d.ultimo_envio)}`}
                  </div>
                  {/* El último error se enseña sin tener que volver a probar:
                      un servidor mal configurado falla en silencio, y ese
                      silencio es idéntico al de «no hay nada que avisar». */}
                  {d.ultimo_error && (
                    <div className="mini-texto error">Último intento falló: {d.ultimo_error}</div>
                  )}
                  {res && (
                    <div className={`mini-texto ${res.ok ? 'ok' : 'error'}`}>{res.mensaje}</div>
                  )}
                </div>
                <div className="grupo-acciones">
                  <button onClick={() => void probar(d)} disabled={probando === d.id} data-guia="avi-probar"
                    title="Manda un correo de prueba ahora mismo">
                    {probando === d.id ? 'Enviando…' : 'Probar'}
                  </button>
                  <button onClick={() => setEditando(d)} data-guia="avi-editar">Editar</button>
                  <button onClick={() => void borrar(d)} data-guia="avi-borrar">Borrar</button>
                </div>
              </div>
            )
          })}
        </div>
      </section>

      <div className="nota-previa">
        La contraseña del correo se guarda cifrada y <strong>no vuelve nunca</strong> a
        esta pantalla. Al editar un destino se deja en blanco y se conserva la que ya
        estaba; solo se cambia si escribes una nueva.
      </div>

      <div className="nota-previa" data-guia="avi-nota-envio">
        Los avisos los despacha el <strong>worker</strong>: para que salgan, ese proceso
        tiene que estar arriba (<code>integra worker</code>). Cada aviso se manda una
        sola vez.
      </div>

      {editando && (
        <FormularioDestino
          destino={editando === 'nuevo' ? null : editando}
          onCerrar={() => setEditando(null)}
          onGuardado={() => { setEditando(null); cargar() }}
        />
      )}
    </>
  )
}

const CONFIG_VACIA: ConfigSMTP = {
  host: '', puerto: 587, usuario: '', password: '', remitente: '', destinatarios: [],
}

function FormularioDestino({ destino, onCerrar, onGuardado }: {
  destino: DestinoAviso | null
  onCerrar: () => void
  onGuardado: () => void
}) {
  const [nombre, setNombre] = useState(destino?.nombre ?? 'Operaciones')
  const [minSeveridad, setMinSeveridad] = useState(destino?.min_severidad ?? 'error')
  const [activo, setActivo] = useState(destino?.activo ?? true)
  const [cfg, setCfg] = useState<ConfigSMTP>({
    ...CONFIG_VACIA,
    // Los destinatarios sí vuelven del servidor; el resto de la configuración
    // no, porque va junto a la contraseña.
    destinatarios: destino?.destinatarios ?? [],
  })
  const [correos, setCorreos] = useState((destino?.destinatarios ?? []).join(', '))
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  async function guardar() {
    setError(null)
    const destinatarios = correos.split(',').map((c) => c.trim()).filter(Boolean)
    if (destinatarios.length === 0) {
      setError('Pon al menos un destinatario: un destino sin nadie a quien avisar no avisa.')
      return
    }
    setGuardando(true)
    try {
      await api.guardarDestinoAviso({
        id: destino?.id,
        nombre: nombre.trim(),
        min_severidad: minSeveridad,
        activo,
        config: { ...cfg, destinatarios },
      })
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
            <h2>{destino ? 'Editar destino' : 'Nuevo destino de avisos'}</h2>
            <div className="sub">Correo por SMTP</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          <label className="ancha">
            <span>Nombre</span>
            <input value={nombre} onChange={(e) => setNombre(e.target.value)}
              placeholder="Operaciones" />
          </label>

          <label className="ancha">
            <span>Destinatarios (separados por comas)</span>
            <input value={correos} onChange={(e) => setCorreos(e.target.value)}
              placeholder="operaciones@mdv.com, luis@mdv.com" />
          </label>

          <label>
            <span>Servidor SMTP</span>
            <input value={cfg.host} onChange={(e) => setCfg({ ...cfg, host: e.target.value })}
              placeholder="smtp.gmail.com" />
          </label>
          <label>
            <span>Puerto</span>
            <input type="number" value={cfg.puerto}
              onChange={(e) => setCfg({ ...cfg, puerto: Number(e.target.value) || 587 })} />
          </label>

          <label>
            <span>Usuario</span>
            <input value={cfg.usuario} onChange={(e) => setCfg({ ...cfg, usuario: e.target.value })}
              placeholder="avisos@mdv.com" />
          </label>
          <label>
            <span>Contraseña{destino && ' (en blanco: no se cambia)'}</span>
            <input type="password" value={cfg.password} autoComplete="new-password"
              onChange={(e) => setCfg({ ...cfg, password: e.target.value })} />
          </label>

          <label className="ancha">
            <span>Remitente</span>
            <input value={cfg.remitente} onChange={(e) => setCfg({ ...cfg, remitente: e.target.value })}
              placeholder="avisos@mdv.com" />
          </label>

          <label className="ancha">
            <span>Qué se envía</span>
            <select value={minSeveridad} onChange={(e) => setMinSeveridad(e.target.value as DestinoAviso['min_severidad'])}>
              {SEVERIDADES.map((s) => (
                <option key={s.valor} value={s.valor}>{s.nombre} — {s.pie}</option>
              ))}
            </select>
          </label>

          <label className="ancha casilla">
            <input type="checkbox" checked={activo} onChange={(e) => setActivo(e.target.checked)} />
            <span>Activo</span>
          </label>
        </div>

        <div className="nota-previa">
          Se exige TLS: si el servidor no lo ofrece, el envío falla en vez de mandar la
          contraseña en claro. Tras guardar, usa <strong>Probar</strong> — una
          configuración que nadie ha probado se descubre rota el día que hay un aviso de
          verdad, que es el peor día para descubrirlo.
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando}>
            {guardando ? 'Guardando…' : 'Guardar destino'}
          </button>
        </footer>
      </div>
    </div>
  )
}
