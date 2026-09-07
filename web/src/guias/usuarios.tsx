import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla Usuarios (Usuarios.tsx): quién entra a Integra,
// qué puede hacer cada rol y cómo se da y se quita el acceso.
//
// Cada afirmación está contrastada con la pantalla y con la regla que la
// aplica; si cambia allí, hay que cambiarla aquí.

export const PASOS_USUARIOS: Paso[] = [
  {
    titulo: 'Quién entra a Integra',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-tabla"]',
    texto: (
      <>
        <p>La lista tiene a todas las personas con cuenta: nombre, correo con el
        que entran, rol, cuándo entraron por última vez (<em>nunca</em> si aún no
        lo han hecho) y si tienen acceso.</p>
        <p>Solo los administradores ven esta pantalla y la de Avisos; a los demás
        ni les aparecen en el menú.</p>
      </>
    ),
  },
  {
    titulo: 'Los tres roles',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-roles"]',
    texto: (
      <>
        <p><em>Consulta</em> ve el catálogo, los pedidos y los avisos, y no cambia
        nada. <em>Operación</em> además edita productos y precios, publica y
        despacha. <em>Administración</em> lo hace todo, más usuarios,
        credenciales de canal, destinos de aviso y auditoría.</p>
        <p>Despublicar una ficha del canal también es solo de administradores,
        porque no se deshace.</p>
      </>
    ),
  },
  {
    titulo: 'Crear un usuario',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-nuevo"]',
    texto: (
      <>
        <p><strong>Nuevo usuario</strong> pide el correo, el nombre, el rol y una
        contraseña de al menos 8 caracteres. El rol propuesto es Operación; al
        elegir otro, la hoja recuerda qué puede hacer.</p>
        <p>El nombre importa: es lo que identifica en la auditoría quién hizo
        qué. <strong>Crear usuario</strong> deja la cuenta activa de inmediato.</p>
      </>
    ),
  },
  {
    titulo: 'Editar y cambiar la contraseña',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-editar"]',
    texto: (
      <>
        <p><strong>Editar</strong> permite cambiar el nombre y el rol. El correo no
        se cambia: es la identidad de la cuenta.</p>
        <p>Para cambiar la contraseña, escribe la nueva en esa misma hoja; dejarla
        en blanco la conserva. Las contraseñas no se muestran nunca.</p>
      </>
    ),
  },
  {
    titulo: 'Activo o sin acceso',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-estado"]',
    texto: (
      <>
        <p><em>Activo</em> puede entrar. <em>Sin acceso</em> conserva la cuenta,
        pero no puede iniciar sesión, y si tenía una sesión abierta deja de
        servir.</p>
        <p>Un cambio de rol también aplica enseguida, sin esperar a que la
        persona vuelva a entrar.</p>
      </>
    ),
  },
  {
    titulo: 'Quitar y devolver el acceso',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-acceso"]',
    texto: (
      <>
        <p><strong>Quitar acceso</strong> no borra a la persona: su rastro en la
        auditoría se conserva, que es justo para lo que sirve. Pide confirmación
        y se revierte con <strong>Devolver acceso</strong>.</p>
        <p>No se le puede quitar el acceso al único administrador activo: nadie
        podría volver a entrar a gestionar usuarios.</p>
      </>
    ),
  },
  {
    titulo: 'Borrar del todo',
    seccion: 'usuarios',
    objetivo: '[data-guia="usr-nota"]',
    texto: (
      <>
        <p>Eliminar una cuenta por completo no se hace desde aquí: la nota dice
        el comando que lo hace, y es tarea de quien administra el servidor.</p>
        <p>Para el día a día, quitar el acceso es lo correcto: la persona no
        entra y su historial sigue explicando lo que pasó.</p>
      </>
    ),
  },
]
