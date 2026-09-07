import type { Paso } from '../GuiaPasos'

// Recorrido detallado de Actividad. Todo lo que dice sale de Actividad.tsx y
// de cómo el servidor resume la cola y la línea de tiempo.

export const PASOS_ACTIVIDAD: Paso[] = [
  {
    titulo: 'Actividad: qué está pasando',
    seccion: 'actividad',
    objetivo: '[data-guia="act-actualizar"]',
    texto: (
      <>
        <p>Todo lo que pides —publicar, enviar precios, traer pedidos— se hace
        en segundo plano. Esta pantalla enseña qué está en marcha ahora y qué
        pasó antes. El número junto a <em>Actividad</em> en el menú es lo que
        corre en este momento.</p>
        <p>Mientras haya algo en marcha, la pantalla se refresca sola cada
        cinco segundos. <strong>Actualizar</strong> la vuelve a leer al instante,
        útil cuando la cola está vacía y acabas de pedir algo.</p>
      </>
    ),
  },
  {
    titulo: 'En marcha',
    seccion: 'actividad',
    objetivo: '[data-guia="actividad"]',
    texto: (
      <>
        <p>Una fila por tipo de trabajo: <em>Publicar fichas</em>, <em>Enviar
        precios</em>, <em>Enviar stock</em>, <em>Retirar de la venta</em>,
        <em> Poner a la venta</em>, <em>Contrastar con el canal</em>,
        <em> Confirmar feeds de Falabella</em>, <em>Traer pedidos</em>,
        <em> Montar pedidos en Odoo</em> y <em>Avisar despachos al canal</em>.
        Solo aparecen los tipos que tienen algo corriendo, en cola o fallado.</p>
        <p><em>Nada en cola</em> significa que todo lo pedido ya se envió. La
        línea <em>Hoy además:</em> resume lo que ya terminó sin nada pendiente,
        para que no se pierda pero tampoco estorbe.</p>
      </>
    ),
  },
  {
    titulo: 'Leer una tarea',
    seccion: 'actividad',
    objetivo: '[data-guia="act-tarea"]',
    texto: (
      <>
        <p>Las etiquetas dicen cuántos trabajos hay en cada estado: en verde
        <strong> corriendo</strong>, en amarillo <strong>en cola</strong>
        (esperando turno) y en rojo <strong>fallidos</strong>, que agotaron sus
        intentos. La barra avanza según lo completado hoy frente a lo que
        queda; con el ratón encima dice «tantos de tantos hoy».</p>
        <p>Debajo: cuántos se completaron hoy (últimas 24 horas) y desde cuándo
        espera el más antiguo, que es lo que distingue «va lento» de «lleva
        parado desde ayer». Si hay fallidos, <em>Último fallo</em> muestra el
        motivo del más reciente sin tener que ir a buscarlo.</p>
      </>
    ),
  },
  {
    titulo: 'Cancelar y reintentar',
    seccion: 'actividad',
    objetivo: '[data-guia="act-acciones"]',
    texto: (
      <>
        <p><strong>Cancelar N</strong> retira de la cola lo que aún no ha
        empezado; pide confirmación. Lo que ya se está enviando al canal no se
        detiene, porque a medias dejaría el canal y Integra sin coincidir.</p>
        <p><strong>Reintentar N</strong> devuelve a la cola lo que falló, con los
        intentos a cero. Conviene pulsarlo después de arreglar la causa que
        muestra <em>Último fallo</em>; si el mismo trabajo ya volvió a la cola
        por otro camino, no se duplica.</p>
      </>
    ),
  },
  {
    titulo: 'Si lo encolado no avanza',
    seccion: 'actividad',
    objetivo: '[data-guia="act-nota-proceso"]',
    texto: (
      <>
        <p>Los trabajos los ejecuta un proceso aparte de la pantalla. Si ves
        trabajos <em>en cola</em> que no cambian de estado durante un buen rato,
        ese proceso está apagado: hay que arrancarlo en el servidor con el
        comando que indica la nota, o pedírselo a quien lo administra.</p>
        <p>Integra lo vigila: cuando lleva más de tres minutos sin dar señales
        abre un aviso crítico en <strong>Automatización</strong> y, si en
        <em> Avisos</em> hay un correo configurado, también lo manda por correo.</p>
      </>
    ),
  },
  {
    titulo: 'Lo que ha pasado',
    seccion: 'actividad',
    objetivo: '[data-guia="act-historia"]',
    texto: (
      <>
        <p>La línea de tiempo, de lo más reciente a lo más antiguo, mezcla tres
        cosas: lo que hizo <strong>alguien</strong> desde la interfaz, lo que
        <strong> falló</strong> en segundo plano y los <strong>avisos</strong> que
        se abrieron. Muestra las últimas sesenta entradas.</p>
        <p><em>Todavía no hay nada anotado</em> es lo normal en una instalación
        recién estrenada: en cuanto alguien edite un producto o algo falle,
        aparece aquí.</p>
      </>
    ),
  },
  {
    titulo: 'Leer una línea',
    seccion: 'actividad',
    objetivo: '[data-guia="act-evento"]',
    texto: (
      <>
        <p>La etiqueta dice el origen: <em>Alguien</em> es una acción de una
        persona; <em>Falló</em> es un trabajo que agotó sus intentos y va en
        rojo; <em>Aviso</em> va en rojo si es grave y en amarillo si es una
        advertencia. El texto principal es el tipo de trabajo o la acción y
        sobre qué se hizo; debajo, el motivo del fallo o el mensaje del aviso.</p>
        <p>A la derecha, quién lo hizo (solo en las acciones de personas) y
        cuándo. Los avisos que ves aquí son los mismos que se atienden en
        <strong> Automatización</strong>.</p>
      </>
    ),
  },
  {
    titulo: 'Lo que no aparece',
    seccion: 'actividad',
    objetivo: '[data-guia="act-nota-historia"]',
    texto: (
      <>
        <p>Los envíos que salieron bien <strong>no</strong> se listan uno a uno:
        una publicación de catálogo son cientos de líneas idénticas que
        enterrarían lo único que hay que leer. Su rastro está en la línea
        <em> Hoy además</em> y en las cifras de completados de cada tarea.</p>
        <p>Para saber qué quedó publicado y dónde, mira <strong>Publicación</strong>
        o la columna <em>Publicación</em> de la lista de Productos.</p>
      </>
    ),
  },
]
