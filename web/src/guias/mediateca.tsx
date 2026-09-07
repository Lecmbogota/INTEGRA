import type { Paso } from '../GuiaPasos'

// Recorrido de la sección Imágenes: el banco de fotos visto entero.
//
// Cada afirmación sale de Mediateca.tsx y de las reglas del banco: 600 px
// es el lado mínimo del canal más exigente, 60 fotos por página, tres
// subidas a la vez, y una foto en uso no se borra desde aquí. Si cambia
// allí, hay que cambiarlo aquí.

export const PASOS_MEDIATECA: Paso[] = [
  {
    titulo: 'Subir fotos en masa',
    seccion: 'mediateca',
    objetivo: '[data-guia="subir"]',
    texto: (
      <>
        <p><strong>Arrastra aquí las fotos</strong> o pulsa la zona para
        elegirlas, cuantas quieras de una vez. Se admiten JPEG, PNG y WebP de
        hasta 25 MB cada una; lo que no sea una imagen se descarta antes de
        subirlo.</p>
        <p>Suben de tres en tres y debajo va apareciendo el informe, archivo
        por archivo: a qué producto fue cada foto, cuál quedó en el banco sin
        producto y cuál falló y por qué. Al terminar, una línea resume la
        tanda.</p>
      </>
    ),
  },
  {
    titulo: 'Que cada foto caiga en su producto',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-por-nombre"]',
    texto: (
      <>
        <p>Con esta casilla marcada, Integra lee el nombre de cada archivo y
        busca el producto cuya referencia sea ese nombre. Es como llegan las
        carpetas del fabricante: <code>SKU-1.jpg</code>, <code>SKU-2.jpg</code>…</p>
        <p>Se prueba el nombre tal cual y, si no coincide, sin el número
        final (vale con guion, guion bajo, espacio o entre paréntesis, de una
        o dos cifras). Lo que no case con ninguna referencia se queda en el
        banco como huérfana, y el informe dice qué referencias se probaron.</p>
      </>
    ),
  },
  {
    titulo: 'Productos sin foto',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-cifra-sin-foto"]',
    texto: (
      <p>Cuántos productos del catálogo (activos y no excluidos) no tienen
      ninguna foto. Ninguno de ellos puede publicarse en ningún canal. La
      cifra se calcula sobre el catálogo entero, no sobre esta página, y va
      en rojo mientras quede alguno.</p>
    ),
  },
  {
    titulo: 'Fotos pequeñas',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-cifra-pequenas"]',
    texto: (
      <p>Fotos del banco cuyo lado menor no llega a <strong>600 px</strong>.
      Es el mínimo del canal más exigente: MercadoLibre las rechaza en el
      alta, así que no sirven en ninguna ficha. El filtro «Pequeñas» las
      lista para reemplazarlas.</p>
    ),
  },
  {
    titulo: 'Disco en huérfanas',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-cifra-huerfanas"]',
    texto: (
      <p>Cuánto ocupan en disco las fotos que no usa ningún producto. Suelen
      ser lo que dejó una búsqueda en internet y no convenció. No cuentan
      para publicar, solo cuestan espacio: desde aquí se rescatan asignándolas
      a un producto, o se borran.</p>
    ),
  },
  {
    titulo: 'Filtros: qué responde cada uno',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-filtros"]',
    texto: (
      <>
        <p><strong>Todas</strong> es el banco entero. <strong>Aptas</strong>:
        lado menor de 600 px o más, valen en los cuatro canales.
        <strong> Pequeñas</strong>: por debajo de 600 px, MercadoLibre las
        rechaza.</p>
        <p><strong>Huérfanas</strong>: no las usa ningún producto, ocupan
        disco sin publicarse. <strong>Duplicadas</strong>: hay otra foto en el
        banco con el mismo peso exacto y las mismas medidas; casi siempre es
        la misma foto subida dos veces.</p>
        <p>El total junto a «Banco de imágenes» es el del filtro activo.</p>
      </>
    ),
  },
  {
    titulo: 'Buscar por producto',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-buscar"]',
    texto: (
      <p>Escribe una referencia o parte del nombre de un producto y quedan
      solo las fotos que ese producto usa. Se combina con el filtro activo.
      Las huérfanas no tienen producto, así que no salen por búsqueda: para
      ellas está el filtro.</p>
    ),
  },
  {
    titulo: 'Orden',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-orden"]',
    texto: (
      <p><strong>Recientes</strong> pone primero lo último que entró.
      <strong> Más pesadas</strong> sirve para liberar disco. <strong>Más
      pequeñas</strong> saca arriba lo que primero rechaza un canal, y
      <strong> Más grandes</strong> lo contrario. Cambiar de filtro, de
      búsqueda o de orden vuelve a la primera página y desmarca lo marcado.</p>
    ),
  },
  {
    titulo: 'Marcar varias y actuar en lote',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-lote"]',
    texto: (
      <>
        <p>Cada tarjeta tiene su casilla; esta marca o desmarca las de la
        página entera, y al lado se ve cuántas hay marcadas.</p>
        <p><strong>Asignar marcadas a un producto</strong> enlaza todas al
        mismo producto, en el orden en que están en la página.
        <strong> Borrar huérfanas marcadas</strong> solo cuenta las marcadas
        que no usa nadie: una foto en uso nunca se borra desde aquí, porque
        dejaría una ficha publicada con la imagen rota.</p>
      </>
    ),
  },
  {
    titulo: 'Cada foto, de un vistazo',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-tarjeta"]',
    texto: (
      <>
        <p>Debajo de la miniatura: medidas en píxeles, peso y formato. Si el
        lado menor baja de 600 px aparece la pastilla <strong>pequeña</strong>.
        En la línea siguiente, de dónde vino —subida a mano, descargada de
        internet con el sitio de origen, o banco del fabricante— y la fecha
        en que entró.</p>
        <p>La casilla de la izquierda la marca para asignar o borrar en lote.</p>
      </>
    ),
  },
  {
    titulo: 'Quién usa la foto',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-productos"]',
    texto: (
      <p>Cada referencia es un producto que usa esta foto; la estrella ★
      señala que es su portada. Pulsa una referencia para abrir la vista
      previa del producto, que es donde se suben, se quitan y se elige
      portada. Si nadie la usa, verás la pastilla <strong>Sin producto</strong>.</p>
    ),
  },
  {
    titulo: 'Editar, asignar o borrar',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-acciones"]',
    texto: (
      <>
        <p><strong>Editar</strong> abre el editor de fotos: recortar, girar,
        encajar en cuadrado, cambiar formato. <strong>Asignar a producto</strong>
        la enlaza a un producto sin volver a subirla.</p>
        <p><strong>Borrar del disco</strong> solo aparece en las huérfanas:
        pide confirmación y elimina el archivo con sus copias reducidas. Para
        quitar una foto que sí usa un producto, hazlo desde el producto.</p>
      </>
    ),
  },
  {
    titulo: 'Verla en grande',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-miniatura"]',
    texto: (
      <p>Pulsa la miniatura para abrir el visor: una versión de hasta 800 px,
      las medidas, el peso, el formato y, si vino de internet, un enlace a la
      página de origen. Desde el visor también puedes <strong>Editar</strong> o
      <strong> Asignar a producto</strong>. Se cierra con Cerrar, pulsando
      fuera o con Escape.</p>
    ),
  },
  {
    titulo: 'Páginas de 60',
    seccion: 'mediateca',
    objetivo: '[data-guia="med-paginacion"]',
    texto: (
      <p>El banco se ve de 60 fotos en 60. <strong>Anterior</strong> y
      <strong> Siguiente</strong> cambian de página y el texto del medio dice
      en qué tramo del total estás. Al cambiar de página se desmarca lo
      marcado, para no actuar sobre fotos que ya no están a la vista.</p>
    ),
  },
  {
    titulo: 'El diálogo «Asignar a producto»',
    seccion: 'mediateca',
    texto: (
      <>
        <p>Se abre desde el botón de una tarjeta, desde el visor o con
        «Asignar marcadas». Arriba, las miniaturas de lo que vas a asignar.
        Escribe al menos dos letras de la referencia o del nombre y elige una
        fila: las que ya tienen esa foto salen como «ya la tiene» y no se
        pueden elegir; las que no tienen ninguna, como «sin fotos».</p>
        <p><strong>Ponerla como portada</strong> (o <strong>La primera como
        portada</strong> con varias) la convierte en la cara del producto en
        los cuatro canales. Se maneja con el teclado: flechas para elegir,
        Enter para asignar. El diálogo tiene su propio botón <strong>?</strong>.</p>
      </>
    ),
  },
  {
    titulo: 'Buscar imágenes faltantes',
    seccion: 'mediateca',
    texto: (
      <p>El botón de la cabecera de esta sección recorre el catálogo entero
      y busca en internet, por referencia, fotos para los productos que aún
      no tienen una imagen apta para los cuatro canales. Solo guarda las de
      600 px o más y las deja enlazadas a su producto. Lo que después quites
      de un producto por no convencer acaba en «Huérfanas», y este banco es
      donde se limpia o se rescata.</p>
    ),
  },
]

// Recorrido corto del diálogo «Asignar a producto», para su botón «?».
export const PASOS_ASIGNAR: Paso[] = [
  {
    titulo: 'Lo que vas a asignar',
    objetivo: '[data-guia="med-asignar-tira"]',
    texto: (
      <p>Las miniaturas de las fotos que se van a enlazar, en el orden en que
      quedarán. Con varias marcadas, mirarlas aquí evita enlazar la moto
      junto con la cámara. Quedan detrás de las fotos que el producto ya
      tenga.</p>
    ),
  },
  {
    titulo: 'Buscar el producto',
    objetivo: '[data-guia="med-asignar-buscador"]',
    texto: (
      <>
        <p>Escribe al menos dos letras de la referencia o del nombre. Salen
        hasta 12 coincidencias; si hay más, el texto de debajo lo dice y
        conviene afinar.</p>
        <p>Cada fila trae referencia, nombre y marca. «Ya la tiene» marca los
        productos que ya usan todas estas fotos y no se pueden elegir; «sin
        fotos», los que no tienen ninguna.</p>
      </>
    ),
  },
  {
    titulo: 'Como portada',
    objetivo: '[data-guia="med-asignar-portada"]',
    texto: (
      <p>Marcada, la foto (o la primera de varias) pasa a ser la portada del
      producto: la cara que ve el comprador en los cuatro canales. Sin
      marcarla, se añade al final y solo queda de portada si el producto no
      tenía ninguna foto.</p>
    ),
  },
  {
    titulo: 'Asignar',
    objetivo: '[data-guia="med-asignar-confirmar"]',
    texto: (
      <p>Elige una fila con clic o con las flechas y pulsa <strong>Asignar</strong>
      o Enter; el doble clic sobre la fila también asigna. El diálogo se
      cierra al terminar y la mediateca se refresca. Cancelar, pulsar fuera o
      Escape cierran sin cambios.</p>
    ),
  },
]
