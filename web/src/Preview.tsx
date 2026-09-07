import { useCallback, useEffect, useRef, useState } from 'react'
import { api, money, type Competencia, type PreviewRespuesta, type Proyeccion } from './api'
import { Imagenes } from './Imagenes'
import { PanelAtributos } from './Atributos'
import { Guia } from './Guia'
import { Hoja } from './Hoja'
import { PASOS_PREVIEW } from './guias/preview'
import type { Pestana } from './Editar'

const NOMBRES: Record<string, string> = {
  woocommerce: 'WooCommerce',
  shopify: 'Shopify',
  mercadolibre: 'MercadoLibre',
  falabella: 'Falabella',
}

// Dónde se arregla cada faltante. Un aviso que dice «falta el peso» y no
// lleva a la casilla del peso obliga a cerrar la previa, buscar el producto
// en la lista y recorrer un editor de cinco pestañas. Lo que viene de Odoo
// (referencia, existencias) no tiene sitio en Integra y se dice tal cual.
type Arreglo =
  | { tipo: 'editar'; pestana: Pestana; texto: string }
  | { tipo: 'categorias'; texto: string }
  | { tipo: 'imagenes'; texto: string }
  | { tipo: 'odoo' }

const ARREGLOS: Record<string, Arreglo> = {
  title: { tipo: 'editar', pestana: 'titulos', texto: 'Corregir el título' },
  descripcion: { tipo: 'editar', pestana: 'venta', texto: 'Escribir la descripción' },
  precio: { tipo: 'editar', pestana: 'venta', texto: 'Poner el precio' },
  'attributes.BRAND': { tipo: 'editar', pestana: 'venta', texto: 'Poner la marca' },
  Brand: { tipo: 'editar', pestana: 'venta', texto: 'Poner la marca' },
  vendor: { tipo: 'editar', pestana: 'venta', texto: 'Poner la marca' },
  PackageWeight: { tipo: 'editar', pestana: 'envio', texto: 'Poner el peso' },
  category_id: { tipo: 'categorias', texto: 'Mapear la categoría' },
  PrimaryCategory: { tipo: 'categorias', texto: 'Mapear la categoría' },
  categories: { tipo: 'categorias', texto: 'Mapear la categoría' },
  imagenes: { tipo: 'imagenes', texto: 'Subir fotos' },
  sku: { tipo: 'odoo' },
  stock: { tipo: 'odoo' },
}

// Lo que la previa le pide a la aplicación cuando el arreglo está en otra
// pantalla: abrir el editor de este producto en una pestaña, o ir al mapeo
// de categorías de un canal.
export type DestinoFaltante =
  | { tipo: 'editar'; pestana: Pestana; varianteId: number; sku: string }
  | { tipo: 'categorias'; canal: string }

export function Preview({ varianteId, onCerrar, onIr, onCambio, enVentana }: {
  varianteId: number
  onCerrar: () => void
  onIr?: (destino: DestinoFaltante) => void
  // Algo del producto cambió desde aquí (una foto menos, una portada nueva):
  // la lista de detrás tiene que enterarse sin recargar la página.
  onCambio?: () => void
  // Como página de una ventana del escritorio: sin velo, sin tarjeta y sin
  // Escape (la ventana ya encuadra y ← ya vuelve). Ver Hoja.tsx.
  enVentana?: boolean
}) {
  const refImagenes = useRef<HTMLDivElement>(null)
  const [datos, setDatos] = useState<PreviewRespuesta | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [verJson, setVerJson] = useState<string | null>(null)
  const [competencia, setCompetencia] = useState<Competencia | null>(null)
  const [compEstado, setCompEstado] = useState<'inactivo' | 'cargando' | 'error'>('inactivo')
  const [compError, setCompError] = useState('')
  // Recorrido guiado de esta pantalla, abierto desde el «?» de la cabecera.
  const [ayuda, setAyuda] = useState(false)

  function verCompetencia() {
    setCompEstado('cargando')
    api.competencia(varianteId)
      .then((c) => { setCompetencia(c); setCompEstado('inactivo') })
      .catch((e) => { setCompError(e instanceof Error ? e.message : String(e)); setCompEstado('error') })
  }

  const recargar = useCallback(() => {
    api.preview(varianteId)
      .then(setDatos)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [varianteId])

  useEffect(() => {
    setDatos(null)
    setError(null)
    recargar()
  }, [varianteId, recargar])

  // Escape cierra el panel: es lo que espera cualquiera al ver una capa encima.
  // Aquí no se pregunta nada porque esta pantalla no edita: solo enseña.
  // Con el recorrido abierto, Escape cierra el recorrido y no la previa.
  // Como página de una ventana no hay capa que cerrar (el recorrido cierra
  // con su propio Escape).
  useEffect(() => {
    if (enVentana) return
    const h = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (ayuda) setAyuda(false)
      else onCerrar()
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar, ayuda, enVentana])

  // Cuántos canales están bloqueados. En escritorio se ve de un vistazo con
  // las cuatro tarjetas en fila; en el móvil van apiladas y hay que
  // desplazarse hasta la cuarta, así que el recuento se adelanta arriba.
  const bloqueados = (datos?.proyecciones ?? []).filter(
    (p) => (p.faltantes ?? []).some((f) => f.severidad === 'bloquea'))

  function arreglar(campo: string, canal: string) {
    const a = ARREGLOS[campo]
    if (!a || !datos || a.tipo === 'odoo') return
    if (a.tipo === 'imagenes') {
      // Las fotos se suben aquí mismo, más arriba: basta con ir hasta ellas.
      refImagenes.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return
    }
    if (a.tipo === 'editar') {
      onIr?.({ tipo: 'editar', pestana: a.pestana, varianteId, sku: datos.producto.sku })
    } else {
      onIr?.({ tipo: 'categorias', canal })
    }
  }

  return (
    <Hoja enVentana={enVentana} alFondo={onCerrar}>
        <header className="hoja-cabecera">
          <div>
            <h2>Qué se enviará a cada canal</h2>
            {datos && (
              <div className="sub">
                {datos.producto.sku || '(sin referencia)'} · {datos.producto.nombre_odoo}
              </div>
            )}
          </div>
          <div className="grupo-acciones">
            <button className="mini" type="button" title="Cómo funciona esta pantalla"
              aria-label="Cómo funciona esta pantalla" onClick={() => setAyuda(true)}>?</button>
            <button onClick={onCerrar}>Cerrar ✕</button>
          </div>
        </header>

        {ayuda && <Guia pasos={PASOS_PREVIEW} nombre="Vista previa" onCerrar={() => setAyuda(false)} />}

        {error && <div className="aviso-caja">No se pudo proyectar el producto: {error}</div>}
        {!datos && !error && <div className="vacio">Proyectando…</div>}

        {datos && (
          <>
            {/* Se dice explícitamente qué NO es esto: las maquetas se parecen
                tanto a las tiendas reales que se leían como «así va a quedar
                la publicación», y no lo son. */}
            <div className="nota-previa" data-guia="prev-nota">
              Esto es una <strong>comprobación del contenido</strong> que se va a enviar:
              título, precio, foto de portada y stock, sacados del payload real.
              <strong> No es una simulación de cómo se verá la publicación</strong>: cada
              canal la monta a su manera y añade datos suyos (cuotas, costes de envío,
              impuestos, promociones de la plataforma) que Integra no conoce.
              Nada se publica desde aquí; no se abre conexión con ninguna tienda.
            </div>

            <div className="nota-previa solo-movil">
              {bloqueados.length === 0
                ? `Los ${datos.proyecciones.length} canales pueden publicarse.`
                : `${bloqueados.length} de ${datos.proyecciones.length} canales están bloqueados: ` +
                  bloqueados.map((p) => NOMBRES[p.canal] ?? p.canal).join(', ') + '.'}
            </div>

            <div ref={refImagenes}>
              <Imagenes varianteId={varianteId} onCambio={() => { recargar(); onCambio?.() }} />
            </div>

            <div className="previa-rejilla" data-guia="prev-rejilla">
              {datos.proyecciones.map((p) => (
                <TarjetaCanal key={p.canal} p={p} marca={datos.producto.marca}
                  onVerJson={() => setVerJson(p.canal)}
                  onArreglar={(campo) => arreglar(campo, p.canal)} />
              ))}
            </div>

            <PanelAtributos varianteId={varianteId} />

            <div className="competencia" data-guia="prev-competencia">
              <div className="competencia-cabecera fila-apilable">
                <strong>Competencia en MercadoLibre</strong>
                <button onClick={verCompetencia} disabled={compEstado === 'cargando'}>
                  {compEstado === 'cargando' ? 'Buscando…' : competencia ? 'Actualizar' : 'Comparar precios'}
                </button>
              </div>
              {compEstado === 'error' && (
                <div className="aviso-caja">No se pudo consultar MercadoLibre: {compError}</div>
              )}
              {compEstado === 'cargando' && <div className="vacio">Buscando publicaciones parecidas…</div>}
              {competencia && competencia.items.length === 0 && (
                <div className="vacio">Nadie publica «{competencia.consulta}» en MercadoLibre Colombia.</div>
              )}
              {competencia && competencia.items.length > 0 && (
                <div className="tabla-envoltorio">
                  <table className="tabla-tarjetas">
                    <thead>
                      <tr><th>Publicación</th><th>Vendedor</th><th className="num">Precio</th><th className="num">vs. propio</th></tr>
                    </thead>
                    <tbody>
                      {competencia.items.map((it, i) => {
                        const diff = competencia.precio_propio > 0 && it.precio > 0
                          ? ((competencia.precio_propio - it.precio) / it.precio) * 100
                          : null
                        return (
                          <tr key={i}>
                            <td className="titulo-tarjeta">
                              <a href={it.permalink} target="_blank" rel="noreferrer">{it.titulo}</a>
                            </td>
                            <td className="tenue" data-etiqueta="Vendedor">{it.vendedor}</td>
                            <td className="num" data-etiqueta="Precio">{money(it.precio)}</td>
                            <td className={`num ${diff !== null && diff > 5 ? 'caro' : diff !== null && diff < -5 ? 'barato' : 'tenue'}`}
                              data-etiqueta="vs. propio">
                              {diff === null ? '—' : `${diff > 0 ? '+' : ''}${diff.toFixed(1)} %`}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              )}
            </div>

            {verJson && (
              <div className="json-panel">
                <div className="json-cabecera fila-apilable">
                  <strong>{NOMBRES[verJson] ?? verJson}</strong>
                  <code>
                    {datos.proyecciones.find((p) => p.canal === verJson)?.metodo}{' '}
                    {datos.proyecciones.find((p) => p.canal === verJson)?.endpoint}
                  </code>
                  <button onClick={() => setVerJson(null)}>Ocultar</button>
                </div>
                <pre>
                  {JSON.stringify(
                    datos.proyecciones.find((p) => p.canal === verJson)?.payload, null, 2)}
                </pre>
              </div>
            )}
          </>
        )}
    </Hoja>
  )
}

function TarjetaCanal({ p, marca, onVerJson, onArreglar }: {
  p: Proyeccion; marca: string; onVerJson: () => void; onArreglar: (campo: string) => void
}) {
  // Go serializa los slices vacíos como null, así que todo lo que venga de la
  // API en forma de lista hay que normalizarlo antes de usarlo.
  const bloqueos = p.faltantes?.filter((f) => f.severidad === 'bloquea') ?? []
  const avisos = p.faltantes?.filter((f) => f.severidad === 'advierte') ?? []
  const notas = p.notas ?? []
  const publicable = bloqueos.length === 0
  const chars = [...(p.titulo ?? '')].length

  return (
    <div className={`canal-tarjeta ${publicable ? 'ok' : 'bloqueado'}`}>
      <div className="canal-cabecera">
        <strong>{NOMBRES[p.canal] ?? p.canal}</strong>
        <span className={`pastilla ${publicable ? 'ok' : 'bloqueante'}`}>
          {publicable ? 'Publicable' : `${bloqueos.length} bloqueo${bloqueos.length > 1 ? 's' : ''}`}
        </span>
      </div>

      {/* Los anclajes del recorrido (data-guia) se repiten en las cuatro
          tarjetas a propósito: el recorrido resalta el primero que encuentre,
          y así el paso tiene foco aunque la primera tarjeta no tenga avisos. */}
      {/* El envoltorio conserva el hueco de 10 px que la tarjeta pone entre
          la maqueta y el contador. */}
      <div data-guia="prev-maqueta" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <MaquetaTienda p={p} marca={marca} />
        <div className={`contador ${chars > p.titulo_limite ? 'excede' : ''}`}>
          {chars}/{p.titulo_limite} caracteres del título
        </div>
      </div>

      {bloqueos.length > 0 && (
        <ul className="lista-faltantes bloqueo" data-guia="prev-faltantes">
          {bloqueos.map((f) => (
            <li key={f.campo}>
              <code>{f.campo}</code> — {f.motivo}
              <Arreglar campo={f.campo} onArreglar={onArreglar} />
            </li>
          ))}
        </ul>
      )}
      {avisos.length > 0 && (
        <ul className="lista-faltantes aviso" data-guia="prev-faltantes">
          {avisos.map((f) => (
            <li key={f.campo}>
              <code>{f.campo}</code> — {f.motivo}
              <Arreglar campo={f.campo} onArreglar={onArreglar} />
            </li>
          ))}
        </ul>
      )}
      {notas.length > 0 && (
        <ul className="lista-notas">
          {notas.map((n, i) => <li key={i}>{n}</li>)}
        </ul>
      )}

      <button className="ver-json" data-guia="prev-payload" onClick={onVerJson}>Ver payload JSON</button>
    </div>
  )
}

// Arreglar es el enlace que acompaña a cada faltante: dice qué hacer y
// lleva a donde se hace. Sin él la lista es un diagnóstico sin tratamiento.
function Arreglar({ campo, onArreglar }: { campo: string; onArreglar: (campo: string) => void }) {
  const a = ARREGLOS[campo]
  if (!a) return null
  if (a.tipo === 'odoo') return <span className="tenue"> · se corrige en Odoo</span>
  return (
    <button className="enlace arreglo" type="button" onClick={() => onArreglar(campo)}>
      {a.texto} ›
    </button>
  )
}

// MaquetaTienda enseña el contenido —título, precio, portada, stock— con los
// colores y la tipografía de cada tienda, para reconocer de un vistazo de qué
// canal se habla. NO es la ficha real: solo se pinta lo que Integra envía, así
// que aquí no aparece nada que decida la plataforma (cuotas, envío, impuestos).
// Se quitaron las cuotas «en 36x» de MercadoLibre y el «Envío gratis» porque
// eran inventados: ni el número de cuotas ni el coste del envío salen del
// payload, y quien los leía se los creía.
function MaquetaTienda({ p, marca }: { p: Proyeccion; marca: string }) {
  const sinStock = p.stock <= 0
  const img = p.imagen
    ? <img className="mk-img" src={p.imagen} alt="" />
    : <div className="mk-img mk-img-vacia">sin imagen</div>
  const titulo = p.titulo || 'Sin título'

  switch (p.canal) {
    case 'mercadolibre':
      return (
        <div className="mk">
          <div className="mk-barra mk-barra-ml">Mercado Libre</div>
          {img}
          <div className="mk-cuerpo">
            <div className="mk-titulo-ml">{titulo}</div>
            {p.precio_tachado > 0 && <div className="mk-tachado">{money(p.precio_tachado)}</div>}
            <div className="mk-precio-ml">{money(p.precio)}</div>
            {sinStock
              ? <div className="mk-nodisp">Sin stock</div>
              : <div className="mk-envio">{p.stock} disponibles</div>}
          </div>
        </div>
      )
    case 'falabella':
      return (
        <div className="mk">
          <div className="mk-barra mk-barra-fa">falabella.com</div>
          {img}
          <div className="mk-cuerpo">
            {marca && <div className="mk-marca-fa">{marca.toUpperCase()}</div>}
            <div className="mk-titulo-fa">{titulo}</div>
            <div className="mk-vendedor-fa">Por MDV Distribuidora</div>
            {p.precio_tachado > 0 && <div className="mk-tachado">{money(p.precio_tachado)}</div>}
            <div className="mk-precio-fa">{money(p.precio)}</div>
            {sinStock && <div className="mk-nodisp">No disponible</div>}
          </div>
        </div>
      )
    case 'woocommerce':
      return (
        <div className="mk">
          <div className="mk-barra mk-barra-woo">Tu tienda · WooCommerce</div>
          {img}
          <div className="mk-cuerpo">
            <div className="mk-titulo-woo">{titulo}</div>
            <div className="mk-precio-woo">
              {p.precio_tachado > 0 && <span className="mk-tachado">{money(p.precio_tachado)}</span>}
              {' '}{money(p.precio)}
            </div>
            <div className={`mk-boton mk-boton-woo ${sinStock ? 'agotado' : ''}`}>
              {sinStock ? 'Agotado' : 'Añadir al carrito'}
            </div>
          </div>
        </div>
      )
    default: // shopify
      return (
        <div className="mk">
          <div className="mk-barra mk-barra-sh">Tu tienda · Shopify</div>
          {img}
          <div className="mk-cuerpo">
            <div className="mk-titulo-sh">{titulo}</div>
            <div className="mk-precio-sh">
              {p.precio_tachado > 0 && <span className="mk-tachado">{money(p.precio_tachado)}</span>}
              {' '}{money(p.precio)}
            </div>
            <div className={`mk-boton mk-boton-sh ${sinStock ? 'agotado' : ''}`}>
              {sinStock ? 'Agotado' : 'Agregar al carrito'}
            </div>
          </div>
        </div>
      )
  }
}
