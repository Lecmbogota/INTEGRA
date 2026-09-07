import { createContext, useContext, type ReactNode } from 'react'
import type { SesionActiva } from './tipos'

// La sesión, disponible para cualquier pieza del escritorio sin pasarla por
// props: la barra enseña el nombre, el gestor de ventanas guarda por usuario,
// el menú de inicio cierra sesión.
const Contexto = createContext<SesionActiva | null>(null)

export function ProveedorSesion({ valor, children }: { valor: SesionActiva; children: ReactNode }) {
  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

export function useSesion(): SesionActiva {
  const s = useContext(Contexto)
  if (!s) throw new Error('useSesion fuera de ProveedorSesion')
  return s
}
