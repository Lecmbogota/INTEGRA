import { useCallback, useEffect, useState } from 'react'
import { api, fecha, type Alerta, type Horario } from './api'

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

// Automatización: lo que hace que Integra funcione sola. Sin horarios, cada
// sincronización y cada publicación dependen de que alguien pulse un botón.
export function Automatizacion() {
  const [horarios, setHorarios] = useState<Horario[]>([])
  const [alertas, setAlertas] = useState<Alerta[]>([])
  const [nuevo, setNuevo] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const cargar = useCallback(() => {
    Promise.all([api.horarios(), api.alertas()])
      .then(([h, a]) => { setHorarios(h); setAlertas(a) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => { cargar() }, [cargar])

  async function alternar(h: Horario) {
    await api.guardarHorario({ ...h, activo: !h.activo })
    cargar()
  }

  async function borrar(id: number) {
    await api.borrarHorario(id)
    cargar()
  }

  async function reconocer(id: number) {
    await api.reconocerAlerta(id)
    cargar()
  }

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Automatización</h1>
          <div className="sub">Horarios que corren solos y avisos de lo que se rompió</div>
        </div>
        <button className="primario" onClick={() => setNuevo(true)}>Nuevo horario</button>
      </header>

      {error && <div className="aviso-caja">Error: {error}</div>}

      <section className="panel">
        <h2>Avisos abiertos</h2>
        <div className="cuerpo">
          {alertas.length === 0 && <div className="vacio">Nada roto. Todo en orden.</div>}
          {alertas.map((a) => (
            <div key={a.id} className="fila-cuenta">
              <span className={`pastilla ${SEVERIDAD[a.severidad] ?? 'aviso'}`}>
                {a.severidad === 'critical' ? 'Crítico' : a.severidad === 'error' ? 'Error' : 'Aviso'}
              </span>
              <div className="crece">
                <div>{a.mensaje}</div>
                <div className="tenue mini-texto">
                  {a.canal && `${a.canal} · `}{fecha(a.creada_at)}
                </div>
              </div>
              <button onClick={() => void reconocer(a.id)} title="Marcar como visto; volverá a avisar si reaparece">
                Visto
              </button>
            </div>
          ))}
        </div>
      </section>

      <section className="panel">
        <h2>Tareas programadas</h2>
        <div className="cuerpo">
          {horarios.length === 0 && (
            <div className="vacio">
              Sin horarios: hoy nada corre solo. Crea uno para que Integra sincronice
              y publique sin que nadie pulse un botón.
            </div>
          )}
          {horarios.map((h) => (
            <div key={h.id} className="fila-cuenta">
              <div className="hora-horario">{h.hora}</div>
              <div className="crece">
                <div>{h.nombre}</div>
                <div className="tenue mini-texto">
                  {ALCANCES[h.alcance] ?? h.alcance}
                  {' · '}
                  {h.dias.length === 0 ? 'todos los días' : h.dias.map((d) => DIAS[d - 1]?.letra).join(' ')}
                  {h.canal && ` · solo ${h.canal}`}
                  {h.proxima_ejecucion && ` · próxima: ${fecha(h.proxima_ejecucion)}`}
                </div>
              </div>
              {h.activo
                ? <span className="pastilla ok">Activo</span>
                : <span className="pastilla dudosa">Pausado</span>}
              <button onClick={() => void alternar(h)}>{h.activo ? 'Pausar' : 'Activar'}</button>
              <button onClick={() => void borrar(h.id)}>Borrar</button>
            </div>
          ))}
        </div>
      </section>

      <div className="nota-previa">
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
    setGuardando(true)
    setError(null)
    try {
      await api.guardarHorario({ nombre, hora, alcance, dias, activo: true, zona: 'America/Bogota' })
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
