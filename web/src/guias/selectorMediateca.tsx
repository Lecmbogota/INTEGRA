import type { Paso } from '../GuiaPasos'

// Recorrido del selector «Elegir de la mediateca»: fotos que ya están en
// el banco, para un producto, sin volver a subirlas.

export const PASOS_SELECTOR: Paso[] = [
  {
    titulo: 'Fotos que ya están en el banco',
    objetivo: '[data-guia="sel-filtros"]',
    texto: (
      <>
        <p>Aquí eliges fotos que Integra ya tiene, sin volver a subirlas: la
        del fabricante que usa otra variante, o la buena que una búsqueda
        dejó sin producto.</p>
        <p><strong>Todas</strong> es el banco entero; <strong>Aptas</strong>,
        las de 600 px o más de lado menor, que valen en los cuatro canales;
        <strong> Huérfanas</strong>, las que no usa ningún producto;
        <strong> Duplicadas</strong>, las que tienen otra igual en peso y
        medidas.</p>
      </>
    ),
  },
  {
    titulo: 'Buscar por el producto que las usa',
    objetivo: '[data-guia="sel-buscar"]',
    texto: (
      <p>Escribe la referencia o parte del nombre de un producto y quedan las
      fotos que ese producto usa: es la forma de traer las de una variante
      hermana. Se muestran como mucho 80, las más recientes primero; si hay
      más, abajo lo dice y conviene afinar.</p>
    ),
  },
  {
    titulo: 'Marcar las que quieres',
    objetivo: '[data-guia="sel-celda"]',
    texto: (
      <>
        <p>Pulsa una foto para marcarla o desmarcarla. Debajo, sus medidas y
        quién la usa: la primera referencia y cuántos productos más,
        <strong> huérfana</strong> si nadie, o <strong>ya está</strong> si este
        producto ya la tiene (esas no se pueden marcar).</p>
        <p>La pastilla <strong>pequeña</strong> avisa de que baja de 600 px y
        MercadoLibre la rechazará.</p>
      </>
    ),
  },
  {
    titulo: 'Añadir al producto',
    objetivo: '[data-guia="sel-anadir"]',
    texto: (
      <p>El pie cuenta las marcadas. <strong>Añadir al producto</strong> las
      enlaza de una vez, detrás de las fotos que el producto ya tenga; si no
      tenía ninguna, la primera queda como portada. Portada y orden se
      cambian luego desde el producto. Cancelar, pulsar fuera o Escape
      cierran sin cambios.</p>
    ),
  },
]
