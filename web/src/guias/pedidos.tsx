import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla de Pedidos, elemento por elemento.
//
// Cada afirmación sale de la pantalla (web/src/Pedidos.tsx) o del código que
// la respalda: internal/ordenes/ordenes.go para la traída, el montaje en Odoo
// y el stock; internal/ordenes/despacho.go para el aviso de despacho;
// internal/api/ordenes.go para reintentar y despachar a mano. Si algo cambia
// allí, hay que cambiarlo aquí.
//
// El detalle de un pedido solo existe al desplegarlo: si el operador tiene
// una fila abierta, esos pasos la resaltan; si no, el globo va centrado.

export const PASOS_PEDIDOS: Paso[] = [
  {
    titulo: 'Qué muestra esta pantalla',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-actualizar"]',
    texto: (
      <>
        <p>Lo que se vendió en los canales y su camino hasta Odoo: cada pedido
        llega aquí, se guarda y se crea en Odoo como pedido de venta.</p>
        <p><strong>Actualizar</strong> vuelve a leer las tarjetas y la lista. No
        pide nada a los canales.</p>
      </>
    ),
  },
  {
    titulo: 'Traer pedidos nuevos',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-traer"]',
    texto: (
      <>
        <p>Pide a cada cuenta conectada los pedidos que hubo desde la última vez.
        La primera vez trae solo los últimos siete días, para no crear en Odoo
        cientos de pedidos viejos.</p>
        <p>La descarga corre en segundo plano y la lista se actualiza sola en
        unos segundos. Si una cuenta no responde, se dice cuál. Esto mismo
        ocurre solo en cada pasada del horario de Automatización; el botón sirve
        para no esperar.</p>
      </>
    ),
  },
  {
    titulo: 'Las tarjetas del resumen',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-resumen"]',
    texto: (
      <>
        <p><em>Pedidos</em>: todos los recibidos alguna vez. <em>Vendido hoy</em>:
        la suma de los pedidos de hoy. <em>En Odoo</em>: los ya creados como
        pedido de venta. <em>Pendientes</em>: guardados en Integra y aún sin
        montar en Odoo; se montan solos en segundo plano.</p>
        <p><em>Fallidos</em>, en rojo: el montaje en Odoo no pudo hacerse y
        alguien tiene que mirarlo. <em>Líneas sin SKU</em>: líneas cuya
        referencia no existe en el catálogo o la tienen dos productos; mientras
        un pedido tenga una, no se puede montar.</p>
      </>
    ),
  },
  {
    titulo: 'La lista y sus columnas',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-tabla"]',
    texto: (
      <>
        <p>Los cincuenta pedidos más recientes. <em>Pedido</em> es el número que
        le dio el canal; <em>Comprador</em>, quien compró; <em>Total</em>, lo que
        cobró el canal; <em>Fecha</em>, la del pedido en el canal.</p>
        <p>Pulsar la fila, o <strong>Ver detalle</strong>, despliega las líneas, el
        error si lo hubo y las acciones. Si todavía no llegó ningún pedido, la
        lista lo dice y te manda a conectar cuentas y traerlos.</p>
      </>
    ),
  },
  {
    titulo: 'Filtrar por estado',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-filtro"]',
    texto: (
      <>
        <p>El desplegable deja un solo estado y el texto de al lado cuenta
        cuántos de los recientes lo tienen. Está pensado para encontrar los
        fallidos sin recorrer cincuenta filas.</p>
        <p>Filtra sobre los pedidos ya cargados; si con ese estado no hay
        ninguno, la lista lo dice.</p>
      </>
    ),
  },
  {
    titulo: 'Qué significa cada estado',
    seccion: 'pedidos',
    texto: (
      <>
        <p><em>Recibido</em> y <em>Mapeado</em>: el pedido ya está guardado en
        Integra y espera a montarse en Odoo, cosa que ocurre sola en segundo
        plano. <em>En Odoo</em>: ya existe allí como pedido de venta.</p>
        <p><em>Falló</em>: el montaje no pudo hacerse; el detalle dice por qué.
        <em> Ignorado</em>: el canal canceló la venta. No se monta, y las
        unidades apartadas vuelven al stock. Si ya estaba en Odoo, Integra
        <strong> no lo cancela allí</strong>: eso lo decide una persona, y llega
        un aviso para que lo sepa.</p>
      </>
    ),
  },
  {
    titulo: 'Qué pasa en Odoo',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-detalle"]',
    texto: (
      <>
        <p>El pedido se crea en Odoo <strong>en borrador</strong>, a nombre del
        <strong> comprador real</strong>: se busca el contacto por correo y, si
        no existe, se crea con lo mínimo para facturar y despachar. Cada línea
        lleva el precio final que pagó el comprador, en la moneda del canal, y
        descuenta de la bodega asignada a esa cuenta.</p>
        <p><strong>Los impuestos no se tocan</strong>: Odoo no suma nada encima de
        lo que ya cobró el canal, y su configuración fiscal queda como estaba.
        Confirmar y facturar se hace en Odoo como siempre. Si Odoo calculara un
        total distinto, llega un aviso de descuadre antes de que alguien
        confirme. El número del pedido de venta aparece en el detalle.</p>
      </>
    ),
  },
  {
    titulo: 'El detalle de un pedido',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-detalle"]',
    texto: (
      <>
        <p>Arriba, el error del último intento y cuántos lleva, si falló; el
        número del pedido en Odoo, si ya está; y el envío y los impuestos que
        reportó el canal, si los hubo.</p>
        <p>Debajo, las líneas: referencia, producto, cantidad y precio. Una
        línea con la pastilla <em>sin mapear</em> tiene una referencia que no
        existe en el catálogo de Integra, o que comparten dos productos; hay
        que resolverla antes de que el pedido pueda montarse.</p>
      </>
    ),
  },
  {
    titulo: 'Errores y cómo se resuelven',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-reintentar"]',
    texto: (
      <>
        <p>Las causas habituales: una referencia que no está en el catálogo (se
        arregla sincronizando desde el Panel o corrigiendo el SKU), ninguna
        tarifa de Odoo en la moneda del pedido (hay que crearla en Odoo) u Odoo
        caído un rato, que se arregla solo. El montaje se reintenta por su
        cuenta hasta cinco veces; después queda en Falló.</p>
        <p><strong>Reintentar en Odoo</strong> vuelve a emparejar las líneas por
        referencia, pone el contador a cero y devuelve el pedido a la cola. Si
        aún queda una referencia sin mapear, lo dice y no reintenta. No sirve
        para lo que ya está en Odoo ni para lo que el canal canceló.</p>
      </>
    ),
  },
  {
    titulo: 'Despacho: qué avisa al canal',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-despacho"]',
    texto: (
      <>
        <p>Solo aparece con el pedido ya en Odoo. Lo que dispara el aviso al canal
        es <strong>validar el albarán de salida en Odoo</strong>: Integra lo
        comprueba en segundo plano y, en cuanto está validado, avisa al canal
        de que el pedido salió.</p>
        <p>Mientras tanto la pastilla dice «no sabe que salió», con la fecha en
        que salió de bodega si Odoo ya la tiene. Importa porque MercadoLibre y
        Falabella miden el tiempo hasta el despacho: pasado el plazo, cancelan,
        reembolsan y bajan la reputación.</p>
      </>
    ),
  },
  {
    titulo: 'Guía y transportadora a mano',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-guia"]',
    texto: (
      <>
        <p>La guía no llega de Odoo —el Odoo de MDV no tiene módulo de
        transporte—, así que se escribe aquí. Las dos casillas son opcionales:
        en Mercado Envíos y en Falabella la logística la pone el canal y no hay
        guía que mandar.</p>
        <p><strong>Avisar del despacho</strong> guarda lo escrito (no se pierde si
        el aviso falla) y adelanta la comprobación para no esperar al horario.
        Si el albarán aún no está validado, la pantalla lo dice y el aviso
        saldrá cuando lo esté. Al confirmarse, la pastilla pasa a «Canal
        avisado» con la fecha, la guía y la transportadora.</p>
      </>
    ),
  },
  {
    titulo: 'Si el aviso al canal falla',
    seccion: 'pedidos',
    objetivo: '[data-guia="ped-despacho-error"]',
    texto: (
      <>
        <p>Cuando el canal rechaza el aviso, el detalle muestra «Último intento
        falló» con el motivo. Se vuelve a intentar solo, y a partir del tercer
        fallo llega un aviso, porque los dos primeros suelen ser un corte
        pasajero.</p>
        <p>Si el canal responde que ese pedido no admite confirmación —un envío
        que gestiona él mismo, uno ya despachado, uno digital—, se da por
        cerrado y no vuelve a intentarse.</p>
      </>
    ),
  },
  {
    titulo: 'Una venta baja el stock en los demás canales',
    seccion: 'pedidos',
    texto: (
      <>
        <p>Al llegar un pedido nuevo, Integra descuenta las unidades vendidas en
        el acto y manda el stock nuevo a los otros canales enseguida, por
        delante de cualquier otro envío, sin esperar al horario. Así los cuatro
        canales no venden la misma unidad dos veces.</p>
        <p>Si el canal cancela el pedido, las unidades vuelven y se avisa otra
        vez a los demás canales.</p>
      </>
    ),
  },
]
