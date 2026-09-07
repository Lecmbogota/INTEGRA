import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla Canales (Cuentas.tsx + Canales.tsx): la cuenta
// de cada canal, sus credenciales, las bodegas que la alimentan, el suelo de
// coste y las comisiones que compensa el precio publicado.
//
// Cada afirmación está contrastada con la pantalla y con la regla que la
// aplica; si cambia allí, hay que cambiarla aquí.

export const PASOS_CANALES: Paso[] = [
  {
    titulo: 'Una cuenta por canal',
    seccion: 'canales',
    objetivo: '[data-guia="menu-canales"]',
    texto: (
      <>
        <p>Integra publica en MercadoLibre, Falabella Seller Center, WooCommerce y
        Shopify. Cada uno tiene aquí una fila con su cuenta: si está conectada,
        si la última prueba funcionó y desde qué bodegas publica.</p>
        <p>Abajo están las comisiones de cada canal, que el precio publicado
        compensa para que el margen no se regale.</p>
      </>
    ),
  },
  {
    titulo: 'El estado de la cuenta',
    seccion: 'canales',
    objetivo: '[data-guia="can-conexion"]',
    texto: (
      <>
        <p>La línea bajo el nombre resume la cuenta: <em>sin conectar</em>,
        <em> credenciales guardadas, sin probar</em>, una marca verde con la fecha
        de la última prueba buena, o el mensaje con el que falló.</p>
        <p>Las pastillas de la derecha lo repiten de un vistazo:
        <em> Conectado</em>, <em>Falla</em> o <em>Sin bodegas</em>. Cualquiera de
        las dos últimas significa que ese canal no está publicando.</p>
      </>
    ),
  },
  {
    titulo: 'Conectar o reemplazar credenciales',
    seccion: 'canales',
    objetivo: '[data-guia="can-conectar"]',
    texto: (
      <>
        <p><strong>Conectar</strong> abre la hoja con los datos que pide ese canal
        (los da el propio canal en su portal de desarrolladores o de vendedor;
        cada campo trae una pista de dónde sacarlo). Lo marcado como
        <em> secreto</em> se guarda cifrado y no vuelve a mostrarse.</p>
        <p><strong>Guardar y probar</strong> guarda y prueba enseguida. En una
        cuenta ya conectada el botón dice <em>Reemplazar</em>: es la forma de
        renovar credenciales vencidas, y borra las anteriores sin vuelta atrás.</p>
      </>
    ),
  },
  {
    titulo: 'Probar la cuenta',
    seccion: 'canales',
    objetivo: '[data-guia="can-probar"]',
    texto: (
      <>
        <p><strong>Probar</strong> hace la llamada mínima al canal que demuestra
        que las credenciales sirven, y deja anotado el resultado con su fecha en
        la misma fila.</p>
        <p>Es lo primero que hay que pulsar cuando llega un aviso de credencial
        vencida o una publicación falla. En MercadoLibre la credencial se renueva
        sola con cada uso; si aun así falla, toca reemplazarla.</p>
      </>
    ),
  },
  {
    titulo: 'Bodegas: de dónde sale el stock',
    seccion: 'canales',
    objetivo: '[data-guia="can-bodegas"]',
    texto: (
      <>
        <p><strong>Bodegas</strong> abre la lista de bodegas de Odoo con sus
        unidades; marcas las que alimentan a este canal y el canal publica la
        <em> suma</em> de su stock. <strong>Sin bodegas asignadas no se publica
        nada</strong>, y hay que dejar al menos una marcada.</p>
        <p>El cambio sale en la siguiente planificación de envíos. Si la lista
        está vacía, todavía no se ha sincronizado con Odoo. Lo vendido en un canal
        y aún sin despachar se aparta solo del stock que ven los demás.</p>
      </>
    ),
  },
  {
    titulo: 'Margen mínimo sobre el coste',
    seccion: 'canales',
    objetivo: '[data-guia="can-margen"]',
    texto: (
      <>
        <p>El porcentaje es el margen mínimo que esta cuenta exige sobre el coste
        del producto. Por debajo, el producto entra en la cola de atención del
        Panel y se abre un aviso.</p>
        <p>Con 0 % la regla es «que al menos cubra el coste». Cada canal tiene su
        propio suelo, porque cada uno cobra distinto.</p>
      </>
    ),
  },
  {
    titulo: 'No publicar por debajo del coste',
    seccion: 'canales',
    objetivo: '[data-guia="can-bloquear"]',
    texto: (
      <>
        <p>Con la casilla <strong>No publicar</strong> marcada, un producto que no
        llega al margen se frena: no sale a este canal hasta que se corrija el
        precio. Sin marcarla, se publica igual y solo queda el aviso.</p>
        <p><strong>Guardar</strong> se activa cuando cambias algo; el valor
        guardado es el que rige en la próxima planificación.</p>
      </>
    ),
  },
  {
    titulo: 'Comisiones por canal',
    seccion: 'canales',
    objetivo: '[data-guia="can-comisiones"]',
    texto: (
      <>
        <p>Cada canal cobra una comisión por venta, y algunos un costo fijo. El
        precio que se publica en cada canal se calcula para que, después de
        descontar eso, quede tu precio de Integra.</p>
        <p>Por eso el mismo producto puede verse con precios distintos en cada
        canal sin que hayas puesto ninguno a mano.</p>
      </>
    ),
  },
  {
    titulo: 'Comisión, costo fijo y ejemplo',
    seccion: 'canales',
    objetivo: '[data-guia="can-ejemplo"]',
    texto: (
      <>
        <p>Escribe la <em>Comisión %</em> (entre 0 y 99,99) y el <em>Costo fijo</em>
        de cada canal. La columna <em>$100.000 →</em> muestra en vivo a cuánto
        saldría un producto de cien mil pesos, redondeado a la centena.</p>
        <p>Al cambiar algo aparecen <strong>Guardar</strong> y
        <strong> Descartar</strong> en esa fila. Al guardar se recalculan los
        precios de ese canal para todo el catálogo; llegan a las fichas con la
        siguiente planificación de envíos.</p>
      </>
    ),
  },
]
