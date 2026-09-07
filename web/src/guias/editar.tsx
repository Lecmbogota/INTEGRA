import type { Paso } from '../GuiaPasos'

// Recorrido del diálogo «Editar producto». Se abre siempre en la pestaña
// Venta, así que sus campos se resaltan uno a uno; las demás pestañas no se
// pueden abrir desde el recorrido y se explican sobre la barra de pestañas.
// Quién exige cada dato sale de las reglas de cada canal y de los motivos
// que bloquean la publicación.

export const PASOS_EDITAR: Paso[] = [
  {
    titulo: 'La ficha del producto',
    objetivo: '[data-guia="edit-cabecera"]',
    texto: (
      <>
        <p>De Odoo llegan solo la <strong>referencia, el nombre y el stock</strong>,
        que se ven aquí arriba y no se editan. Todo lo demás —precio, marca,
        descripción, títulos, medidas, promociones— es de Integra y se
        completa en esta ficha.</p>
        <p>Las sincronizaciones con Odoo nunca pisan lo que guardes aquí.</p>
      </>
    ),
  },
  {
    titulo: 'Precio de venta',
    objetivo: '[data-guia="edit-precio"]',
    texto: (
      <>
        <p>En pesos colombianos. Lo exigen los <strong>cuatro canales</strong>: sin
        precio el producto sale en rojo como <em>Sin precio asignado</em> y no se
        publica.</p>
        <p>Si Odoo tiene una tarifa para el producto, aparece debajo como
        sugerencia con un enlace <strong>usar</strong>. Dejar la casilla vacía borra
        el precio. Un precio que no cubra el coste se marca luego como
        <em> Precio por debajo del coste</em> y también bloquea.</p>
      </>
    ),
  },
  {
    titulo: 'Marca',
    objetivo: '[data-guia="edit-marca"]',
    texto: (
      <>
        <p>Escribe la marca o elígela de las ya conocidas; si no existe, se
        crea. Vacía, el producto sale en amarillo como <em>Sin marca</em>.</p>
        <p><strong>MercadoLibre</strong> la exige en casi todas las categorías y
        <strong> Falabella</strong> la exige registrada en su catálogo: sin marca,
        esos dos no publican. Shopify la usa como fabricante para filtrar en la
        tienda; WooCommerce no la pide.</p>
      </>
    ),
  },
  {
    titulo: 'Condición',
    objetivo: '[data-guia="edit-condicion"]',
    texto: (
      <>
        <p><strong>Nuevo</strong>, <strong>Usado</strong> o
        <strong> Reacondicionado</strong>. Queda guardado en la ficha del producto
        para que el equipo sepa qué está vendiendo; por defecto es Nuevo.</p>
      </>
    ),
  },
  {
    titulo: 'Código de barras (EAN)',
    objetivo: '[data-guia="edit-ean"]',
    texto: (
      <>
        <p><strong>Falabella lo exige</strong>: sin EAN rechaza la publicación de
        ese producto. Shopify lo lleva en la ficha; MercadoLibre y WooCommerce
        no lo piden.</p>
        <p>No sale como problema en la lista, así que para encontrar los que
        no lo tienen usa el criterio <em>sin EAN</em> de la plantilla masiva.</p>
      </>
    ),
  },
  {
    titulo: 'Garantía',
    objetivo: '[data-guia="edit-garantia"]',
    texto: (
      <>
        <p>Los <strong>meses</strong> de garantía, en número entero, y al lado
        <strong> quién responde</strong>: fabricante, vendedor o sin garantía.
        Ninguno de los dos es obligatorio para publicar.</p>
        <p>Si escribes algo que no es un número, la ficha no se guarda y te lo
        dice en esta misma pestaña.</p>
      </>
    ),
  },
  {
    titulo: 'Descripción de venta',
    objetivo: '[data-guia="edit-descripcion"]',
    texto: (
      <>
        <p>Es el texto que verán los compradores. Sin ella el producto sale en
        rojo como <em>Sin descripción</em> y <strong>no se publica en ningún
        canal</strong>.</p>
        <p>Una descripción escrita a mano queda protegida: el generador
        automático de contenido no vuelve a tocarla.</p>
      </>
    ),
  },
  {
    titulo: 'Excluir del catálogo publicable',
    objetivo: '[data-guia="edit-excluir"]',
    texto: (
      <>
        <p>Marcado, el producto <strong>sale de la lista</strong>, deja de contar
        como problema y no se publica. Sirve para lo que no es mercancía o para
        lo que no quieres vender por internet.</p>
        <p>Para verlo de nuevo, activa <em>Ver excluidos</em> en la lista y
        desmarca esta casilla, o usa <em>Editar en masa → Devolver al
        catálogo</em>.</p>
      </>
    ),
  },
  {
    titulo: 'Pestaña Promoción',
    objetivo: '[data-guia="edit-pestanas"]',
    texto: (
      <>
        <p>Rebajas con fecha de inicio y fin, <strong>por canal</strong>: eliges la
        cuenta, un precio menor que el normal —se ve el porcentaje de
        descuento— y cuándo empieza y termina; sin fin, dura hasta que la
        canceles.</p>
        <p>Se <strong>guardan al crearlas</strong>, aparte del botón Guardar de la
        ficha, y se cancelan una a una. WooCommerce y Falabella las programan
        de forma nativa; en Shopify y MercadoLibre, Integra baja el precio al
        empezar y lo devuelve al terminar. Hace falta un precio normal y una
        cuenta conectada.</p>
      </>
    ),
  },
  {
    titulo: 'Pestaña Títulos',
    objetivo: '[data-guia="edit-pestanas"]',
    texto: (
      <>
        <p>Un título por canal, cada uno con su límite: <strong>MercadoLibre
        60</strong> caracteres, Falabella 150, WooCommerce y Shopify 255. El que
        dejes vacío se publica con el nombre de Odoo.</p>
        <p>El contador se pone en rojo al pasarse. En MercadoLibre el título
        decide si te encuentran: pon primero producto, marca y modelo. Un
        título largo para MercadoLibre se marca en la lista como
        <em> Título demasiado largo</em>.</p>
      </>
    ),
  },
  {
    titulo: 'Pestaña Envío',
    objetivo: '[data-guia="edit-pestanas"]',
    texto: (
      <>
        <p><strong>Peso real</strong> en kilos y <strong>largo, ancho y alto</strong> en
        centímetros. Con las tres medidas se calcula en vivo el peso
        volumétrico (largo × ancho × alto ÷ 5000); el canal cobra el envío por
        el mayor de los dos.</p>
        <p><strong>Falabella no publica sin peso</strong>. WooCommerce y Shopify lo
        envían con la ficha. Dejar una casilla vacía borra la medida guardada.</p>
      </>
    ),
  },
  {
    titulo: 'Pestaña Interno',
    objetivo: '[data-guia="edit-pestanas"]',
    texto: (
      <>
        <p><strong>Vídeo</strong>: la dirección de YouTube del producto, guardada
        en la ficha. <strong>Nota interna</strong>: lo que el equipo necesite
        recordar de este producto; <strong>nunca se publica</strong> en ningún
        canal.</p>
      </>
    ),
  },
  {
    titulo: 'Guardar o cancelar',
    objetivo: '[data-guia="edit-pie"]',
    texto: (
      <>
        <p><strong>Guardar cambios</strong> escribe solo lo que tocaste y cierra la
        ficha; la lista y los avisos de estado se actualizan al momento. Si
        no cambiaste nada, el botón lo dice y no hace falta pulsarlo.</p>
        <p>Si algo está mal escrito, no se guarda: la ficha salta a la pestaña
        del campo y explica qué corregir. <strong>Cancelar</strong>, la tecla
        Escape o tocar fuera de la ficha cierran sin guardar, y si hay cambios
        preguntan antes.</p>
      </>
    ),
  },
]
