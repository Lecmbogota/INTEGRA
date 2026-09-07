import type { ReactNode } from 'react'
import type { Seccion } from './Sidebar'

// Los pasos del recorrido guiado. Viven aparte del componente para que
// cambiar un texto o el orden no toque la mecanica del recorrido.

export type Paso = {
  titulo: string
  texto: ReactNode
  // A qué sección hay que ir antes de enseñar el paso.
  seccion?: Seccion
  // Selector del elemento que se resalta. Sin él, el paso va centrado.
  objetivo?: string
}

export const PASOS: Paso[] = [
  {
    titulo: 'Bienvenido a Integra',
    texto: (
      <>
        <p>Integra toma tu catálogo de <strong>Odoo</strong>, lo completa con lo que
        los marketplaces exigen y lo <strong>publica</strong> en MercadoLibre,
        Falabella, WooCommerce y Shopify. Los <strong>pedidos</strong> de esas
        tiendas vuelven a Odoo solos.</p>
        <p>De Odoo llegan solo la <strong>referencia, el nombre y el stock</strong>.
        Precio, marca, descripción, fotos y categorías se manejan aquí, y Odoo
        nunca los pisa.</p>
        <p>Este recorrido dura dos minutos y sigue el orden en que se trabaja.</p>
      </>
    ),
  },
  {
    titulo: 'El panel: qué hay que atender',
    seccion: 'panel',
    objetivo: '[data-guia="resumen"]',
    texto: (
      <>
        <p>Cuántos productos tienes, cuántos están listos y cuántos
        <strong> requieren atención</strong>: sin precio, sin foto, sin descripción,
        con el título demasiado largo para un canal…</p>
        <p>Debajo está la lista de motivos. Es por donde conviene empezar cada día.</p>
      </>
    ),
  },
  {
    titulo: 'Productos: tu catálogo',
    seccion: 'catalogo',
    objetivo: '[data-guia="buscar"]',
    texto: (
      <>
        <p>Todo lo que vino de Odoo, con lo que Integra le añade. Busca por
        referencia o nombre, filtra por marca o categoría, y ordena pulsando las
        cabeceras.</p>
        <p>La columna <strong>Publicación</strong> dice en qué canales está cada
        producto; <strong>Estado</strong>, si le falta algo para publicarse.</p>
      </>
    ),
  },
  {
    titulo: 'Pendientes por publicar',
    seccion: 'catalogo',
    objetivo: '[data-guia="pendientes"]',
    texto: (
      <>
        <p>Este filtro contesta la pregunta de todos los días: <strong>¿qué me
        falta por subir?</strong> Muestra solo lo que aún no está en ningún canal.</p>
        <p>Hay más: solo con problemas, sin precio, y desde la plantilla, sin foto,
        sin descripción o sin código de barras.</p>
      </>
    ),
  },
  {
    titulo: 'Editar un producto',
    seccion: 'catalogo',
    objetivo: '[data-guia="editar"]',
    texto: (
      <>
        <p>Aquí se completa lo que Odoo no trae: <strong>precio</strong>, marca y
        descripción en <em>Venta</em>; un <strong>título por canal</strong> en
        <em> Títulos</em> (MercadoLibre corta a 60 caracteres); <strong>peso y
        medidas</strong> en <em>Envío</em>, que Falabella exige para calcular el
        envío.</p>
        <p>Para muchos productos a la vez: <strong>Actualizar por plantilla</strong>
        (una hoja de cálculo) o <strong>Editar en masa</strong>.</p>
      </>
    ),
  },
  {
    titulo: 'Qué se enviará a cada canal',
    seccion: 'catalogo',
    objetivo: '[data-guia="fila"]',
    texto: (
      <>
        <p>Pulsa una fila y verás la <strong>vista previa</strong>: cómo quedaría
        el producto en cada canal con el título, el precio y la portada reales, y
        <strong> qué le falta</strong> para poder publicarse allí.</p>
        <p>Cada falta trae un enlace que te lleva justo a donde se corrige.</p>
      </>
    ),
  },
  {
    titulo: 'Imágenes',
    seccion: 'mediateca',
    objetivo: '[data-guia="subir"]',
    texto: (
      <>
        <p>Sin foto no se publica. Las fotos se suben desde cada producto o
        <strong> en masa aquí</strong>: si el archivo se llama como la referencia
        (<code>SKU-1.jpg</code>), cae solo en su producto.</p>
        <p>Cada foto se puede <strong>editar</strong> (recortar, quitar el fondo,
        dejarla cuadrada de 1200 px, que es lo que mejor se ve) y
        <strong> asignar</strong> a uno o varios productos. Abajo, el banco entero
        con filtros: pequeñas, huérfanas, duplicadas.</p>
      </>
    ),
  },
  {
    titulo: 'Categorías',
    seccion: 'categorias',
    objetivo: '[data-guia="mapeos"]',
    texto: (
      <>
        <p>MercadoLibre y Falabella exigen que cada producto vaya en
        <strong> una categoría de su propio árbol</strong>. Aquí se dice a qué
        categoría del canal equivale cada categoría de Odoo, una vez por rama:
        todos los productos de esa rama la heredan.</p>
        <p>Las filas son sugerencias; <strong>ninguna se usa hasta que la
        confirmes</strong>.</p>
      </>
    ),
  },
  {
    titulo: 'Publicación',
    seccion: 'publicacion',
    objetivo: '[data-guia="publicacion"]',
    texto: (
      <>
        <p>El estado de cada canal: cuántas fichas están vivas, cuántas
        pendientes de enviar cambios, cuántas pausadas. Desde aquí se
        <strong> publica todo lo que esté listo</strong>, se envían precios y stock,
        y se <strong>retira de la venta</strong> o se reanuda.</p>
        <p>Retirar deja la ficha pausada y se puede reabrir; despublicar la quita
        del canal. El producto nunca se borra de Integra: viene de Odoo.</p>
      </>
    ),
  },
  {
    titulo: 'Publicar un lote desde la lista',
    seccion: 'catalogo',
    objetivo: '[data-guia="publicar"]',
    texto: (
      <>
        <p>Marca los productos que quieras y pulsa <strong>Publicar</strong>: van a
        la cola y salen a los canales donde cumplan los requisitos. Lo que no
        cumpla se queda con su motivo a la vista, no se envía a fallar.</p>
        <p>Con los mismos marcados puedes <strong>despublicar</strong> de uno, varios
        o todos los canales.</p>
      </>
    ),
  },
  {
    titulo: 'Pedidos',
    seccion: 'pedidos',
    objetivo: '[data-guia="pedidos"]',
    texto: (
      <>
        <p>Lo que vendes en los canales llega aquí y se crea en Odoo como
        <strong> pedido en borrador con el comprador real</strong>, sin tocar los
        impuestos: en Odoo se confirma y se factura como siempre.</p>
        <p>Cuando en Odoo se valida el envío, Integra avisa al canal con la guía
        y la transportadora.</p>
      </>
    ),
  },
  {
    titulo: 'Actividad: qué está pasando',
    seccion: 'actividad',
    objetivo: '[data-guia="actividad"]',
    texto: (
      <>
        <p>Todo lo que pides —publicar, enviar precios, traer pedidos— se hace en
        segundo plano. Aquí ves <strong>qué está en marcha</strong>, con su avance,
        y puedes cancelar lo que aún no empezó o reintentar lo que falló.</p>
        <p>Debajo, la línea de tiempo: quién hizo qué, qué falló y qué avisos se
        abrieron. El número junto a <em>Actividad</em> en el menú es lo que corre
        ahora mismo.</p>
      </>
    ),
  },
  {
    titulo: 'Cuando algo falla',
    objetivo: '[data-guia="menu-automatizacion"]',
    texto: (
      <>
        <p>Integra vigila sola: Odoo que no responde, un canal que rechaza una
        ficha, pedidos que no se pudieron crear. Cada problema abre un
        <strong> aviso</strong>, que se ve en <em>Automatización</em> y llega por
        correo a quien esté configurado.</p>
        <p>No hace falta estar mirando la pantalla para enterarse.</p>
      </>
    ),
  },
  {
    titulo: 'Eso es todo',
    objetivo: '[data-guia="ver-guia"]',
    texto: (
      <>
        <p>El orden de trabajo: <strong>completar</strong> lo que falta en
        Productos e Imágenes, <strong>mapear</strong> categorías,
        <strong> publicar</strong>, y dejar que los <strong>pedidos</strong>
        lleguen solos a Odoo.</p>
        <p>Puedes repetir este recorrido cuando quieras desde <strong>Ver guía</strong>,
        aquí abajo en el menú.</p>
      </>
    ),
  },
]
