import type { Paso } from '../GuiaPasos'

// Recorrido del editor de fotos. Los grupos del panel de ajustes están
// plegados y el recorrido no los abre: se resalta la cabecera de cada
// grupo y se cuenta lo que hay dentro. Rangos y valores iniciales salen de
// EditorFoto.tsx (INICIAL, AUTO, RAPIDOS y cada deslizador).

export const PASOS_EDITOR_FOTO: Paso[] = [
  {
    titulo: 'Recortar sobre la foto',
    objetivo: '[data-guia="ef-edicion"]',
    texto: (
      <>
        <p>Aquí se trabaja sobre la foto original entera, no sobre una
        miniatura. <strong>Arrastra la caja</strong> para moverla y
        <strong> sus esquinas</strong> para cambiar el recorte; el tamaño en
        píxeles de lo recortado se lee en el título de la zona.</p>
        <p>Si no tocas la caja no hay recorte: se guarda la foto completa.</p>
      </>
    ),
  },
  {
    titulo: 'Proporciones y giros rápidos',
    objetivo: '[data-guia="ef-herramientas"]',
    texto: (
      <>
        <p><strong>Libre</strong> deja mover las esquinas a gusto;
        <strong> Cuadrado</strong>, <strong>4:3</strong>, <strong>3:4</strong> y
        <strong> 16:9</strong> ponen una caja centrada con esa proporción y la
        mantienen al arrastrar. <strong>Quitar recorte</strong> vuelve a la
        foto entera.</p>
        <p>Las flechas giran 90° a la izquierda o a la derecha y voltean en
        horizontal o vertical. Girar o voltear quita el recorte que hubiera:
        la caja ya no encaja con el nuevo marco.</p>
      </>
    ),
  },
  {
    titulo: 'Así quedará',
    objetivo: '[data-guia="ef-resultado"]',
    texto: (
      <>
        <p>Lo que se va a guardar, con todos los ajustes aplicados. En el
        título, las medidas finales, el peso y el formato; se recalcula un
        instante después de cada cambio.</p>
        <p>Con las guías se dibujan los tercios, en azul el área útil que
        deja el margen del lienzo cuadrado y, alrededor del producto
        detectado, un contorno verde si llena bien el encuadre o naranja si
        no.</p>
      </>
    ),
  },
  {
    titulo: 'Comparar con la original',
    objetivo: '[data-guia="ef-comparar"]',
    texto: (
      <p><strong>Mantén pulsado Ver original</strong> para ver en el mismo
      sitio la foto sin ajustes (solo con el giro aplicado); al soltar vuelve
      el resultado. La casilla <strong>Guías</strong> enciende o apaga las
      líneas de encuadre; no salen en lo que se guarda.</p>
    ),
  },
  {
    titulo: 'Lo que dirían los canales',
    objetivo: '[data-guia="ef-avisos"]',
    texto: (
      <>
        <p>Antes de guardar, esta lista avisa de lo que rechazaría o afearía
        cada canal: lado menor por debajo de 600 px (MercadoLibre la
        rechaza), menos de 1200 px (vale, pero se ve mejor con 1200), foto no
        cuadrada (bandas en MercadoLibre y Falabella), PNG (pesa más y
        Falabella prefiere JPEG) y más de 3 MB de peso.</p>
        <p>La última línea dice qué porcentaje del lienzo ocupa el producto:
        lo ideal es entre 75 y 90 %. Por debajo del 60 % se ve pequeño; por
        encima del 95 % toca los bordes y la miniatura lo cortará. En rojo
        lo que hay que corregir; en verde lo que está bien.</p>
      </>
    ),
  },
  {
    titulo: 'Autoajustar',
    objetivo: '[data-guia="ef-auto"]',
    texto: (
      <p>Lo que casi siempre necesita una foto de producto, de una vez:
      quita los bordes blancos sobrantes, endereza los niveles, blanquea el
      fondo, la centra en un cuadrado de 1200 px con un 5 % de margen, le da
      un punto de nitidez y la deja en JPEG. Cada cosa se puede deshacer
      después en su grupo.</p>
    ),
  },
  {
    titulo: 'Ajustes rápidos por destino',
    objetivo: '[data-guia="ef-rapidos"]',
    texto: (
      <>
        <p><strong>Marketplace</strong>, para MercadoLibre y Falabella:
        cuadrada de 1200, fondo blanco, margen del 5 %, sin bordes sobrantes,
        JPEG.</p>
        <p><strong>Tienda propia</strong>, para WooCommerce y Shopify: hasta
        1600 px, sin lienzo cuadrado, JPEG.</p>
        <p><strong>Solo recortar</strong>: guarda el recorte tal cual, sin
        lienzo, sin blanquear y con el color sin tocar, hasta 2000 px.</p>
      </>
    ),
  },
  {
    titulo: 'Recorte y giro',
    objetivo: '[data-guia="ef-grupo-recorte"]',
    texto: (
      <>
        <p>Pulsa la cabecera para abrir el grupo; cerrado, resume lo que hay
        puesto. <strong>Enderezar</strong> gira en pasos de medio grado hasta
        ±15° y amplía lo justo para que no asomen esquinas vacías: para un
        horizonte torcido. Los botones giran 90° y voltean, igual que los de
        la zona de edición.</p>
        <p><strong>Quitar bordes blancos sobrantes</strong> recorta solo el
        marco blanco o transparente que rodea al producto. Cada grupo tiene
        su <strong>Restablecer grupo</strong>, que vuelve a los valores
        iniciales sin tocar los demás.</p>
      </>
    ),
  },
  {
    titulo: 'Color y nitidez',
    objetivo: '[data-guia="ef-grupo-color"]',
    texto: (
      <>
        <p><strong>Brillo</strong>, <strong>contraste</strong> y
        <strong> saturación</strong> van de 50 a 150 %; 100 es dejarlo como
        está. <strong>Temperatura</strong>: hacia positivo más cálida (más
        rojo), hacia negativo más fría (más azul). <strong>Nitidez</strong>
        afila los bordes y se aplica sobre el tamaño final, que es donde se
        nota.</p>
        <p><strong>Niveles automáticos</strong> estira los tonos para que el
        negro sea negro y el blanco, blanco: arregla fotos lavadas.</p>
      </>
    ),
  },
  {
    titulo: 'Fondo',
    objetivo: '[data-guia="ef-grupo-fondo"]',
    texto: (
      <>
        <p><strong>Quitar el fondo</strong> borra el fondo liso que toca los
        bordes de la foto, como una varita mágica; lo que queda encerrado
        dentro del producto no se toca. La <strong>tolerancia</strong> lo
        ajusta: súbela si quedan restos, bájala si se come el producto. Puede
        quedar <strong>transparente</strong>, lo que obliga a PNG; si no, se
        rellena con el color del lienzo.</p>
        <p><strong>Blanquear el fondo</strong> pasa a blanco puro todo lo casi
        blanco, desde el umbral que elijas (200 a 254): MercadoLibre exige
        fondo blanco y las fotos de estudio traen un gris que se nota.
        <strong> Color del lienzo</strong> es el fondo del cuadrado y solo se
        usa con lienzo cuadrado.</p>
      </>
    ),
  },
  {
    titulo: 'Lienzo y tamaño',
    objetivo: '[data-guia="ef-grupo-lienzo"]',
    texto: (
      <>
        <p><strong>Lienzo cuadrado con fondo</strong> centra la foto en un
        cuadrado con margen: así se ve mejor en MercadoLibre y Falabella. El
        <strong> lado</strong> (de 800 a 2000 px) es el tamaño final; sin
        lienzo, es el máximo al que se reduce la foto.</p>
        <p>El <strong>margen</strong> (0 a 25 %) es el aire alrededor del
        producto. <strong>Ampliar si es más pequeña</strong> estira una foto
        que no llega al lado pedido; no añade detalle, solo píxeles, y por
        eso viene apagado.</p>
      </>
    ),
  },
  {
    titulo: 'Formato de salida',
    objetivo: '[data-guia="ef-grupo-exportar"]',
    texto: (
      <p><strong>JPEG</strong> es lo normal: pesa menos y lo prefieren los
      canales; su <strong>calidad</strong> va de 50 a 100 y arranca en 88.
      <strong> PNG</strong> solo hace falta para dejar el fondo transparente,
      y pesa más. Elegir JPEG apaga la transparencia.</p>
    ),
  },
  {
    titulo: 'Guardar: reemplazar o como nueva',
    objetivo: '[data-guia="ef-guardar"]',
    texto: (
      <>
        <p><strong>Reemplazar la original en sus productos</strong> pone la
        versión editada en el lugar de la vieja en todos los productos que la
        usaban —misma posición, misma portada— y borra la vieja del disco. Es
        lo normal al recortar un borde o pasar a cuadrado.</p>
        <p><strong>Guardar como nueva y conservar la original</strong> añade
        la editada detrás en esos mismos productos y deja la original, para
        cuando se quieren las dos. Lo guardado entra en el banco como una
        foto más.</p>
      </>
    ),
  },
  {
    titulo: 'Deshacer todo y cerrar',
    objetivo: '[data-guia="ef-deshacer"]',
    texto: (
      <p><strong>Deshacer todo</strong> devuelve cada ajuste a su valor
      inicial (lienzo cuadrado de 1200, margen del 5 %, JPEG a 88) y se
      activa en cuanto cambias algo. <strong>Cerrar</strong>, pulsar fuera o
      Escape cierran sin guardar; mientras se guarda no se puede cerrar.</p>
    ),
  },
]
