import type { Paso } from '../GuiaPasos'

// Recorrido de la sección Atributos (Atributos.tsx): qué exigen los
// marketplaces por categoría, de dónde salen los valores y cómo se completan.
//
// Lo que se afirma sale de internal/atributos (deducción y refresco) y de
// internal/store/atributos.go (resumen y lo que bloquea la publicación).

export const PASOS_ATRIBUTOS: Paso[] = [
  {
    titulo: 'Qué son los atributos',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-cabecera"]',
    texto: (
      <>
        <p>MercadoLibre y Falabella piden, para cada categoría de su árbol, una
        lista de datos de ficha: marca, modelo, color, capacidad, tamaño…
        Algunos son <strong>obligatorios</strong>: si falta uno, el canal
        <strong> rechaza la publicación entera</strong>.</p>
        <p>Esta sección resume cómo va el catálogo con eso. Los valores de cada
        producto se ven y se completan desde su vista previa, en
        <em> Atributos del canal</em>.</p>
      </>
    ),
  },
  {
    titulo: 'El resumen de cada marketplace',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-tarjetas"]',
    texto: (
      <>
        <p>Por canal, tres cifras. <strong>Con categoría</strong>: productos con
        una categoría del canal ya confirmada; solo a esos se les puede pedir
        atributos. <strong>Completos</strong>: los que tienen valor en todos los
        obligatorios, listos para publicar, con su porcentaje.
        <strong> Incompletos</strong>: los que aún tienen algún obligatorio vacío.</p>
        <p>Si un canal dice <em>Sin categorías mapeadas</em>, no hay nada que
        completar todavía: primero se confirman las categorías en
        <strong> Categorías</strong>.</p>
      </>
    ),
  },
  {
    titulo: 'Por qué solo dos canales',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-nota-tiendas"]',
    texto: (
      <>
        <p>Shopify y WooCommerce no aparecen porque son <strong>tiendas
        propias</strong>, no marketplaces: no exigen ningún atributo para
        publicar.</p>
        <p>Lo que allí se ve como «especificaciones» sale del texto del propio
        producto (las que Integra extrae del nombre), así que no hay nada que
        traer ni que completar.</p>
      </>
    ),
  },
  {
    titulo: 'De dónde salen los valores',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-nota-deduccion"]',
    texto: (
      <>
        <p>Integra deduce cada valor de lo que ya sabe del producto: la marca
        del catálogo para «Marca», la referencia para «Modelo», y las
        especificaciones sacadas del nombre (color, capacidad, tecnología…)
        para el resto. Cuando el canal tiene una lista de valores, se busca la
        equivalencia exacta.</p>
        <p><strong>Nunca se inventa</strong>: si un atributo no se puede deducir, o
        el canal solo admite valores de su lista y el nuestro no está, queda
        vacío para que lo escribas tú. Un dato plausible pero falso en una ficha
        es peor que un hueco.</p>
      </>
    ),
  },
  {
    titulo: 'Cómo se completan',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-nota-deduccion"]',
    texto: (
      <>
        <p>Abre el producto desde <strong>Productos</strong>: en su vista previa,
        el bloque <em>Atributos del canal</em> lista los obligatorios (con
        asterisco) y los que ya tienen valor, con su origen: deducido del
        nombre, sugerido, a mano o por defecto. Escribes el valor y pulsas
        <em> Guardar</em>; los opcionales se añaden desde el buscador.</p>
        <p>Lo escrito a mano <strong>se respeta siempre</strong>: volver a deducir
        no lo pisa. Y al publicar se leen los valores en ese momento, así que
        una corrección de hace un minuto ya viaja.</p>
      </>
    ),
  },
  {
    titulo: 'Refrescar los requisitos',
    seccion: 'atributos',
    objetivo: '[data-guia="attr-nota-refrescar"]',
    texto: (
      <>
        <p>Los requisitos de cada categoría se traen del canal con el comando
        <code> integra atributos</code>, que además vuelve a deducir los valores
        de todos los productos con categoría confirmada.</p>
        <p>Conviene ejecutarlo después de confirmar categorías nuevas en
        <em> Categorías</em>. Para MercadoLibre no hace falta cuenta conectada;
        para Falabella sí.</p>
      </>
    ),
  },
]
