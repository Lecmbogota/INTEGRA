import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'

// Modo de vista de una lista, como en el explorador de Windows: la misma
// información se enseña como tabla, como filas compactas, como tarjetas o
// como iconos grandes. Cada pantalla decide cuáles admite; la elección se
// recuerda por pantalla para que Productos pueda quedarse en mosaico sin
// arrastrar a Pedidos.
export type ModoVista = 'detalles' | 'lista' | 'mosaico' | 'iconos'

const TODOS: ModoVista[] = ['detalles', 'lista', 'mosaico', 'iconos']

const NOMBRE: Record<ModoVista, string> = {
  detalles: 'Detalles',
  lista: 'Lista',
  mosaico: 'Mosaico',
  iconos: 'Iconos',
}

const PISTA: Record<ModoVista, string> = {
  detalles: 'Tabla con columnas',
  lista: 'Filas compactas, una línea por elemento',
  mosaico: 'Tarjetas con imagen y datos clave',
  iconos: 'Cuadrícula de iconos grandes',
}

const CLAVE = (clave: string) => `integra.vista.${clave}`

function leer(clave: string, porDefecto: ModoVista, admitidos: ModoVista[]): ModoVista {
  // localStorage puede no existir (navegación privada estricta) y lo guardado
  // puede ser de una versión que admitía un modo que ya no: en ambos casos
  // se vuelve al modo por defecto sin romper la pantalla.
  try {
    const v = window.localStorage.getItem(CLAVE(clave)) as ModoVista | null
    if (v && admitidos.includes(v)) return v
  } catch { /* sin almacenamiento: se usa el modo por defecto */ }
  return porDefecto
}

// useVista devuelve el modo actual de una pantalla y la función para
// cambiarlo. `clave` identifica la pantalla en localStorage.
export function useVista(
  clave: string,
  porDefecto: ModoVista,
  admitidos: ModoVista[] = TODOS,
): [ModoVista, (m: ModoVista) => void] {
  const [modo, setModo] = useState<ModoVista>(() => leer(clave, porDefecto, admitidos))
  // `admitidos` suele llegar como literal nuevo en cada render; se compara por
  // contenido para que `cambiar` no cambie de identidad sin motivo.
  const firma = admitidos.join(',')
  const cambiar = useCallback((m: ModoVista) => {
    if (!firma.split(',').includes(m)) return
    setModo(m)
    try { window.localStorage.setItem(CLAVE(clave), m) } catch { /* se pierde al recargar, nada más */ }
  }, [clave, firma])
  return [modo, cambiar]
}

// Iconos de 16×16 en currentColor, para que hereden el color del botón en
// los dos temas.
function Icono({ modo }: { modo: ModoVista }) {
  const comun = { width: 16, height: 16, viewBox: '0 0 16 16', fill: 'currentColor', 'aria-hidden': true }
  switch (modo) {
    case 'detalles':
      // Tabla: cabecera y tres filas partidas en columnas.
      return (
        <svg {...comun}>
          <path d="M1 2h14v2H1zM1 5.5h4v2H1zm5 0h4v2H6zm5 0h4v2h-4zM1 9h4v2H1zm5 0h4v2H6zm5 0h4v2h-4zM1 12.5h4v2H1zm5 0h4v2H6zm5 0h4v2h-4z" />
        </svg>
      )
    case 'lista':
      // Filas compactas: punto y línea.
      return (
        <svg {...comun}>
          <path d="M1 2.5h2v2H1zm3 0h11v2H4zM1 7h2v2H1zm3 0h11v2H4zm-3 4.5h2v2H1zm3 0h11v2H4z" />
        </svg>
      )
    case 'mosaico':
      // Tarjetas medianas: cuatro bloques con su «imagen» encima.
      return (
        <svg {...comun}>
          <path d="M1 1h6v4H1zm0 4.5h6v1.5H1zM9 1h6v4H9zm0 4.5h6v1.5H9zM1 9h6v4H1zm0 4.5h6V15H1zM9 9h6v4H9zm0 4.5h6V15H9z" />
        </svg>
      )
    case 'iconos':
      // Cuadrícula grande: cuatro cuadrados.
      return (
        <svg {...comun}>
          <path d="M1 1h6v6H1zm8 0h6v6H9zM1 9h6v6H1zm8 0h6v6H9z" />
        </svg>
      )
  }
}

// Imagen es el hueco de foto de una tarjeta o un icono: la miniatura si hay
// sha256 y, si no, un marco vacío con un icono, para que la rejilla no baile
// entre productos con foto y sin ella. Los hijos (una casilla, una pastilla)
// se posan encima.
export function Imagen({ sha, variante = 'miniatura_300', titulo, onClick, className = '', children }: {
  sha: string
  // Derivada del servidor: miniatura_300 para rejillas, web_800 para grande.
  variante?: string
  titulo?: string
  onClick?: () => void
  className?: string
  children?: ReactNode
}) {
  return (
    <div className={`vista-imagen ${onClick ? 'abrible' : ''} ${className}`} title={titulo}
      onClick={onClick ? (e) => { e.stopPropagation(); onClick() } : undefined}>
      {sha
        ? <img src={`/imagenes/${sha}/${variante}`} alt="" loading="lazy" />
        : (
          <svg width="32" height="32" viewBox="0 0 16 16" fill="currentColor" aria-label="Sin foto">
            <path d="M2 2h12v12H2zm1 1v7.6l3-3 3 3 2-2 3 3V3zm0 10h10v-.6l-3-3-2 2-3-3-2 2zM10 5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3z" />
          </svg>
        )}
      {children}
    </div>
  )
}

// SelectorVista es el grupo de botones que cambia el modo. Va a la derecha
// de la barra de filtros de cada pantalla. Atajo: Ctrl+Shift+1..4 (en el
// orden detalles, lista, mosaico, iconos) mientras el foco esté dentro de
// la pantalla que lo contiene —en el escritorio, dentro de su ventana— para
// que dos ventanas abiertas no cambien de vista a la vez.
export function SelectorVista({ modo, onCambiar, admitidos = TODOS }: {
  modo: ModoVista
  onCambiar: (m: ModoVista) => void
  admitidos?: ModoVista[]
}) {
  const raiz = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return
      // Se mira el código físico y no `key`: con Shift, la tecla 1 da «!».
      const n = /^Digit([1-4])$/.exec(e.code)
      if (!n) return
      const objetivo = TODOS[Number(n[1]) - 1]
      if (!admitidos.includes(objetivo)) return
      // Solo si el foco está en esta pantalla. Fuera del escritorio la
      // pantalla es `.contenido`; dentro, su `.ventana`, y vale también
      // que la ventana sea la activa aunque el foco esté en el cuerpo.
      const ambito = raiz.current?.closest('.ventana, .contenido')
      const activo = document.activeElement
      const dentro = ambito
        ? ambito.contains(activo) || ambito.classList.contains('activa')
        : true
      if (!dentro) return
      e.preventDefault()
      onCambiar(objetivo)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [admitidos, onCambiar])

  return (
    <div className="selector-vista" role="group" aria-label="Modo de vista" ref={raiz}>
      {TODOS.filter((m) => admitidos.includes(m)).map((m) => (
        <button key={m} type="button" className={`badge ${modo === m ? 'activo' : ''}`}
          aria-pressed={modo === m} aria-label={NOMBRE[m]}
          title={`${NOMBRE[m]} — ${PISTA[m]} (Ctrl+Shift+${TODOS.indexOf(m) + 1})`}
          onClick={() => onCambiar(m)}>
          <Icono modo={m} />
        </button>
      ))}
    </div>
  )
}
