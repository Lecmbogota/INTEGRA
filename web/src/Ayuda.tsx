import { useEffect, useMemo, useState } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { RECORRIDOS, TOTAL_PASOS, type Recorrido } from './guias'
import type { Seccion } from './Sidebar'

// Centro de ayuda: la puerta a todos los recorridos.
//
// Tres formas de llegar a una explicación: la lista de recorridos (con lo
// que ya se vio y por dónde se iba), la búsqueda sobre el texto de todos los
// pasos (para «¿dónde estaba lo del margen mínimo?») y el manual, que es
// todo el contenido seguido, para leer o imprimir. Es el mismo contenido en
// tres formas, no tres contenidos.

export type Progreso = Record<string, { paso: number; total: number; terminado: boolean; cuando: string }>

type Pestana = 'recorridos' | 'buscar' | 'manual'

const TIPO: Record<Recorrido['tipo'], string> = {
  general: 'Recorrido general',
  seccion: 'Pantalla',
  dialogo: 'Diálogo',
}

// El texto de un paso es JSX; para buscar hace falta texto plano. Se renderiza
// una vez a HTML estático y se quitan las etiquetas.
function textoPlano(nodo: React.ReactNode): string {
  return renderToStaticMarkup(<>{nodo}</>).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()
}

function normalizar(s: string): string {
  return s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
}

// Recorta el texto alrededor de la primera coincidencia, para que el
// resultado enseñe por qué ha salido.
function fragmento(texto: string, consulta: string, ancho = 140): string {
  const i = normalizar(texto).indexOf(normalizar(consulta))
  if (i < 0) return texto.slice(0, ancho) + (texto.length > ancho ? '…' : '')
  const ini = Math.max(0, i - ancho / 3)
  const fin = Math.min(texto.length, i + consulta.length + (ancho * 2) / 3)
  return (ini > 0 ? '…' : '') + texto.slice(ini, fin) + (fin < texto.length ? '…' : '')
}

export function Ayuda({ seccion, progreso, onCerrar, onIniciar }: {
  seccion: Seccion
  progreso: Progreso
  onCerrar: () => void
  onIniciar: (rec: Recorrido, paso: number) => void
}) {
  const [pestana, setPestana] = useState<Pestana>('recorridos')
  const [consulta, setConsulta] = useState('')

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  // Índice de búsqueda: se construye una vez por apertura.
  const indice = useMemo(() => RECORRIDOS.flatMap((rec) =>
    rec.pasos.map((p, k) => ({ rec, k, titulo: p.titulo, texto: textoPlano(p.texto) }))), [])

  const q = consulta.trim()
  const resultados = useMemo(() => {
    if (q.length < 2) return []
    const n = normalizar(q)
    return indice
      .map((e) => {
        const enTitulo = normalizar(e.titulo).includes(n)
        const enTexto = normalizar(e.texto).includes(n)
        return { ...e, puntos: (enTitulo ? 2 : 0) + (enTexto ? 1 : 0) }
      })
      .filter((e) => e.puntos > 0)
      .sort((a, b) => b.puntos - a.puntos)
      .slice(0, 40)
  }, [indice, q])

  const vistos = RECORRIDOS.filter((r) => progreso[r.clave]?.terminado).length
  const actual = RECORRIDOS.find((r) => r.seccion === seccion)

  return (
    <div className="capa ayuda-capa" onClick={onCerrar}>
      <div className="hoja hoja-ancha ayuda-hoja" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Ayuda de Integra</h2>
            <div className="sub">
              {RECORRIDOS.length} recorridos · {TOTAL_PASOS} pasos · {vistos} de {RECORRIDOS.length} vistos
            </div>
          </div>
          <div className="grupo-acciones">
            {pestana === 'manual' && (
              <button type="button" onClick={() => window.print()} title="Imprime el manual o guárdalo como PDF">Imprimir</button>
            )}
            <button onClick={onCerrar}>Cerrar ✕</button>
          </div>
        </header>

        <div className="ayuda-tabs pestanas">
          {([['recorridos', 'Recorridos'], ['buscar', 'Buscar'], ['manual', 'Manual completo']] as [Pestana, string][]).map(([id, nombre]) => (
            <button key={id} type="button" className={`pestana ${pestana === id ? 'activa' : ''}`} onClick={() => setPestana(id)}>{nombre}</button>
          ))}
        </div>

        {pestana === 'recorridos' && (
          <div className="ayuda-cuerpo">
            {actual && (
              <div className="nota-previa">
                Estás en <strong>{actual.nombre}</strong>: su recorrido también se abre con el botón
                «?» de abajo a la derecha o con la tecla <code>?</code>.
              </div>
            )}
            <div className="lista-recorridos">
              {RECORRIDOS.map((r) => {
                const p = progreso[r.clave]
                const aMedias = p && !p.terminado && p.paso > 0
                return (
                  <div key={r.clave} className={`fila-recorrido ${p?.terminado ? 'visto' : ''} ${r.seccion === seccion ? 'actual' : ''}`}>
                    <div className="expande-recorta">
                      <div className="fila">
                        <strong>{r.nombre}</strong>
                        <span className="tenue mini-texto">{TIPO[r.tipo]} · {r.pasos.length} pasos</span>
                        {p?.terminado && <span className="pastilla ok">visto</span>}
                        {aMedias && <span className="pastilla aviso">en el paso {p.paso + 1}</span>}
                      </div>
                      <div className="tenue mini-texto">{r.resumen}</div>
                      {r.tipo === 'dialogo' && (
                        <div className="tenue mini-texto">
                          Se abre completo desde el «?» de ese diálogo; desde aquí se lee sin resaltar nada.
                        </div>
                      )}
                    </div>
                    <div className="grupo-acciones">
                      {aMedias && <button type="button" className="primario" onClick={() => onIniciar(r, p.paso)}>Continuar</button>}
                      <button type="button" onClick={() => onIniciar(r, 0)}>{p ? 'Ver de nuevo' : 'Empezar'}</button>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {pestana === 'buscar' && (
          <div className="ayuda-cuerpo">
            <input type="search" autoFocus className="buscador-producto" placeholder="¿Qué quieres saber? Por ejemplo: portada, margen mínimo, despacho, plantilla…"
              value={consulta} onChange={(e) => setConsulta(e.target.value)} />
            {q.length < 2 && <div className="vacio">Escribe al menos dos letras. Se busca en el título y el texto de los {TOTAL_PASOS} pasos.</div>}
            {q.length >= 2 && resultados.length === 0 && <div className="vacio">Nada con «{q}». Prueba con otra palabra.</div>}
            {resultados.length > 0 && (
              <div className="lista-resultados">
                {resultados.map((e) => (
                  <button type="button" key={`${e.rec.clave}-${e.k}`} className="fila-resultado" onClick={() => onIniciar(e.rec, e.k)}>
                    <div className="fila">
                      <strong>{e.titulo}</strong>
                      <span className="tenue mini-texto">{e.rec.nombre} · paso {e.k + 1}</span>
                    </div>
                    <div className="tenue mini-texto">{fragmento(e.texto, q)}</div>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        {pestana === 'manual' && (
          <div className="ayuda-cuerpo manual">
            <div className="manual-portada">
              <h1>Integra · Manual de uso</h1>
              <p className="tenue">Generado del mismo contenido que los recorridos guiados. {RECORRIDOS.length} capítulos, {TOTAL_PASOS} apartados.</p>
              <ol className="manual-indice">
                {RECORRIDOS.map((r) => <li key={r.clave}><a href={`#manual-${r.clave}`}>{r.nombre}</a> <span className="tenue">· {r.resumen}</span></li>)}
              </ol>
            </div>
            {RECORRIDOS.map((r) => (
              <section key={r.clave} id={`manual-${r.clave}`} className="manual-capitulo">
                <h2>{r.nombre}</h2>
                <p className="tenue">{TIPO[r.tipo]}. {r.resumen}</p>
                {r.pasos.map((p, k) => (
                  <article key={k} className="manual-apartado">
                    <h3>{k + 1}. {p.titulo}</h3>
                    <div className="guia-texto">{p.texto}</div>
                  </article>
                ))}
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
