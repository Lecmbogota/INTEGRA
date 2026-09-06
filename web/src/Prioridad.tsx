import { useEffect, useState } from 'react'
import { api, money, num, type FilaPrioridad } from './api'

// Qué publicar primero: lo que ya cumple todos los requisitos, ordenado por
// el valor de inventario que espera venderse. Con las órdenes (Fase 5) se
// sumará la rotación real como criterio.
export function Prioridad({ onVer }: { onVer: (varianteId: number) => void }) {
  const [filas, setFilas] = useState<FilaPrioridad[]>([])

  useEffect(() => {
    api.prioridad(10).then(setFilas).catch(() => setFilas([]))
  }, [])

  return (
    <section className="panel">
      <h2>Qué publicar primero</h2>
      <div className="cuerpo">
        {filas.length === 0 && <div className="vacio">Sin candidatos todavía.</div>}
        {filas.map((f, i) => (
          <div key={f.variante_id} className="fila-prioridad clicable"
            onClick={() => onVer(f.variante_id)} title="Ver vista previa">
            <div className="puesto">{i + 1}</div>
            <div className="crece">
              <div className="nombre-prioridad">{f.nombre}</div>
              <div className="tenue mini-texto">
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
