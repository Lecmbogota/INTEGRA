import { useEffect, useRef, useState } from 'react'
import { api, money, motivo, num, type Categoria, type Marca, type PaginaProductos, type Producto, rolActual , type CuentaCanal } from './api'
import { Editar, type Pestana } from './Editar'
import { EdicionMasiva } from './EdicionMasiva'
import { PlantillaMasiva } from './PlantillaMasiva'
import { useEvento, useSistemaOpcional } from './escritorio/sistema'
import { Imagen, SelectorVista, useVista } from './Vista'

const POR_PAGINA = 50
// Motivos que impiden publicar. Un producto con cualquiera de ellos no sale al
// canal, así que la pantalla los marca distinto de los que solo empeoran la
// ficha.
// Los canales por su nombre y los estados por lo que significan, para que la
// pastilla se lea sin conocer las claves internas.
const NOMBRE_CANAL: Record<string, string> = {
  mercadolibre: 'MercadoLibre', falabella: 'Falabella',
  woocommerce: 'WooCommerce', shopify: 'Shopify',
}
const ESTADO_CANAL: Record<string, string> = {
  published: 'publicado', paused: 'retirado de la venta',
  error: 'falló el último envío', pending: 'en cola',
}

const BLOQUEANTES = new Set([
  'missing_sku', 'duplicate_sku', 'missing_description', 'missing_price', 'price_below_cost',
  // Sin foto no publica ningún canal.
  'missing_image',
])
const bloqueante = (m: string) => BLOQUEANTES.has(m)

// AbrirEditor es lo que pide la vista previa al pulsar «Poner el peso»: que
// esta pantalla abra el editor de ese producto ya en la pestaña del peso.
export type AbrirEditor = { id: number; sku: string; pestana: Pestana }

export function Catalogo({ marcas, categorias = [], onVer, onCambio, abrir = null, onAbierto, refresco = 0 }: {
  marcas: Marca[]
  categorias?: Categoria[]
  onVer: (varianteId: number) => void
  onCambio: () => void
  abrir?: AbrirEditor | null
  onAbierto?: () => void
  // Sube cuando algo cambió fuera de esta pantalla (la vista previa quitó
  // una foto): la lista se vuelve a pedir sin tocar filtros ni página.
  refresco?: number
}) {
  const [pagina, setPagina] = useState<PaginaProductos | null>(null)
  const [busqueda, setBusqueda] = useState('')
  const [marca, setMarca] = useState('')
  const [categoria, setCategoria] = useState('')
  const [soloProblemas, setSoloProblemas] = useState(false)
  const [verExcluidos, setVerExcluidos] = useState(false)
  const [sinPrecio, setSinPrecio] = useState(false)
  // Lo que aún no está en ningún canal. Es el filtro que contesta «¿qué me
  // falta por subir?», que antes obligaba a comparar dos pantallas a ojo.
  const [sinPublicar, setSinPublicar] = useState(false)
  // Criterios que solo se usan desde la plantilla, de momento. Viven aquí y no
  // allí porque el filtro es uno solo: lo que se descarga tiene que ser lo
  // mismo que se ve en la lista de detrás.
  const [sinFoto, setSinFoto] = useState(false)
  const [sinDescripcion, setSinDescripcion] = useState(false)
  const [sinEAN, setSinEAN] = useState(false)
  const [conPromo, setConPromo] = useState(false)
  const [orden, setOrden] = useState('nombre')
  const [ordenDesc, setOrdenDesc] = useState(false)
  const [offset, setOffset] = useState(0)
  const [editando, setEditando] = useState<Producto | null>(null)
  const [pestanaEditor, setPestanaEditor] = useState<Pestana>('venta')
  // La petición de abrir un producto concreto se resuelve cuando llega la
  // lista que lo contiene, no antes: la lista se recarga al buscarlo.
  const pendiente = useRef<AbrirEditor | null>(null)
  const [version, setVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [cargando, setCargando] = useState(true)
  const [marcados, setMarcados] = useState<Set<number>>(new Set())
  const [masiva, setMasiva] = useState(false)
  const [plantilla, setPlantilla] = useState(false)
  const [publicando, setPublicando] = useState(false)
  const esAdmin = rolActual() === 'admin'
  const [despublicando, setDespublicando] = useState(false)
  // El resultado de publicar tiene su propio aviso: el banner de error de la
  // pantalla dice «no se pudo cargar la lista», que no es lo que pasó.
  const [aviso, setAviso] = useState<{ texto: string; malo: boolean } | null>(null)
  // Cómo se enseña la lista: la tabla de siempre o tarjetas/iconos con la
  // portada, que es lo que permite reconocer un producto de un vistazo.
  const [vista, setVista] = useVista('catalogo', 'detalles')

  // Dentro del escritorio, los diálogos se abren en su propia ventana y el
  // resultado vuelve por el bus (la ventana no sabe quién la abrió). Fuera
  // (sistema null) siguen siendo modales de esta pantalla, sin cambios.
  const sistema = useSistemaOpcional()
  useEvento('producto-cambiado', () => setVersion((v) => v + 1))
  useEvento('fotos-cambiadas', () => setVersion((v) => v + 1))
  function abrirEditor(p: Producto, pestana: Pestana) {
    if (sistema) {
      sistema.abrir('editar', { varianteId: p.id, sku: p.sku, pestana }, { titulo: `Editar · ${p.sku || p.nombre}` })
    } else {
      setPestanaEditor(pestana)
      setEditando(p)
    }
  }
  function abrirPlantilla() {
    if (sistema) sistema.abrir('plantilla', {})
    else setPlantilla(true)
  }
  function abrirMasiva() {
    if (sistema) {
      const cuantos = pagina?.total ?? 0
      sistema.abrir('edicion-masiva', { seleccion: { ids: [...marcados], filtro: filtroActual }, cuantos },
        { titulo: marcados.size > 0 ? `Editar en masa · ${num(marcados.size)} marcados` : `Editar en masa · ${num(cuantos)} del filtro` })
    } else {
      setMasiva(true)
    }
  }

  // Publicar lo seleccionado. Planificar la cuenta entera es lo correcto para
  // la corrida nocturna, pero quien acaba de arreglar tres fichas quiere
  // verlas en el canal sin esperar a que pase por delante todo el catálogo.
  // Despublicar quita la ficha del canal. El producto NO se borra: viene de
  // Odoo y sigue en Integra intacto, listo para volver a publicarse cuando se
  // quiera. Lo que se pierde es lo que vivía en el canal —historial,
  // preguntas, reseñas, posición en el buscador— y la dirección de la ficha.
  //
  // Es distinto de «Retirar de la venta»: eso la deja pausada y se puede
  // reabrir con su historial entero. Por eso son dos acciones y no una.
  async function despublicarDe(cuentas: CuentaCanal[]) {
    setAviso(null)
    setDespublicando(false)
    setPublicando(true)
    try {
      const ids = [...marcados]
      let total = 0
      for (const c of cuentas) total += (await api.borrarPublicaciones(c.id, ids)).encoladas
      setMarcados(new Set())
      setAviso({
        texto: `${num(total)} publicaciones encoladas para quitarse del canal. Los productos siguen en Integra.`,
        malo: false,
      })
    } catch (e) {
      setAviso({ texto: `No se pudo despublicar: ${e instanceof Error ? e.message : String(e)}`, malo: true })
    } finally {
      setPublicando(false)
    }
  }

  async function publicarSeleccion() {
    setError(null)
    setAviso(null)
    setPublicando(true)
    try {
      const cuentas = await api.cuentas()
      const activas = cuentas.filter((c) => c.activa)
      if (activas.length === 0) {
        setAviso({ texto: 'No hay ninguna cuenta de canal activa: configúrala en Canales antes de publicar.', malo: true })
        return
      }
      const ids = [...marcados]
      const partes: string[] = []
      for (const c of activas) {
        const p = await api.planificar(c.id, ids)
        const total = p.publicar + p.precio + p.stock
        partes.push(total === 0
          ? `${c.canal}: nada que enviar${p.no_listos > 0 ? ` (${num(p.no_listos)} sin requisitos)` : ''}`
          : `${c.canal}: ${num(total)} envíos`)
      }
      setAviso({
        texto: `${partes.join(' · ')}. El worker los procesa en segundo plano; su avance se ve en Publicación.`,
        malo: false,
      })
    } catch (e) {
      setAviso({
        texto: `No se pudo publicar la selección: ${e instanceof Error ? e.message : String(e)}`,
        malo: true,
      })
    } finally {
      setPublicando(false)
    }
  }

  const filtroActual = {
    q: busqueda, marca, categoria, problemas: soloProblemas,
    excluidos: verExcluidos, sin_precio: sinPrecio, sin_publicar: sinPublicar,
    sin_foto: sinFoto, sin_descripcion: sinDescripcion, sin_ean: sinEAN, con_promo: conPromo,
    orden, desc: ordenDesc,
  }
  const hayFiltro = !!(busqueda || marca || categoria || soloProblemas || verExcluidos || sinPrecio || sinPublicar)

  // Pulsar una cabecera ordena por ella; volver a pulsarla invierte el
  // sentido. Se vuelve a la primera página porque, si no, se sigue viendo la
  // página 3 de un orden que ya no existe.
  const ordenarPor = (col: string) => {
    if (orden === col) setOrdenDesc(!ordenDesc)
    else { setOrden(col); setOrdenDesc(false) }
    setOffset(0)
  }
  const flecha = (col: string) => orden === col ? (ordenDesc ? ' ↓' : ' ↑') : ''

  const alternar = (id: number) => {
    const s = new Set(marcados)
    s.has(id) ? s.delete(id) : s.add(id)
    setMarcados(s)
  }
  const items = pagina?.items ?? []
  // `every` sobre una lista vacía da true, y eso dejaría la casilla de
  // «seleccionar todo» marcada en una página sin productos.
  const paginaEntera = items.length > 0 && items.every((p) => marcados.has(p.id))
  const alternarPagina = () => {
    const s = new Set(marcados)
    for (const p of items) paginaEntera ? s.delete(p.id) : s.add(p.id)
    setMarcados(s)
  }

  // Al venir de la vista previa se busca el producto por su referencia en vez
  // de confiar en que esté en la página actual: esta pantalla se monta de
  // cero al cambiar de sección y la primera página son cincuenta de muchos.
  useEffect(() => {
    if (!abrir) return
    pendiente.current = abrir
    setBusqueda(abrir.sku)
    setOffset(0)
    setVersion((v) => v + 1)
  }, [abrir])

  // La búsqueda se retrasa 300 ms para no lanzar una consulta por tecla.
  // `vigente` descarta la respuesta de una consulta que ya quedó atrás: al
  // teclear rápido hay varias en vuelo y no siempre vuelven en orden, así que
  // sin esto la lista puede quedarse mostrando el resultado de un texto viejo.
  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.productos({
        q: busqueda, marca, categoria, problemas: soloProblemas,
        excluidos: verExcluidos, sin_precio: sinPrecio, sin_publicar: sinPublicar,
        sin_foto: sinFoto, sin_descripcion: sinDescripcion, sin_ean: sinEAN, con_promo: conPromo,
        orden, desc: ordenDesc,
        limite: POR_PAGINA, offset,
      })
        .then((p) => {
          if (!vigente) return
          setPagina(p)
          // Un error de hace dos búsquedas no debe seguir en pantalla cuando
          // la siguiente ya trajo datos buenos.
          setError(null)
          const a = pendiente.current
          if (a) {
            pendiente.current = null
            const item = p.items.find((x) => x.id === a.id)
            if (item) {
              abrirEditor(item, a.pestana)
            } else {
              setAviso({ texto: `${a.sku} no aparece en la lista con los filtros actuales.`, malo: true })
            }
            onAbierto?.()
          }
        })
        .catch((e) => {
          if (!vigente) return
          setError(e instanceof Error ? e.message : String(e))
        })
        .finally(() => { if (vigente) setCargando(false) })
    }, 300)
    return () => { vigente = false; clearTimeout(t) }
  }, [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio, sinPublicar,
    sinFoto, sinDescripcion, sinEAN, conPromo, orden, ordenDesc, offset, version, refresco])

  // Al cambiar un filtro se vuelve a la primera página: quedarse en la página 7
  // de un resultado que ahora tiene 2 muestra una tabla vacía sin explicación.
  // Cambiar de filtro limpia la selección: aplicar una operación a productos
  // que ya no se ven en pantalla sería una sorpresa desagradable.
  useEffect(() => { setOffset(0); setMarcados(new Set()) },
    [busqueda, marca, categoria, soloProblemas, verExcluidos, sinPrecio, sinPublicar,
      sinFoto, sinDescripcion, sinEAN, conPromo])

  return (
    <>
      <header className="principal">
        <div>
          <h1 className="titulo-seccion">Productos</h1>
          <div className="sub">
            {verExcluidos
              ? 'Fuera del catálogo — gastos, activos fijos y servicios'
              : 'Precio, marca, descripción e imágenes son de Integra; Odoo solo aporta SKU, nombre y stock'}
          </div>
        </div>
      </header>

      {aviso && (
        <div className={aviso.malo ? 'aviso-caja' : 'nota-previa'}>
          <div className="fila">
            <span className="expande-recorta">{aviso.texto}</span>
            <button onClick={() => setAviso(null)}>Cerrar</button>
          </div>
        </div>
      )}

      {error && (
        <div className="aviso-caja">
          <div className="fila">
            <span className="expande-recorta">No se pudo cargar la lista: {error}</span>
            <button onClick={() => setVersion((v) => v + 1)}>Reintentar</button>
          </div>
        </div>
      )}

      <section className="panel">
        <div className="cuerpo">
          <div className="filtros">
            <input className="crece" placeholder="Buscar por nombre o referencia…" data-guia="buscar"
              value={busqueda} onChange={(e) => setBusqueda(e.target.value)} />
            <select value={marca} onChange={(e) => setMarca(e.target.value)} data-guia="cat-marca">
              <option value="">Todas las marcas</option>
              {marcas.filter((m) => m.cantidad > 0).map((m) => (
                <option key={m.codigo} value={m.codigo}>{m.nombre} ({m.cantidad})</option>
              ))}
            </select>
            <select value={categoria} onChange={(e) => setCategoria(e.target.value)}>
              <option value="">Todas las categorías</option>
              {categorias.filter((c) => c.cantidad > 0).map((c) => (
                <option key={c.nombre} value={c.nombre}>{c.nombre} ({c.cantidad})</option>
              ))}
            </select>
            <label className="casilla">
              <input type="checkbox" checked={soloProblemas} data-guia="cat-problemas"
                onChange={(e) => setSoloProblemas(e.target.checked)} />
              Solo con problemas
            </label>
            <label className="casilla">
              <input type="checkbox" checked={sinPrecio}
                onChange={(e) => { setSinPrecio(e.target.checked); setOffset(0) }} />
              Sin precio
            </label>
            <label className="casilla" title="Los que no tienen ficha en ningún canal">
              <input type="checkbox" data-guia="pendientes" checked={sinPublicar}
                onChange={(e) => { setSinPublicar(e.target.checked); setOffset(0) }} />
              Pendientes por publicar
            </label>
            <label className="casilla">
              <input type="checkbox" checked={verExcluidos} data-guia="cat-excluidos"
                onChange={(e) => setVerExcluidos(e.target.checked)} />
              Ver excluidos
            </label>
            <SelectorVista modo={vista} onCambiar={setVista} />
          </div>
        </div>

        {(marcados.size > 0 || (pagina?.total ?? 0) > 0) && (
          <div className="barra-masiva" data-guia="cat-barra">
            <span className="crece">
              {marcados.size > 0
                ? `${num(marcados.size)} seleccionados`
                : cargando
                  ? 'Buscando…'
                  : `${num(pagina?.total ?? 0)} productos en el filtro actual`}
            </span>
            {/* En el móvil no hay cabecera de tabla, así que la casilla de
                «seleccionar toda la página» desaparece; este botón la sustituye. */}
            {items.length > 0 && (
              <button className="solo-movil" onClick={alternarPagina}>
                {paginaEntera ? 'Quitar esta página' : 'Marcar esta página'}
              </button>
            )}
            {marcados.size > 0 && (
              <button onClick={() => setMarcados(new Set())}>Limpiar selección</button>
            )}
            {marcados.size > 0 && (
              <button className="primario" onClick={() => void publicarSeleccion()} data-guia="publicar"
                disabled={publicando}
                title="Encola el envío a los canales de los productos marcados">
                {publicando ? 'Encolando…' : `Publicar ${num(marcados.size)}`}
              </button>
            )}
            {marcados.size > 0 && esAdmin && (
              <button onClick={() => setDespublicando(true)} disabled={publicando}
                title="Quita la ficha del canal. El producto sigue en Integra.">
                Despublicar
              </button>
            )}
            <button onClick={abrirPlantilla} data-guia="plantilla"
              title="Descarga el catálogo como hoja de Excel, edítalo y súbelo para actualizar precios y promociones de muchos productos a la vez.">
              Actualizar por plantilla
            </button>
            <button className="primario" onClick={abrirMasiva} data-guia="cat-editar-masa">
              Editar en masa
            </button>
          </div>
        )}

        <div className="tabla-envoltorio">
          {vista === 'detalles' && (
          <table className="tabla-tarjetas">
            <thead>
              <tr data-guia="cat-cabecera">
                <th className="col-check">
                  <input type="checkbox" checked={paginaEntera} data-guia="cat-seleccionar"
                    title="Seleccionar los de esta página"
                    onChange={alternarPagina} />
                </th>
                <th className="oculto-movil ordenable" onClick={() => ordenarPor('sku')}>Referencia{flecha('sku')}</th>
                <th className="ordenable" onClick={() => ordenarPor('nombre')}>Producto{flecha('nombre')}</th>
                <th className="oculto-movil ordenable" onClick={() => ordenarPor('marca')}>Marca{flecha('marca')}</th>
                <th className="num ordenable" onClick={() => ordenarPor('precio')}>Precio{flecha('precio')}</th>
                <th className="num ordenable" onClick={() => ordenarPor('stock')}>Stock{flecha('stock')}</th>
                <th data-guia="cat-publicacion">Publicación</th>
                <th data-guia="estado">Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {pagina?.items.map((p) => (
                <tr key={p.id} data-guia="fila" className={`clicable ${marcados.has(p.id) ? 'marcada' : ''}`}
                  onClick={() => onVer(p.id)}
                  title="Ver cómo quedaría en cada canal">
                  <td className="col-check" onClick={(e) => e.stopPropagation()}>
                    <label className="casilla">
                      <input type="checkbox" checked={marcados.has(p.id)}
                        onChange={() => alternar(p.id)} />
                      {/* En tarjeta la casilla queda suelta sin nada que la
                          nombre; en la tabla el encabezado ya lo dice. */}
                      <span className="solo-movil mini-texto tenue">Seleccionar</span>
                    </label>
                  </td>
                  <td className="sku oculto-movil">{p.sku || <span className="tenue">—</span>}</td>
                  <td className="titulo-tarjeta">
                    <div>{p.nombre}</div>
                    {p.categoria && <div className="categoria">{p.categoria}</div>}
                    {/* La columna Referencia se esconde en el móvil, pero saber
                        qué SKU es sigue siendo lo primero que se busca. */}
                    <div className="solo-movil mini-texto tenue">
                      Ref. {p.sku || '—'}{p.marca ? ` · ${p.marca}` : ''}
                    </div>
                  </td>
                  <td className="oculto-movil">{p.marca || <span className="tenue">—</span>}</td>
                  <td className="num" data-etiqueta="Precio">
                    {p.precio !== null
                      ? <strong>{money(p.precio)}</strong>
                      : p.precio_sugerido !== null
                        ? <span className="tenue" title="Sugerencia según tarifas de Odoo; asígnalo al editar">
                            ({money(p.precio_sugerido)}) sugerido
                          </span>
                        : <span className="tenue">Sin precio</span>}
                  </td>
                  <td className="num" data-etiqueta="Stock">{num(p.stock)}</td>
                  {/* Dónde vive la ficha. Sin esto había que ir canal por canal
                      a adivinar si el producto estaba subido y a cuál. */}
                  <td className="apilada" data-etiqueta="Publicación">
                    <div className="etiquetas">
                      {!p.publicado || p.publicado.length === 0
                        ? <span className="pastilla dudosa">Sin publicar</span>
                        : p.publicado.map((c) => (
                          <span key={c.canal}
                            className={`pastilla ${c.estado === 'published' ? 'ok' : c.estado === 'error' ? 'bloqueante' : 'aviso'}`}
                            title={ESTADO_CANAL[c.estado] ?? c.estado}>
                            {NOMBRE_CANAL[c.canal] ?? c.canal}
                          </span>
                        ))}
                    </div>
                  </td>
                  <td className="apilada" data-etiqueta="Estado">
                    <div className="etiquetas">
                      {p.problemas.length === 0
                        ? <span className="pastilla ok">Listo</span>
                        : p.problemas.map((m) => (
                          // El detalle va en el title: es lo que explica el
                          // aviso, y sin él «la portada no cuadra» no se puede
                          // ni juzgar ni resolver.
                          <span key={m} title={p.detalles?.[m] || motivo(m)}
                            className={`pastilla ${bloqueante(m) ? 'bloqueante' : 'aviso'}`}>
                            {motivo(m)}
                          </span>
                        ))}
                    </div>
                  </td>
                  <td className="acciones-fila">
                    {/* Tocar la tarjeta abre la vista previa, pero eso no se ve;
                        en el móvil el botón lo hace explícito. */}
                    <button className="solo-movil"
                      onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>
                      Ver en canales
                    </button>
                    <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }} data-guia="editar"
                      title="Editar precio, marca y descripción">
                      Editar
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          )}

          {/* Los otros modos comparten las mismas acciones que la fila de la
              tabla: casilla, vista previa al pulsar, Editar y Ver en canales. */}
          {vista === 'lista' && items.length > 0 && (
            <div className="vista-lista">
              {items.map((p) => (
                <div key={p.id} className={`fila-lista clicable ${marcados.has(p.id) ? 'marcada' : ''}`}
                  onClick={() => onVer(p.id)} title="Ver cómo quedaría en cada canal">
                  <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                    onClick={(e) => e.stopPropagation()} onChange={() => alternar(p.id)} />
                  <span className="sku">{p.sku || '—'}</span>
                  <span className="principal" title={p.nombre}>
                    {p.nombre}{p.marca && <span className="tenue"> · {p.marca}</span>}
                  </span>
                  <span className="num"><PrecioDe p={p} /></span>
                  <span className="dato">{num(p.stock)} en stock</span>
                  <span className="etiquetas">
                    <Publicacion p={p} />
                    <Estado p={p} resumen />
                  </span>
                  <span className="vista-acciones">
                    <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver en canales</button>
                    <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}
                      title="Editar precio, marca y descripción">
                      Editar
                    </button>
                  </span>
                </div>
              ))}
            </div>
          )}

          {vista === 'mosaico' && items.length > 0 && (
            <div className="vista-mosaico">
              {items.map((p) => (
                <div key={p.id} className={`tarjeta-vista clicable ${marcados.has(p.id) ? 'marcada' : ''}`}
                  onClick={() => onVer(p.id)} title="Ver cómo quedaría en cada canal">
                  <Imagen sha={p.portada_sha}>
                    <label className="marca-esquina" onClick={(e) => e.stopPropagation()}>
                      <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                        onChange={() => alternar(p.id)} />
                    </label>
                  </Imagen>
                  <div className="cuerpo-tarjeta">
                    <div className="titulo" title={p.nombre}>{p.nombre}</div>
                    <div className="sku">{p.sku || '—'}{p.marca ? ` · ${p.marca}` : ''}</div>
                    <div className="datos">
                      <span className="num"><PrecioDe p={p} /></span>
                      <span>{num(p.stock)} en stock</span>
                      {p.categoria && <span className="recorta" title={p.categoria}>{p.categoria}</span>}
                    </div>
                    <div className="etiquetas">
                      <Publicacion p={p} />
                      <Estado p={p} />
                    </div>
                    <div className="vista-acciones">
                      <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver en canales</button>
                      <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}
                        title="Editar precio, marca y descripción">
                        Editar
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {vista === 'iconos' && items.length > 0 && (
            <div className="vista-iconos">
              {items.map((p) => (
                <div key={p.id} className={`icono-vista clicable ${marcados.has(p.id) ? 'marcada' : ''}`}
                  onClick={() => onVer(p.id)} title={`${p.nombre} — ver cómo quedaría en cada canal`}>
                  <Imagen sha={p.portada_sha}>
                    <label className="marca-esquina" onClick={(e) => e.stopPropagation()}>
                      <input type="checkbox" checked={marcados.has(p.id)} aria-label="Seleccionar"
                        onChange={() => alternar(p.id)} />
                    </label>
                  </Imagen>
                  <div className="nombre">{p.nombre}</div>
                  <div className="sku">{p.sku || '—'}</div>
                  <div className="etiquetas">
                    <Publicacion p={p} resumen />
                    <Estado p={p} resumen />
                  </div>
                  <div className="vista-acciones">
                    <button onClick={(e) => { e.stopPropagation(); onVer(p.id) }}>Ver</button>
                    <button onClick={(e) => { e.stopPropagation(); abrirEditor(p, 'venta') }}>Editar</button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {pagina === null && cargando && !error && (
            <div className="vacio">Cargando productos…</div>
          )}
          {pagina && pagina.items.length === 0 && !cargando && (
            <div className="vacio">
              {hayFiltro
                ? 'Ningún producto coincide con el filtro. Prueba a quitar alguna condición.'
                : 'Todavía no hay productos. Sincroniza con Odoo para traer el catálogo.'}
            </div>
          )}
        </div>

        {pagina && pagina.total > 0 && (
          <div className="paginacion" data-guia="cat-paginacion">
            <button disabled={offset === 0 || cargando}
              onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))}>
              ← Anterior
            </button>
            <span className="tenue">
              {pagina.total === 0 ? 0 : offset + 1}–{Math.min(offset + POR_PAGINA, pagina.total)} de {num(pagina.total)}
            </span>
            <button disabled={offset + POR_PAGINA >= pagina.total || cargando}
              onClick={() => setOffset(offset + POR_PAGINA)}>
              Siguiente →
            </button>
          </div>
        )}
      </section>

      {despublicando && (
        <DialogoDespublicar
          cuantos={marcados.size}
          onCerrar={() => setDespublicando(false)}
          onConfirmar={(cs) => void despublicarDe(cs)}
        />
      )}

      {plantilla && (
        <PlantillaMasiva filtro={filtroActual} total={pagina?.total ?? 0}
          marcas={marcas} categorias={categorias}
          onFiltrar={(c) => {
            // Los filtros del diálogo son los mismos de la lista: cambiarlos
            // aquí mueve también lo que se ve detrás, que es lo que evita
            // descargar una cosa y encontrarse otra al cerrar.
            if ('marca' in c) setMarca(c.marca ?? '')
            if ('categoria' in c) setCategoria(c.categoria ?? '')
            if ('q' in c) setBusqueda(c.q ?? '')
            if ('problemas' in c) setSoloProblemas(!!c.problemas)
            if ('excluidos' in c) setVerExcluidos(!!c.excluidos)
            if ('sin_precio' in c) setSinPrecio(!!c.sin_precio)
            if ('sin_publicar' in c) setSinPublicar(!!c.sin_publicar)
            if ('sin_foto' in c) setSinFoto(!!c.sin_foto)
            if ('sin_descripcion' in c) setSinDescripcion(!!c.sin_descripcion)
            if ('sin_ean' in c) setSinEAN(!!c.sin_ean)
            if ('con_promo' in c) setConPromo(!!c.con_promo)
          }}
          onCerrar={() => setPlantilla(false)}
          onAplicado={() => {
            // No se cierra el diálogo al aplicar: el resumen de lo que cambió
            // es lo que el operador necesita leer justo después.
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {masiva && (
        <EdicionMasiva ids={[...marcados]} filtro={filtroActual}
          totalFiltro={pagina?.total ?? 0}
          onCerrar={() => setMasiva(false)}
          onAplicado={() => {
            setMasiva(false)
            setMarcados(new Set())
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}

      {editando !== null && (
        <Editar producto={editando} marcas={marcas} pestanaInicial={pestanaEditor}
          onCerrar={() => setEditando(null)}
          onGuardado={() => {
            setEditando(null)
            setVersion((v) => v + 1)
            onCambio()
          }} />
      )}
    </>
  )
}

// Piezas que comparten lista, mosaico e iconos. La tabla de «detalles»
// conserva su marcado propio: es la vista de siempre y no se toca.
function PrecioDe({ p }: { p: Producto }) {
  if (p.precio !== null) return <strong>{money(p.precio)}</strong>
  if (p.precio_sugerido !== null) {
    return (
      <span className="tenue" title="Sugerencia según tarifas de Odoo; asígnalo al editar">
        ({money(p.precio_sugerido)}) sugerido
      </span>
    )
  }
  return <span className="tenue">Sin precio</span>
}

// Dónde vive la ficha. Con `resumen`, una sola pastilla que cuenta canales,
// para los sitios donde no cabe una por canal.
function Publicacion({ p, resumen = false }: { p: Producto; resumen?: boolean }) {
  const canales = p.publicado ?? []
  if (canales.length === 0) return <span className="pastilla dudosa">Sin publicar</span>
  if (resumen) {
    const conError = canales.some((c) => c.estado === 'error')
    return (
      <span className={`pastilla ${conError ? 'bloqueante' : 'ok'}`}
        title={canales.map((c) => `${NOMBRE_CANAL[c.canal] ?? c.canal}: ${ESTADO_CANAL[c.estado] ?? c.estado}`).join(' · ')}>
        {canales.length === 1 ? (NOMBRE_CANAL[canales[0].canal] ?? canales[0].canal) : `${canales.length} canales`}
      </span>
    )
  }
  return (
    <>
      {canales.map((c) => (
        <span key={c.canal}
          className={`pastilla ${c.estado === 'published' ? 'ok' : c.estado === 'error' ? 'bloqueante' : 'aviso'}`}
          title={ESTADO_CANAL[c.estado] ?? c.estado}>
          {NOMBRE_CANAL[c.canal] ?? c.canal}
        </span>
      ))}
    </>
  )
}

// Los problemas de la ficha. Con `resumen`, una pastilla que los cuenta y
// los enumera en el title, que es lo que cabe en una línea o bajo un icono.
function Estado({ p, resumen = false }: { p: Producto; resumen?: boolean }) {
  if (p.problemas.length === 0) return <span className="pastilla ok">Listo</span>
  if (resumen) {
    const grave = p.problemas.some(bloqueante)
    return (
      <span className={`pastilla ${grave ? 'bloqueante' : 'aviso'}`}
        title={p.problemas.map((m) => p.detalles?.[m] || motivo(m)).join(' · ')}>
        {p.problemas.length === 1 ? motivo(p.problemas[0]) : `${p.problemas.length} problemas`}
      </span>
    )
  }
  return (
    <>
      {p.problemas.map((m) => (
        <span key={m} title={p.detalles?.[m] || motivo(m)}
          className={`pastilla ${bloqueante(m) ? 'bloqueante' : 'aviso'}`}>
          {motivo(m)}
        </span>
      ))}
    </>
  )
}

// DialogoDespublicar deja elegir de qué canales se quita la ficha.
//
// Antes la acción alcanzaba siempre a todos los canales activos, que es justo
// lo que no se quiere cuando un producto va bien en uno y mal en otro. Y el
// aviso explica la diferencia con «retirar de la venta», porque la palabra
// «despublicar» no la deja clara por sí sola.
function DialogoDespublicar({ cuantos, onCerrar, onConfirmar }: {
  cuantos: number
  onCerrar: () => void
  onConfirmar: (cuentas: CuentaCanal[]) => void
}) {
  const [cuentas, setCuentas] = useState<CuentaCanal[]>([])
  const [elegidas, setElegidas] = useState<Set<number>>(new Set())
  const [cargando, setCargando] = useState(true)

  useEffect(() => {
    api.cuentas()
      .then((cs) => setCuentas(cs.filter((c) => c.activa)))
      .catch(() => setCuentas([]))
      .finally(() => setCargando(false))
  }, [])

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar])

  const alternar = (id: number) => {
    const s = new Set(elegidas)
    s.has(id) ? s.delete(id) : s.add(id)
    setElegidas(s)
  }

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-editor" onClick={(e) => e.stopPropagation()}>
        <header className="hoja-cabecera">
          <div>
            <h2>Despublicar {num(cuantos)} productos</h2>
            <div className="sub">Elige de qué canales se quita la ficha</div>
          </div>
          <button onClick={onCerrar}>Cerrar ✕</button>
        </header>

        <div className="cuerpo">
          {cargando && <div className="vacio">Cargando canales…</div>}
          {!cargando && cuentas.length === 0 && (
            <div className="vacio">No hay ninguna cuenta de canal activa.</div>
          )}
          {cuentas.map((c) => (
            <label key={c.id} className="casilla fila-cuenta">
              <input type="checkbox" checked={elegidas.has(c.id)}
                onChange={() => alternar(c.id)} />
              <span className="expande">{NOMBRE_CANAL[c.canal] ?? c.canal}</span>
            </label>
          ))}
          {cuentas.length > 1 && (
            <button className="enlace"
              onClick={() => setElegidas(new Set(cuentas.map((c) => c.id)))}>
              Marcar todos
            </button>
          )}
        </div>

        <div className="aviso-caja">
          <strong>El producto no se borra.</strong> Viene de Odoo y sigue en Integra,
          listo para volver a publicarse. Lo que se pierde es lo que vivía en el canal:
          el historial, las preguntas, las reseñas y la posición en el buscador, y la
          dirección de la ficha deja de existir.
        </div>

        <div className="nota-previa">
          Si solo quieres que deje de venderse un tiempo, usa <strong>Retirar</strong> en
          Publicación: eso la pausa y se puede reabrir con su historial entero. En
          MercadoLibre y en Falabella no hay despublicado real, así que la ficha queda
          cerrada para siempre.
        </div>

        <footer className="hoja-pie">
          <button onClick={onCerrar}>Cancelar</button>
          <button className="primario" disabled={elegidas.size === 0}
            onClick={() => onConfirmar(cuentas.filter((c) => elegidas.has(c.id)))}>
            Despublicar de {num(elegidas.size)} canales
          </button>
        </footer>
      </div>
    </div>
  )
}
