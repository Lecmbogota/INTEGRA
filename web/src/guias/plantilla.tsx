import type { Paso } from '../GuiaPasos'

// Recorrido del diálogo «Actualización masiva por plantilla». Las columnas y
// las reglas del archivo son las que Integra escribe y revisa; si cambian,
// hay que cambiarlas aquí. El paso 3 del diálogo solo aparece tras subir un
// archivo, así que se explica sin resaltar nada.

export const PASOS_PLANTILLA: Paso[] = [
  {
    titulo: 'Precios y promociones desde Excel',
    objetivo: '[data-guia="plan-cabecera"]',
    texto: (
      <>
        <p>Tres pasos: <strong>descargar</strong> una hoja con los productos,
        <strong> subirla</strong> editada para ver qué va a cambiar y
        <strong> confirmar</strong>. Hasta el tercero no se guarda nada.</p>
        <p>Sirve para precios y promociones. La marca, la descripción o las
        medidas se cambian desde la ficha o con <em>Editar en masa</em>.</p>
      </>
    ),
  },
  {
    titulo: 'Paso 1: qué trae la plantilla',
    objetivo: '[data-guia="plan-paso1"]',
    texto: (
      <>
        <p>Una fila por producto del filtro actual; el número que ves aquí es
        el que vas a descargar. Las columnas grises son de referencia:
        <strong> SKU</strong>, Producto, Marca, Stock y Precio actual.</p>
        <p>Las de <strong>encabezado verde</strong> son las que se editan:
        <strong> Precio nuevo</strong>, <strong>Canal promoción</strong>,
        <strong> Precio promoción</strong>, <strong>Promoción empieza</strong> y
        <strong> Promoción termina</strong>. Una hoja <em>Instrucciones</em> dentro
        del archivo repite estas reglas.</p>
      </>
    ),
  },
  {
    titulo: 'Acotar por marca',
    objetivo: '[data-guia="plan-marcas"]',
    texto: (
      <>
        <p>Cada botón es una marca con cuántos productos tiene. Púlsala para
        descargar solo esa marca; púlsala otra vez para quitarla.</p>
        <p>Estos filtros son los mismos de la lista de detrás: lo que acotes
        aquí se ve también allí, y aparece como etiqueta con una <strong>✕</strong>
        para quitarlo, junto a <em>Quitar todos</em>.</p>
      </>
    ),
  },
  {
    titulo: 'Acotar por categoría',
    objetivo: '[data-guia="plan-categorias"]',
    texto: (
      <>
        <p>Igual que la marca, por categoría de Odoo. Se enseñan las ocho
        primeras; <strong>ver las N</strong> despliega el resto. Pasa el ratón por
        un botón para leer la ruta completa de la categoría.</p>
      </>
    ),
  },
  {
    titulo: 'Acotar por lo que le falta',
    objetivo: '[data-guia="plan-falta"]',
    texto: (
      <>
        <p><strong>sin precio</strong>: sin precio de venta. <strong>sin fotos</strong> y
        <strong> sin descripción</strong>: sin ellas no publica ningún canal.
        <strong> sin EAN</strong>: solo lo exige Falabella, pero es lo que más
        falta. <strong>sin publicar</strong>: aún en ningún canal.
        <strong> con promoción</strong>: tienen una oferta vigente ahora mismo.
        <strong> con problemas</strong>: algo les impide publicarse.</p>
        <p>Se pueden combinar varios a la vez.</p>
      </>
    ),
  },
  {
    titulo: 'Descargar la plantilla',
    objetivo: '[data-guia="plan-descargar"]',
    texto: (
      <>
        <p>Genera el archivo <strong>.xlsx</strong> con el filtro que tengas puesto
        y lo descarga. Ábrelo en Excel y edita solo las columnas verdes.</p>
        <p><strong>No cambies ni borres la columna SKU</strong>: es lo que enlaza
        cada fila con su producto. Una casilla verde vacía significa «no
        tocar»; la mayoría del archivo quedará así.</p>
      </>
    ),
  },
  {
    titulo: 'Cómo se escribe una promoción',
    texto: (
      <>
        <p>Hacen falta <strong>Canal promoción</strong> (mercadolibre, falabella,
        woocommerce o shopify, con una cuenta conectada) y <strong>Precio
        promoción</strong>, que debe ser menor que el precio de venta que quede
        tras la carga. Media promoción es un error: no se aplica nada de esa
        fila.</p>
        <p>Fechas en formato <strong>dd/mm/aaaa hh:mm</strong>. <em>Empieza</em> vacío:
        arranca al cargar. <em>Termina</em> vacío: sin fecha de fin, hasta que la
        canceles desde la ficha.</p>
      </>
    ),
  },
  {
    titulo: 'Paso 2: subir el archivo',
    objetivo: '[data-guia="plan-elegir"]',
    texto: (
      <>
        <p><strong>Elegir archivo…</strong> sube la hoja editada. Integra la revisa
        entera y enseña qué va a cambiar; todavía no se guarda nada.</p>
        <p>Si corriges el archivo en Excel, <em>Cambiar archivo</em> lo vuelve a
        subir, aunque tenga el mismo nombre. Las columnas se reconocen por su
        encabezado, así que reordenarlas o añadir una tuya no rompe la carga.</p>
      </>
    ),
  },
  {
    titulo: 'Paso 3: revisar',
    texto: (
      <>
        <p>Tras subir aparece el resumen: <strong>filas con cambios</strong>,
        <strong> precios</strong>, <strong>promociones</strong> y
        <strong> errores</strong>.</p>
        <p>Con errores no se aplica nada: la tabla los lista con el número de
        fila tal como lo ves en Excel y qué pasa (un SKU que no existe, un
        precio con letras, una promoción sin canal, un canal sin cuenta…).
        Corrige y vuelve a subir. Sin errores, la tabla enseña cada cambio
        con el precio anterior tachado y el nuevo, y la promoción con su
        canal y su vigencia.</p>
      </>
    ),
  },
  {
    titulo: 'Confirmar',
    objetivo: '[data-guia="plan-pie"]',
    texto: (
      <>
        <p>Cuando la revisión no tiene errores aparece aquí
        <strong> Aplicar N cambios</strong>. Pide confirmación con cuántos precios y
        promociones va a guardar; <strong>no se puede deshacer</strong>.</p>
        <p>Al terminar, el paso 3 dice <em>Aplicado</em> con las cifras. Las
        promociones quedan programadas y Integra las lleva al canal cuando
        llegue su hora. Cerrar antes de aplicar descarta la revisión y habrá
        que volver a subir el archivo.</p>
      </>
    ),
  },
]
