import { fecha, motivo, num } from './api'
import { Prioridad } from './Prioridad'
import { Canales } from './Canales'
import { useDatos } from './escritorio/datos'
import { useIr } from './escritorio/sistema'

// El panel: la primera pantalla. Antes era la sección `panel` de App; ahora
// es una app del escritorio que lee los datos globales y navega a la vista
// previa dentro de su propia ventana (← vuelve al panel). Los `data-guia`
// son los que sigue la guía.
export function Panel() {
  const { resumen, atencion, stock, sincronizar, sincronizando } = useDatos()
  const ir = useIr()

  const maxAtencion = Math.max(1, ...atencion.map((a) => a.cantidad))
  const maxStock = Math.max(1, ...stock.map((s) => s.unidades))

  return (
    <>
      <header className="principal" data-guia="pan-cabecera">
        <div>
          <h1 className="titulo-seccion">Panel</h1>
          <div className="sub">
            Catálogo Odoo → canales de venta
            {resumen && <> · última sincronización: {fecha(resumen.ultima_sincronizacion)}</>}
          </div>
        </div>
        <button className="primario" onClick={() => void sincronizar()} disabled={sincronizando} data-guia="pan-sincronizar">
          {sincronizando ? 'Sincronizando…' : 'Sincronizar ahora'}
        </button>
      </header>

      {resumen && (
        <div className="tarjetas" data-guia="resumen">
          <Tarjeta etiqueta="Productos" valor={num(resumen.productos)} pie={`${num(resumen.variantes)} variantes`} />
          <Tarjeta etiqueta="Marcas" valor={num(resumen.marcas)} pie="normalizadas" />
          <Tarjeta etiqueta="Con precio" valor={num(resumen.con_precio)} pie={`de ${num(resumen.variantes)}`} />
          <Tarjeta etiqueta="Con existencias" valor={num(resumen.con_stock)}
            pie={`${num(resumen.stock_total)} unidades`} />
          <Tarjeta etiqueta="Publicables hoy" valor={num(resumen.publicables)}
            tono={resumen.publicables > 0 ? 'ok' : 'error'} destacada
            pie="cumplen todos los requisitos" />
          <Tarjeta etiqueta="Requieren atención" valor={num(resumen.en_atencion)}
            tono="error" pie="avisos abiertos" />
          {/* Los excluidos se muestran para que se vea que existen y no
              parezca que Integra perdió productos por el camino. */}
          <Tarjeta etiqueta="Fuera del catálogo" valor={num(resumen.excluidos)}
            pie="gastos, activos fijos, servicios" />
        </div>
      )}

      <div className="rejilla">
        <section className="panel" data-guia="bloqueos">
          <h2>Qué bloquea la publicación</h2>
          <div className="cuerpo">
            {atencion.length === 0 && <div className="vacio">Nada pendiente.</div>}
            {atencion.map((a) => (
              <div className="barra-fila" key={a.motivo}>
                <div className="nombre">{motivo(a.motivo)}</div>
                <div className="pista">
                  <div className={`relleno ${a.severidad === 'blocking' ? 'error' : 'aviso'}`}
                    style={{ width: `${(a.cantidad / maxAtencion) * 100}%` }} />
                </div>
                <div className="cifra">{num(a.cantidad)}</div>
              </div>
            ))}
          </div>
        </section>

        <section className="panel" data-guia="pan-bodegas">
          <h2>Existencias por bodega</h2>
          <div className="cuerpo">
            {stock.length === 0 && <div className="vacio">Sin datos de stock.</div>}
            {stock.map((s) => (
              <div className="barra-fila" key={s.codigo}>
                <div className="nombre" title={s.nombre}>{s.nombre}</div>
                <div className="pista">
                  <div className="relleno"
                    style={{ width: `${Math.max(0, (s.unidades / maxStock) * 100)}%` }} />
                </div>
                <div className="cifra">{num(s.unidades)}</div>
              </div>
            ))}
          </div>
        </section>
      </div>

      <div className="rejilla">
        <Prioridad onVer={(id) => ir('preview', { varianteId: id })} />
        <Canales />
      </div>
    </>
  )
}

function Tarjeta(props: {
  etiqueta: string; valor: string; pie?: string
  tono?: 'ok' | 'error'; destacada?: boolean
}) {
  return (
    <div className={`tarjeta ${props.destacada ? 'destacada' : ''}`}>
      <div className="etiqueta">{props.etiqueta}</div>
      <div className={`valor ${props.tono ?? ''}`}>{props.valor}</div>
      {props.pie && <div className="pie">{props.pie}</div>}
    </div>
  )
}
