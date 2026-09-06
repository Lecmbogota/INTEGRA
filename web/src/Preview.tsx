import { useCallback, useEffect, useState } from 'react'
import { api, money, type Competencia, type PreviewRespuesta, type Proyeccion } from './api'
import { Imagenes } from './Imagenes'
import { PanelAtributos } from './Atributos'

const NOMBRES: Record<string, string> = {
  woocommerce: 'WooCommerce',
  shopify: 'Shopify',
  mercadolibre: 'MercadoLibre',
  falabella: 'Falabella',
}

export function Preview({ varianteId, onCerrar }: { varianteId: number; onCerrar: () => void }) {
  const [datos, setDatos] = useState<PreviewRespuesta | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [verJson, setVerJson] = useState<string | null>(null)
  const [competencia, setCompetencia] = useState<Competencia | null>(null)
  const [compEstado, setCompEstado] = useState<'inactivo' | 'cargando' | 'error'>('inactivo')
  const [compError, setCompError] = useState('')

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
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Vista previa de publicación</h2>
            {datos && (
              <div className="sub">
                {datos.producto.sku || '(sin referencia)'} · {datos.producto.nombre_odoo}
              </div>
            )}
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        {error && <div className="aviso-caja">Error: {error}</div>}
        {!datos && !error && <div className="vacio">Proyectando…</div>}

        {datos && (
          <>
            <div className="nota-previa">
              Lo que ves se genera <strong>desde el payload real</strong> que se enviaría.
              Nada se publica: la proyección no abre conexión con ninguna tienda.
            </div>

            <Imagenes varianteId={varianteId} onCambio={recargar} />

            <div className="previa-rejilla">
              {datos.proyecciones.map((p) => (
                <TarjetaCanal key={p.canal} p={p} marca={datos.producto.marca}
                  onVerJson={() => setVerJson(p.canal)} />
              ))}
            </div>

            <PanelAtributos varianteId={varianteId} />

            <div className="competencia">
              <div className="competencia-cabecera">
                <strong>Competencia en MercadoLibre</strong>
                <button onClick={verCompetencia} disabled={compEstado === 'cargando'}>
                  {compEstado === 'cargando' ? 'Buscando…' : competencia ? 'Actualizar' : 'Comparar precios'}
                </button>
              </div>
              {compEstado === 'error' && <div className="aviso-caja">{compError}</div>}
              {competencia && competencia.items.length === 0 && (
                <div className="vacio">Nadie publica «{competencia.consulta}» en MercadoLibre Colombia.</div>
              )}
              {competencia && competencia.items.length > 0 && (
                <table>
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
                          <td><a href={it.permalink} target="_blank" rel="noreferrer">{it.titulo}</a></td>
                          <td className="tenue">{it.vendedor}</td>
                          <td className="num">{money(it.precio)}</td>
                          <td className={`num ${diff !== null && diff > 5 ? 'caro' : diff !== null && diff < -5 ? 'barato' : 'tenue'}`}>
                            {diff === null ? '—' : `${diff > 0 ? '+' : ''}${diff.toFixed(1)} %`}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>

            {verJson && (
              <div className="json-panel">
                <div className="json-cabecera">
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
      </div>
    </div>
  )
}

function TarjetaCanal({ p, marca, onVerJson }: { p: Proyeccion; marca: string; onVerJson: () => void }) {
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

      <MaquetaTienda p={p} marca={marca} />
      <div className={`contador ${chars > p.titulo_limite ? 'excede' : ''}`}>
        {chars}/{p.titulo_limite} caracteres del título
      </div>

      {bloqueos.length > 0 && (
        <ul className="lista-faltantes bloqueo">
          {bloqueos.map((f) => (
            <li key={f.campo}><code>{f.campo}</code> — {f.motivo}</li>
          ))}
        </ul>
      )}
      {avisos.length > 0 && (
        <ul className="lista-faltantes aviso">
          {avisos.map((f) => (
            <li key={f.campo}><code>{f.campo}</code> — {f.motivo}</li>
          ))}
        </ul>
      )}
      {notas.length > 0 && (
        <ul className="lista-notas">
          {notas.map((n, i) => <li key={i}>{n}</li>)}
        </ul>
      )}

      <button className="ver-json" onClick={onVerJson}>Ver payload JSON</button>
    </div>
  )
}

// MaquetaTienda imita la ficha real de cada plataforma, con su tipografía,
// colores y elementos característicos: el objetivo es que quien revisa vea el
// producto como lo verá el comprador, no una tarjeta genérica.
function MaquetaTienda({ p, marca }: { p: Proyeccion; marca: string }) {
  const sinStock = p.stock <= 0
  const img = p.imagen
    ? <img className="mk-img" src={p.imagen} alt="" />
    : <div className="mk-img mk-img-vacia">sin imagen</div>
  const titulo = p.titulo || 'Sin título'

  switch (p.canal) {
    case 'mercadolibre': {
      // MercadoLibre Colombia muestra el precio en cuotas sin interés.
      const cuota = p.precio > 0 ? p.precio / 36 : 0
      return (
        <div className="mk">
          <div className="mk-barra mk-barra-ml">Mercado Libre</div>
          {img}
          <div className="mk-cuerpo">
            <div className="mk-titulo-ml">{titulo}</div>
            {p.precio_tachado > 0 && <div className="mk-tachado">{money(p.precio_tachado)}</div>}
            <div className="mk-precio-ml">{money(p.precio)}</div>
            {cuota > 0 && <div className="mk-cuotas">en 36x {money(cuota)}</div>}
            {sinStock
              ? <div className="mk-nodisp">Sin stock</div>
              : <div className="mk-envio">Envío gratis</div>}
          </div>
        </div>
      )
    }
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
