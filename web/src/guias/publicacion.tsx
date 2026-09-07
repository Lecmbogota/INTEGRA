import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla de Publicación, elemento por elemento.
//
// Cada afirmación sale de la pantalla (web/src/Publicacion.tsx) o del código
// que la respalda: internal/publicar/estado.go para las situaciones,
// internal/publicar/motor.go para lo que encola Planificar,
// internal/publicar/vivas.go para poner a la venta, retirar y el contraste
// con el canal, e internal/store/publicacion_viva.go para las pausas. Si algo
// cambia allí, hay que cambiarlo aquí.

export const PASOS_PUBLICACION: Paso[] = [
  {
    titulo: 'Qué muestra esta pantalla',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-actualizar"]',
    texto: (
      <>
        <p>Publicación responde dos preguntas: qué tiene ya cada canal y qué
        falta por enviar. Arriba, un bloque por cuenta conectada; abajo, el
        detalle producto a producto.</p>
        <p><strong>Actualizar</strong> vuelve a leer las cifras. No envía nada a
        ningún canal.</p>
      </>
    ),
  },
  {
    titulo: 'Estado por canal',
    seccion: 'publicacion',
    objetivo: '[data-guia="publicacion"]',
    texto: (
      <>
        <p>Hay una fila por cada cuenta conectada —MercadoLibre, Falabella,
        WooCommerce o Shopify— con el nombre que le pusiste en Canales.</p>
        <p>Si no aparece ninguna, primero hay que conectar una cuenta en
        <strong> Canales</strong>. Desde aquí no se conecta nada.</p>
      </>
    ),
  },
  {
    titulo: 'Las cifras de cada canal',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-cifras"]',
    texto: (
      <>
        <p><em>N publicados</em> cuenta las fichas que Integra ya dejó en ese
        canal; <em>última</em> es la fecha del último envío que terminó bien.
        <em> Sin publicaciones todavía</em> quiere decir que a esa cuenta nunca
        se le ha enviado nada.</p>
        <p>Ojo: «publicado» significa que la ficha existe en el canal, no que el
        comprador ya la vea. Eso lo decide <strong>Poner a la venta</strong>, unos
        pasos más adelante.</p>
      </>
    ),
  },
  {
    titulo: 'Las pastillas: lo que pide atención',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-pastillas"]',
    texto: (
      <>
        <p>Solo salen cuando hay algo que mirar. <em>Con error</em>: fichas cuyo
        último envío al canal falló; el motivo está en Actividad. <em>En
        cola</em>: envíos de esa cuenta pendientes o en curso; se hacen en
        segundo plano y se siguen desde Actividad.</p>
        <p><em>Conexión falló</em> y <em>sin verificar</em> vienen de la prueba de
        conexión de Canales: falló o nunca se hizo, y el texto de la fila lo
        repite. <strong>Arréglala en Canales antes de planificar</strong>; si no,
        los envíos fallarán uno a uno (la pantalla lo advierte al pulsar).</p>
      </>
    ),
  },
  {
    titulo: 'Planificar envíos: solo lo que cambió',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-planificar"]',
    texto: (
      <>
        <p>Compara el catálogo con lo que ya está en el canal y deja pendiente
        únicamente lo que cambió: la ficha completa si cambiaron título,
        descripción o fotos; solo el precio si cambió el precio; solo el stock
        si cambió el stock. Un producto que cumple los requisitos y nunca se
        publicó sale entero, con su precio y su stock dentro.</p>
        <p><strong>El stock va primero</strong>: vender lo que no hay es lo que más
        cuesta. Lo que ya está al día no se reenvía, y lo que no cumple los
        requisitos se cuenta aparte. Nada sale del botón mismo: queda en segundo
        plano, con reintentos, y arriba aparece el resumen de lo que se dejó
        pendiente.</p>
      </>
    ),
  },
  {
    titulo: 'Lo que Planificar retiene, retira y reabre',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-nota-planificar"]',
    texto: (
      <>
        <p>Un precio que no cubre el coste no se envía. Si el producto es nuevo,
        se retiene entero; si ya está publicado, se retiene solo el precio —el
        canal sigue mostrando el bueno— y el stock se manda igual. El resumen lo
        dice: «no salen porque su precio no cubre el coste».</p>
        <p>Planificar también <strong>retira del canal</strong> las fichas cuyo
        producto salió del catálogo (archivado en Odoo, excluido a mano o sin
        referencia), para que nadie compre lo que la empresa ya no tiene, y
        <strong> reabre</strong> las que había retirado por eso cuando el producto
        vuelve. Lo que retiró el propio canal no se toca.</p>
      </>
    ),
  },
  {
    titulo: 'Poner a la venta',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-activar"]',
    texto: (
      <>
        <p>Una ficha recién publicada queda <strong>en borrador</strong> en el
        canal: existe con su precio, su stock y sus fotos, pero el comprador no
        la ve. Es a propósito: ponerla a la venta es una decisión, no un efecto
        de sincronizar.</p>
        <p><strong>Poner a la venta</strong> activa de cara al público todo lo que
        esa cuenta tiene en el canal, incluido lo que retiraste con Retirar.
        Pide confirmación y luego se hace en segundo plano, con prioridad.
        Pulsar dos veces no avisa dos veces. Nunca toca lo que retiró el propio
        canal.</p>
      </>
    ),
  },
  {
    titulo: 'Retirar de la venta no es despublicar',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-retirar"]',
    texto: (
      <>
        <p><strong>Retirar</strong> pausa en el canal todas las fichas vivas de esa
        cuenta: dejan de verse y de venderse, pero siguen allí con su historial,
        sus preguntas, sus reseñas y su posición en el buscador. Abajo aparecen
        como «Retirado por nosotros» y se reabren con Poner a la venta.</p>
        <p>Despublicar es otra cosa: está en Productos, solo para
        administradores, y <strong>borra la ficha del canal</strong> con todo eso
        (en MercadoLibre y Falabella queda cerrada para siempre). Para parar la
        venta un tiempo, usa Retirar.</p>
      </>
    ),
  },
  {
    titulo: 'Producto a producto',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-producto"]',
    texto: (
      <>
        <p>Aquí se ve cada producto en cada canal: una fila por producto y
        cuenta, con la referencia solo en la primera. Las columnas son Producto,
        Canal, Situación y Detalle, que dice qué falta enviar o qué impide
        publicar.</p>
        <p>La situación se calcula con el catálogo de ahora mismo, así que
        después de editar un producto o de planificar ya refleja el cambio.</p>
      </>
    ),
  },
  {
    titulo: 'Buscar un producto',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-buscar"]',
    texto: (
      <>
        <p>Escribe parte de la referencia o del nombre y la lista se acorta
        mientras tecleas. Sirve para responder rápido «¿este producto está en
        Falabella?» sin entrar al canal.</p>
        <p>Si no aparece ninguno, la pantalla lo dice: «Ningún producto en esa
        situación».</p>
      </>
    ),
  },
  {
    titulo: 'Filtrar por situación',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-situacion"]',
    texto: (
      <>
        <p>El desplegable deja una sola situación; un producto sale si la tiene
        en al menos uno de sus canales.</p>
        <p>Dos filtros que se usan a diario: <strong>No se pueden publicar</strong>,
        para saber qué arreglar, y <strong>Con cambios sin enviar</strong>, para
        ver qué saldrá con el siguiente Planificar.</p>
      </>
    ),
  },
  {
    titulo: 'Sin publicar, Al día, Cambios sin enviar',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-tabla"]',
    texto: (
      <>
        <p><em>Sin publicar</em>: cumple los requisitos y nunca se envió a ese
        canal. Es el estado natural de un producto nuevo, no un problema; sale
        con el siguiente Planificar o, desde Productos, marcándolo y pulsando
        Publicar.</p>
        <p><em>Al día</em>: publicado y sin nada pendiente. <em>Cambios sin
        enviar</em>: publicado, pero la ficha, el precio o el stock cambiaron
        desde el último envío; Detalle dice cuál («falta enviar: ficha, precio,
        stock»).</p>
      </>
    ),
  },
  {
    titulo: 'No se puede publicar, y los dos retirados',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-tabla"]',
    texto: (
      <>
        <p><em>No se puede publicar</em>: le falta algo y Detalle lo dice en rojo:
        referencia, título, descripción, precio o fotos; MercadoLibre exige
        además categoría mapeada, y Falabella EAN, peso, medidas del paquete y
        categoría. Un precio que no cubre el coste también frena. Se arregla en
        <strong> Productos</strong> o en <strong>Categorías</strong>. Si la ficha ya
        estaba publicada, se muestra por su estado y lo que falta se sigue
        listando.</p>
        <p><em>Retirado por nosotros</em>: pausado con Retirar o porque salió del
        catálogo. <em>Lo retiró el canal</em>: el canal la bajó, y detrás suele
        haber una infracción; no se reabre desde aquí, se resuelve en el canal.
        Cuando el canal la vuelve a activar, Integra lo nota.</p>
      </>
    ),
  },
  {
    titulo: 'El contraste con el canal',
    seccion: 'publicacion',
    objetivo: '[data-guia="pub-nota-borrador"]',
    texto: (
      <>
        <p>No hay botón para contrastar: Integra revisa por su cuenta, en segundo
        plano y por tandas, lo que da por publicado contra lo que el canal tiene
        de verdad, y cada ficha se vuelve a mirar como mucho una vez al día.</p>
        <p>Si el canal borró una ficha, vuelve a contar como «Sin publicar» y
        sale con el siguiente Planificar. Si la retiró de la venta, pasa a «Lo
        retiró el canal» y queda contada en los avisos. Es lo único que descubre
        una baja que nadie hizo desde Integra.</p>
      </>
    ),
  },
]
