import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla Categorías (Mapeos.tsx): qué es el mapeo, de
// dónde salen las sugerencias, qué significa la ⚠ y qué desbloquea
// confirmar.
//
// Cada afirmación está contrastada con la pantalla y con la regla que la
// aplica; si cambia allí, hay que cambiarla aquí.

export const PASOS_CATEGORIAS: Paso[] = [
  {
    titulo: 'Para qué sirve el mapeo',
    seccion: 'categorias',
    objetivo: '[data-guia="mapeos"]',
    texto: (
      <>
        <p>MercadoLibre y Falabella exigen que cada producto vaya en una
        categoría de <em>su propio árbol</em>, y ese árbol no se parece al de
        Odoo. Aquí se dice a qué categoría del canal equivale cada categoría de
        Odoo.</p>
        <p>Se decide una vez por rama: todos los productos de esa rama la
        heredan. Mientras una rama no tenga equivalente confirmado, sus
        productos no salen a ese canal.</p>
      </>
    ),
  },
  {
    titulo: 'Elegir el canal',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-canal"]',
    texto: (
      <>
        <p>Cada canal tiene su árbol y su lista aparte: pulsa
        <strong> MercadoLibre</strong> o <strong>Falabella</strong> para ver
        las equivalencias de ese canal. Lo que se confirma para uno no vale
        para el otro.</p>
        <p>WooCommerce y Shopify no piden categoría, así que no aparecen.</p>
      </>
    ),
  },
  {
    titulo: 'Lo que dice la nota de cada canal',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-nota"]',
    texto: (
      <>
        <p>Para MercadoLibre las filas son <em>sugerencias</em> y ninguna se usa
        hasta que la confirmes. Para Falabella la nota es honesta: Integra
        todavía no tiene forma de proponer la categoría ni una pantalla para
        elegirla a mano.</p>
        <p>Mientras eso no exista, todo producto queda bloqueado para Falabella
        por falta de categoría, y así se ve en la vista previa de cada
        producto.</p>
      </>
    ),
  },
  {
    titulo: 'De dónde salen las sugerencias',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-tabla"]',
    texto: (
      <>
        <p>Las sugerencias de MercadoLibre las produce su propio predictor
        público, consultado con el título del producto con más inventario de
        cada rama. Se generan con una tarea aparte, la que nombra la pantalla
        cuando la lista está vacía; volver a correrla no toca lo ya
        confirmado.</p>
        <p>Cada fila muestra la <em>categoría en Odoo</em> y la <em>categoría en
        el canal</em> propuesta, con su código y su nombre. Las filas vienen
        ordenadas por el valor del inventario que espera.</p>
      </>
    ),
  },
  {
    titulo: 'Productos e inventario',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-inventario"]',
    texto: (
      <>
        <p><em>Productos</em> es cuántos productos activos de Integra cuelgan de
        esa rama de Odoo. <em>Inventario</em> es lo que valen sus existencias al
        coste: el dinero que está parado por esa categoría.</p>
        <p>Por eso conviene empezar por arriba: confirmar la rama con más
        inventario libera más ventas que confirmar diez ramas con un producto
        suelto.</p>
      </>
    ),
  },
  {
    titulo: 'Atributos deducidos',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-atributos"]',
    texto: (
      <>
        <p>Junto con la categoría, el predictor deduce algunos atributos que
        MercadoLibre pide en esa categoría (marca, modelo, tipo…) a partir del
        título de ejemplo.</p>
        <p>Aquí solo se muestran para saber qué entendió. Se revisan producto a
        producto al completar la ficha, no en esta pantalla. En el móvil esta
        columna se oculta.</p>
      </>
    ),
  },
  {
    titulo: 'Las dudosas, marcadas con ⚠',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-dudosas"]',
    texto: (
      <>
        <p>El predictor se engancha a palabras sueltas del título y a veces
        propone disparates. Una fila lleva ⚠ cuando el nombre sugerido no
        comparte ni una palabra con la rama de Odoo: probablemente esté mal.</p>
        <p>La casilla <strong>Solo dudosas</strong> deja a la vista únicamente
        esas, para revisarlas con calma. Confirmar una dudosa pide confirmación
        aparte, porque publicaría toda la rama en la categoría equivocada.</p>
      </>
    ),
  },
  {
    titulo: 'Confirmar y qué desbloquea',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-confirmar"]',
    texto: (
      <>
        <p><strong>Confirmar</strong> convierte la sugerencia en la categoría
        con la que se publica: la fila pasa a <em>Confirmada</em> y los
        productos de la rama dejan de estar bloqueados por categoría en
        MercadoLibre (si cumplen lo demás: precio, foto, descripción).</p>
        <p>No se deshace desde esta pantalla, así que en las dudosas vale la
        pena mirar dos veces antes de pulsar.</p>
      </>
    ),
  },
  {
    titulo: 'Cuánto va',
    seccion: 'categorias',
    objetivo: '[data-guia="catg-resumen"]',
    texto: (
      <>
        <p>El contador dice cuántas ramas están confirmadas de las que hay en la
        lista y cuánto inventario, al coste, quedó <em>desbloqueado</em> con
        ellas.</p>
        <p>Cuando todas estén confirmadas, ningún producto de la lista queda
        bloqueado por categoría en este canal; lo que siga sin salir tendrá otro
        motivo, y la vista previa del producto lo dice.</p>
      </>
    ),
  },
]
