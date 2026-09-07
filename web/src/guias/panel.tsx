import type { Paso } from '../GuiaPasos'

// Recorrido detallado del Panel. Cada afirmación sale de lo que pinta
// App.tsx (bloque del panel), Prioridad.tsx y Canales.tsx, y de cómo se
// calculan esas cifras en el servidor. Si cambia la pantalla, cambia esto.

export const PASOS_PANEL: Paso[] = [
  {
    titulo: 'El panel: la foto del día',
    seccion: 'panel',
    objetivo: '[data-guia="pan-cabecera"]',
    texto: (
      <>
        <p>El panel resume en qué estado está el catálogo: cuánto hay, cuánto
        se puede publicar ya y qué lo está frenando. Es la pantalla con la que
        conviene empezar cada día.</p>
        <p>Debajo del título está la <strong>última sincronización</strong>: la
        última vez que Integra leyó Odoo para traer productos nuevos y el stock
        actual. Si dice <em>nunca</em>, todavía no se ha sincronizado.</p>
      </>
    ),
  },
  {
    titulo: 'Sincronizar ahora',
    seccion: 'panel',
    objetivo: '[data-guia="pan-sincronizar"]',
    texto: (
      <>
        <p><strong>Sincronizar ahora</strong> trae de Odoo las referencias, los
        nombres y las existencias. Corre en segundo plano: el botón queda en
        <em> Sincronizando…</em> y, pasados unos segundos, las cifras del panel
        se refrescan solas.</p>
        <p>No toca nada de lo que se completa en Integra: precios, marcas,
        descripciones, títulos y fotos siguen como estaban. Para no depender
        de este botón, en <em>Automatización</em> se programa un horario que lo
        hace solo.</p>
      </>
    ),
  },
  {
    titulo: 'Las tarjetas: cuánto hay',
    seccion: 'panel',
    objetivo: '[data-guia="resumen"]',
    texto: (
      <>
        <p><em>Productos</em> son las fichas de Odoo, y el pie dice cuántas
        variantes suman (cada variante es una referencia distinta).
        <em> Marcas</em> cuenta las marcas distintas que ya tienen asignadas
        tus productos, una vez unificadas las formas de escribirlas.</p>
        <p><em>Con precio</em> son las variantes que ya tienen precio en Integra,
        de cuántas en total. <em>Con existencias</em> son las que tienen stock
        en alguna bodega; el pie suma las unidades de todas. Todas estas cifras
        miran solo mercancía: lo que está fuera del catálogo no cuenta.</p>
      </>
    ),
  },
  {
    titulo: 'Publicables, atención y fuera del catálogo',
    seccion: 'panel',
    objetivo: '[data-guia="resumen"]',
    texto: (
      <>
        <p><strong>Publicables hoy</strong> son las variantes que ya cumplen lo
        que exigen los cuatro canales: referencia, descripción, precio y
        existencias. Va en verde si hay alguna y en rojo si es cero.</p>
        <p><strong>Requieren atención</strong> cuenta los avisos abiertos sobre
        productos; un mismo producto puede tener varios (sin precio y sin foto,
        por ejemplo). Es la misma cifra que aparece junto a <em>Productos</em>
        en el menú. <em>Fuera del catálogo</em> son los productos que Integra
        aparta por no ser mercancía —gastos, activos fijos, servicios— o que
        alguien excluyó a mano desde Productos; se muestran para que se vea que
        no se perdieron.</p>
      </>
    ),
  },
  {
    titulo: 'Qué bloquea la publicación',
    seccion: 'panel',
    objetivo: '[data-guia="bloqueos"]',
    texto: (
      <>
        <p>Cada fila es un motivo y la cifra es cuántos productos lo tienen; la
        barra es proporcional al motivo más frecuente. Las <strong>rojas
        impiden publicar</strong>: sin referencia interna, referencia repetida
        en otro producto de Odoo, sin descripción, sin fotos, sin precio y
        precio por debajo del coste.</p>
        <p>Las <strong>amarillas no bloquean</strong>, pero empeoran la ficha o
        la venta: título demasiado largo (MercadoLibre admite 60 caracteres),
        sin marca, sin existencias y SKU renombrado en Odoo después de
        publicar. Si dice <em>Nada pendiente</em>, no hay motivos abiertos.</p>
      </>
    ),
  },
  {
    titulo: 'Dónde se arregla cada motivo',
    seccion: 'panel',
    objetivo: '[data-guia="bloqueos"]',
    texto: (
      <>
        <p>Precio, descripción, marca y título se completan en
        <strong> Productos</strong>, producto a producto o en masa. Las fotos se
        suben en <strong>Imágenes</strong> o desde el propio producto. El precio
        por debajo del coste se corrige subiendo el precio o revisando la
        comisión del canal.</p>
        <p>La referencia faltante o repetida y las existencias se corrigen
        <strong> en Odoo</strong>, que es de donde vienen; tras la siguiente
        sincronización el motivo desaparece solo. En Productos, la casilla
        <em> Solo con problemas</em> lista exactamente estos productos.</p>
      </>
    ),
  },
  {
    titulo: 'Existencias por bodega',
    seccion: 'panel',
    objetivo: '[data-guia="pan-bodegas"]',
    texto: (
      <>
        <p>Las bodegas que Odoo tiene registradas, con las unidades que hay en
        cada una, de mayor a menor; la barra compara contra la bodega más
        llena. Si un nombre se corta, déjale el ratón encima para leerlo
        entero. <em>Sin datos de stock</em> significa que aún no se ha
        sincronizado.</p>
        <p>Importa porque cada cuenta de canal publica el stock de las bodegas
        que tenga asignadas en <strong>Cuentas</strong>: una cuenta sin bodegas
        no publica nada y abre un aviso en Automatización.</p>
      </>
    ),
  },
  {
    titulo: 'Qué publicar primero',
    seccion: 'panel',
    objetivo: '[data-guia="pan-prioridad"]',
    texto: (
      <>
        <p>Los diez productos por los que conviene empezar. Van primero los que
        ya están <strong>listos</strong> —sin motivos rojos, con precio y con
        existencias— y, dentro de cada grupo, los de mayor <strong>valor de
        inventario</strong>: las unidades en stock multiplicadas por el precio.
        Es el dinero que espera a que se publique.</p>
        <p>Si dice <em>Sin candidatos todavía</em>, ningún producto tiene stock
        esperando publicación. La lista se calcula sobre el catálogo entero,
        también sobre productos que ya están en algún canal.</p>
      </>
    ),
  },
  {
    titulo: 'Leer una fila y abrir la vista previa',
    seccion: 'panel',
    objetivo: '[data-guia="pan-prioridad-fila"]',
    texto: (
      <>
        <p>Cada fila trae el puesto, el nombre, la referencia, la marca y las
        unidades en stock. A la derecha, el valor de inventario y una etiqueta:
        <strong> Listo</strong> si cumple todo, o <strong>N bloqueos</strong> con
        cuántos motivos rojos le faltan por resolver.</p>
        <p>Pulsa la fila (o Enter con el teclado) y se abre la
        <strong> vista previa</strong>: el título, el precio, la portada y el
        stock que saldrían a cada canal, y qué le falta para publicarse allí.
        Nada se envía desde ahí.</p>
      </>
    ),
  },
  {
    titulo: 'Comisiones por canal',
    objetivo: '[data-guia="can-comisiones"]',
    seccion: 'panel',
    texto: (
      <>
        <p>Abajo a la derecha, <em>Comisiones por canal</em>: lo que cada
        plataforma cobra por venta, en porcentaje y en costo fijo. El precio
        que se publica en cada canal se calcula para que, tras descontar esa
        comisión, quede tu precio de Integra; la columna
        <em> $100.000 →</em> muestra el resultado con ese precio de ejemplo,
        redondeado hacia arriba a la centena.</p>
        <p>Escribe la comisión o el costo fijo y aparecen <strong>Guardar</strong> y
        <strong> Descartar</strong>. La comisión va entre 0 y 99,99 % y el costo
        fijo no puede ser negativo; si escribes algo que no es un número, la
        casilla del ejemplo lo dice. El precio nuevo sale a los canales en el
        siguiente envío de precios.</p>
      </>
    ),
  },
]
