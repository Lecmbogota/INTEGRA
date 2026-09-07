import type { Paso } from '../GuiaPasos'

// Recorrido de la lista de productos. Cada paso resalta un elemento real de
// la pantalla —un filtro, una columna, un botón— y cuenta qué hace. Lo que
// se afirma sobre canales sale de la regla que lo aplica (qué exige cada uno
// y qué bloquea la publicación); si eso cambia, hay que cambiarlo aquí.

export const PASOS_CATALOGO: Paso[] = [
  {
    titulo: 'Buscar un producto',
    seccion: 'catalogo',
    objetivo: '[data-guia="buscar"]',
    texto: (
      <>
        <p>Escribe parte del <strong>nombre</strong> o de la <strong>referencia</strong> y
        la lista se acota sola, sin pulsar nada.</p>
        <p>Al cambiar la búsqueda, o cualquier otro filtro, se vuelve a la
        primera página y se vacía la selección: así una acción en lote nunca
        alcanza a productos que ya no ves.</p>
      </>
    ),
  },
  {
    titulo: 'Marca y categoría',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-marca"]',
    texto: (
      <>
        <p>Los dos desplegables acotan por <strong>marca</strong> y por
        <strong> categoría de Odoo</strong>. Cada opción lleva entre paréntesis
        cuántos productos tiene; las que no tienen ninguno no aparecen.</p>
        <p>Se combinan con la búsqueda y con las casillas de al lado: todo lo
        que actives se aplica a la vez.</p>
      </>
    ),
  },
  {
    titulo: 'Solo con problemas y Sin precio',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-problemas"]',
    texto: (
      <>
        <p><strong>Solo con problemas</strong> deja los productos que tienen algún
        aviso pendiente en la columna Estado, sea de los que impiden publicar o
        de los que solo empeoran la ficha.</p>
        <p><strong>Sin precio</strong> deja los que aún no tienen precio de venta.
        Es el filtro que conviene poner antes de una edición en masa del tipo
        «precio = coste × factor», para no pisar precios ya puestos.</p>
      </>
    ),
  },
  {
    titulo: 'Pendientes por publicar',
    seccion: 'catalogo',
    objetivo: '[data-guia="pendientes"]',
    texto: (
      <>
        <p>Deja los productos que <strong>no tienen ficha en ningún canal</strong>
        conectado. Responde a «¿qué me falta por subir?» sin comparar pantallas.</p>
        <p>Combinado con <em>Solo con problemas</em> desmarcado, es la lista de
        lo que ya se puede seleccionar y publicar.</p>
      </>
    ),
  },
  {
    titulo: 'Ver excluidos',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-excluidos"]',
    texto: (
      <>
        <p>La lista normal solo enseña mercancía. Esta casilla cambia a lo que
        está <strong>fuera del catálogo</strong>: gastos, activos fijos, servicios
        y lo que alguien excluyó a mano desde la ficha o en masa.</p>
        <p>Un producto excluido no se publica ni cuenta como problema. Desde
        aquí puedes abrirlo con <em>Editar</em> y desmarcar la exclusión, o usar
        <em> Editar en masa → Devolver al catálogo</em>.</p>
      </>
    ),
  },
  {
    titulo: 'Las columnas y el orden',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-cabecera"]',
    texto: (
      <>
        <p><strong>Referencia</strong> es el SKU de Odoo; <strong>Producto</strong> lleva
        el nombre y, debajo, su categoría; <strong>Marca</strong> es la que se
        asignó en Integra; <strong>Stock</strong> suma las existencias de todos los
        almacenes.</p>
        <p><strong>Precio</strong> es el de venta puesto en Integra. Si no lo hay pero
        Odoo tiene una tarifa, se ve entre paréntesis como <em>sugerido</em>: no
        se publica hasta que alguien lo asigne al editar.</p>
        <p>Pulsa una cabecera para <strong>ordenar</strong> por ella y otra vez para
        invertir el sentido; la flecha marca cuál manda.</p>
      </>
    ),
  },
  {
    titulo: 'Publicación: dónde vive la ficha',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-publicacion"]',
    texto: (
      <>
        <p>Una pastilla por canal en el que el producto tiene ficha. Pasa el
        ratón para leer el estado: <strong>verde</strong> está publicado,
        <strong> rojo</strong> falló el último envío, <strong>amarillo</strong> está en
        cola o retirado de la venta.</p>
        <p><em>Sin publicar</em> significa que todavía no está en ningún canal.</p>
      </>
    ),
  },
  {
    titulo: 'Estado: qué le falta',
    seccion: 'catalogo',
    objetivo: '[data-guia="estado"]',
    texto: (
      <>
        <p><strong>Listo</strong> en verde: no le falta nada. Las pastillas
        <strong> rojas impiden publicar</strong>: sin referencia, referencia repetida
        en otro producto de Odoo, sin descripción, sin precio, precio por debajo
        del coste o sin fotos.</p>
        <p>Las <strong>amarillas</strong> se publican, pero peor: título demasiado
        largo para MercadoLibre, sin marca, sin existencias, SKU renombrado en
        Odoo. Pasa el ratón por una pastilla para ver el detalle.</p>
      </>
    ),
  },
  {
    titulo: 'Seleccionar productos',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-seleccionar"]',
    texto: (
      <>
        <p>La casilla de cada fila marca ese producto; esta de la cabecera
        <strong> marca los de la página</strong> entera. La barra de arriba pasa a
        decir cuántos hay seleccionados y ofrece <em>Limpiar selección</em>.</p>
        <p>La selección se conserva al pasar de página, pero se vacía al tocar
        cualquier filtro.</p>
      </>
    ),
  },
  {
    titulo: 'Publicar y Despublicar',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-barra"]',
    texto: (
      <>
        <p>Con productos seleccionados aparecen aquí dos botones.
        <strong> Publicar</strong> encola el envío de los marcados a todas las
        cuentas de canal activas; los que no cumplen los requisitos se quedan
        fuera y se cuentan aparte. El avance se sigue en Publicación.</p>
        <p><strong>Despublicar</strong> solo lo ve un administrador. Deja elegir de
        qué canales se quita la ficha; el producto sigue en Integra, pero se
        pierde el historial, las preguntas y la posición en el buscador. En
        <strong> MercadoLibre y Falabella la ficha queda cerrada para siempre</strong>.
        Si solo quieres parar la venta un tiempo, usa <em>Retirar</em> en
        Publicación.</p>
      </>
    ),
  },
  {
    titulo: 'Actualizar por plantilla',
    seccion: 'catalogo',
    objetivo: '[data-guia="plantilla"]',
    texto: (
      <>
        <p>Descarga los productos del filtro actual como hoja de
        <strong> Excel</strong>, con su precio y su promoción ya rellenados; se
        editan las columnas verdes y se vuelve a subir.</p>
        <p>Integra revisa el archivo entero y enseña qué va a cambiar antes de
        guardar nada. Es el camino para tocar cientos de precios y promociones
        de una vez.</p>
      </>
    ),
  },
  {
    titulo: 'Editar en masa',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-editar-masa"]',
    texto: (
      <>
        <p>Aplica una misma operación a los seleccionados o a todo el filtro:
        precio desde el coste, subir o bajar un porcentaje, precio fijo, borrar
        el precio, asignar marca, excluir o devolver al catálogo.</p>
        <p>Siempre se <strong>simula primero</strong>: enseña cuántos cambiarían y
        una muestra con antes y después, y solo entonces se puede aplicar.</p>
      </>
    ),
  },
  {
    titulo: 'Editar o ver la vista previa',
    seccion: 'catalogo',
    objetivo: '[data-guia="editar"]',
    texto: (
      <>
        <p><strong>Editar</strong> abre la ficha del producto: precio, marca,
        descripción, títulos por canal, medidas y peso, promociones y nota
        interna. Lo que va en ella es de Integra: Odoo no lo toca.</p>
        <p>Pulsar en cualquier otro punto de la fila abre la
        <strong> vista previa</strong>: cómo quedaría en cada canal y qué le falta
        para salir.</p>
      </>
    ),
  },
  {
    titulo: 'Páginas',
    seccion: 'catalogo',
    objetivo: '[data-guia="cat-paginacion"]',
    texto: (
      <>
        <p>La lista va de <strong>50 en 50</strong>. Aquí se ve qué tramo estás
        mirando y cuántos productos hay en total con el filtro puesto.</p>
        <p>Las acciones en lote no dependen de la página: <em>Editar en masa</em> y
        la plantilla pueden alcanzar a todos los del filtro, no solo a los
        cincuenta que ves.</p>
      </>
    ),
  },
]
