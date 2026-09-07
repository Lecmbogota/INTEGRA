import type { Paso } from '../GuiaPasos'

// Recorrido detallado de Automatización. Los avisos y lo que hace cada horario
// están contrastados con la vigilancia y el planificador del servidor.

export const PASOS_AUTOMATIZACION: Paso[] = [
  {
    titulo: 'Avisos abiertos',
    seccion: 'automatizacion',
    objetivo: '[data-guia="avisos"]',
    texto: (
      <>
        <p>Integra vigila sola y, cuando algo se rompe sin que nadie esté
        mirando, abre un <strong>aviso</strong> aquí. El número junto a
        <em> Automatización</em> en el menú es cuántos siguen abiertos.
        <em> Nada roto. Todo en orden</em> es lo que quieres ver.</p>
        <p>Un problema que persiste no llena la lista: mientras el aviso siga
        abierto, el mismo problema no vuelve a anotarse.</p>
      </>
    ),
  },
  {
    titulo: 'Qué vigila Integra',
    seccion: 'automatizacion',
    objetivo: '[data-guia="avisos"]',
    texto: (
      <>
        <p>Como <strong>crítico</strong>: una conexión con un canal que no
        funciona, una cuenta sin bodegas asignadas (no publica nada) y un
        proceso de fondo que dejó de dar señales. Como <strong>error</strong>:
        pedidos que no se pudieron crear en Odoo, fichas que el canal rechazó,
        fichas que el propio canal retiró de la venta, trabajos que agotaron
        sus reintentos, precios que no cubren el coste y un horario que falló.</p>
        <p>Como <strong>aviso</strong>: líneas de pedido con una referencia que
        no existe en el catálogo, productos publicados que se quedaron sin
        existencias, fichas cuya referencia cambió en Odoo después de publicar
        y fichas que ya no existen en su canal.</p>
      </>
    ),
  },
  {
    titulo: 'Leer un aviso',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-aviso"]',
    texto: (
      <>
        <p>La etiqueta es la gravedad: <em>Crítico</em> y <em>Error</em> en rojo,
        <em> Aviso</em> en amarillo, <em>Info</em> en gris. Al lado, el mensaje
        con la cifra o el nombre de lo afectado, que es lo que hay que ir a
        resolver.</p>
        <p>Debajo, el canal al que pertenece —si el aviso es de una cuenta
        concreta— y cuándo se abrió. Los más recientes van primero.</p>
      </>
    ),
  },
  {
    titulo: 'Darlo por visto',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-visto"]',
    texto: (
      <>
        <p><strong>Visto</strong> marca el aviso como atendido: desaparece de la
        lista y deja de contar en el menú. No arregla nada por sí mismo; es la
        forma de decir «ya lo sé».</p>
        <p>Si el problema sigue ahí o reaparece, Integra abre un aviso nuevo en
        la siguiente revisión. Si en <em>Avisos</em> hay un correo configurado,
        cada aviso nuevo llega también por correo.</p>
      </>
    ),
  },
  {
    titulo: 'Tareas programadas',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-horarios"]',
    texto: (
      <>
        <p>Los horarios que corren solos, ordenados por hora. Sin ellos, cada
        sincronización y cada publicación dependen de que alguien pulse un
        botón; por eso, si la lista dice <em>Sin horarios</em>, lo primero es
        crear uno.</p>
        <p>Lo habitual es una sola tarea nocturna que sincroniza con Odoo y
        publica lo que cambió, y que trae los pedidos en la misma pasada.</p>
      </>
    ),
  },
  {
    titulo: 'Leer un horario',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-horario"]',
    texto: (
      <>
        <p>Cada fila trae la hora (de Colombia), el nombre y qué hace.
        <strong> Sincronizar Odoo y publicar todo</strong> lee Odoo y después
        envía a todos los canales lo que cambió. <em>Solo precios y stock</em> y
        <em> Solo stock</em> no leen Odoo: revisan el catálogo tal como está y
        envían solo lo que cambió desde el último envío.</p>
        <p>En cualquiera de los tres, la misma pasada trae los pedidos nuevos,
        monta en Odoo los que faltaban, avisa al canal los despachos ya
        validados y contrasta con el canal lo que Integra da por publicado.
        Después van los días (L M X J V S D, o <em>todos los días</em>), <em>solo
        tal canal</em> si la tarea está atada a una cuenta, y <em>próxima</em>:
        cuándo le toca correr.</p>
      </>
    ),
  },
  {
    titulo: 'Pausar, activar y borrar',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-horario-acciones"]',
    texto: (
      <>
        <p><em>Activo</em> o <em>Pausado</em> es el estado. <strong>Pausar</strong>
        deja la tarea en la lista pero no la ejecuta; <strong>Activar</strong> la
        reanuda y recalcula su próxima corrida. Es la forma de parar una
        automatización un tiempo sin perder cómo estaba configurada.</p>
        <p><strong>Borrar</strong> la elimina y pide confirmación: no se deshace y
        no se nota, porque la tarea simplemente deja de correr esa noche. Si
        solo quieres detenerla un tiempo, pausa.</p>
      </>
    ),
  },
  {
    titulo: 'Crear un horario',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-nuevo"]',
    texto: (
      <>
        <p><strong>Nuevo horario</strong> abre el formulario. Pide un nombre (por
        defecto <em>Sincronización nocturna</em>), la hora en formato de 24 horas
        y hora de Colombia (por defecto 02:00), qué hace —las tres opciones del
        paso anterior— y los días de la semana: sin marcar ninguno, corre todos
        los días.</p>
        <p><strong>Crear horario</strong> lo guarda ya activo y calcula su primera
        corrida. Sin nombre no se guarda, para que la lista no tenga una fila en
        blanco. Cancelar o la tecla Esc cierran sin guardar.</p>
      </>
    ),
  },
  {
    titulo: 'Si a esa hora nadie estaba',
    seccion: 'automatizacion',
    objetivo: '[data-guia="aut-nota-proceso"]',
    texto: (
      <>
        <p>Los horarios los dispara el mismo proceso de fondo que ejecuta los
        trabajos, así que tiene que estar arriba en el servidor. Si estuvo
        apagado a la hora de una tarea, <strong>la ejecuta al volver</strong> en
        vez de saltársela. Si una corrida falla, se abre un aviso de error con
        el nombre del horario.</p>
        <p>Ese mismo proceso aplica las <strong>promociones</strong> de los
        productos cuando llega su fecha de inicio y las deshace cuando termina,
        sin necesidad de un horario aparte.</p>
      </>
    ),
  },
]
