import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla Avisos (Avisos.tsx): a dónde salen por correo los
// avisos que Integra levanta, con qué severidad mínima, y cómo se relaciona
// con la lista de avisos de Automatización.
//
// Cada afirmación está contrastada con la pantalla y con el envío real; si
// cambia allí, hay que cambiarla aquí.

export const PASOS_AVISOS: Paso[] = [
  {
    titulo: 'A dónde sale lo que Integra tiene que contar',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-destinos"]',
    texto: (
      <>
        <p>Integra vigila sola: una credencial de canal vencida, un canal caído,
        un pedido que no se pudo crear en Odoo, un producto publicado sin stock
        o por debajo del coste. Cada problema abre un aviso en
        <em> Automatización</em>.</p>
        <p>Esta pantalla decide si, además, ese aviso llega por correo y a
        quién. Cada fila es un <em>destino</em>: un grupo de direcciones con su
        propio servidor de correo y su propio umbral.</p>
      </>
    ),
  },
  {
    titulo: 'Mientras no haya destino',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-alerta"]',
    texto: (
      <>
        <p>Sin un destino activo, los avisos se guardan y esperan a que alguien
        entre a mirarlos: nadie se entera de una credencial vencida hasta que
        una publicación falla.</p>
        <p>La franja roja de arriba lo recuerda mientras siga así. Desaparece con
        el primer destino activo.</p>
      </>
    ),
  },
  {
    titulo: 'Crear un destino',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-nuevo"]',
    texto: (
      <>
        <p><strong>Nuevo destino</strong> pide un nombre para reconocerlo en la
        lista, los destinatarios separados por comas, y los datos del correo
        saliente: servidor SMTP, puerto (587 por defecto), usuario, contraseña y
        remitente.</p>
        <p>Se exige conexión cifrada con el servidor: si no la ofrece, el envío
        falla antes que mandar la contraseña en claro. <strong>Guardar
        destino</strong> lo deja listo; después hay que probarlo.</p>
      </>
    ),
  },
  {
    titulo: 'Qué se envía: la severidad mínima',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-resumen"]',
    texto: (
      <>
        <p>En la hoja, <em>Qué se envía</em> fija desde qué gravedad sale correo:
        <em> Solo lo crítico</em> (canal caído, credencial vencida),
        <em> Errores y peor</em>, que es lo recomendado, <em>Avisos y peor</em>,
        que incluye los publicados sin stock, o <em>Todo</em>, que llena el buzón
        y entrena a ignorarlos.</p>
        <p>La línea bajo el nombre del destino muestra sus destinatarios, el
        umbral elegido y la fecha del último envío.</p>
      </>
    ),
  },
  {
    titulo: 'Activo o pausado',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-estado"]',
    texto: (
      <>
        <p>La casilla <em>Activo</em> de la hoja se ve aquí como pastilla. Un
        destino <em>Pausado</em> conserva toda su configuración pero no manda
        nada: sirve para callar un buzón unos días sin borrar nada.</p>
        <p>Si todos quedan pausados, vuelve la franja roja de «no le llegan a
        nadie».</p>
      </>
    ),
  },
  {
    titulo: 'Probar el envío',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-probar"]',
    texto: (
      <>
        <p><strong>Probar</strong> manda un correo de prueba ahora mismo a los
        destinatarios del destino y escribe el resultado junto a la fila. Es la
        única forma de saber que la configuración sirve: una que nadie probó se
        descubre rota el día que hay un aviso de verdad.</p>
        <p>Si el último envío real falló, la fila lo dice también, sin tener que
        volver a probar.</p>
      </>
    ),
  },
  {
    titulo: 'Editar un destino',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-editar"]',
    texto: (
      <>
        <p><strong>Editar</strong> abre la misma hoja con el nombre, los
        destinatarios, el umbral y la casilla Activo. La contraseña vuelve en
        blanco: dejarla así conserva la que estaba; solo se cambia si escribes
        otra.</p>
        <p>El servidor, el puerto, el usuario y el remitente también vuelven en
        blanco, porque se guardan junto a la contraseña: al editar hay que
        escribirlos otra vez, o el guardado avisa de que falta el servidor.</p>
      </>
    ),
  },
  {
    titulo: 'Borrar un destino',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-borrar"]',
    texto: (
      <>
        <p><strong>Borrar</strong> quita el destino y pide confirmación. Si es el
        único, la pregunta lo advierte: Integra volvería a guardar los avisos sin
        mandarlos a nadie.</p>
        <p>Para callarlo un tiempo sin perder la configuración, mejor editarlo y
        desmarcar Activo.</p>
      </>
    ),
  },
  {
    titulo: 'Cómo se relaciona con Automatización',
    seccion: 'avisos',
    objetivo: '[data-guia="avi-nota-envio"]',
    texto: (
      <>
        <p>Los avisos nacen en <em>Automatización</em>; allí se leen y se marcan
        como vistos. Lo que se manda por correo es lo que está abierto, sin ver
        y con la gravedad suficiente, agrupado en un solo mensaje por tanda, y
        <strong> cada aviso se envía una sola vez</strong>.</p>
        <p>Los manda el mismo proceso de fondo que corre los horarios de
        Automatización; si ese proceso está apagado, los avisos se acumulan sin
        salir.</p>
      </>
    ),
  },
]
