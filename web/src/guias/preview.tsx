import type { Paso } from '../GuiaPasos'

// Recorrido del diálogo «Qué se enviará a cada canal» (Preview.tsx), con las
// fotos (Imagenes.tsx) y los atributos del canal (PanelAtributos) que van
// dentro. Es un diálogo: ningún paso lleva sección.
//
// Cada cifra sale de la regla que la aplica: los límites de título de
// internal/proyeccion/canales.go, los tamaños de foto de
// internal/imagen/requisitos.go, la búsqueda en internet de
// internal/api/imagenes.go y la comparación de precios de
// internal/mercadolibre/competencia.go. Si cambian allí, cambian aquí.

export const PASOS_PREVIEW: Paso[] = [
  {
    titulo: 'Qué es esta pantalla',
    objetivo: '[data-guia="prev-nota"]',
    texto: (
      <>
        <p>Es una <strong>comprobación</strong> de lo que Integra enviaría a cada
        canal para este producto: título, precio, foto de portada y stock, tal
        cual saldrían. <strong>No es una simulación</strong> de cómo se verá la
        publicación: cada tienda la monta a su manera y le añade cuotas, envío,
        impuestos o promociones que Integra no conoce.</p>
        <p>Desde aquí <strong>no se publica nada</strong> ni se abre conexión con
        ninguna tienda. Cierra con <em>Cerrar</em> o con la tecla Escape.</p>
      </>
    ),
  },
  {
    titulo: 'Fotos: tres formas de conseguirlas',
    objetivo: '[data-guia="img-acciones"]',
    texto: (
      <>
        <p>Sin foto el producto no se publica, y las que trae Odoo son
        miniaturas que ningún canal acepta. Las fotos que se envían son las
        del <strong>banco de Integra</strong>, y aquí se le dan al producto.</p>
        <p><strong>Subir desde el PC</strong> elige archivos de este equipo (JPEG,
        PNG o WebP); también puedes arrastrarlos a la zona de abajo. Si subes
        varios, van uno tras otro y se ve por cuál va.</p>
      </>
    ),
  },
  {
    titulo: 'Mediateca e internet',
    objetivo: '[data-guia="img-internet"]',
    texto: (
      <>
        <p><strong>Elegir de la mediateca</strong> toma fotos que ya están en el
        banco de Integra: de otra variante del mismo producto, o huérfanas que
        quedaron de una subida en masa.</p>
        <p><strong>Buscar en internet</strong> busca por la referencia y la marca
        (o por el nombre, si no hay referencia) y descarga varias fotos de al
        menos 500 px por lado; las más pequeñas se cuentan como descartadas.
        Suelen ser del fabricante: <strong>comprueba que puedes usarlas</strong>
        antes de publicar, y quita las que no sirvan.</p>
      </>
    ),
  },
  {
    titulo: 'Cada foto y en qué canales sirve',
    objetivo: '[data-guia="img-galeria"]',
    texto: (
      <>
        <p>Debajo de cada foto van sus medidas y su peso, y una pastilla por
        canal: <strong>verde</strong> si cumple sus requisitos, <strong>roja</strong>
        si ese canal la rechazaría. Deja el ratón sobre la pastilla para leer el
        motivo.</p>
        <p>Lo que se mira es la foto original: el lado menor debe llegar a
        500 px en MercadoLibre, 600 en Falabella, 400 en Shopify y 300 en
        WooCommerce; por debajo de 1200 px se pierde el zoom. MercadoLibre y
        Falabella tampoco aceptan WebP: para esos canales se envía siempre
        una copia cuadrada de 1200 px en JPEG.</p>
      </>
    ),
  },
  {
    titulo: 'Portada, Editar y Quitar',
    objetivo: '[data-guia="img-foto-acciones"]',
    texto: (
      <>
        <p>La <strong>portada</strong> es la primera foto que se envía y la que
        aparece en las maquetas de abajo. <em>Hacer portada</em> la cambia; la
        que lo es lleva la pastilla <em>Portada</em>.</p>
        <p><strong>Editar</strong> abre la foto para recortarla, girarla, encajarla
        en un cuadrado o cambiarle el formato. <strong>Quitar</strong> la saca del
        banco de Integra y no se deshace: si es la portada, se avisa, porque el
        producto queda sin portada hasta que elijas otra.</p>
      </>
    ),
  },
  {
    titulo: 'Una tarjeta por canal',
    objetivo: '[data-guia="prev-rejilla"]',
    texto: (
      <>
        <p>Cuatro tarjetas, una por canal: WooCommerce, Shopify, MercadoLibre y
        Falabella. Arriba de cada una, <strong>Publicable</strong> en verde, o en
        rojo cuántos <strong>bloqueos</strong> tiene: cosas sin las que ese canal
        rechazaría la ficha.</p>
        <p>El precio puede cambiar de una tarjeta a otra: a cada canal se le
        compensa su comisión y su costo fijo, redondeando al centenar de pesos,
        para que lo que queda después de la comisión sea el precio de Integra.</p>
      </>
    ),
  },
  {
    titulo: 'La maqueta y el título',
    objetivo: '[data-guia="prev-maqueta"]',
    texto: (
      <>
        <p>La maqueta pinta el título, el precio, la portada y el stock con los
        colores de cada tienda, para reconocer de un vistazo de qué canal se
        habla. Si hay una promoción vigente, el precio anterior sale tachado
        (menos en MercadoLibre, que no tiene «antes y después»: allí la oferta
        baja el precio).</p>
        <p>Debajo, el <strong>contador del título</strong>: MercadoLibre admite
        60 caracteres, Falabella 150, WooCommerce y Shopify 255. Si se pasa, el
        contador se pone en rojo y en MercadoLibre es un bloqueo. Sin título
        propio para el canal se usa el de otro canal o el nombre de Odoo.</p>
      </>
    ),
  },
  {
    titulo: 'Bloqueos y avisos',
    objetivo: '[data-guia="prev-faltantes"]',
    texto: (
      <>
        <p>La lista roja son <strong>bloqueos</strong>: el canal no aceptaría la
        ficha. Comunes a todos: sin referencia, sin precio o sin descripción.
        Además, MercadoLibre exige categoría mapeada, marca y título de 60
        caracteres; Falabella exige marca, categoría y peso.</p>
        <p>La lista amarilla son <strong>avisos</strong>: se publicaría, pero peor.
        Sin fotos no convierte; sin stock sale agotado; sin categoría en
        WooCommerce cae en «Sin categorizar»; sin marca en Shopify no se puede
        filtrar por fabricante. Las notas grises de más abajo son
        particularidades del canal, no fallos.</p>
      </>
    ),
  },
  {
    titulo: 'Corregir desde aquí',
    objetivo: '[data-guia="prev-faltantes"]',
    texto: (
      <>
        <p>Casi cada falta trae un enlace que <strong>lleva a donde se
        arregla</strong>: <em>Poner el precio</em>, <em>Escribir la descripción</em>
        y <em>Poner la marca</em> abren el editor del producto en Venta;
        <em> Corregir el título</em>, en Títulos; <em>Poner el peso</em>, en
        Envío; <em>Mapear la categoría</em> va a Categorías del canal.</p>
        <p><em>Subir fotos</em> sube hasta las fotos de esta misma pantalla. La
        referencia y las existencias vienen de Odoo y <strong>se corrigen
        allí</strong>: aquí solo se dice.</p>
      </>
    ),
  },
  {
    titulo: 'Ver payload',
    objetivo: '[data-guia="prev-payload"]',
    texto: (
      <>
        <p><strong>Ver payload JSON</strong> abre, al final de la pantalla, lo
        que se enviaría a ese canal <strong>tal cual</strong>, para quien quiera
        verlo con lupa. Es el mismo contenido que resumen la maqueta y las
        listas; si a la ficha le falta la marca, aquí se ve el hueco.</p>
        <p>No hace falta entenderlo para publicar. <em>Ocultar</em> lo cierra.</p>
      </>
    ),
  },
  {
    titulo: 'Atributos del canal',
    objetivo: '[data-guia="attr-panel"]',
    texto: (
      <>
        <p>MercadoLibre y Falabella exigen, según la categoría, unos
        <strong> atributos</strong> (marca, modelo, color, capacidad…) y sin los
        obligatorios rechazan la publicación entera. Elige el canal con las
        pestañas; la pastilla dice cuántos obligatorios faltan o
        <em> Completos</em>.</p>
        <p>Los obligatorios llevan asterisco. Integra deduce lo que puede de la
        marca, la referencia y el nombre (columna <em>Origen</em>) y
        <strong> nunca inventa</strong>: escribe el valor y pulsa <em>Guardar</em>.
        Con <em>Añadir otro atributo</em> se traen los opcionales. Si no aparece
        ninguno, falta mapear la categoría del producto.</p>
      </>
    ),
  },
  {
    titulo: 'Competencia en MercadoLibre',
    objetivo: '[data-guia="prev-competencia"]',
    texto: (
      <>
        <p><strong>Comparar precios</strong> busca en MercadoLibre Colombia
        publicaciones de este mismo producto, por su código de barras o, si no
        lo tiene, por la referencia, y trae hasta ocho: título con enlace,
        vendedor, precio y la diferencia con el precio de Integra.</p>
        <p>En rojo si el propio está más de un 5 % por encima, en verde si está
        más de un 5 % por debajo. Es una consulta en vivo que
        <strong> necesita la cuenta de MercadoLibre conectada</strong>; no guarda
        nada. <em>Actualizar</em> la repite.</p>
      </>
    ),
  },
]
