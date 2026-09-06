import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ImagenProducto } from './api'

const CANALES = ['woocommerce', 'shopify', 'mercadolibre', 'falabella'] as const
const NOMBRE_CORTO: Record<string, string> = {
  woocommerce: 'Woo', shopify: 'Shopify', mercadolibre: 'ML', falabella: 'Falabella',
}

export function Imagenes({ varianteId, onCambio }: { varianteId: number; onCambio?: () => void }) {
  const [imgs, setImgs] = useState<ImagenProducto[]>([])
  const [error, setError] = useState<string | null>(null)
  const [subiendo, setSubiendo] = useState(false)
  const [buscando, setBuscando] = useState(false)
  const [resultado, setResultado] = useState<string | null>(null)
  const [arrastrando, setArrastrando] = useState(false)
  const input = useRef<HTMLInputElement>(null)

  const cargar = useCallback(() => {
    api.imagenes(varianteId)
      .then(setImgs)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [varianteId])

  useEffect(() => { cargar() }, [cargar])

  async function subir(archivos: FileList | null) {
    if (!archivos || archivos.length === 0) return
    setSubiendo(true)
    setError(null)
    try {
      // Se suben en serie a propósito: el procesado genera tres derivadas por
      // fichero y lanzar veinte a la vez satura el servidor sin ganar nada.
      for (const a of Array.from(archivos)) {
        await api.subirImagen(varianteId, a)
      }
      cargar()
      onCambio?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSubiendo(false)
    }
  }

  async function quitar(id: number) {
    try {
      await api.quitarImagen(varianteId, id)
      cargar()
      onCambio?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function buscarEnInternet() {
    setBuscando(true)
    setError(null)
    setResultado(null)
    try {
      const r = await api.buscarImagenes(varianteId)
      setResultado(r.agregadas.length > 0
        ? `${r.agregadas.length} fotos descargadas buscando «${r.consulta}» — quita las que no sirvan y elige portada.`
        : `Sin resultados útiles para «${r.consulta}» (${r.descartadas} descartadas por tamaño o error). Prueba subirlas a mano.`)
      cargar()
      onCambio?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBuscando(false)
    }
  }

  async function principal(id: number) {
    try {
      await api.imagenPrincipal(varianteId, id)
      cargar()
      onCambio?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="imagenes-panel">
      <div className="imagenes-cabecera">
        <strong>Imágenes</strong>
        <span className="tenue crece">
          {imgs.length === 0
            ? 'ninguna — el producto no se puede publicar sin foto'
            : `${imgs.length} en el banco de Integra`}
        </span>
        <button onClick={() => void buscarEnInternet()} disabled={buscando}
          title="Busca por SKU y marca; descarga varias opciones de al menos 500 px. Verifica que puedes usar las fotos antes de publicar.">
          {buscando ? 'Buscando en internet…' : 'Buscar en internet'}
        </button>
      </div>

      {error && <div className="aviso-caja">{error}</div>}
      {resultado && <div className="nota-previa">{resultado}</div>}

      <div
        className={`soltar ${arrastrando ? 'activo' : ''}`}
        onDragOver={(e) => { e.preventDefault(); setArrastrando(true) }}
        onDragLeave={() => setArrastrando(false)}
        onDrop={(e) => {
          e.preventDefault()
          setArrastrando(false)
          void subir(e.dataTransfer.files)
        }}
        onClick={() => input.current?.click()}
      >
        {subiendo
          ? 'Procesando…'
          : 'Arrastra las fotos aquí o haz clic para elegirlas · JPEG, PNG o WebP'}
        <input ref={input} type="file" accept="image/*" multiple hidden
          onChange={(e) => { void subir(e.target.files); e.target.value = '' }} />
      </div>

      {imgs.length > 0 && (
        <div className="galeria">
          {imgs.map((i) => (
            <figure key={i.id} className={i.principal ? 'principal' : ''}>
              <img src={i.url} alt="" loading="lazy" />
              <figcaption>
                <div className="dim">{i.ancho}×{i.alto} · {Math.round(i.bytes / 1024)} KB</div>
                <div className="etiquetas">
                  {CANALES.map((c) => (
                    <span key={c}
                      className={`pastilla ${i.publicable?.[c] ? 'ok' : 'bloqueante'}`}
                      title={(i.problemas?.[c] ?? []).map((p) => p.motivo).join(' · ') || 'cumple'}>
                      {NOMBRE_CORTO[c]}
                    </span>
                  ))}
                </div>
                {i.verificacion !== '' && i.verificacion !== 'corresponde' && (
                  <div className="etiquetas">
                    <span className={`pastilla ${i.verificacion === 'no_corresponde' ? 'bloqueante' : 'dudosa'}`}
                      title={i.verificacion_nota}>
                      {i.verificacion === 'no_corresponde' ? 'No es el producto' : 'Dudosa'}
                    </span>
                  </div>
                )}
                <div className="acciones">
                  {i.principal
                    ? <span className="pastilla ok">Portada</span>
                    : <button onClick={() => principal(i.id)}>Hacer portada</button>}
                  <button onClick={() => quitar(i.id)}>Quitar</button>
                </div>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </div>
  )
}
