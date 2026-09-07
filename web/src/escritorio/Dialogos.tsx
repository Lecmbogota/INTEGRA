import { useEffect, useRef, useState } from 'react'

// Diálogos del sistema: confirmar y avisar.
//
// Sustituyen a window.confirm y window.alert, que sacan un cuadro del
// navegador ajeno a todo lo demás. Se llaman desde cualquier sitio
// (`await confirmar({...})`) y los pinta un único componente montado en la
// pantalla; si no estuviera montado, se cae al del navegador para que nada
// se quede sin respuesta.

type Peticion = {
  id: number
  titulo?: string
  texto: string
  aceptar: string
  cancelar?: string
  peligroso: boolean
  resolver: (ok: boolean) => void
}

let siguienteId = 1
let cola: Peticion[] = []
let notificar: ((p: Peticion[]) => void) | null = null

function encolar(p: Omit<Peticion, 'id' | 'resolver'>): Promise<boolean> {
  if (!notificar) {
    // Sin el componente montado (por ejemplo antes de entrar), el navegador.
    return Promise.resolve(p.cancelar === undefined ? (window.alert(p.texto), true) : window.confirm(p.texto))
  }
  return new Promise((resolver) => {
    cola = [...cola, { ...p, id: siguienteId++, resolver }]
    notificar?.(cola)
  })
}

export function confirmar(opciones: string | { titulo?: string; texto: string; aceptar?: string; cancelar?: string; peligroso?: boolean }): Promise<boolean> {
  const o = typeof opciones === 'string' ? { texto: opciones } : opciones
  return encolar({ titulo: o.titulo, texto: o.texto, aceptar: o.aceptar ?? 'Aceptar', cancelar: o.cancelar ?? 'Cancelar', peligroso: o.peligroso ?? false })
}

export function avisar(opciones: string | { titulo?: string; texto: string; aceptar?: string }): Promise<void> {
  const o = typeof opciones === 'string' ? { texto: opciones } : opciones
  return encolar({ titulo: o.titulo, texto: o.texto, aceptar: o.aceptar ?? 'Entendido', peligroso: false }).then(() => undefined)
}

// El anfitrión: se monta una vez y pinta el primero de la cola.
export function Dialogos() {
  const [pendientes, setPendientes] = useState<Peticion[]>([])
  const aceptarRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    notificar = setPendientes
    setPendientes(cola)
    return () => { notificar = null }
  }, [])

  const actual = pendientes[0]

  useEffect(() => {
    if (!actual) return
    aceptarRef.current?.focus()
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); cerrar(false) }
      if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); cerrar(true) }
    }
    // En captura, antes que los Escape de las ventanas de detrás.
    window.addEventListener('keydown', h, true)
    return () => window.removeEventListener('keydown', h, true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [actual?.id])

  function cerrar(ok: boolean) {
    if (!actual) return
    cola = cola.filter((p) => p.id !== actual.id)
    setPendientes(cola)
    actual.resolver(ok)
  }

  if (!actual) return null
  return (
    <div className="dialogo-capa" role="presentation" onPointerDown={(e) => { if (e.target === e.currentTarget && actual.cancelar !== undefined) cerrar(false) }}>
      <div className="dialogo" role="alertdialog" aria-modal="true" aria-labelledby="dialogo-titulo" aria-describedby="dialogo-texto">
        <div className="dialogo-icono" aria-hidden="true">
          {actual.peligroso
            ? <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M12 9v4" /><path d="M12 17h.01" /><path d="M10.3 3.9 2.4 17.6A2 2 0 0 0 4.1 20.6h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" /></svg>
            : <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><circle cx="12" cy="12" r="9" /><path d="M12 8h.01" /><path d="M11 12h1v4h1" /></svg>}
        </div>
        <div className="dialogo-cuerpo">
          <h3 id="dialogo-titulo">{actual.titulo ?? (actual.cancelar === undefined ? 'Aviso' : 'Confirmar')}</h3>
          <p id="dialogo-texto">{actual.texto}</p>
          <div className="dialogo-acciones">
            {actual.cancelar !== undefined && <button type="button" onClick={() => cerrar(false)}>{actual.cancelar}</button>}
            <button ref={aceptarRef} type="button" className={actual.peligroso ? 'peligroso' : 'primario'} onClick={() => cerrar(true)}>{actual.aceptar}</button>
          </div>
        </div>
      </div>
    </div>
  )
}
