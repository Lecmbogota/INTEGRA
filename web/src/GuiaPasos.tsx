import type { ReactNode } from 'react'
import type { Seccion } from './Sidebar'

// Los pasos del recorrido guiado. Viven aparte del componente para que
// cambiar un texto o el orden no toque la mecanica del recorrido.
//
// Cada afirmación está contrastada con la pantalla o con la regla del canal
// que la aplica; si algo cambia allí, hay que cambiarlo aquí. El orden es el
// del trabajo de todos los días: mirar el panel, completar lo que falta,
// poner fotos, mapear categorías, publicar y dejar que lleguen los pedidos.

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
        <p>Integra toma el catálogo de <strong>Odoo</strong>, lo completa con lo
        que piden los marketplaces y lo <strong>publica</strong> en MercadoLibre,
        Falabella, WooCommerce y Shopify. Lo que se vende allí vuelve a Odoo
        como pedido.</p>
        <p>De Odoo llegan solo la <strong>referencia, el nombre y el stock</strong>.
        Precio, marca, descripción, fotos y categorías se completan aquí, y las
        sincronizaciones con Odoo no los tocan.</p>
      </>
    ),
  },
  {
    titulo: 'El panel: cuánto hay listo',
    seccion: 'panel',
    objetivo: '[data-guia="resumen"]',
    texto: (
      <>
        <p>Las tarjetas resumen el catálogo: cuántos productos hay, cuántos
        tienen precio y existencias, cuántos son <strong>publicables hoy</strong> y
        cuántos <strong>requieren atención</strong>.</p>
        <p><strong>Sincronizar ahora</strong> trae de Odoo los productos nuevos y el
        stock actual. También se hace solo con un horario, desde Automatización.</p>
      </>
    ),
  },
  {
    titulo: 'Qué bloquea la publicación',
    seccion: 'panel',
    objetivo: '[data-guia="bloqueos"]',
    texto: (
      <>
        <p>Los motivos por los que un producto no sale, contados: sin precio, sin
        descripción, sin foto, referencia repetida, título largo para
        MercadoLibre, sin marca, sin existencias… Las barras rojas
        <strong> impiden publicar</strong>; las amarillas solo empeoran la ficha.</p>
        <p>Es por donde conviene empezar cada día. Más abajo, <em>Qué publicar
        primero</em> ordena los candidatos por el valor del inventario que espera.</p>
      </>
    ),
  },
  {
    titulo: 'Productos: buscar y filtrar',
    seccion: 'catalogo',
    objetivo: '[data-guia="buscar"]',
    texto: (
      <>
        <p>Todo lo que vino de Odoo, con lo que Integra le añade. Busca por
        nombre o referencia, filtra por marca o categoría y ordena pulsando las
        cabeceras.</p>
        <p>Las casillas contestan las preguntas de todos los días: <strong>Solo con
        problemas</strong>, <strong>Sin precio</strong> y <strong>Pendientes por
        publicar</strong>, que muestra lo que aún no está en ningún canal.</p>
      </>
    ),
  },
  {
    titulo: 'Dónde está y qué le falta',
    seccion: 'catalogo',
    objetivo: '[data-guia="estado"]',
    texto: (
      <>
        <p>La columna <strong>Publicación</strong> dice en qué canales tiene ficha
        cada producto: en verde si está publicada, en rojo si falló el último
        envío. <em>Sin publicar</em> es el estado normal de un producto nuevo.</p>
        <p><strong>Estado</strong> dice si está listo o qué le falta. Deja el ratón
        sobre una etiqueta para leer el detalle.</p>
      </>
    ),
  },
  {
    titulo: 'Editar un producto',
    seccion: 'catalogo',
    objetivo: '[data-guia="editar"]',
    texto: (
      <>
        <p>Aquí se completa lo que Odoo no trae. En <em>Venta</em>:
        <strong> precio</strong>, marca, descripción y código de barras, que
        Falabella exige. En <em>Títulos</em>, un título por canal: MercadoLibre
        admite <strong>60 caracteres</strong>. En <em>Envío</em>, peso y medidas:
        Falabella no publica sin peso, y con las medidas el envío se cobra bien.</p>
        <p>En <em>Promoción</em> se crean ofertas con fecha de inicio y fin. Lo que
        dejes vacío en Títulos se publica con el nombre de Odoo.</p>
      </>
    ),
  },
  {
    titulo: 'Muchos productos a la vez',
    seccion: 'catalogo',
    objetivo: '[data-guia="plantilla"]',
    texto: (
      <>
        <p><strong>Actualizar por plantilla</strong> descarga los productos del filtro
        actual en una hoja de Excel; escribes precios y promociones, la subes y
        Integra te enseña qué va a cambiar antes de guardar nada.</p>
        <p><strong>Editar en masa</strong> aplica una operación a los marcados o al
        filtro entero: precio desde el coste, multiplicar el precio actual, poner
        marca, excluir del catálogo. Siempre simula primero.</p>
      </>
    ),
  },
  {
    titulo: 'Qué se enviará a cada canal',
    seccion: 'catalogo',
    objetivo: '[data-guia="fila"]',
    texto: (
      <>
        <p>Pulsa una fila y verás la <strong>vista previa</strong>: el título, el
        precio, la portada y el stock que saldrían a cada canal, y
        <strong> qué le falta</strong> para poder publicarse allí.</p>
        <p>Casi cada falta trae un enlace que lleva a donde se corrige; las fotos
        se suben en esa misma pantalla. Nada se envía desde la vista previa.</p>
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
        (<code>SKU-1.jpg</code>), cae solo en su producto; el resto queda en
        <em> Huérfanas</em>, listo para <strong>asignarse</strong> a mano.</p>
        <p>Cada foto se puede <strong>editar</strong>: recortar, blanquear el fondo,
        dejarla cuadrada de 1200 px, que es lo que piden los marketplaces. Las
        menores de 600 px las rechaza MercadoLibre y aquí se marcan como pequeñas.</p>
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
        categoría del canal equivale cada categoría de Odoo, una vez por rama;
        todos los productos de esa rama la heredan.</p>
        <p>Para MercadoLibre las filas son sugerencias y <strong>ninguna se usa
        hasta que la confirmes</strong>. Para Falabella todavía no hay forma de
        asignarla, así que ese canal sigue bloqueado por categoría.</p>
      </>
    ),
  },
  {
    titulo: 'Publicación',
    seccion: 'publicacion',
    objetivo: '[data-guia="publicacion"]',
    texto: (
      <>
        <p>El estado de cada canal. <strong>Planificar envíos</strong> compara el
        catálogo con lo ya publicado y envía solo lo que cambió: publica lo que
        esté listo, actualiza precios y manda el stock primero.</p>
        <p>En WooCommerce y Shopify la ficha nueva queda en borrador hasta que
        pulses <strong>Poner a la venta</strong>. <strong>Retirar</strong> la pausa sin
        borrarla y se puede reabrir. Abajo, la lista producto a producto dice
        qué falta por enviar.</p>
      </>
    ),
  },
  {
    titulo: 'Publicar un lote desde la lista',
    seccion: 'catalogo',
    objetivo: '[data-guia="publicar"]',
    texto: (
      <>
        <p>Marca los productos que acabas de arreglar y pulsa
        <strong> Publicar</strong>: salen a los canales donde cumplan los requisitos
        sin esperar a la corrida del catálogo entero. Lo que no cumpla se queda
        con su motivo a la vista.</p>
        <p><strong>Despublicar</strong> (solo administradores) quita la ficha del
        canal, con su historial y sus reseñas; el producto sigue en Integra. Si
        solo quieres parar la venta un tiempo, usa Retirar en Publicación.</p>
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
        impuestos: en Odoo se confirma y se factura como siempre. Con cada venta
        el stock baja y se avisa a los demás canales enseguida.</p>
        <p>Cuando en Odoo se valida la salida de bodega, Integra
        <strong> avisa al canal del despacho</strong>; la guía y la transportadora
        se escriben aquí si las hay. Lo que falló se reintenta con un botón.</p>
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
    seccion: 'automatizacion',
    objetivo: '[data-guia="avisos"]',
    texto: (
      <>
        <p>Integra vigila sola: una conexión de canal vencida, una ficha que el
        canal rechazó o retiró, un pedido que no se pudo crear en Odoo, un
        producto publicado sin stock. Cada problema abre un <strong>aviso</strong>
        aquí y, si hay un correo configurado en <em>Avisos</em>, también llega por
        correo.</p>
        <p>Debajo están los horarios: la corrida nocturna que sincroniza con Odoo,
        publica lo que cambió y trae los pedidos sin que nadie pulse un botón.</p>
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
