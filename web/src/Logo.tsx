import { useState } from 'react'

// Logo de Integra Logistics & Solutions.
//
// La cabecera intenta cargar el fichero oficial del diseñador desde
// web/public/logo.png (basta con copiarlo ahí, sin tocar código). Mientras no
// exista, se muestra una recreación en SVG del doble hexágono con el
// monograma: es un sustituto, no el original.

export function Logo() {
  const [sinFichero, setSinFichero] = useState(false)

  if (!sinFichero) {
    return (
      <img className="logo-img" src="/logo.png" alt="Integra — Logistics & Solutions"
        onError={() => setSinFichero(true)} />
    )
  }
  return (
    <div className="logo">
      <Marca />
      <div>
        <h1 className="logo-nombre">integra</h1>
        <div className="logo-sub">Logistics &amp; Solutions</div>
      </div>
    </div>
  )
}

// La marca oficial (las dos formas entrelazadas) vive en web/public/marca.png,
// sacada del logo del diseñador y con el fondo transparente para que valga
// sobre cualquier color. Si el fichero faltara, queda la recreación en SVG.
export function Marca({ alto = 44 }: { alto?: number }) {
  const [sinFichero, setSinFichero] = useState(false)
  if (!sinFichero) {
    return (
      <img src="/marca.png" alt="Integra" height={alto} style={{ height: alto, width: 'auto', display: 'block' }}
        onError={() => setSinFichero(true)} />
    )
  }
  return (
    <svg viewBox="0 0 100 100" height={alto} role="img" aria-label="Integra"
      fill="none" stroke="#24476E" strokeWidth="6.5" strokeLinejoin="round" strokeLinecap="round">
      {/* hexágono superior derecho */}
      <polygon points="57,8 32.8,22 32.8,50 57,64 81.2,50 81.2,22" />
      {/* hexágono inferior izquierdo, entrelazado */}
      <polygon points="43,36 18.8,50 18.8,78 43,92 67.2,78 67.2,50" />
      {/* círculo central con el monograma */}
      <circle cx="50" cy="50" r="21" fill="#F8F9FB" />
      <path d="M43 62 V44 l-7 7" />
      <path d="M57 62 V38 l-7 7" />
    </svg>
  )
}
