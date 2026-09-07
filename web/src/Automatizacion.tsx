import { useCallback, useEffect, useState } from 'react'
import { confirmar } from './escritorio/Dialogos'
import { api, fecha, type Alerta, type Horario } from './api'
import { SelectorVista, useVista } from './Vista'

const DIAS = [
  { n: 1, letra: 'L' }, { n: 2, letra: 'M' }, { n: 3, letra: 'X' },
  { n: 4, letra: 'J' }, { n: 5, letra: 'V' }, { n: 6, letra: 'S' }, { n: 7, letra: 'D' },
]

const ALCANCES: Record<string, string> = {
  full: 'Sincronizar Odoo y publicar todo',
  price: 'Solo precios y stock',
  stock: 'Solo stock',
}

const SEVERIDAD: Record<string, string> = {
  critical: 'bloqueante', error: 'bloqueante', warning: 'aviso', info: 'dudosa',
}

const NOMBRE_SEVERIDAD: Record<string, string> = {
  critical: 'Crítico', error: 'Error', warning: 'Aviso', info: 'Info',
}

// Automatización: lo que hace que Integra funcione sola. Sin horarios, cada
// sincronización y cada publicación dependen de que alguien pulse un botón.
export function Automatizacion() {
  const [horarios, setHorarios] = useState<Horario[]>([])
  const [alertas, setAlertas] = useState<Alerta[]>([])
  const [cargando, setCargando] = useState(true)
  // Qué fila está esperando respuesta. En el móvil un botón sin estado se
  // pulsa dos veces sin querer y la acción se manda repetida.
  const [ocupadoHorario, setOcupadoHorario] = useState<number | null>(null)
  const [ocupadaAlerta, setOcupadaAlerta] = useState<number | null>(null)
  const [nuevo, setNuevo] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Un selector por sección: los avisos y los horarios son listas distintas
  // y cada una recuerda su modo.
  const [vistaAvisos, setVistaAvisos] = useVista('automatizacion.avisos', 'detalles', ['detalles', 'lista'])
  const [vistaHorarios, setVistaHorarios] = useVista('automatizacion.horarios', 'detalles', ['detalles', 'lista'])

  const cargar = useCallback(() => {
    Promise.all([api.horarios(), api.alertas()])
      .then(([h, a]) => { setHorarios(h); setAlertas(a) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  // Estas tres acciones fallaban en silencio: la promesa se rompía, nadie la
  // recogía y la pantalla se quedaba igual. Pausar una automatización y creer
  // que quedó pausada es peor que ver el error, porque se sigue publicando.
  async function conAviso(que: string, accion: () => Promise<unknown>) {
    setError(null)
    try {
      await accion()
      cargar()
    } catch (e) {
      setError(`No se pudo ${que}: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  async function alternar(h: Horario) {
    setOcupadoHorario(h.id)
    await conAviso(h.activo ? 'pausar la automatización' : 'reanudar la automatización',
      () => api.guardarHorario({ ...h, activo: !h.activo }))
    setOcupadoHorario(null)
  }

  async function borrar(h: Horario) {
    // Borrar un horario no se deshace y no se nota: la tarea simplemente deja
    // de correr esa noche, y eso solo se descubre cuando el catálogo lleva
    // días sin sincronizar.
    if (!(await confirmar(
      `Se borrará la tarea «${h.nombre}» (${h.hora}). Dejará de ejecutarse y habrá que volver a crearla a mano. ¿Continuar?`))) return
    setOcupadoHorario(h.id)
    await conAviso('borrar la automatización', () => api.borrarHorario(h.id))
    setOcupadoHorario(null)
  }

  async function reconocer(id: number) {
    setOcupadaAlerta(id)
    await conAviso('marcar el aviso como visto', () => api.reconocerAlerta(id))
    setOcupadaAlerta(null)
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Automatización</h1>
          <div className="sub">Horarios que corren solos y avisos de lo que se rompió</div>
        </div>
        <button className="primario" onClick={() => setNuevo(true)} data-guia="aut-nuevo">Nuevo horario</button>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}

      <section className="panel" data-guia="avisos">
        <h2>Avisos abiertos</h2>
        <div className="cuerpo">
          <div className="filtros">
            <SelectorVista modo={vistaAvisos} onCambiar={setVistaAvisos} admitidos={['detalles', 'lista']} />
          </div>
          {cargando && <div className="vacio">Cargando…</div>}
          {!cargando && alertas.length === 0 && <div className="vacio">Nada roto. Todo en orden.</div>}
          {vistaAvisos === 'lista' && alertas.length > 0 && (
            <div className="vista-lista">
              {alertas.map((a) => (
                <div key={a.id} className="fila-lista">
                  <span className={`pastilla ${SEVERIDAD[a.severidad] ?? 'aviso'}`}>
                    {NOMBRE_SEVERIDAD[a.severidad] ?? 'Aviso'}
                  </span>
                  <span className="principal" title={a.mensaje}>{a.mensaje}</span>
                  <span className="dato">{a.canal && `${a.canal} · `}{fecha(a.creada_at)}</span>
                  <span className="vista-acciones">
                    <button onClick={() => void reconocer(a.id)} disabled={ocupadaAlerta === a.id}
                      title="Marcar como visto; volverá a avisar si reaparece">
                      {ocupadaAlerta === a.id ? 'Guardando…' : 'Visto'}
                    </button>
                  </span>
                </div>
              ))}
            </div>
          )}
          {vistaAvisos === 'detalles' && alertas.map((a) => (
            <div key={a.id} className="fila-cuenta fila-apilable" data-guia="aut-aviso">
              {/* La pastilla va dentro del bloque de texto: suelta, al apilarse
                  la fila en el móvil se estiraría a todo el ancho. */}
              <div className="expande-recorta">
                <div className="fila">
                  <span className={`pastilla ${SEVERIDAD[a.severidad] ?? 'aviso'}`}>
                    {NOMBRE_SEVERIDAD[a.severidad] ?? 'Aviso'}
                  </span>
                  <span className="expande">{a.mensaje}</span>
                </div>
                <div className="tenue mini-texto">
                  {a.canal && `${a.canal} · `}{fecha(a.creada_at)}
                </div>
              </div>
              <div className="grupo-acciones">
                <button onClick={() => void reconocer(a.id)} disabled={ocupadaAlerta === a.id} data-guia="aut-visto"
                  title="Marcar como visto; volverá a avisar si reaparece">
                  {ocupadaAlerta === a.id ? 'Guardando…' : 'Visto'}
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="panel" data-guia="aut-horarios">
        <h2>Tareas programadas</h2>
        <div className="cuerpo">
          <div className="filtros">
            <SelectorVista modo={vistaHorarios} onCambiar={setVistaHorarios} admitidos={['detalles', 'lista']} />
          </div>
          {cargando && <div className="vacio">Cargando…</div>}
          {!cargando && horarios.length === 0 && (
            <div className="vacio">
              Sin horarios: hoy nada corre solo. Crea uno para que Integra sincronice
              y publique sin que nadie pulse un botón.
            </div>
          )}
          {vistaHorarios === 'lista' && horarios.length > 0 && (
            <div className="vista-lista">
              {horarios.map((h) => (
                <div key={h.id} className="fila-lista">
                  <span className="sku">{h.hora}</span>
                  <span className="principal" title={ALCANCES[h.alcance] ?? h.alcance}>
                    {h.nombre}<span className="tenue"> · {ALCANCES[h.alcance] ?? h.alcance}</span>
                  </span>
                  <span className="dato">
                    {h.dias.length === 0 ? 'todos los días' : h.dias.map((d) => DIAS[d - 1]?.letra).join(' ')}
                    {h.canal && ` · solo ${h.canal}`}
                  </span>
                  {h.proxima_ejecucion && <span className="dato">próxima: {fecha(h.proxima_ejecucion)}</span>}
                  {h.activo
                    ? <span className="pastilla ok">Activo</span>
                    : <span className="pastilla dudosa">Pausado</span>}
                  <span className="vista-acciones">
                    <button onClick={() => void alternar(h)} disabled={ocupadoHorario === h.id}>
                      {h.activo ? 'Pausar' : 'Activar'}
                    </button>
                    <button onClick={() => void borrar(h)} disabled={ocupadoHorario === h.id}>Borrar</button>
                  </span>
                </div>
              ))}
            </div>
          )}
          {vistaHorarios === 'detalles' && horarios.map((h) => (
            <div key={h.id} className="fila-cuenta fila-apilable" data-guia="aut-horario">
              <div className="expande-recorta">
                <div className="fila">
                  {/* La hora se queda en fila con el nombre en cualquier ancho:
                      su ancho fijo de 52px, apilada, se leería como una altura. */}
                  <span className="hora-horario">{h.hora}</span>
                  <span className="expande">{h.nombre}</span>
                </div>
                <div className="tenue mini-texto">
                  {ALCANCES[h.alcance] ?? h.alcance}
                  {' · '}
                  {h.dias.length === 0 ? 'todos los días' : h.dias.map((d) => DIAS[d - 1]?.letra).join(' ')}
                  {h.canal && ` · solo ${h.canal}`}
                  {h.proxima_ejecucion && ` · próxima: ${fecha(h.proxima_ejecucion)}`}
                </div>
              </div>
              <div className="grupo-acciones" data-guia="aut-horario-acciones">
                {h.activo
                  ? <span className="pastilla ok">Activo</span>
                  : <span className="pastilla dudosa">Pausado</span>}
                <button onClick={() => void alternar(h)} disabled={ocupadoHorario === h.id}>
                  {h.activo ? 'Pausar' : 'Activar'}
                </button>
                <button onClick={() => void borrar(h)} disabled={ocupadoHorario === h.id}>Borrar</button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <div className="nota-previa" data-guia="aut-nota-proceso">
        El planificador corre dentro del <strong>worker</strong>: para que las tareas se
        disparen, ese proceso tiene que estar arriba
        (<code>integra worker</code>). Si estuvo caído a la hora de una tarea, la ejecuta
        al volver en vez de saltársela.
      </div>

      {nuevo && <FormularioHorario onCerrar={() => setNuevo(false)}
        onGuardado={() => { setNuevo(false); cargar() }} />}
    </>
  )
}

function FormularioHorario({ onCerrar, onGuardado }: { onCerrar: () => void; onGuardado: () => void }) {
  const [nombre, setNombre] = useState('Sincronización nocturna')
  const [hora, setHora] = useState('02:00')
  const [alcance, setAlcance] = useState<'full' | 'price' | 'stock'>('full')
  const [dias, setDias] = useState<number[]>([])
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  async function guardar() {
    // Un horario sin nombre se guarda igual, pero luego la lista es una fila
    // en blanco imposible de distinguir de las demás.
    if (nombre.trim() === '') {
      setError('Ponle un nombre a la tarea para reconocerla en la lista.')
      return
    }
    if (!/^\d{2}:\d{2}$/.test(hora)) {
      setError('La hora tiene que estar completa, en formato 24 h (por ejemplo 02:00).')
      return
    }
    setGuardando(true)
    setError(null)
    try {
      await api.guardarHorario({ nombre: nombre.trim(), hora, alcance, dias, activo: true, zona: 'America/Bogota' })
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
            <h2>Nueva tarea programada</h2>
            <div className="sub">Hora de Colombia (America/Bogota)</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}

        <div className="form-edicion">
          <label className="ancha">
            <span>Nombre</span>
            <input value={nombre} onChange={(e) => setNombre(e.target.value)} />
          </label>
          <label>
            <span>Hora</span>
            <input type="time" value={hora} onChange={(e) => setHora(e.target.value)} />
          </label>
          <label>
            <span>Qué hace</span>
            <select value={alcance} onChange={(e) => setAlcance(e.target.value as typeof alcance)}>
              <option value="full">Sincronizar Odoo y publicar todo</option>
              <option value="price">Solo precios y stock</option>
              <option value="stock">Solo stock</option>
            </select>
          </label>
          <label className="ancha">
            <span>Días (ninguno = todos)</span>
            <div className="dias">
              {DIAS.map((d) => (
                <button key={d.n} type="button"
                  className={`dia ${dias.includes(d.n) ? 'activo' : ''}`}
                  aria-pressed={dias.includes(d.n)}
                  onClick={() => setDias(dias.includes(d.n)
                    ? dias.filter((x) => x !== d.n)
                    : [...dias, d.n].sort())}>
                  {d.letra}
                </button>
              ))}
            </div>
          </label>
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar} disabled={guardando}>Cancelar</button>
          <button className="primario" onClick={() => void guardar()} disabled={guardando}>
            {guardando ? 'Guardando…' : 'Crear horario'}
          </button>
        </footer>
      </div>
    </div>
  )
}
