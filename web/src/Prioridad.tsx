import { useEffect, useState } from 'react'
import { api, money, num, type FilaPrioridad } from './api'

// Qué publicar primero: lo que ya cumple todos los requisitos, ordenado por
// el valor de inventario que espera venderse. Con las órdenes (Fase 5) se
// sumará la rotación real como criterio.
export function Prioridad({ onVer }: { onVer: (varianteId: number) => void }) {
  const [filas, setFilas] = useState<FilaPrioridad[]>([])
  const [cargado, setCargado] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    // Antes el catch dejaba la lista vacía sin decir nada: un fallo del
    // servidor era indistinguible de «no hay candidatos».
    api.prioridad(10)
      .then((f) => { setFilas(f); setError(null) })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargado(true))
  }, [])

  return (
    <section className="panel">
      <h2>Qué publicar primero</h2>
      <div className="cuerpo">
        {error && <div className="aviso-caja">No se pudo cargar la lista: {error}</div>}
        {!cargado && !error && <div className="vacio">Cargando…</div>}
        {cargado && !error && filas.length === 0 && (
          <div className="vacio">Sin candidatos todavía: ningún producto con stock esperando publicación.</div>
        )}
        {filas.length > 0 && (
          <div className="tenue mini-texto">
            Ordenado por valor de inventario. «Listo» significa que cumple todos los
            requisitos para publicar. Toca una fila para ver la vista previa.
          </div>
        )}
        {filas.map((f, i) => (
          <div key={f.variante_id} className="fila-prioridad"
            role="button" tabIndex={0}
            onClick={() => onVer(f.variante_id)}
            // La fila es un div: sin esto no hay forma de abrirla con el teclado.
            onKeyDown={(ev) => {
              if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); onVer(f.variante_id) }
            }}
            title={`Ver la vista previa de ${f.nombre}`}>
            <div className="puesto">{i + 1}</div>
            <div className="crece">
              <div className="nombre-prioridad">{f.nombre}</div>
              <div className="tenue mini-texto recorta">
                {f.sku}{f.marca ? ` · ${f.marca}` : ''} · {num(f.stock)} uds
              </div>
            </div>
            <div className="num">
              <div><strong>{money(f.valor_stock)}</strong></div>
              {f.listo
                ? <span className="pastilla ok">Listo</span>
                : <span className="pastilla aviso">{f.bloqueos} bloqueo{f.bloqueos === 1 ? '' : 's'}</span>}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}
