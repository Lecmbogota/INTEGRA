import type { Paso } from '../GuiaPasos'

// Recorrido de la pantalla Odoo (Integraciones.tsx): de dónde sale el
// catálogo, qué se lee de allí y qué no, y qué hace cada botón de la fila.
//
// Cada afirmación está contrastada con la pantalla y con lo que hace la
// sincronización; si cambia allí, hay que cambiarla aquí.

export const PASOS_INTEGRACIONES: Paso[] = [
  {
    titulo: 'Odoo: de dónde sale el catálogo',
    seccion: 'integraciones',
    objetivo: '[data-guia="menu-integraciones"]',
    texto: (
      <>
        <p>Aquí se conecta la instancia de Odoo de la que sale todo lo que
        Integra publica. Puede haber varias conexiones guardadas, pero
        <strong> solo una está activa</strong>: Integra sincroniza contra un
        único Odoo a la vez.</p>
        <p>Cambiar de conexión activa cambia el catálogo entero que ven los
        canales, así que es lo más delicado que se hace desde esta pantalla.</p>
      </>
    ),
  },
  {
    titulo: 'La conexión y sus números',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-datos"]',
    texto: (
      <>
        <p>Cada fila es una conexión: su nombre, la dirección de Odoo, la base
        de datos y el usuario con el que entra. La clave con la que se
        autentica nunca se muestra.</p>
        <p>Debajo, lo que ya trajo: cuántos productos, cuántos tienen precio y
        cuántos tienen imagen, y cuándo fue la <em>última sincronización</em>.
        Si un producto de Odoo no aparece en Integra, esa fecha es lo primero
        que hay que mirar.</p>
      </>
    ),
  },
  {
    titulo: 'Qué se trae de Odoo y qué no',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-nota"]',
    texto: (
      <>
        <p>De Odoo se leen la referencia (SKU), el nombre, la categoría de Odoo
        y el stock de cada bodega. Si un producto se archiva o se borra en
        Odoo, deja de ser mercancía y Integra deja de publicarlo.</p>
        <p>Precio, marca, descripción, fotos y títulos por canal son de Integra:
        se completan en Productos e Imágenes, y ninguna sincronización los pisa.
        Por eso el precio de venta de Odoo no cuenta aquí.</p>
      </>
    ),
  },
  {
    titulo: 'Activa o inactiva',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-estado"]',
    texto: (
      <>
        <p>La pastilla dice contra cuál Odoo se sincroniza hoy. Solo las bodegas
        de la conexión <em>activa</em> se pueden asignar a los canales, y solo
        su catálogo se publica.</p>
        <p>Una conexión inactiva se queda guardada con sus productos, sin recibir
        sincronizaciones, hasta que alguien la active o la borre.</p>
      </>
    ),
  },
  {
    titulo: 'Probar la conexión',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-probar"]',
    texto: (
      <>
        <p><strong>Probar</strong> entra a Odoo con la credencial guardada y
        escribe el resultado debajo de la fila: una marca verde si autenticó, o
        el motivo del rechazo si no.</p>
        <p>Es lo que hay que pulsar cuando la sincronización deja de traer
        cambios: si aquí falla, la clave se venció o alguien la cambió en Odoo,
        y toca editarla.</p>
      </>
    ),
  },
  {
    titulo: 'Activar otra conexión',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-activar"]',
    texto: (
      <>
        <p><strong>Activar</strong> solo aparece en las conexiones inactivas.
        Antes de cambiar nada se comprueba la credencial: si no conecta, todo
        sigue como estaba.</p>
        <p>Pide confirmación porque, desde la siguiente sincronización, el
        catálogo sale de ese otro Odoo. No borra nada de la conexión anterior.</p>
      </>
    ),
  },
  {
    titulo: 'Editar los datos',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-editar"]',
    texto: (
      <>
        <p><strong>Editar</strong> abre la misma hoja con la que se conectó:
        nombre, dirección, base de datos, usuario y zona horaria. La clave se
        deja en blanco para conservar la actual; solo se escribe si cambió.</p>
        <p>Si cambian la dirección, la base, el usuario o la clave, la
        combinación nueva se prueba contra Odoo antes de guardarse. La zona
        horaria es la que usan los horarios programados de Automatización.</p>
      </>
    ),
  },
  {
    titulo: 'Borrar una conexión',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-borrar"]',
    texto: (
      <>
        <p><strong>Borrar</strong> solo aparece en las inactivas: la activa no se
        puede borrar, primero hay que activar otra.</p>
        <p>Se lleva la conexión y todos los productos que trajo. El trabajo hecho
        en Integra sobre ellos (precios, imágenes) queda sin producto al que
        pertenecer, así que la pregunta de confirmación dice cuántos son.</p>
      </>
    ),
  },
  {
    titulo: 'Conectar otra instancia',
    seccion: 'integraciones',
    objetivo: '[data-guia="odoo-conectar"]',
    texto: (
      <>
        <p>La hoja pide la dirección de Odoo, el nombre real de la base de datos
        (no el subdominio), el usuario y su clave de API, que en Odoo se crea en
        <em> Mi perfil → Seguridad de la cuenta → Nueva clave de API</em>. El
        nombre y la zona horaria son opcionales.</p>
        <p><strong>Comprobar y guardar</strong> entra a Odoo primero y solo si
        conecta guarda la clave, cifrada; no vuelve a mostrarse. La casilla
        <em> Ver la API key mientras la escribo</em> evita un dedazo. La
        conexión nueva pasa a ser la activa.</p>
      </>
    ),
  },
  {
    titulo: 'Sincronizar y las bodegas',
    seccion: 'integraciones',
    objetivo: '[data-guia="menu-panel"]',
    texto: (
      <>
        <p>Desde aquí no se sincroniza: se hace con <strong>Sincronizar
        ahora</strong> en el Panel, o sola con el horario de Automatización.
        Cada sincronización trae los productos nuevos o cambiados, las bajas y el
        stock completo por bodega.</p>
        <p>Las bodegas de Odoo llegan con esa misma sincronización. Cuáles
        alimentan a cada canal se decide en <em>Canales</em>, cuenta por cuenta;
        sin esa asignación el canal no publica nada.</p>
      </>
    ),
  },
]
