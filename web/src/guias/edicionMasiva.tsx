import type { Paso } from '../GuiaPasos'

// Recorrido del diálogo «Editar en masa». Nada de lo que dice sobre omisiones
// o redondeos está inventado: es lo que hace la operación al calcular.

export const PASOS_EDICION_MASIVA: Paso[] = [
  {
    titulo: 'Una operación, muchos productos',
    objetivo: '[data-guia="masa-cabecera"]',
    texto: (
      <>
        <p>Aquí se aplica un mismo cambio a un lote entero: precios, marca o
        exclusión del catálogo. Debajo del título se ve a <strong>cuántos
        productos</strong> alcanza con el ajuste actual.</p>
        <p>Nada se guarda hasta que hayas visto qué cambiaría y pulses
        <em> Aplicar</em>. Con una operación en curso, el diálogo no se cierra.</p>
      </>
    ),
  },
  {
    titulo: 'A qué productos',
    objetivo: '[data-guia="masa-alcance"]',
    texto: (
      <>
        <p><strong>Los seleccionados</strong> son los que marcaste con las casillas
        de la lista. <strong>Todos los del filtro actual</strong> alcanza a cuanto
        cumple la búsqueda y los filtros que tienes puestos, no solo a la
        página que ves.</p>
        <p>Si no marcaste ninguno, la única opción es el filtro. El tope por
        operación son 2.000 productos.</p>
      </>
    ),
  },
  {
    titulo: 'Qué hacer',
    objetivo: '[data-guia="masa-tipo"]',
    texto: (
      <>
        <p><strong>Precio = coste × factor</strong> parte del coste registrado en
        Odoo; los que no tienen coste se omiten, nunca quedan a cero.
        <strong> Ajustar el precio actual × factor</strong> sube o baja lo que ya
        hay; los que aún no tienen precio se omiten.</p>
        <p><strong>Poner un precio fijo</strong> pone el mismo a todos.
        <strong> Borrar el precio</strong> los deja sin precio, y por tanto sin
        publicar. <strong>Asignar marca</strong>, <strong>Excluir</strong> y
        <strong> Devolver al catálogo</strong> hacen lo que dicen.</p>
      </>
    ),
  },
  {
    titulo: 'Factor',
    objetivo: '[data-guia="masa-factor"]',
    texto: (
      <>
        <p>El multiplicador de las dos operaciones de precio proporcional.
        Desde el coste, <strong>1.35</strong> es un 35 % sobre el coste. Sobre el
        precio actual, <strong>1.10</strong> sube un 10 % y <strong>0.90</strong> baja
        un 10 %.</p>
        <p>Tiene que ser mayor que cero: vacío o con letras, el botón de
        simular se bloquea y te lo dice. Vale con coma o con punto.</p>
      </>
    ),
  },
  {
    titulo: 'Precio fijo y marca',
    objetivo: '[data-guia="masa-tipo"]',
    texto: (
      <>
        <p>Al elegir <em>Poner un precio fijo</em> aparece la casilla
        <strong> Precio</strong>: en pesos, sin puntos de miles.</p>
        <p>Al elegir <em>Asignar marca</em> aparece la casilla
        <strong> Marca</strong>: si la marca no existe se crea; dejarla vacía
        <strong> quita</strong> la marca a todos los del lote.</p>
      </>
    ),
  },
  {
    titulo: 'Redondeo',
    objetivo: '[data-guia="masa-redondeo"]',
    texto: (
      <>
        <p>Cómo se deja el precio calculado. <strong>Comercial</strong> lo baja al
        millar y lo termina en 900 (el clásico $ 119.900); por debajo de
        1.000 pesos redondea al centenar. <strong>Al centenar</strong> y
        <strong> Al millar</strong> redondean al más cercano.
        <strong> Sin redondeo</strong> deja el resultado con dos decimales.</p>
        <p>No aplica al borrar el precio ni a las operaciones que no son de
        precio.</p>
      </>
    ),
  },
  {
    titulo: 'Ver qué cambiaría',
    objetivo: '[data-guia="masa-simular"]',
    texto: (
      <>
        <p>Calcula el efecto <strong>sin guardar nada</strong>. Enseña cuántos
        productos cambiarían, cuántos se omiten y por qué, y una muestra de
        hasta cinco con el precio de antes y el de después.</p>
        <p>Es obligatorio: sin simulación el botón Aplicar no se activa. Si
        tocas cualquier ajuste, la simulación se descarta y hay que repetirla,
        para que lo que apliques sea exactamente lo que viste.</p>
      </>
    ),
  },
  {
    titulo: 'Aplicar',
    objetivo: '[data-guia="masa-aplicar"]',
    texto: (
      <>
        <p>Pide confirmación diciendo en palabras qué va a hacer y a cuántos.
        <strong> No se puede deshacer</strong>: un precio pisado no se recupera.</p>
        <p>Al terminar se cierra el diálogo, se limpia la selección y la lista
        se recarga con los avisos de estado ya recalculados.</p>
      </>
    ),
  },
]
