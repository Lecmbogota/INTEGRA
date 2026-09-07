import type { KeyboardEvent, ReactNode } from 'react'

// Envoltorio de los diálogos (editar, previa, editor de fotos…), que viven
// de dos maneras:
//
//   - como modal de una pantalla (fuera del escritorio): un velo `.capa`
//     que cierra al tocar fuera y una tarjeta `.hoja` encima;
//   - como página de una ventana del escritorio (`enVentana`): sin velo ni
//     tarjeta, porque la ventana ya encuadra y ← ya vuelve. Entonces es un
//     `.pagina-dialogo` a todo el ancho (apps.css) que conserva las clases
//     modificadoras de la hoja (`hoja-editor-foto`, `ayuda-hoja`…) para que
//     su disposición interna siga aplicando.
//
// Antes la segunda forma se conseguía pintando igualmente `.capa` y `.hoja`
// y anulando por CSS su posición fija, su velo y su tarjeta; pintar de
// entrada lo que toca evita depender de esas anulaciones (y de que una
// regla más específica las gane y deje algo flotando fuera de la ventana).
export function Hoja({ enVentana, clase, claseCapa, alFondo, onKeyDown, children }: {
  enVentana?: boolean
  // Clases modificadoras de la hoja, además de `hoja` (p. ej. 'hoja-editor').
  clase?: string
  // Clases del velo, además de `capa` (solo como modal).
  claseCapa?: string
  // Clic en el velo, fuera de la hoja (solo como modal).
  alFondo?: () => void
  onKeyDown?: (e: KeyboardEvent<HTMLDivElement>) => void
  children: ReactNode
}) {
  const extra = clase ? ` ${clase}` : ''
  if (enVentana) {
    return <div className={`pagina-dialogo${extra}`} onKeyDown={onKeyDown}>{children}</div>
  }
  return (
    <div className={`capa${claseCapa ? ` ${claseCapa}` : ''}`} onClick={alFondo}>
      <div className={`hoja${extra}`} onClick={(e) => e.stopPropagation()} onKeyDown={onKeyDown}>
        {children}
      </div>
    </div>
  )
}
