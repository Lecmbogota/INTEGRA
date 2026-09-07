import { useEffect, useRef, useState } from 'react'
import { confirmar } from './escritorio/Dialogos'
import { api, fecha, num, type FiltroMediateca, type ImagenBanco, type OrdenMediateca, type PaginaImagenes, type Producto } from './api'
import { EditorFoto } from './EditorFoto'
import { Guia } from './Guia'
import { PASOS_ASIGNAR } from './guias/mediateca'
import { useEvento, useSistemaOpcional } from './escritorio/sistema'
import { Imagen, SelectorVista, useVista } from './Vista'

// El banco de imágenes visto entero, no producto a producto.
//
// Las fotos son lo único de Integra que ocupa disco de verdad y que ningún
// canal perdona: una ficha sin foto no vende y una foto pequeña la rechaza
// MercadoLibre en el alta. Desde cada producto se ve una; aquí se ven todas,
// con las preguntas que importan sobre el conjunto: qué productos no pueden
// publicarse por falta de foto, qué fotos no sirven en ningún canal, y cuánto
// disco ocupa lo que ya no usa nadie.
//
// Lo que se puede hacer aquí y no desde el producto: limpiar en lote lo que
// dejó una búsqueda en internet, y rescatar de entre esas huérfanas las fotos
// buenas marcándolas y asignándolas a su producto sin volver a subirlas.

const FILTROS: [FiltroMediateca, string, string][] = [
  ['todas', 'Todas', ''],
  ['aptas', 'Aptas', 'Lado menor de 600 px o más: valen en los cuatro canales'],
  ['pequenas', 'Pequeñas', 'Por debajo de 600 px: MercadoLibre las rechaza'],
  ['huerfanas', 'Huérfanas', 'No las usa ningún producto: ocupan disco sin publicarse'],
  ['duplicadas', 'Duplicadas', 'Mismo tamaño y dimensiones que otra: casi siempre la misma foto dos veces'],
]

const ORDENES: [OrdenMediateca, string][] = [
  ['recientes', 'Recientes'],
  ['pesadas', 'Más pesadas'],
  ['pequenas', 'Más pequeñas'],
  ['grandes', 'Más grandes'],
]

const ORIGEN: Record<string, string> = {
  subida: 'Subida a mano',
  web: 'Descargada de internet',
  url: 'Descargada de internet',
  banco_fabricante: 'Banco del fabricante',
}

const POR_PAGINA = 60

function tamano(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

// La fuente de una descarga es una URL larga; se enseña el dominio y el
// resto va en el título, que es lo que hace falta para saber si la foto
// vino de la web del fabricante o de un blog cualquiera.
function dominio(ref: string): string {
  try { return new URL(ref).hostname.replace(/^www\./, '') } catch { return ref }
}

export function Mediateca({ onVer }: { onVer?: (varianteId: number) => void }) {
  const [filtro, setFiltro] = useState<FiltroMediateca>('todas')
  const [orden, setOrden] = useState<OrdenMediateca>('recientes')
  const [busqueda, setBusqueda] = useState('')
  const [offset, setOffset] = useState(0)
  const [pagina, setPagina] = useState<PaginaImagenes | null>(null)
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [aviso, setAviso] = useState<string | null>(null)
  const [ocupada, setOcupada] = useState(false)
  const [version, setVersion] = useState(0)
  const [seleccion, setSeleccion] = useState<Set<number>>(new Set())
  const [visor, setVisor] = useState<ImagenBanco | null>(null)
  // Lo que se va a asignar: una foto desde su botón, o todas las marcadas.
  const [asignando, setAsignando] = useState<ImagenBanco[] | null>(null)
  const [editando, setEditando] = useState<ImagenBanco | null>(null)
  // La galería de siempre es «mosaico»; «iconos» es la misma con celdas más
  // grandes y solo la foto y el SKU; «detalles» y «lista» son para comparar
  // medidas y pesos, que en una cuadrícula de fotos no se leen.
  const [vista, setVista] = useVista('mediateca', 'mosaico')

  // Dentro del escritorio, editar y asignar se abren en su propia ventana y
  // el resultado vuelve por el bus. Fuera (sistema null) siguen siendo
  // modales de esta pantalla, sin cambios.
  const sistema = useSistemaOpcional()
  useEvento('fotos-cambiadas', () => setVersion((v) => v + 1))
  function editarFoto(i: ImagenBanco) {
    if (sistema) {
      // Solo lo que el editor necesita: las props de una ventana viajan
      // sueltas, sin la lista de productos ni el origen.
      const foto = { id: i.id, sha256: i.sha256, ancho: i.ancho, alto: i.alto, formato: i.formato }
      sistema.abrir('editor-foto', { foto }, { titulo: `Editar foto · ${i.ancho}×${i.alto}` })
    } else {
      setEditando(i)
    }
  }
  function asignarFotos(lista: ImagenBanco[]) {
    if (sistema) {
      sistema.abrir('asignar-foto', { imagenIds: lista.map((i) => i.id) },
        { titulo: lista.length === 1 ? 'Asignar la foto' : `Asignar ${num(lista.length)} fotos` })
    } else {
      setAsignando(lista)
    }
  }

  // Subida masiva. Cada fichero sale en su propia petición: así el informe
  // avanza línea a línea y un fichero corrupto no tumba la tanda entera.
  const input = useRef<HTMLInputElement>(null)
  const [porNombre, setPorNombre] = useState(true)
  const [encima, setEncima] = useState(false)
  const [subiendo, setSubiendo] = useState<{ hechos: number; total: number } | null>(null)
  const [informe, setInforme] = useState<{ nombre: string; texto: string; mal: boolean }[]>([])

  async function subir(lista: FileList | File[]) {
    const archivos = Array.from(lista).filter((a) => a.type.startsWith('image/') || /\.(jpe?g|png|webp|gif|bmp|tiff?)$/i.test(a.name))
    if (archivos.length === 0) { setError('Ninguno de los archivos es una imagen.'); return }
    setSubiendo({ hechos: 0, total: archivos.length })
    setInforme([])
    setError(null)
    setAviso(null)
    let asignadas = 0, huerfanas = 0, fallidas = 0
    // Tres a la vez: suficiente para no esperar una a una y sin abrir
    // cincuenta conexiones contra el mismo servidor.
    let indice = 0
    const trabajador = async () => {
      while (indice < archivos.length) {
        const a = archivos[indice++]
        try {
          const r = await api.subirAlBanco(a, { porNombre })
          if (r.asignada_a) {
            asignadas++
            setInforme((l) => [...l, { nombre: a.name, texto: `→ ${r.asignada_a!.sku} · ${r.asignada_a!.nombre}`, mal: false }])
          } else {
            huerfanas++
            const probado = porNombre && r.candidatos?.length ? ` (se probó ${r.candidatos.join(', ')})` : ''
            setInforme((l) => [...l, { nombre: a.name, texto: `en el banco, sin producto${probado}`, mal: false }])
          }
        } catch (e) {
          fallidas++
          setInforme((l) => [...l, { nombre: a.name, texto: e instanceof Error ? e.message : String(e), mal: true }])
        } finally {
          setSubiendo((s) => s ? { ...s, hechos: s.hechos + 1 } : s)
        }
      }
    }
    await Promise.all([trabajador(), trabajador(), trabajador()])
    setSubiendo(null)
    setAviso(`${num(archivos.length)} archivos: ${num(asignadas)} asignadas a su producto, ${num(huerfanas)} en el banco sin producto` +
      `${fallidas > 0 ? `, ${num(fallidas)} con error` : ''}.`)
    recargar()
  }

  // La búsqueda se retrasa 300 ms para no consultar por tecla, y `vigente`
  // descarta la respuesta de una consulta que ya quedó atrás.
  useEffect(() => {
    let vigente = true
    setCargando(true)
    const t = setTimeout(() => {
      api.banco({ filtro, q: busqueda, orden, limite: POR_PAGINA, offset })
        .then((p) => { if (vigente) { setPagina(p); setError(null) } })
        .catch((e) => { if (vigente) setError(e instanceof Error ? e.message : String(e)) })
        .finally(() => { if (vigente) setCargando(false) })
    }, 300)
    return () => { vigente = false; clearTimeout(t) }
  }, [filtro, orden, busqueda, offset, version])

  // Cambiar de filtro o de página vacía la selección: actuar sobre «lo
  // marcado» cuando lo marcado ya no está a la vista es la forma de tocar
  // lo que no se quería.
  useEffect(() => { setSeleccion(new Set()) }, [filtro, orden, busqueda, offset])

  // Escape cierra el visor. El editor y el diálogo de asignar cierran con
  // su propio Escape, que sabe si tienen la ayuda abierta encima.
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') setVisor(null) }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  function recargar() { setVersion((v) => v + 1) }

  function alternar(id: number) {
    setSeleccion((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id); else n.add(id)
      return n
    })
  }

  const items = pagina?.items ?? []
  // En el orden de la página: al asignar, la primera marcada es la portada
  // si se pide, y ese orden es el que se ve.
  const seleccionadas = items.filter((i) => seleccion.has(i.id))
  const seleccionadasHuerfanas = seleccionadas.filter((i) => i.productos.length === 0)
  const todasMarcadas = items.length > 0 && seleccionadas.length === items.length

  function marcarTodas() {
    setSeleccion(todasMarcadas ? new Set() : new Set(items.map((i) => i.id)))
  }

  async function borrar(ids: number[]) {
    if (ids.length === 0) return
    const cuantas = ids.length === 1 ? 'esta imagen' : `${num(ids.length)} imágenes`
    if (!(await confirmar(`Se borra del disco ${cuantas}. No las usa ningún producto, así que ninguna ficha se queda sin foto. ¿Continuar?`))) return
    setOcupada(true)
    setError(null)
    try {
      const r = ids.length === 1
        ? (await api.borrarDelBanco(ids[0]), { borradas: 1, rechazadas: 0 })
        : await api.borrarVariasDelBanco(ids)
      setAviso(`${num(r.borradas)} borradas${r.rechazadas > 0 ? ` · ${num(r.rechazadas)} no se borraron porque las usa algún producto` : ''}.`)
      setSeleccion(new Set())
      recargar()
    } catch (e) {
      setError(`No se pudo borrar: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupada(false)
    }
  }

  async function asignar(imagenes: ImagenBanco[], producto: Producto, principal: boolean) {
    setOcupada(true)
    setError(null)
    try {
      const nombre = producto.sku || producto.nombre
      if (imagenes.length === 1) {
        await api.asociarDelBanco(imagenes[0].id, producto.id, principal)
        setAviso(`Foto asignada a ${nombre}${principal ? ' como portada' : ''}.`)
      } else {
        const r = await api.asociarVariasDelBanco(imagenes.map((i) => i.id), producto.id, principal)
        setAviso(`${num(r.asociadas)} fotos asignadas a ${nombre}${principal ? ', la primera como portada' : ''}` +
          `${r.rechazadas > 0 ? ` · ${num(r.rechazadas)} no se pudieron` : ''}.`)
      }
      setAsignando(null)
      setSeleccion(new Set())
      recargar()
    } catch (e) {
      setError(`No se pudo asignar: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setOcupada(false)
    }
  }

  const total = pagina?.total ?? 0
  const desde = total === 0 ? 0 : offset + 1
  const hasta = Math.min(offset + POR_PAGINA, total)

  return (
    <>
      <section className="panel" data-guia="subir">
        <h2>Subir fotos</h2>
        <div className="cuerpo">
          <input ref={input} type="file" accept="image/*" multiple hidden
            onChange={(e) => { if (e.target.files?.length) void subir(e.target.files); e.target.value = '' }} />
          <div className={`zona-soltar ${encima ? 'encima' : ''}`}
            onClick={() => { if (!subiendo) input.current?.click() }}
            onDragOver={(e) => { e.preventDefault(); setEncima(true) }}
            onDragLeave={() => setEncima(false)}
            onDrop={(e) => { e.preventDefault(); setEncima(false); if (!subiendo && e.dataTransfer.files.length) void subir(e.dataTransfer.files) }}>
            {subiendo
              ? <strong>Subiendo {num(subiendo.hechos)} de {num(subiendo.total)}…</strong>
              : <><strong>Arrastra aquí las fotos</strong> o pulsa para elegirlas. Cuantas quieras.</>}
          </div>
          {/* La regla que hace útil subir en masa: la carpeta del fabricante
              viene como «SKU-1.jpg», «SKU-2.jpg», y así cada foto cae sola en
              su producto. Lo que no case con ningún SKU queda en huérfanas. */}
          <label className="casilla" style={{ marginTop: 10 }} data-guia="med-por-nombre">
            <input type="checkbox" checked={porNombre} onChange={(e) => setPorNombre(e.target.checked)} />
            Asignar cada foto al producto cuyo SKU lleve en el nombre del archivo
            <span className="tenue"> (HDWT860UZSVA-2.jpg → HDWT860UZSVA)</span>
          </label>
          {informe.length > 0 && (
            <ul className="informe-subida">
              {informe.map((l, i) => (
                <li key={i} className={l.mal ? 'mal' : ''}><code>{l.nombre}</code> — {l.texto}</li>
              ))}
            </ul>
          )}
        </div>
      </section>

      {pagina && (
        <div className="tarjetas">
          <Cifra etiqueta="Productos sin foto" valor={num(pagina.productos_sin_foto)} guia="med-cifra-sin-foto"
            pie="No pueden publicarse en ningún canal" tono={pagina.productos_sin_foto > 0 ? 'error' : 'ok'} />
          <Cifra etiqueta="Fotos pequeñas" valor={num(pagina.pequenas)} guia="med-cifra-pequenas"
            pie="Por debajo de 600 px: MercadoLibre las rechaza" tono={pagina.pequenas > 0 ? 'error' : 'ok'} />
          <Cifra etiqueta="Disco en huérfanas" valor={tamano(pagina.bytes_huerfanos)} guia="med-cifra-huerfanas"
            pie="Fotos que no usa ningún producto" />
        </div>
      )}

      <section className="panel">
        <h2>Banco de imágenes{pagina ? ` · ${num(total)}` : ''}</h2>
        <div className="cuerpo">
          <div className="filtros">
            <div className="grupo-badges" data-guia="med-filtros">
              {FILTROS.map(([id, nombre, pista]) => (
                <button key={id} type="button" className={`badge ${filtro === id ? 'activo' : ''}`}
                  title={pista} onClick={() => { setFiltro(id); setOffset(0) }}>{nombre}</button>
              ))}
            </div>
            <input type="search" className="crece" placeholder="Buscar por SKU o nombre del producto" data-guia="med-buscar"
              value={busqueda} onChange={(e) => { setBusqueda(e.target.value); setOffset(0) }} />
          </div>
          <div className="filtros">
            <span className="tenue">Orden:</span>
            <div className="grupo-badges" data-guia="med-orden">
              {ORDENES.map(([id, nombre]) => (
                <button key={id} type="button" className={`badge ${orden === id ? 'activo' : ''}`}
                  onClick={() => { setOrden(id); setOffset(0) }}>{nombre}</button>
              ))}
            </div>
            <SelectorVista modo={vista} onCambiar={setVista} />
          </div>

          {/* Acciones sobre lo marcado. Asignar vale para cualquier foto;
              borrar solo para las que no usa nadie, porque el servidor
              rechaza el resto y no tiene sentido ofrecerlo. */}
          {items.length > 0 && (
            <div className="filtros" data-guia="med-lote">
              <label className="casilla">
                <input type="checkbox" checked={todasMarcadas} onChange={marcarTodas} />
                Marcar las {num(items.length)} de esta página
              </label>
              <span className="tenue">
                {seleccionadas.length === 0 ? 'Nada marcado' : `${num(seleccionadas.length)} marcadas`}
              </span>
              <button className="primario" disabled={seleccionadas.length === 0 || ocupada}
                onClick={() => asignarFotos(seleccionadas)}
                title="Enlaza todas las marcadas al mismo producto, en este orden">
                Asignar {seleccionadas.length > 0 ? num(seleccionadas.length) : ''} marcadas a un producto
              </button>
              <button disabled={seleccionadasHuerfanas.length === 0 || ocupada}
                onClick={() => void borrar(seleccionadasHuerfanas.map((i) => i.id))}
                title="Solo las marcadas que no usa ningún producto">
                Borrar {seleccionadasHuerfanas.length > 0 ? num(seleccionadasHuerfanas.length) : ''} huérfanas marcadas
              </button>
            </div>
          )}

          {error && <div className="aviso-caja">{error}</div>}
          {aviso && !error && <div className="nota-previa">{aviso}</div>}
          {cargando && !pagina && <div className="vacio">Cargando el banco…</div>}
          {pagina && items.length === 0 && !cargando && (
            <div className="vacio">
              {filtro === 'todas' && !busqueda
                ? 'El banco está vacío: sube fotos desde cualquier producto o usa «Buscar imágenes faltantes».'
                : 'Ninguna imagen coincide con el filtro.'}
            </div>
          )}

          {items.length > 0 && vista === 'detalles' && (
            <div className="tabla-envoltorio">
              <table className="tabla-tarjetas">
                <thead>
                  <tr>
                    <th className="col-check">
                      <input type="checkbox" checked={todasMarcadas} onChange={marcarTodas}
                        title="Marcar las de esta página" />
                    </th>
                    <th></th>
                    <th>Medidas</th>
                    <th className="num">Peso</th>
                    <th>Formato</th>
                    <th>Origen</th>
                    <th>Productos</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((i) => {
                    const huerfana = i.productos.length === 0
                    const pequena = Math.min(i.ancho, i.alto) < 600
                    const marcada = seleccion.has(i.id)
                    return (
                      <tr key={i.id} className={marcada ? 'marcada' : ''}>
                        <td className="col-check">
                          <input type="checkbox" checked={marcada} onChange={() => alternar(i.id)}
                            title="Marcar para asignar o borrar en lote" />
                        </td>
                        <td>
                          <img src={`/imagenes/${i.sha256}/miniatura_300`} alt="" loading="lazy"
                            className="abrible" title="Ver en grande" onClick={() => setVisor(i)}
                            style={{ width: 48, height: 48, objectFit: 'contain', display: 'block' }} />
                        </td>
                        <td className="titulo-tarjeta">
                          {i.ancho}×{i.alto}
                          {pequena && <span className="pastilla bloqueante" style={{ marginLeft: 6 }}>pequeña</span>}
                        </td>
                        <td className="num" data-etiqueta="Peso">{tamano(i.bytes)}</td>
                        <td data-etiqueta="Formato">{i.formato}</td>
                        <td className="tenue" data-etiqueta="Origen"
                          title={`${ORIGEN[i.origen] ?? i.origen}${i.origen_ref ? ` · ${i.origen_ref}` : ''} · ${fecha(i.creada)}`}>
                          {ORIGEN[i.origen] ?? i.origen}{i.origen_ref ? ` · ${dominio(i.origen_ref)}` : ''} · {fecha(i.creada)}
                        </td>
                        <td className="apilada" data-etiqueta="Productos">
                          <div className="etiquetas">
                            {huerfana
                              ? <span className="pastilla aviso">Sin producto</span>
                              : i.productos.map((p) => (
                                <button key={p.variante_id} type="button" className="enlace"
                                  title={`${p.nombre}${p.principal ? ' · portada' : ''}`}
                                  onClick={() => onVer?.(p.variante_id)}>
                                  {p.principal ? '★ ' : ''}{p.sku || p.nombre}
                                </button>
                              ))}
                          </div>
                        </td>
                        <td className="acciones-fila">
                          <button onClick={() => editarFoto(i)} disabled={ocupada}
                            title="Recortar, girar, encajar en cuadrado, cambiar formato">Editar</button>
                          <button onClick={() => asignarFotos([i])} disabled={ocupada}
                            title="Enlazarla a un producto sin volver a subirla">Asignar</button>
                          {huerfana && (
                            <button onClick={() => void borrar([i.id])} disabled={ocupada}>Borrar</button>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}

          {items.length > 0 && vista === 'lista' && (
            <div className="vista-lista">
              {items.map((i) => {
                const huerfana = i.productos.length === 0
                const pequena = Math.min(i.ancho, i.alto) < 600
                const marcada = seleccion.has(i.id)
                return (
                  <div key={i.id} className={`fila-lista ${marcada ? 'marcada' : ''}`}>
                    <input type="checkbox" checked={marcada} onChange={() => alternar(i.id)}
                      title="Marcar para asignar o borrar en lote" />
                    <img className="mini-foto abrible" src={`/imagenes/${i.sha256}/miniatura_300`} alt=""
                      loading="lazy" title="Ver en grande" onClick={() => setVisor(i)}
                      style={{ objectFit: 'contain', cursor: 'zoom-in' }} />
                    <span className="principal">
                      {huerfana
                        ? <span className="tenue">Sin producto</span>
                        : i.productos.map((p) => (
                          <button key={p.variante_id} type="button" className="enlace"
                            title={`${p.nombre}${p.principal ? ' · portada' : ''}`}
                            onClick={() => onVer?.(p.variante_id)}>
                            {p.principal ? '★ ' : ''}{p.sku || p.nombre}
                          </button>
                        ))}
                    </span>
                    <span className="dato">{i.ancho}×{i.alto}</span>
                    {pequena && <span className="pastilla bloqueante">pequeña</span>}
                    <span className="num">{tamano(i.bytes)}</span>
                    <span className="dato">{i.formato}</span>
                    <span className="dato" title={`${ORIGEN[i.origen] ?? i.origen}${i.origen_ref ? ` · ${i.origen_ref}` : ''} · ${fecha(i.creada)}`}>
                      {ORIGEN[i.origen] ?? i.origen}{i.origen_ref ? ` · ${dominio(i.origen_ref)}` : ''}
                    </span>
                    <span className="vista-acciones">
                      <button onClick={() => editarFoto(i)} disabled={ocupada}>Editar</button>
                      <button onClick={() => asignarFotos([i])} disabled={ocupada}>Asignar</button>
                      {huerfana && <button onClick={() => void borrar([i.id])} disabled={ocupada}>Borrar</button>}
                    </span>
                  </div>
                )
              })}
            </div>
          )}

          {/* Iconos: solo la foto y el SKU, en celdas anchas. Es para mirar
              fotos, no fichas; editar y asignar aparecen al pasar por encima. */}
          {items.length > 0 && vista === 'iconos' && (
            <div className="vista-iconos grande">
              {items.map((i) => {
                const huerfana = i.productos.length === 0
                const marcada = seleccion.has(i.id)
                const principal = i.productos.find((p) => p.principal) ?? i.productos[0]
                return (
                  <div key={i.id} className={`icono-vista ${marcada ? 'marcada' : ''}`}>
                    <Imagen sha={i.sha256} titulo="Ver en grande" onClick={() => setVisor(i)}>
                      <label className="marca-esquina" onClick={(e) => e.stopPropagation()}>
                        <input type="checkbox" checked={marcada} onChange={() => alternar(i.id)}
                          title="Marcar para asignar o borrar en lote" />
                      </label>
                    </Imagen>
                    {huerfana
                      ? <span className="pastilla aviso">Sin producto</span>
                      : (
                        <button type="button" className="enlace sku" title={principal.nombre}
                          onClick={() => onVer?.(principal.variante_id)}>
                          {principal.sku || principal.nombre}{i.productos.length > 1 ? ` +${i.productos.length - 1}` : ''}
                        </button>
                      )}
                    <div className="vista-acciones">
                      <button onClick={() => editarFoto(i)} disabled={ocupada}>Editar</button>
                      <button onClick={() => asignarFotos([i])} disabled={ocupada}>Asignar</button>
                      {huerfana && <button onClick={() => void borrar([i.id])} disabled={ocupada}>Borrar</button>}
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {items.length > 0 && vista === 'mosaico' && (
            <div className="galeria">
              {items.map((i) => {
                const huerfana = i.productos.length === 0
                const pequena = Math.min(i.ancho, i.alto) < 600
                const marcada = seleccion.has(i.id)
                return (
                  <figure key={i.id} className={`${huerfana ? 'huerfana' : ''} ${marcada ? 'seleccionada' : ''}`} data-guia="med-tarjeta">
                    <img className="abrible" src={`/imagenes/${i.sha256}/miniatura_300`} alt="" loading="lazy"
                      title="Ver en grande" onClick={() => setVisor(i)} data-guia="med-miniatura" />
                    <figcaption>
                      <span className="dim">
                        <input type="checkbox" checked={marcada} onChange={() => alternar(i.id)}
                          title="Marcar para asignar o borrar en lote" style={{ marginRight: 6 }} />
                        {i.ancho}×{i.alto} · {tamano(i.bytes)} · {i.formato}
                        {pequena && <span className="pastilla bloqueante" style={{ marginLeft: 6 }}>pequeña</span>}
                      </span>
                      <span className="origen" title={`${ORIGEN[i.origen] ?? i.origen}${i.origen_ref ? ` · ${i.origen_ref}` : ''} · ${fecha(i.creada)}`}>
                        {ORIGEN[i.origen] ?? i.origen}{i.origen_ref ? ` · ${dominio(i.origen_ref)}` : ''} · {fecha(i.creada)}
                      </span>
                      {/* Cada producto que la usa es un enlace a su vista previa,
                          que es donde se sube, se quita o se elige portada. */}
                      <div className="etiquetas" data-guia="med-productos">
                        {huerfana
                          ? <span className="pastilla aviso">Sin producto</span>
                          : i.productos.map((p) => (
                            <button key={p.variante_id} type="button" className="enlace"
                              title={`${p.nombre}${p.principal ? ' · portada' : ''}`}
                              onClick={() => onVer?.(p.variante_id)}>
                              {p.principal ? '★ ' : ''}{p.sku || p.nombre}
                            </button>
                          ))}
                      </div>
                      <div className="acciones" data-guia="med-acciones">
                        <button onClick={() => editarFoto(i)} disabled={ocupada}
                          title="Recortar, girar, encajar en cuadrado, cambiar formato">
                          Editar
                        </button>
                        <button onClick={() => asignarFotos([i])} disabled={ocupada}
                          title="Enlazarla a un producto sin volver a subirla">
                          Asignar a producto
                        </button>
                        {huerfana && (
                          <button onClick={() => void borrar([i.id])} disabled={ocupada}>
                            Borrar del disco
                          </button>
                        )}
                      </div>
                    </figcaption>
                  </figure>
                )
              })}
            </div>
          )}

          {pagina && total > POR_PAGINA && (
            <div className="paginacion" data-guia="med-paginacion">
              <button onClick={() => setOffset(Math.max(0, offset - POR_PAGINA))} disabled={offset === 0 || cargando}>
                ← Anterior
              </button>
              <span className="tenue">{num(desde)}–{num(hasta)} de {num(total)}</span>
              <button onClick={() => setOffset(offset + POR_PAGINA)} disabled={hasta >= total || cargando}>
                Siguiente →
              </button>
            </div>
          )}
        </div>
      </section>

      <div className="nota-previa">
        Las fotos se suben, se quitan y se eligen como portada desde cada producto:
        pulsa su referencia aquí o ábrelo desde <strong>Productos</strong>. Aquí se
        marcan varias y se asignan de una vez a un producto, y se borran las que no
        usa nadie. «Buscar imágenes faltantes» recorre el catálogo entero buscando en
        internet por SKU; lo que descarga y no convence acaba en «Huérfanas».
      </div>

      {visor && (
        <div className="capa" onClick={() => setVisor(null)}>
          <div className="hoja visor" onClick={(e) => e.stopPropagation()}>
            <img src={`/imagenes/${visor.sha256}/web_800`} alt="" />
            <div className="tenue mini-texto">
              {visor.ancho}×{visor.alto} · {tamano(visor.bytes)} · {visor.formato}
              {visor.origen_ref && <> · <a href={visor.origen_ref} target="_blank" rel="noreferrer">{dominio(visor.origen_ref)}</a></>}
            </div>
            <div className="grupo-acciones">
              <button onClick={() => { editarFoto(visor); setVisor(null) }}>Editar</button>
              <button onClick={() => { asignarFotos([visor]); setVisor(null) }}>Asignar a producto</button>
              <button onClick={() => setVisor(null)}>Cerrar ✕</button>
            </div>
          </div>
        </div>
      )}

      {editando && (
        <EditorFoto foto={editando}
          onCerrar={() => setEditando(null)}
          onGuardada={(modo) => {
            setEditando(null)
            setAviso(modo === 'reemplazar' ? 'Foto reemplazada en todos sus productos.' : 'Foto guardada como nueva.')
            recargar()
          }} />
      )}

      {asignando && (
        <DialogoAsignar imagenes={asignando} ocupada={ocupada}
          onCerrar={() => setAsignando(null)}
          onConfirmar={(p, principal) => void asignar(asignando, p, principal)} />
      )}
    </>
  )
}

// DialogoAsignar busca el producto por lo que el operador tiene a mano —la
// referencia o el nombre— y enlaza las fotos. Con «como portada», la primera
// pasa a ser la cara del producto en los cuatro canales. Se maneja entero
// con el teclado: escribir, flechas para elegir, Enter para asignar.
// Exportado porque en el escritorio se abre como ventana propia (apps.tsx).
export function DialogoAsignar({ imagenes, ocupada, onCerrar, onConfirmar }: {
  imagenes: ImagenBanco[]
  ocupada: boolean
  onCerrar: () => void
  onConfirmar: (producto: Producto, principal: boolean) => void
}) {
  const [q, setQ] = useState('')
  const [candidatos, setCandidatos] = useState<Producto[]>([])
  const [total, setTotal] = useState(0)
  const [buscando, setBuscando] = useState(false)
  const [elegido, setElegido] = useState<Producto | null>(null)
  const [principal, setPrincipal] = useState(false)
  const [ayuda, setAyuda] = useState(false)

  // Escape cierra el diálogo, salvo que la ayuda esté abierta encima: ahí
  // cierra la ayuda y el diálogo se queda.
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !ayuda && !ocupada) onCerrar() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onCerrar, ayuda, ocupada])

  useEffect(() => {
    if (q.trim().length < 2) { setCandidatos([]); setTotal(0); return }
    let vigente = true
    setBuscando(true)
    const t = setTimeout(() => {
      api.productos({ q: q.trim(), limite: 12 })
        .then((p) => { if (vigente) { setCandidatos(p.items); setTotal(p.total) } })
        .catch(() => { if (vigente) { setCandidatos([]); setTotal(0) } })
        .finally(() => { if (vigente) setBuscando(false) })
    }, 250)
    return () => { vigente = false; clearTimeout(t) }
  }, [q])

  // Un producto que ya tiene TODAS las fotos marcadas no se ofrece: asignar
  // no haría nada. Si tiene solo algunas, se asignan las que faltan.
  const yaLasTiene = (p: Producto) => imagenes.every((i) => i.productos.some((x) => x.variante_id === p.id))
  const elegibles = candidatos.filter((p) => !yaLasTiene(p))
  const varias = imagenes.length > 1

  function teclado(e: React.KeyboardEvent) {
    if (ayuda || elegibles.length === 0) return
    const i = elegido ? elegibles.findIndex((p) => p.id === elegido.id) : -1
    if (e.key === 'ArrowDown') { e.preventDefault(); setElegido(elegibles[Math.min(elegibles.length - 1, i + 1)]) }
    if (e.key === 'ArrowUp') { e.preventDefault(); setElegido(elegibles[Math.max(0, i - 1)]) }
    if (e.key === 'Enter' && elegido && !ocupada) { e.preventDefault(); onConfirmar(elegido, principal) }
  }

  return (
    <div className="capa" onClick={onCerrar}>
      <div className="hoja hoja-media" onClick={(e) => e.stopPropagation()} onKeyDown={teclado}>
        <header className="hoja-cabecera">
          <div>
            <h2>{varias ? `Asignar ${num(imagenes.length)} fotos a un producto` : 'Asignar la foto a un producto'}</h2>
            <div className="sub">
              {varias
                ? 'Quedarán en este orden detrás de las que el producto ya tenga.'
                : `${imagenes[0].ancho}×${imagenes[0].alto} · ${tamano(imagenes[0].bytes)}`}
            </div>
          </div>
          <div className="grupo-acciones">
            <button type="button" className="mini" title="Cómo funciona esta pantalla" onClick={() => setAyuda(true)}>?</button>
            <button onClick={onCerrar} disabled={ocupada}>Cerrar ✕</button>
          </div>
        </header>
        {ayuda && <Guia pasos={PASOS_ASIGNAR} nombre="Asignar a producto" onCerrar={() => setAyuda(false)} />}

        <div className="dialogo-asignar">
          {/* Las miniaturas de lo que se va a asignar: con varias marcadas,
              ver cuáles son evita enlazar la moto junto con la cámara. */}
          <div className="tira-miniaturas" data-guia="med-asignar-tira">
            {imagenes.map((i, n) => (
              <img key={i.id} src={`/imagenes/${i.sha256}/miniatura_300`} alt=""
                title={`${n + 1} · ${i.ancho}×${i.alto}`} />
            ))}
          </div>

          <input type="search" autoFocus className="buscador-producto" data-guia="med-asignar-buscador"
            placeholder="Referencia o parte del nombre (mínimo 2 letras)"
            value={q} onChange={(e) => { setQ(e.target.value); setElegido(null) }} />

          <div className="tenue mini-texto">
            {buscando && 'Buscando…'}
            {!buscando && q.trim().length >= 2 && candidatos.length === 0 && 'Ningún producto coincide.'}
            {!buscando && candidatos.length > 0 && (total > candidatos.length
              ? `${num(candidatos.length)} de ${num(total)} coincidencias: escribe más para afinar`
              : `${num(candidatos.length)} coincidencia${candidatos.length > 1 ? 's' : ''}`)}
            {!buscando && q.trim().length < 2 && 'Usa las flechas para elegir y Enter para asignar.'}
          </div>

          {candidatos.length > 0 && (
            <div className="lista-productos" role="listbox">
              {candidatos.map((p) => {
                const bloqueado = yaLasTiene(p)
                const activo = elegido?.id === p.id
                return (
                  <div key={p.id} role="option" aria-selected={activo}
                    className={`fila-producto ${activo ? 'activa' : ''} ${bloqueado ? 'bloqueada' : ''}`}
                    onClick={() => { if (!bloqueado) setElegido(p) }}
                    onDoubleClick={() => { if (!bloqueado && !ocupada) onConfirmar(p, principal) }}>
                    <input type="radio" name="producto" checked={activo} disabled={bloqueado} readOnly />
                    <code className="sku">{p.sku || '—'}</code>
                    <span className="nombre" title={p.nombre}>{p.nombre}</span>
                    <span className="tenue mini-texto marca">{p.marca}</span>
                    {bloqueado && <span className="pastilla ok">ya la tiene</span>}
                    {!bloqueado && p.problemas?.includes('missing_image') && <span className="pastilla aviso">sin fotos</span>}
                  </div>
                )
              })}
            </div>
          )}

          <label className="casilla" data-guia="med-asignar-portada">
            <input type="checkbox" checked={principal} onChange={(e) => setPrincipal(e.target.checked)} />
            {varias ? 'La primera como portada' : 'Ponerla como portada'}
          </label>
        </div>

        <div className="hoja-pie">
          <button onClick={onCerrar} disabled={ocupada}>Cancelar</button>
          <button className="primario" disabled={!elegido || ocupada} data-guia="med-asignar-confirmar"
            onClick={() => elegido && onConfirmar(elegido, principal)}>
            {ocupada ? 'Asignando…' : elegido ? `Asignar a ${elegido.sku || elegido.nombre}` : 'Asignar'}
          </button>
        </div>
      </div>
    </div>
  )
}

function Cifra({ etiqueta, valor, pie, tono, guia }: {
  etiqueta: string; valor: string; pie?: string; tono?: 'ok' | 'error'; guia?: string
}) {
  return (
    <div className="tarjeta" data-guia={guia}>
      <div className="etiqueta">{etiqueta}</div>
      <div className={`valor ${tono ?? ''}`}>{valor}</div>
      {pie && <div className="pie">{pie}</div>}
    </div>
  )
}
