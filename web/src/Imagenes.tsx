import { useCallback, useEffect, useRef, useState } from 'react'
import { EditorFoto, type FotoEditable } from './EditorFoto'
import { SelectorMediateca } from './SelectorMediateca'
import { api, type ImagenProducto } from './api'

const CANALES = ['woocommerce', 'shopify', 'mercadolibre', 'falabella'] as const
const NOMBRE_CORTO: Record<string, string> = {
  woocommerce: 'Woo', shopify: 'Shopify', mercadolibre: 'ML', falabella: 'Falabella',
}

export function Imagenes({ varianteId, onCambio }: { varianteId: number; onCambio?: () => void }) {
  const [imgs, setImgs] = useState<ImagenProducto[]>([])
  const [error, setError] = useState<string | null>(null)
  const [cargando, setCargando] = useState(true)
  const [subiendo, setSubiendo] = useState(false)
  // Las subidas van en serie, así que se puede decir por cuál va: con diez
  // fotos, un «Procesando…» quieto parece que se ha colgado.
  const [progreso, setProgreso] = useState<{ hecho: number; total: number } | null>(null)
  const [buscando, setBuscando] = useState(false)
  const [resultado, setResultado] = useState<string | null>(null)
  const [arrastrando, setArrastrando] = useState(false)
  // Id de la foto sobre la que hay una acción en marcha, para bloquear sus
  // botones mientras el servidor responde.
  const [ocupada, setOcupada] = useState<number | null>(null)
  const input = useRef<HTMLInputElement>(null)

  const [editando, setEditando] = useState<FotoEditable | null>(null)
  // Elegir de la mediateca: la tercera forma de darle fotos al producto.
  const [eligiendo, setEligiendo] = useState(false)

  const cargar = useCallback(() => {
    setCargando(true)
    api.imagenes(varianteId)
      .then(setImgs)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setCargando(false))
  }, [varianteId])

  useEffect(() => { cargar() }, [cargar])

  async function subir(archivos: FileList | null) {
    if (!archivos || archivos.length === 0) return
    const lista = Array.from(archivos)
    setSubiendo(true)
    setError(null)
    setProgreso({ hecho: 0, total: lista.length })
    try {
      // Se suben en serie a propósito: el procesado genera tres derivadas por
      // fichero y lanzar veinte a la vez satura el servidor sin ganar nada.
      for (const [i, a] of lista.entries()) {
        setProgreso({ hecho: i, total: lista.length })
        await api.subirImagen(varianteId, a)
      }
      cargar()
      onCambio?.()
    } catch (e) {
      // Se recarga igualmente: si falló la cuarta de seis, las tres primeras
      // ya están subidas y hay que verlas.
      cargar()
      onCambio?.()
      setError(`No se pudieron subir todas las fotos: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setSubiendo(false)
      setProgreso(null)
    }
  }

  // Quitar una foto no se deshace y puede dejar el producto sin portada —o sin
  // ninguna imagen, con lo que deja de ser publicable—, así que se avisa.
  async function quitar(img: ImagenProducto) {
    const aviso = img.principal
      ? 'Esta es la foto de portada. Si la quitas, el producto se queda sin portada hasta que elijas otra. ¿Quitarla?'
      : 'Se quita esta foto del banco de Integra. ¿Seguro?'
    if (!window.confirm(aviso)) return

    setOcupada(img.id)
    setError(null)
    try {
      await api.quitarImagen(varianteId, img.id)
      cargar()
      onCambio?.()
    } catch (e) {
      setError(`No se pudo quitar la foto: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupada(null)
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
      setError(`La búsqueda en internet falló: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setBuscando(false)
    }
  }

  async function principal(id: number) {
    setOcupada(id)
    setError(null)
    try {
      await api.imagenPrincipal(varianteId, id)
      cargar()
      onCambio?.()
    } catch (e) {
      setError(`No se pudo cambiar la portada: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupada(null)
    }
  }

  function abrirSelector() {
    // Mientras sube no se abre otra tanda: se mezclarían las dos series.
    if (subiendo) return
    input.current?.click()
  }

  return (
    <div className="imagenes-panel">
      <div className="imagenes-cabecera fila-apilable">
        <strong>Imágenes</strong>
        <span className="tenue crece">
          {cargando
            ? 'cargando…'
            : imgs.length === 0
              ? 'ninguna — el producto no se puede publicar sin foto'
              : `${imgs.length} en el banco de Integra`}
        </span>
        {/* Las tres formas de darle fotos al producto, una al lado de la
            otra: subirlas del PC, elegirlas del banco o buscarlas fuera. */}
        <div className="grupo-acciones" data-guia="img-acciones">
          <button onClick={abrirSelector} disabled={subiendo} title="Elegir archivos de este equipo"
            data-guia="img-subir">
            Subir desde el PC
          </button>
          <button onClick={() => setEligiendo(true)} disabled={subiendo}
            title="Fotos que ya están en el banco de Integra: de otras variantes o huérfanas"
            data-guia="img-mediateca">
            Elegir de la mediateca
          </button>
          <button onClick={() => void buscarEnInternet()} disabled={buscando}
            title="Busca por referencia y marca y descarga varias fotos de al menos 500 px"
            data-guia="img-internet">
            {buscando ? 'Buscando en internet…' : 'Buscar en internet'}
          </button>
        </div>
      </div>

      {/* La explicación estaba solo en el title del botón, y en una tablet o un
          móvil no hay puntero que lo saque: se pasa a texto visible. */}
      <div className="nota-previa">
        <strong>Subir desde el PC</strong> o arrastrar aquí abajo; <strong>elegir de la
        mediateca</strong> las que ya están en Integra (de otra variante, o huérfanas de
        una búsqueda); o <strong>buscar en internet</strong> por referencia y marca, que
        descarga varias de al menos 500 px. Lo descargado hay que revisarlo: comprueba
        que puedes usarlas antes de publicar.
      </div>

      {error && <div className="aviso-caja">{error}</div>}
      {resultado && <div className="nota-previa">{resultado}</div>}

      <div
        className={`soltar ${arrastrando ? 'activo' : ''}`}
        data-guia="img-soltar"
        onDragOver={(e) => { e.preventDefault(); setArrastrando(true) }}
        onDragLeave={() => setArrastrando(false)}
        onDrop={(e) => {
          e.preventDefault()
          setArrastrando(false)
          if (subiendo) return
          void subir(e.dataTransfer.files)
        }}
        onClick={abrirSelector}
        // Es la única forma de añadir fotos: tiene que alcanzarse también con
        // el teclado, no solo con el ratón o el dedo.
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); abrirSelector() }
        }}
      >
        {subiendo
          ? `Procesando foto ${(progreso?.hecho ?? 0) + 1} de ${progreso?.total ?? 1}…`
          : 'Toca aquí para elegir fotos, o arrástralas · JPEG, PNG o WebP'}
        <input ref={input} type="file" accept="image/*" multiple hidden
          onChange={(e) => { void subir(e.target.files); e.target.value = '' }} />
      </div>

      {eligiendo && (
        <SelectorMediateca varianteId={varianteId}
          onCerrar={() => setEligiendo(false)}
          onElegidas={(n) => {
            setEligiendo(false)
            setResultado(`${n} foto${n === 1 ? '' : 's'} añadida${n === 1 ? '' : 's'} desde la mediateca.`)
            cargar()
            onCambio?.()
          }} />
      )}

      {editando && (
        <EditorFoto foto={editando}
          onCerrar={() => setEditando(null)}
          onGuardada={() => { setEditando(null); cargar(); onCambio?.() }} />
      )}

      {imgs.length > 0 && (
        <div className="galeria" data-guia="img-galeria">
          {/* Los anclajes del recorrido (data-guia) van en cada foto: el
              recorrido resalta el primero que encuentre, o sea, la portada. */}
          {imgs.map((i) => (
            <figure key={i.id} className={i.principal ? 'principal' : ''}>
              <img src={i.url} alt="" loading="lazy" />
              <figcaption>
                <div className="dim">{i.ancho}×{i.alto} · {Math.round(i.bytes / 1024)} KB</div>
                <div className="etiquetas" data-guia="img-etiquetas">
                  {CANALES.map((c) => (
                    <span key={c}
                      className={`pastilla ${i.publicable?.[c] ? 'ok' : 'bloqueante'}`}
                      title={(i.problemas?.[c] ?? []).map((p) => p.motivo).join(' · ') || 'cumple'}>
                      {NOMBRE_CORTO[c]}
                    </span>
                  ))}
                </div>
                <div className="acciones" data-guia="img-foto-acciones">
                  {i.principal
                    ? <span className="pastilla ok">Portada</span>
                    : <button onClick={() => void principal(i.id)} disabled={ocupada === i.id}>
                        {ocupada === i.id ? 'Guardando…' : 'Hacer portada'}
                      </button>}
                  <button onClick={() => setEditando(i)} disabled={ocupada === i.id}
                    title="Recortar, girar, encajar en cuadrado, cambiar formato">Editar</button>
                  <button onClick={() => void quitar(i)} disabled={ocupada === i.id}>Quitar</button>
                </div>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </div>
  )
}
