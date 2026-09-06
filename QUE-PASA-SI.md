# ¿Y qué pasa si...?

Resultado de la mesa de seis participantes, con cada hueco contrastado contra el código.
Está ordenado por daño: lo que cuesta dinero o clientes va primero, sin importar de qué
parte del sistema venga. Cuando dos participantes señalaron lo mismo desde ángulos
distintos, aquí aparece una sola entrada.

Cada entrada dice qué pasaría, qué hace Integra hoy y qué haría falta. Las referencias a
ficheros están para quien vaya a arreglarlo; el resto se puede leer sin abrirlas.

---

## Lo que rompe el negocio (crítico)

### 1. MDV despacha y el canal nunca se entera

El pedido entra a Odoo pero al marketplace no se le confirma jamás que salió: MercadoLibre
y Falabella miden el tiempo hasta el despacho, y pasado el plazo cancelan, reembolsan al
comprador y bajan la reputación del vendedor, lo que además reduce la exposición de todas
las publicaciones y retiene el pago.
Hoy `AckOrder` está en el contrato (`internal/channel/adapter.go:61`) y los cuatro
conectores lo implementan con mucho detalle —Shopify por fulfillment orders
(`shopify.go:586`), WooCommerce con nota al comprador y protección contra avisar dos veces
(`woocommerce.go:504`), MercadoLibre distinguiendo ME1 de ME2 (`mercadolibre.go:849`),
Falabella con `SetStatusToReadyToShip` (`falabella.go:607`)— pero no lo llama nadie fuera
de sus propias pruebas, e Integra tampoco vuelve a leer nunca el estado del `sale.order`,
así que ni siquiera sabe que se despachó.
Falta un trabajo de despacho que detecte en Odoo el albarán validado del `sale.order` y
llame a `AckOrder` con la guía y la transportadora: es trabajo ya escrito que no está
enchufado.

### 2. Una venta en un canal no baja el stock en los otros tres hasta el día siguiente

La última unidad vendida en WooCommerce se sigue ofreciendo en MercadoLibre, Falabella y
Shopify hasta la corrida programada, y con cuatro canales y catálogo de rotación esta es la
fuente principal de ventas que hay que cancelarle al comprador, con la penalización de
reputación que cada marketplace aplica.
Hoy la venta baja el stock en la base al instante (`internal/ordenes/ordenes.go:239` →
`internal/store/ordenes.go:534`), pero nadie encola el envío de stock a las demás cuentas:
`publicar.Planificar` solo se dispara desde el horario
(`internal/planificador/planificador.go:172`) o desde el botón de la API
(`internal/api/server.go:674`), y los horarios son de una hora fija al día
(`migrations/005_sync.sql:78-82`), sin opción de «cada 10 minutos».
Falta que la ingesta, después de descontar, encole el trabajo de stock de esa variante en
las otras cuentas, o que exista un horario de alcance «stock» medido en minutos.

### 3. Nadie ha asignado bodegas: se publica stock que no se puede despachar

Cada canal está publicando la suma de Muestras, Garantías y las tres bodegas de
consignación de Falabella, así que WooCommerce ofrece unidades que están físicamente en el
centro de distribución de Falabella: la venta se acepta y luego hay que cancelarla.
Hoy el stock publicable filtra por `channel_account_warehouses` y, si la cuenta no tiene
filas, suma todas las bodegas (`internal/store/publicacion.go:88-99`); esa tabla no la
escribe ningún código de producción —solo aparece en consultas de lectura y en los tests—
y no hay pantalla ni endpoint que la rellene.
Falta una pantalla o comando para asignar bodegas por cuenta, y negarse a publicar una
cuenta sin asignación en vez de asumir «todas», que es justo la mentira que el desglose por
bodega existía para evitar (`migrations/002_catalog.sql:83-86`).

### 4. Dos productos de Odoo con el mismo SKU se pisan y se despacha el equivocado

La ficha viva en el canal se queda con el título, la descripción y las fotos del producto
equivocado y se sobreescribe indefinidamente, quemando cupo de API; y cuando llega una
venta de ese SKU se descuenta stock y se monta el pedido en Odoo contra uno de los dos
elegido al azar, así que se despacha otra mercancía.
Hoy nada lo impide ni lo detecta: `product_variants.sku` no tiene índice único
(`migrations/002_catalog.sql:73`), la cola de atención no tiene motivo «sku_duplicado»
(`internal/store/edicion.go:205-238`), el segundo producto adopta por SKU la publicación
del primero y luego choca contra `product_listings_external_idx`
(`migrations/004_listings.sql:38-40`), de modo que su hash nunca llega a guardarse y vuelve
a intentarlo en cada pasada; el emparejamiento de líneas usa `LIMIT 1` sin `ORDER BY`
(`internal/store/ordenes.go:136-139`).
Falta un chequeo bloqueante de SKU duplicado en la cola de atención que impida publicar los
dos, más un orden determinista en el emparejamiento de líneas.

### 5. Renombrar un SKU en Odoo rompe todos los pedidos futuros de ese producto

El canal se queda con el SKU viejo, así que cada pedido de ese producto llega con una
referencia que el catálogo ya no tiene: la línea queda sin variante, el pedido falla cinco
veces al montarse en Odoo y no lo rescata nadie. Es una venta cobrada al comprador que
nunca llega a Odoo.
Hoy el sync pisa el SKU (`internal/store/store.go:334`) y, aunque los envíos de precio y
stock siguen llegando porque `RefDePublicacion` prefiere el `channel_sku` guardado
(`internal/store/publicacion.go:178-206`), el renombrado entra en el hash de contenido
(`internal/publicar/motor.go:135`) y dispara una republicación en la que ninguno de los
cuatro `Update` manda el SKU nuevo —Falabella incluso fuerza el viejo,
`falabella.go:296`—, y el motor guarda el hash como publicado
(`internal/publicar/manejadores.go:199-202`), sellando la divergencia.
Falta emparejar los pedidos también contra `variant_channel_listings.channel_sku`, y avisar
(o bloquear) cuando el SKU de Odoo deje de coincidir con el publicado.

### 6. Cambiar la comisión de un canal no cambia ni un precio publicado

Se sube la comisión de MercadoLibre del 12% al 16% porque cambió el contrato, la pantalla
dice que se guardó, y el catálogo entero sigue publicado al precio viejo: cada venta deja
cuatro puntos de margen en el canal hasta que alguien pulse «recalcular precios» cuenta por
cuenta.
Hoy el handler `internal/api/server.go:792-808` guarda `comision_pct` y `costo_fijo` y no
llama a `refrescarPreciosEfectivos` —solo lo hacen la edición de precio de la ficha
(`server.go:1019`) y la masiva (`server.go:394`)—, y como
`internal/store/publicacion.go:121-125` prefiere el valor ya guardado en `effective_prices`,
el precio publicado sigue calculado con la comisión vieja; el comentario de
`internal/publicar/motor.go:155-156` da por hecho lo contrario.
Falta que editar un canal dispare el recálculo de precios efectivos de todas sus cuentas,
igual que hace la edición de precio.

### 7. Un precio manual o una regla de canal se guarda y no llega nunca a la tienda

Un precio especial pactado, o una regla de «+8% en la marca X», se guarda, se ve en
pantalla y no sale al canal: se vende durante semanas al precio anterior creyendo que el
ajuste está aplicado.
Hoy `guardarOverridePrecio` (`internal/api/precios.go:146-165`) y `guardarReglaPrecio`
(`precios.go:94-118`) escriben y devuelven `ok:true` sin recalcular; el único sitio que
aplica esas reglas es `RecalcularPreciosCuenta`
(`internal/store/precios.go:437-509`), que solo se invoca desde el botón manual, desde la
edición de precio y desde `moverPromociones`, y el horario periódico
(`internal/planificador/planificador.go:172-190`) llama directo a `publicar.Planificar` sin
recalcular nada.
Falta recalcular la cuenta afectada al guardar un override o una regla, y recalcular también
dentro del horario antes de planificar.

### 8. Nada impide publicar por debajo del coste

Un coste que sube en Odoo, o un precio tecleado con un dígito de menos, hace que se venda
por debajo de coste sin un solo aviso: se descubre al cuadrar el mes, con la mercancía ya
despachada.
Hoy el suelo de margen solo existe si hay una regla de canal aplicable y esa regla trae
`min_margin_percent` (`internal/pricing/resolucion.go:143-156`); sin regla no hay
comprobación ninguna, la cola de atención tiene siete motivos y ninguno mira el coste
(`internal/store/edicion.go:190-241`), y `price_below_cost` aparece declarado en el
comentario de `migrations/005_sync.sql:107` pero no lo genera nadie.
Falta un motivo `price_below_cost` en la cola de atención y una alerta cuando el precio
efectivo no cubra coste más un margen mínimo por defecto de la cuenta.

### 9. Falabella cancela un pedido y para Integra sigue vivo

Si aún no se había montado, se crea igualmente el `sale.order` en Odoo por una venta
muerta, el stock queda apartado una semana y no salta ninguna alerta.
Hoy el conector de Falabella nunca rellena el estado del pedido —`ordenResp` no lo lee y
`channel.Order` se construye sin `Status` ni `UpdatedAt`,
`internal/conectores/falabella/falabella.go:481-495` y `511-527`— y además filtra por
`CreatedAfter` en vez de por fecha de modificación (`falabella.go:454-456`), así que un
pedido viejo que cambia de estado ni siquiera se vuelve a bajar; `esCancelado("")` es
siempre falso (`internal/ordenes/ordenes.go:323-329`).
Falta leer el estado del pedido y el de las líneas (que sí llega en `GetOrderItems`),
volcarlo en `channel.Order`, y filtrar por fecha de modificación.

### 10. Un pedido que falla cinco veces se queda fuera de Odoo para siempre

Un pedido cobrado al comprador no llega nunca a Odoo por una caída pasajera o por una
tarifa que falta; la alerta sigue sonando pero no hay manera de reintentarlo salvo entrar a
la base de datos a mano.
Hoy cada fallo suma un intento (`internal/store/ordenes.go:312-319`) y las dos consultas de
pendientes exigen `sync_attempts < 5` (`ordenes.go:163` y `260`), el trabajo de la cola
también muere a los cinco (`migrations/013_jobs.sql:24`), el único sitio que devuelve el
contador a cero es `ReemparejarLineasHuerfanas` —y solo para pedidos cuyo problema era el
SKU— y la pantalla de Pedidos solo lista y trae nuevos (`web/src/Pedidos.tsx:86-93`).
Falta un botón con su endpoint para reintentar un pedido fallido, que ponga `sync_attempts`
a cero y vuelva a encolar el montaje.

### 11. El token de MercadoLibre se puede invalidar solo

MercadoLibre invalida el refresh token en cuanto uno lo canjea: los demás reciben
`invalid_grant` y, si el que persiste último es el que perdió la carrera, queda guardado un
token muerto. La cuenta deja de publicar precio y stock y de traer pedidos hasta que alguien
vuelva a autorizar a mano, mientras se sigue vendiendo con el stock y el precio congelados.
Hoy el adaptador se construye nuevo en cada trabajo a propósito
(`internal/conectores/cuenta.go:21-48`) y el candado que protege el canje es un campo de la
instancia (`mercadolibre.go:81`, usado en `:1163-1204`), así que con
`INTEGRA_WORKER_CONCURRENCY=8` (`internal/config/config.go:62`) hay hasta ocho adaptadores
de la misma cuenta refrescando a la vez, cada uno con su propio candado.
Falta serializar el canje por cuenta fuera del proceso: un lock en la base al refrescar, o
un único adaptador cacheado por cuenta.

### 12. Cuando el canal dice «para», Integra acelera

Un bloqueo de una hora de MercadoLibre hace que los 452 productos encolados gasten sus
cinco intentos en menos de ocho minutos contra una API que ya nos pidió parar —lo que
alarga el bloqueo— y acaben en `failed`; cuando el bloqueo se levanta nada los reintenta
hasta el siguiente horario, y durante horas el stock publicado es el viejo.
Hoy el contrato tiene `channel.Error.RetryAfter` para esto
(`internal/channel/adapter.go:326-327`) y MercadoLibre y Shopify lo rellenan de verdad
(`mercadolibre.go:1290-1294`, `shopify.go:1198-1205`), pero nadie lo lee: la cola reintenta
siempre con su backoff fijo de 30s, 1m, 2m, 4m (`internal/jobs/cola.go:141-150`), y
Falabella y WooCommerce ni siquiera parsean el 429.
Falta que `Fallar` acepte un `run_at` mínimo tomado de `RetryAfter` y que el limitador de
cupo de esa cuenta se frene hasta esa hora.

### 13. Falabella: siete segundos para dar por bueno un feed que tarda minutos

Un feed que Seller Center procesa en minutos —lo normal cuando hay cola— se da por bueno a
los siete segundos; si luego lo rechaza (atributo obligatorio ausente, EAN duplicado), el
motor ya guardó el hash y ese producto, ese precio o esa bajada de stock no se vuelven a
encolar nunca, sin aparecer en ninguna alerta.
Hoy `enviarFeed` consulta el estado antes de dar la escritura por buena, pero el sondeo son
tres esperas de 1s, 2s y 4s (`falabella.go:45-50` y `997-1023`); si el feed sigue en cola,
`Rechazo()` devuelve cadena vacía (`falabella.go:854-882`), `resultadoDe` marca `OK=true`
(`falabella.go:1277-1285`) y el manejador sella el hash de contenido, precio o stock como
publicado (`internal/publicar/manejadores.go:234-256`); las columnas `last_feed_id` y
`last_feed_status` existen desde la migración 019 y no las escribe nadie.
Falta encolar un trabajo de verificación diferida del `FeedID` en vez de decidir dentro de
la llamada con siete segundos de margen.

### 14. Una publicación borrada o despublicada en el canal no se recrea jamás

Si MercadoLibre da de baja una publicación por infracción, o alguien la borra a mano en la
tienda, Integra sigue creyendo que está publicada: no la vuelve a crear y cada actualización
falla con 404 hasta agotar reintentos, así que el producto desaparece del canal y en Integra
sigue contando como publicado.
Hoy los adaptadores saben detectarlo —WooCommerce traduce el 404 a «eliminado»
(`woocommerce.go:269-279`), Shopify a «no_encontrado» (`shopify.go:311-318`)— pero nadie
llama a `FetchStatus` ni a `ListRemote` fuera de sus pruebas, y como los candidatos leen
`pcl.external_id` sin mirar el estado (`internal/store/publicacion.go:71`) y el manejador
nunca vuelve a crear si ese campo no está vacío (`manejadores.go:126-132`), la fila apunta
para siempre a una publicación muerta.
Falta un trabajo periódico de conciliación que llame a `FetchStatus` y, ante «eliminado»,
limpie el `external_id` para que el motor la republique.

### 15. Un producto archivado en Odoo sigue a la venta en los cuatro canales

La ficha sigue viva y vendible con la última cantidad conocida, congelada para siempre:
alguien compra un producto que la empresa retiró del catálogo y hay que cancelarle la venta
y comerse la penalización.
Hoy el sync sí lo detecta y lo marca inactivo (`internal/sync/catalogo.go:230` y `368-407`,
con pruebas propias), pero la única consecuencia es una línea de log que reconoce el
problema («sus publicaciones siguen abiertas en los canales y hay que pausarlas»,
`catalogo.go:250`): `Pause()` está en el contrato (`internal/channel/adapter.go:48`) e
implementado en los cuatro adaptadores y no lo llama nadie, no hay tipo de trabajo de pausa
(`internal/publicar/motor.go:22-26`), y la alerta de publicados sin stock no lo ve porque un
archivado conserva sus quants. Lo mismo ocurre por otra puerta cuando alguien borra el
`default_code`: el producto deja de ser candidato y la ficha se queda abierta.
Falta un trabajo de pausa que, cuando una variante publicada deja de ser candidata (baja o
SKU vacío), encole `Pause()` contra cada cuenta donde tenga publicación viva.

### 16. El worker se puede morir dos días sin que nadie se entere

Durante esos dos días no se sincroniza Odoo, no se publican precios ni stock y, sobre todo,
no entra ni un pedido de los cuatro canales: las ventas se quedan sin `sale.order` y sin
apartar stock, y se sigue vendiendo con el stock congelado del último sync.
Hoy el planificador —lo único que vigila y manda los correos— corre dentro del propio
proceso worker (`cmd/integra/main.go:1213-1231`), no hay latido en base de datos, la API no
comprueba si el worker vive, y `DESPLIEGUE.md:89` lo dice tal cual: «Si está caído, nada
ocurre solo».
Falta un latido del worker en base que la API vigile, y un aviso externo (cron del host o
servicio de uptime) que no dependa del proceso muerto.

### 17. No se puede configurar a dónde llegan los avisos

El módulo de alertas es hoy un panel que nadie mira: un token de MercadoLibre caducado,
pedidos sin montar o publicaciones rechazadas esperan a que alguien abra la interfaz por
casualidad.
Hoy la interfaz llama a `/api/avisos/destinos` (`web/src/api.ts:617-632`) y esa ruta no
existe en el backend —no aparece en ningún `mux.HandleFunc` de `internal/api/`— ni hay un
solo `INSERT INTO notification_destinations` en todo el repositorio, así que
`web/src/Avisos.tsx` apunta a un vacío y la última casilla de `DESPLIEGUE.md:152` no se
puede marcar.
Faltan los cuatro manejadores de `/api/avisos/destinos` y el guardado cifrado del destino
SMTP.

### 18. Las copias de seguridad viven en la máquina que se va a estropear

Si muere el disco se pierden a la vez la base y sus copias: el catálogo comercial —precios
propios de Integra, marcas, descripciones y atributos, que no están en Odoo— hay que
rehacerlo a mano, y las fotos recortadas y verificadas una a una se pierden, porque las de
Odoo son miniaturas que los marketplaces rechazan. Semanas de trabajo.
Hoy el contenedor `backup` vuelca a diario y hasta comprueba el archivo con `gzip -t`
(`docker-compose.prod.yml:68-104`), pero el destino es `./datos/backups` en la misma máquina
(línea 78) y el banco de imágenes vive en el volumen `imagenes_data`
(`docker-compose.yml:101-106`), que no entra en el volcado; ambas cosas figuran como
pendientes en `DESPLIEGUE.md:99-112`, con el comando de tar a mano.
Falta sacar `datos/backups` y el volumen de imágenes fuera de la máquina de forma
automatizada, no en un comando de la documentación.

### 19. No se audita ni un cambio de precio

Ante una discusión con MDV —«se vendieron 40 unidades a mitad de precio, ¿quién lo tocó?»—
la respuesta es que no consta: no hay forma de distinguir un error humano de un fallo del
motor de precios, ni de aprender de él.
Hoy `RegistrarAuditoria` solo se llama desde tres sitios, los tres en
`internal/api/auth.go`: login (:98), alta de usuario (:204) y edición de usuario (:284); no
se audita ningún cambio de precio, ni la edición masiva, ni las reglas, ni las ofertas, ni
las credenciales de canal, ni el borrado de una conexión de Odoo, aunque la consulta
`GET /api/auditoria` existe y está bien protegida (`internal/api/auditoria.go:19`).
Falta auditar toda escritura sobre precio, exclusión, credenciales y conexiones, guardando
el valor anterior y el nuevo.

---

## Lo que hace perder ventas y tiempo (grave)

### 20. Las reservas de stock se cierran por reloj, no por lo que pasa en Odoo

Tres escenarios, una misma causa. Si el pedido se despacha el día 2, Odoo ya bajó la unidad
y la reserva sigue restando encima cinco días más, así que los cuatro canales publican menos
de lo que hay y se deja de vender mercancía disponible; si el pedido no se despacha, al
octavo día las unidades vuelven a ofrecerse aunque sigan apartadas y aparecen dos compradores
para una unidad.
Hoy la reserva nace con `expires_at = now() + 7 días`
(`migrations/020_reservas_de_stock.sql:22-39`) y `ReaplicarReservasDeStock` la libera al
vencer con el motivo «vencida: se supone despachada en Odoo»
(`internal/store/ordenes.go:732-751`), mientras Integra solo vuelve a leer el `sale.order`
una vez, en el acto, para cuadrar importes (`internal/ordenes/ordenes.go:568-577`); el
propio esquema lo deja escrito como pendiente de decisión.
Falta cerrar el asiento con el hecho de Odoo —el albarán del `sale.order`— y avisar por
correo del asiento que caduca sin haberse despachado, en vez de liberarlo en silencio.

### 21. La sobreventa se absorbe callando

Hay un pedido cobrado que no se puede despachar y nadie se entera hasta que el de bodega no
encuentra la mercancía, con el comprador esperando; es el caso más caro en marketplace,
porque la cancelación la paga el vendedor en reputación.
Hoy el reparto por bodegas nunca baja de cero y descarta en silencio lo que no alcanza
(`internal/store/ordenes.go:634-663`), y el llamador solo escribe una línea de log sin
comparar lo apartado con lo vendido (`internal/ordenes/ordenes.go:239-244`).
Falta levantar una alerta cuando lo descontado no cubre lo vendido, con el pedido, el SKU y
las unidades que faltan, y marcar el pedido.

### 22. El stock real de Odoo solo se relee una vez al día

Una rotura de 5 unidades registrada en Odoo a las nueve de la mañana se sigue ofreciendo en
los cuatro canales hasta la corrida nocturna: todo lo que se venda ese día es mercancía que
no existe.
Hoy `variant_stock` solo cambia dentro del sync completo
(`internal/sync/catalogo.go:267-269`), que dispara el horario de alcance «full»
(`internal/planificador/planificador.go:154-161`), y un horario de alcance «stock» no relee
Odoo: solo replanifica sobre lo que ya hay en la base (`planificador.go:163-166`).
Falta un sync de stock frecuente e independiente del sync completo del catálogo.

### 23. Se publica el stock total, no el libre

Las 20 unidades apartadas en Odoo para un cliente mayorista se ofrecen otra vez en los
cuatro marketplaces: se vende dos veces la misma mercancía y alguien tiene que decidir a
quién le falla.
Hoy el sync ya calcula el libre y lo guarda en `variant_stock.qty_free`
(`internal/sync/catalogo.go:527-530`), pero lo que se publica es `qty_on_hand`
(`internal/store/publicacion.go:91`) y `qty_free` no lo lee ningún sitio del código.
Falta publicar sobre `qty_free`, que ya está calculado y guardado.

### 24. Si el proceso se cae entre guardar el pedido y apartar su stock

Un pedido cobrado deja de restar stock en los cuatro canales y nadie lo sabe: la unidad se
vuelve a vender sin que quede rastro del motivo.
Hoy el descuento va después de `GuardarOrden`, en otra transacción y solo cuando la fila es
nueva (`internal/ordenes/ordenes.go:229-243`); si el worker muere en medio, el reintento
encuentra `nuevo=false` (`internal/store/ordenes.go:124`) y el bloque queda saltado para
siempre, sin ninguna pasada de reconciliación que lo detecte.
Falta una pasada periódica que aparte stock de todo pedido vivo sin asientos abiertos, igual
que la red de seguridad que ya existe para el montaje en Odoo.

### 25. Un pedido con un SKU que aún no está en el catálogo no aparta stock nunca

El caso normal se resuelve solo, pero las unidades de ese pedido se siguen ofreciendo en los
otros canales aunque el pedido acabe montándose en Odoo: la misma mercancía se vende otra
vez.
Hoy la línea se guarda con `variant_id` nulo (`internal/store/ordenes.go:128-141`) y el
descuento solo recorre las líneas con variante (`ordenes.go:573-575`); cuando el SKU
aparece, `ReemparejarLineasHuerfanas` rescata el pedido (`ordenes.go:204-241`) pero no
vuelve a apartar stock, y el corte de idempotencia por número de asientos
(`ordenes.go:559-565`) bloquearía el intento; además el rescate exige `v.active`, así que si
la variante se archivó el pedido queda fallido para siempre.
Falta apartar stock por línea y no por pedido, hacerlo también al reemparejar, y emparejar
contra variantes inactivas dejando constancia.

### 26. En Shopify, una cancelación posterior a la ingesta es invisible

Las unidades siguen apartadas hasta que la reserva vence a los siete días, ningún canal
vuelve a ofrecerlas mientras tanto, y en Odoo queda el borrador de una venta que ya no
existe y que nadie sabe que hay que anular.
Hoy el conector descarta el pedido cancelado dentro de `FetchOrders`, antes de que el núcleo
lo vea —`if o.Test || o.CancelledAt != nil { continue }`,
`internal/conectores/shopify/shopify.go:478-483`— y el filtro se aplica a todos los pedidos
de la página, no solo a los nuevos.
Falta que el conector entregue el pedido cancelado con `Status = "cancelled"` en vez de
descartarlo, dejando el filtro de pruebas como único descarte.

### 27. Un reembolso repone stock que ya salió por la puerta

Un reembolso posterior a la entrega devuelve al catálogo publicable una unidad que ya está
en casa del comprador: se publica y se vende stock fantasma, y la corrección solo llega en
el siguiente sync completo.
Hoy `esCancelado` trata `refunded` y `voided` como cancelación
(`internal/ordenes/ordenes.go:314-321`) y `DevolverStockReservado` suma la cantidad de
vuelta a `variant_stock` (`internal/store/ordenes.go:702-712`) sin comprobar si la mercancía
llegó a salir.
Falta no reponer stock en un reembolso sin confirmar antes que la devolución física entró en
Odoo.

### 28. Las devoluciones y los reembolsos parciales no existen

El comprador devuelve una de tres unidades: se le reembolsa por fuera, la unidad vuelve
físicamente a la bodega y para Integra sigue vendida, mientras el pedido de Odoo sigue
diciendo tres unidades cobradas; al revés, si el canal reduce la cantidad, lo devuelto sigue
apartado hasta que el asiento caduque.
Hoy `esCancelado` deja fuera los reembolsos parciales a propósito y hay una prueba que fija
ese comportamiento (`internal/ordenes/ordenes.go:320-329`,
`internal/ordenes/ordenes_test.go:163`), `GuardarOrden` no vuelve a tocar líneas ni
cantidades de un pedido existente (`internal/store/ordenes.go:100-126`) y la devolución de
stock es todo o nada por pedido (`ordenes.go:672-714`).
Falta un camino de devolución que reduzca las líneas, devuelva el stock de lo devuelto y
deje constancia para quien maneje la nota de crédito en Odoo.

### 29. Un pedido sin pagar aparta stock una semana

Una compra no pagada retira unidades reales de los otros tres canales durante siete días y
llega a Odoo como borrador de venta; si el pago nunca entra, se perdió una semana de venta y
queda un borrador falso que alguien tiene que limpiar.
Hoy la ingesta no mira el estado de pago para decidir: guarda, descuenta y encola el montaje
de todo lo que sea nuevo y no esté cancelado (`internal/ordenes/ordenes.go:220-253`), y los
conectores entregan tal cual estados como `pending`, `on-hold`, `authorized` o
`payment_required` (`woocommerce.go:415`, `shopify.go:800`, `mercadolibre.go:733`).
Falta una regla explícita de qué estados de pago dan una venta viva en cada canal, dejando
en «recibido» y sin apartar stock lo que aún no está pagado.

### 30. Si MDV cancela el pedido en Odoo, el canal no se entera

El comprador espera un paquete que no va a llegar y el marketplace cuenta la cancelación
tardía contra la reputación del vendedor; alguien tiene que acordarse de entrar al Seller
Center a cancelarlo a mano.
Hoy el flujo es de una sola dirección: lo único que Integra vuelve a leer de Odoo tras crear
el pedido son el total y la moneda, en el acto y solo para contrastar
(`internal/ordenes/ordenes.go:568-577`), y `channel_orders` no cambia nunca de estado por
algo que pase en Odoo.
Falta leer el estado del `sale.order` en cada sincronización y, cuando esté cancelado,
avisar y decidir si se cancela también en el canal.

### 31. El comprador cambia la dirección y se despacha a la vieja

La guía sale a donde el comprador ya no está, el paquete vuelve y hay que pagar el reenvío y
aguantar el reclamo; si lo que cambió fue una línea o el importe, Odoo factura algo distinto
de lo que cobró el canal.
Hoy, cuando el pedido ya existe, `GuardarOrden` solo refresca el estado del canal y la carga
cruda: dirección, comprador, importes y líneas se dejan como se ingirieron, y está escrito
así a propósito (`internal/store/ordenes.go:99-126`).
Falta refrescar dirección e importes mientras el pedido no esté montado en Odoo, y avisar
cuando cambien después de montarlo.

### 32. El cliente que repite compra se despacha a su dirección antigua

El segundo pedido de un cliente recurrente va a la dirección del primero, y si aquel
contacto nació sin documento fiscal sigue sin él para siempre, así que nunca se le puede
emitir factura electrónica sin completarlo a mano.
Hoy `resolverCliente` busca el `res.partner` por correo y, si lo encuentra, lo devuelve tal
cual sin escribir nada: la dirección, el teléfono y el documento nuevos solo se usan en la
rama de creación (`internal/ordenes/ordenes.go:645-654` y `662-730`).
Falta actualizar el partner encontrado con los datos del pedido, o crear una dirección de
entrega hija.

### 33. Falabella no trae correo ni documento: contactos duplicados y sin NIT

Cada pedido de Falabella crea un contacto nuevo en Odoo aunque sea el mismo comprador, y
todos nacen sin NIT o cédula, así que ninguno se puede facturar electrónicamente —obligatorio
en Colombia— y alguien tiene que completar el contacto a mano antes de confirmar cada
borrador.
Hoy el `ordenResp` de Falabella solo lee nombre, teléfono y dirección de envío
(`falabella.go:511-527`), `resolverCliente` solo busca partner existente si hay correo
(`internal/ordenes/ordenes.go:645-654`), sin documento se registra un warning y el contacto
nace sin `vat` (`ordenes.go:687-694`) y sin nombre se llama «Comprador FALABELLA».
Falta leer el documento del comprador —viene en la dirección de facturación del pedido— y,
sin correo, buscar el partner por documento antes de crear otro.

### 34. La moneda está escrita a mano: todo es peso colombiano

Abrir una tienda que no facture en pesos, o una cuenta de MercadoLibre de otro país,
publicaría cifras de pesos como si fueran dólares: se vendería a 4.000 veces el precio o a
la cuatromilésima parte, y nada lo detectaría antes de la primera venta.
Hoy la moneda se fija en dos sitios sin leerla nunca de la cuenta
(`internal/store/precios.go:451` e `internal/store/publicacion.go:120`) y el redondeo asume
pesos (`math.Ceil(precio/100)*100` en `internal/pricing/resolucion.go:140` y `:254`, y en
`internal/store/canales.go:66`); MercadoLibre publicaría `COP` en un site que factura en
otra moneda, y WooCommerce y Shopify ni siquiera mandan moneda.
Falta guardar la moneda en `channel_accounts`, propagarla hasta el precio efectivo y hacer
que el redondeo dependa de ella.

### 35. El override no compensa la comisión y el de al lado sí

Quien fija un override de 100.000 en MercadoLibre recibe unos 84.000 tras comisión, no
100.000, mientras el producto vecino con precio calculado sí compensa: dos productos de la
misma cuenta se comportan distinto y el override puede quedar por debajo del coste sin que
nada chille.
Hoy el override sale por un `return` temprano y se publica tal cual, sin pasar por
`compensarComision` ni por el suelo de margen mínimo
(`internal/pricing/resolucion.go:112-123`).
Falta decidir y dejar explícito si el override es precio al público o precio neto, y en el
segundo caso pasarlo por la compensación y por el margen mínimo.

### 36. Un precio a cero congela el producto en silencio

Lo bueno: no se publica a cero, no se regala. Lo malo: el producto se queda en el canal con
su precio y su stock viejos, sin ningún aviso, y sigue vendiéndose hasta agotar existencias
inexistentes.
Hoy solo se rechazan los negativos (`internal/store/edicion.go:14-16`,
`internal/store/masivo.go:331-333`); con precio cero el candidato queda `Listo=false`
(`internal/store/publicacion.go:128-129`) y `Planificar` lo salta entero, sin mandarle
precio ni stock (`internal/publicar/motor.go:67-70`), mientras la cola de atención tampoco
lo ve porque `missing_price` comprueba `IS NULL`, no cero (`edicion.go:216-219`).
Falta tratar el precio cero como falta de precio en la cola de atención y avisar de los
publicados que dejaron de estar listos.

### 37. La edición masiva lee mal los decimales

Un precio fijo tecleado a la americana multiplica por cien el precio de hasta 2000 productos
de una vez, y como la vista previa enseña solo cinco filas, el catálogo entero puede salir al
canal a cien veces su precio.
Hoy `aFloat` borra todos los puntos y convierte las comas en punto
(`internal/store/masivo.go:325-334`), mientras la plantilla de Excel sí resuelve la
ambigüedad mirando qué separador va más a la derecha
(`internal/plantilla/plantilla.go:477-515`): las dos vías de carga masiva interpretan el
mismo texto de forma distinta.
Falta que la edición masiva use el mismo lector de números que la plantilla.

### 38. La comisión es un solo número por canal

Con una comisión media, los productos de las categorías caras se publican demasiado baratos
y se venden con menos margen del previsto, y los de las categorías baratas se publican caros
y no se venden; el error no se ve de golpe, se reparte en cada venta.
Hoy la comisión cuelga de `channels`
(`migrations/014_comisiones_y_verificacion.sql:13-14`) y tanto los candidatos
(`internal/store/publicacion.go:52-59`) como el recálculo
(`internal/store/precios.go:440-452`) leen ese único valor para todo el catálogo, aunque el
comentario de la propia migración reconoce que las comisiones reales dependen de la
categoría y del contrato.
Falta una comisión por categoría y por cuenta, aunque sea una tabla de tramos que alimente
la compensación.

### 39. Las promociones llegan como precio bajo, no como rebaja

El comprador no ve que es una rebaja: sin precio tachado no hay gancho, que es justo para lo
que se hace una promoción; y la vuelta al precio normal depende de que el planificador
corra, así que si el proceso está caído el viernes se sigue vendiendo rebajado.
Hoy el contrato tiene `SalePrice`, `SaleStartsAt` y `SaleEndsAt`
(`internal/channel/adapter.go:161-166`) y los cuatro adaptadores los implementan, pero el
núcleo nunca los rellena: `internal/store/publicacion.go:69` aplasta las dos cifras en una
con un `COALESCE` y los manejadores mandan solo el precio regular
(`internal/publicar/manejadores.go:229` y `:291`).
Falta llevar precio regular y oferta separados desde `effective_prices` hasta el envío, con
las fechas, usando las capacidades que ya declaran los adaptadores.

### 40. Falabella conserva para siempre un precio especial puesto a mano

Una rebaja que alguien puso en el Seller Center se queda viva indefinidamente: Integra sube
el precio normal, Falabella lo guarda y sigue cobrando el especial antiguo, mientras en
Integra el precio figura como publicado correctamente.
Hoy `SpecialPrice` solo se rellena si hay oferta y el campo lleva `omitempty`
(`falabella.go:326-332` y `:135-137`), así que la etiqueta no viaja y Falabella no borra lo
que no le mandas; WooCommerce y Shopify sí limpian de forma explícita
(`woocommerce.go:220-226`, `shopify.go:1131-1136`).
Falta mandar `SpecialPrice` vacío cuando no hay oferta, como ya hacen los otros dos.

### 41. Nadie comprueba qué precio y qué stock tiene el canal de verdad

Un precio tocado a mano en la tienda o metido por el canal en una campaña se queda ahí
semanas, y un envío de stock que se perdió o se aplicó a medias no lo descubre nadie: el
hash dice que está enviado y el canal vende con otra cifra hasta que llega un pedido que no
se puede despachar.
Hoy `FetchStatus` está en el contrato (`internal/channel/adapter.go:50`) y los cuatro
adaptadores devuelven el precio y la cantidad vivos, pero no lo llama ningún sitio del
núcleo; `published_price` se escribe (`internal/store/publicacion.go:298-304`) y no se lee
jamás, y el motor decide comparando su hash contra lo que creía haber mandado
(`internal/publicar/motor.go:98`).
Falta una pasada periódica que compare precio y cantidad vivos contra lo publicado, con
alerta y reencolado de las diferencias.

### 42. Un refresco de token borra el resto de la credencial de MercadoLibre

En el primer refresco automático —que ocurre solo, cada seis horas— se pierden el `user_id`
y el `webhook_secret` de la cuenta: la verificación del webhook deja de comprobar que el
aviso sea de nuestro vendedor y el enlace de rastreo desaparece, sin que nadie se entere
porque la publicación sigue funcionando.
Hoy el adaptador arma el juego de credenciales con exactamente cuatro claves
(`mercadolibre.go:1191-1194`) y `PersistCredentials` reemplaza el blob cifrado entero
(`internal/conectores/cuenta.go:37-47` → `internal/store/cuentas.go:123-134`), mientras la
cuenta guarda además `user_id`, `webhook_secret` y `url_seguimiento`
(`internal/conectores/probar.go:39`, `:60`, `:65`).
Falta que `PersistCredentials` mezcle sobre el juego existente en vez de reemplazarlo.

### 43. Se reintentan cinco veces errores que nunca se van a arreglar

Cada producto mal formado gasta cinco llamadas de escritura antes de rendirse: con un lote
de altas donde falla un atributo obligatorio, eso multiplica por cinco el consumo de cupo
justo en la ventana en que queríamos publicar, y empuja al 429 a las publicaciones que sí
eran correctas.
Hoy `channel.EsReintentable` existe y está bien escrito
(`internal/channel/adapter.go:343-365`) y no lo llama nadie: el worker trata todo error
igual (`internal/jobs/worker.go:187-193`) y la cola reintenta cinco veces sin mirar el tipo,
de modo que un 500 pasajero y un 400 permanente acaban idénticos en el panel.
Falta que el worker consulte `EsReintentable` y marque como fallido de inmediato lo que no
es reintentable.

### 44. En WooCommerce y Shopify, un 200 que no aplicó nada

Basta un plugin de precios, un producto en borrador o el control de stock desactivado para
que la tienda acepte la petición y no aplique nada: Integra guarda el hash como publicado y
no lo vuelve a enviar, así que la tienda vende indefinidamente al precio y al stock viejos.
Hoy Falabella detecta el 200 con error en el cuerpo (`falabella.go:1251-1267`) y MercadoLibre
comprueba el warning y compara el precio devuelto con tolerancia de medio centavo
(`mercadolibre.go:245-252` y `268-276`), pero WooCommerce manda el PUT de precio y de stock
sin mirar la respuesta (`woocommerce.go:226-227` y `244-245`) y Shopify igual en el precio
(`shopify.go:260-262`).
Falta releer del cuerpo el precio y la cantidad aplicados y compararlos antes de guardar el
hash, como ya hace MercadoLibre.

### 45. Un canal colgado congela a los otros tres

Una tienda WooCommerce que no cierra la conexión ocupa los ocho huecos treinta segundos por
intento, de modo que MercadoLibre, Falabella y Shopify dejan de recibir stock mientras dure
la caída: un canal roto congela el stock de los demás, y ahí sí se vende lo que no hay.
Hoy el worker tiene un único juego de huecos global (`internal/jobs/worker.go:87-138`) con
concurrencia 8 por defecto (`internal/config/config.go:62`), reclama de la cola sin mirar de
qué cuenta es cada trabajo (`internal/jobs/cola.go:103-129`) y el plazo del cliente HTTP de
cada adaptador es de 30 segundos.
Falta acotar cuántos huecos puede ocupar una misma cuenta, o repartir el reclamo de la cola
por cuenta.

### 46. La caída de una tienda no se detecta hasta que alguien pulse «probar»

La tienda se cae un domingo y lo único que aparece es un contador genérico de trabajos que
agotaron reintentos: nadie sabe qué canal es ni desde cuándo, y la alerta de conexión sigue
diciendo que todo va bien porque se probó hace tres semanas.
Hoy el aviso depende de `c.ProbadaOK`, que es el resultado de la última prueba manual
(`internal/planificador/planificador.go:241-247`), y la única forma de correr esa prueba es
que una persona pulse el botón (`POST /api/cuentas/{id}/probar`,
`internal/api/server.go:728-781`).
Falta que el planificador ejecute la prueba de conexión de cada cuenta en cada pasada y
anote el resultado.

### 47. Los requisitos de categoría solo se refrescan desde la consola

Cuando MercadoLibre añade un atributo obligatorio a una categoría —lo hace varias veces al
año—, la comprobación local sigue diciendo que no falta nada, se manda la publicación y ML
la rechaza con un 400 opaco: las altas de esa categoría dejan de entrar y el motivo real no
aparece por ninguna parte.
Hoy Integra comprueba los obligatorios contra su propia tabla
(`internal/publicar/manejadores.go:112-124`), que se llena con `atributos.Refrescar`
(`internal/atributos/refrescar.go:41-60`), y esa función tiene un único invocador: un
comando de consola (`cmd/integra/main.go:889-922`).
Falta refrescar los requisitos de categoría por horario, no solo a mano.

### 48. Shopify retira su versión de API cada seis meses

El día que caduque `2026-07` no salta ningún error: Shopify sirve las peticiones con el
comportamiento de otra versión y lo que se nota es que campos que antes viajaban dejan de
aplicarse —precios o stock que Integra da por publicados y la tienda no aplica—, descubierto
por un cliente que compra al precio equivocado.
Hoy la versión está en una constante con su fecha de vencimiento escrita a mano y el riesgo
bien explicado (`shopify.go:27-37`), pero nada lee la cabecera
`X-Shopify-API-Deprecated-Reason` que Shopify manda en cada respuesta, ni hay alerta al
acercarse la fecha, y cambiarla exige recompilar.
Falta leer esa cabecera y levantar alerta, y avisar cuando falten menos de dos meses para el
vencimiento.

### 49. Mover una variante a otra plantilla en Odoo descabala el producto

Se acaba con dos variantes activas del mismo SKU —con toda la factura del punto 4— y a la
vez el producto pierde precio, descripción e imágenes, así que deja de publicarse y su ficha
viva se congela con el precio y el stock del día del cambio; reponer a mano ese trabajo
comercial son horas.
Hoy la unicidad es por plantilla (`migrations/002_catalog.sql:30` y `:70`), de modo que un
`product_tmpl_id` nuevo crea filas nuevas y la vieja se queda
(`internal/store/store.go:303-343`), el mapa por `odoo_product_id` se queda con una
arbitraria (`store.go:348-370`) mientras el sync marca activas las dos
(`catalogo.go:566-571`), y todo lo que es propiedad de Integra cuelga de la fila vieja.
Falta detectar en el sync que un `odoo_product_id` conocido cambió de plantilla y trasladar
la fila (existe `MigrarTrabajo` por SKU, pero solo entre conexiones y a mano).

### 50. Un producto con 200 variantes publica una sola

Solo se publica una talla; las otras 199 fallan trabajo tras trabajo hasta agotar reintentos
y disparan la alerta genérica sin decir que la causa es el multivariante, y el producto queda
a la venta sin forma de distinguir las demás.
Hoy el estado de la publicación se lee de una tabla que es una fila por producto y cuenta
(`internal/store/publicacion.go:71`), así que la segunda variante se desvía a actualizar una
ficha que no existe y devuelve «la variante N no está publicada»
(`manejadores.go:130-132`, `publicacion.go:292`); `Capabilities.Variants` está declarado en
los cuatro adaptadores y no lo consulta nadie, y `product_variants.attributes` nunca se
rellena desde Odoo (`catalogo.go:192-201`).
Falta sincronizar los valores de atributo de la variante y que el motor agrupe por producto
en los canales que declaran variantes en la publicación.

### 51. Dos sincronizaciones solapadas hunden el stock

El stock publicado baja el doble de lo vendido y sigue hundiéndose cada vez que las pasadas
se cruzan: unidades que están en la bodega dejan de ofrecerse en los cuatro canales sin que
ningún panel lo explique; la otra rama del cruce corta el sync a medias con un error de
clave duplicada.
Hoy nada lo impide: `POST /api/sincronizar` lanza una goroutine por petición sin comprobar
si ya hay una en marcha (`internal/api/server.go:1079-1097`), el planificador dispara la
suya (`planificador.go:152-162`), el único bloqueo del proyecto es el de las migraciones, y
la reaplicación de reservas solo es segura porque va inmediatamente detrás del reemplazo de
stock (`internal/store/ordenes.go:739-748`, `internal/sync/catalogo.go:269-282`).
Falta un bloqueo por conexión de Odoo alrededor del sync, y que el endpoint responda «ya hay
una sincronización en curso» en vez de lanzar otra.

### 52. Una lectura parcial de Odoo da de baja medio catálogo

Si una regla de registro esconde 300 de 600 productos al usuario del sync, esos 300 se dan
de baja en una sola pasada: media empresa deja de recibir precio y stock con las fichas
todavía vivas vendiendo cantidades congeladas, y el único aviso es una línea de log.
Hoy hay red, pero solo contra el caso total: el guardián aborta si no sobrevive ninguno
(`internal/sync/catalogo.go:398-405`, con prueba propia), y no existe ningún umbral relativo.
Falta ese umbral: si una pasada da de baja más de un porcentaje del catálogo activo, abortar
y levantar alerta en vez de aplicarlo.

### 53. Una bodega nueva se suma sola al stock publicado

El día que alguien cree una bodega en Odoo, su inventario entra automáticamente en el stock
de todas las cuentas sin bodegas asignadas: se ofrecen en MercadoLibre las unidades
consignadas en Falabella, o las de garantías y muestras, y son ventas que hay que cancelar.
Hoy la sincronización de almacenes hace upsert sin filtro y nunca borra
(`internal/sync/catalogo.go:448-463`, `internal/store/store.go:257-273`), y la regla por
defecto es la contraria a la prudente: sin asignación, cuentan todas
(`internal/store/publicacion.go:90-98`).
Falta que una bodega nueva no cuente hasta que alguien la asigne, o al menos una alerta
cuando aparezca una desconocida.

### 54. El coste no se vuelve a sincronizar nunca

Se puede publicar por debajo del coste real sin enterarse, porque el guardián de margen
mínimo compara contra un coste de hace meses; y los productos que entraron en Odoo después
de la migración 012 tienen coste nulo para siempre, así que el tarifado masivo los salta en
silencio y hay que ponerles precio uno a uno.
Hoy la migración lo dice explícitamente («dejan de actualizarse»,
`migrations/012_propiedad_integra.sql:8-10`) y el sync no lee `standard_price`
(`internal/sync/catalogo.go:161`), mientras el coste congelado se sigue usando en la
operación masiva «precio = coste × factor» (`internal/store/masivo.go:126-136`), en el
margen mínimo (`internal/pricing/resolucion.go:145-155`) y en el orden de las categorías.
Falta resincronizar el coste como dato de referencia, o dejar de usarlo en el margen mínimo
y en el tarifado masivo.

### 55. Desmarcar «se puede vender» en Odoo no retira nada

Quien desmarca `sale_ok` —el gesto natural para retirar algo de la venta sin borrarlo— cree
que ya no se vende, y el producto sigue publicándose y recibiendo precio y stock en los
cuatro canales.
Hoy `products.sale_ok` e `is_storable` existen con valor por defecto verdadero
(`migrations/002_catalog.sql:21-22`) y nada los escribe: el sync no pide esos campos
(`internal/sync/catalogo.go:161`) y solo los consulta el explorador de diagnóstico.
Falta leer `sale_ok` en el sync y tratarlo como una baja, o al menos como bloqueo en la cola
de atención.

### 56. Un aviso se manda una vez y luego silencio

Un token de Falabella que caducó el lunes genera un correo el lunes y nada más el resto de
la semana: si ese correo cayó en spam o el operador estaba de baja, la cuenta lleva siete
días sin publicar y nadie lo sabe.
Hoy las alertas sin notificar se filtran por `notified_at IS NULL`
(`internal/store/notificaciones.go:80`), el cuerpo del correo lo dice
(`notificaciones.go:177`) y la creación deduplica mientras la alerta siga sin reconocer
(`internal/store/horarios.go:230-238`), así que tampoco se genera una nueva.
Falta un reenvío periódico, o un resumen diario, de lo crítico que siga abierto sin
reconocer.

### 57. Perder o rotar la clave maestra

Perderla obliga a volver a pedir a MDV las credenciales de Odoo y de las cuatro cuentas y
reintroducirlas a mano, sin publicar ni ingerir nada hasta entonces; y como esa misma clave
firma las sesiones, cambiarla echa a todo el mundo fuera de la interfaz a la vez.
Hoy no hay ningún subcomando de rotación ni recifrado (`cmd/integra/main.go:64-104`) y la
clave de firma de sesión es la clave maestra (`internal/api/auth.go:29-34`); el riesgo está
documentado en `DESPLIEGUE.md:28-30`, pero solo como advertencia.
Falta un subcomando que recifre credenciales con clave nueva, y separar la clave de firma de
sesiones de la maestra.

### 58. El volcado nocturno puede fallar todas las noches en silencio

Se descubre que no hay copias el día que hacen falta, y entre el fallo y el incidente pueden
pasar semanas: para entonces el mensaje de error ya rotó fuera del log.
Hoy el script imprime el error y borra el archivo (`docker-compose.prod.yml:99-102`) y luego
duerme un día: no escribe en la tabla de alertas, no manda correo y no toca nada que Integra
vigile.
Falta registrar en Integra el resultado de cada copia y levantar alerta si no hay una buena
en 48 horas.

### 59. Restaurar una copia deja precios viejos y reservas perdidas

No hay publicaciones ni pedidos duplicados, pero al primer ciclo los cuatro canales reciben
los precios de hace una semana sobre productos reprecificados desde entonces —se vende
barato lo que se había subido— y el stock apartado por ventas recientes vuelve a ofrecerse.
Hoy lo peligroso está bien resuelto: los cuatro adaptadores adoptan la publicación existente
por SKU en vez de duplicarla, y el montaje en Odoo busca por `client_order_ref` y adopta el
pedido que ya exista (`internal/ordenes/ordenes.go:370-377`), pero el precio es propiedad de
Integra desde la migración 012 y los asientos de reserva desaparecen con la restauración.
Falta un procedimiento de post-restauración: recalcular precios y reconstruir reservas desde
los pedidos antes de dejar publicar.

### 60. El disco se llena de imágenes que nadie borra

Cuando el disco se llena, PostgreSQL deja de escribir: se paran las publicaciones y, sobre
todo, deja de entrar cualquier pedido de los cuatro canales, y recuperar exige entrar por
SSH a borrar a mano.
Hoy el almacén sabe borrar (`internal/imagen/almacen.go:104-114`) pero esa función no se
llama desde producción —quitar una foto en la interfaz solo elimina la fila
(`internal/store/imagenes.go:153-185`)— y la búsqueda masiva recorre el catálogo entero
descargando hasta cinco fotos por producto (`internal/api/masivo.go:49-100`), sin cuota ni
recuento de espacio.
Falta un barrido que borre del disco las imágenes sin ninguna referencia, y una alerta de
espacio libre.

### 61. La retención de llamadas a la API está configurada y no se aplica

La base engorda con cuerpos JSON completos de cada petición y respuesta hasta reventar el
mismo disco que guarda las fotos, arrastrando consigo la ingesta de pedidos, mientras la
configuración da falsa sensación de estar cubierto.
Hoy `INTEGRA_API_CALL_RETENTION_DAYS` existe y se documenta como «la tabla que más crece con
diferencia» (`internal/config/config.go:34-36` y `:64`) y no se lee en ningún otro sitio: no
hay un solo borrado programado sobre `channel_api_calls`, `audit_logs` ni `jobs`.
Falta una purga periódica que use de verdad ese valor, extendida a los trabajos terminados.

### 62. No hay botón de pánico

Una edición masiva equivocada seguida de un «planificar» empuja precios erróneos a las
cuatro tiendas en minutos, y quien se da cuenta a los treinta segundos no tiene nada que
pulsar: hay que entrar a PostgreSQL a mano mientras las publicaciones siguen saliendo.
Hoy la edición masiva sí tiene frenos —simulación, tope de 2000 productos, omitir el
producto sin coste (`internal/store/masivo.go:69-134`)— pero
`POST /api/cuentas/{id}/planificar` encola sin límite ni confirmación
(`internal/api/server.go:663-680`), lo abre cualquier operador y no existe ninguna ruta ni
subcomando para vaciar o cancelar la cola.
Falta un botón que pause la cola o cancele los trabajos pendientes de una cuenta.

### 63. `/healthz` responde «ok» con la base caída

La sonda que se le enseña al cliente como prueba de que todo va bien responde «ok» mientras
no entra ni un pedido, y se pierden horas antes de mirar en el sitio correcto.
Hoy son dos líneas que escriben una constante (`internal/api/server.go:183-185`), en el
compose solo `postgres` tiene healthcheck (`docker-compose.yml:20-24`) —así que
`restart: unless-stopped` solo actúa si el proceso muere del todo— y `DESPLIEGUE.md:68`
propone comprobar el despliegue justo con esa ruta.
Falta que `/healthz` haga ping a la base y declare el estado de la cola, y healthchecks para
`api` y `worker` en el compose.

---

## Lo que hay que corregir sin urgencia (importante)

### 64. Las promociones empiezan cinco horas antes de lo que cree el operador

La promoción entra en vigor a las 19:00 del jueves y termina a las 19:00 del día de fin:
cinco horas de descuento regaladas por delante y cinco horas de venta a precio normal
perdidas por detrás, en cada promoción del año.
Hoy las fechas de la plantilla se interpretan en la zona del proceso
(`internal/plantilla/plantilla.go:535` y `:545-547`) y el contenedor no fija `TZ`, así que
en Alpine eso es UTC; la zona de Bogotá está en la configuración pero solo se usa para los
horarios de sincronización.
Falta interpretar las fechas de la plantilla en la zona configurada, o fijar `TZ` en los
contenedores.

### 65. El flete no llega a Odoo

El abono que hace el marketplace no cuadra nunca con la suma de los `sale.order`: falta el
flete de cada pedido, y quien concilia el banco tiene que reconstruirlo a mano pedido a
pedido.
Hoy el flete llega del canal y se guarda en la orden, pero las líneas del `sale.order` son
solo producto (`internal/ordenes/ordenes.go:407-443`) y la verificación compara contra la
suma de líneas, no contra el total del canal (`:581-586`), de modo que el descuadre nunca
dispara alerta y solo queda un warning en el log (`:601-609`).
Falta decidir con qué producto de servicio se factura el flete y, mientras tanto, convertir
ese warning en alerta visible con el importe.

### 66. La comisión que el canal cobra de verdad no se captura

Nadie sabe el margen real de una venta: si MercadoLibre cobra 16,5% donde Integra supuso
14%, el precio publicado lleva meses corto y no hay ningún dato en el sistema que lo delate.
Hoy el pedido normalizado tiene total, flete e impuesto pero ningún campo de comisión
(`internal/channel/adapter.go:265-282`), y el lector de MercadoLibre ignora el `sale_fee` de
las líneas y calcula el total como precio por cantidad
(`internal/conectores/mercadolibre/mercadolibre.go:751-763`).
Falta guardar la comisión real que informa cada canal en la línea del pedido y compararla
con la configurada.

### 67. Una regla de precio mal tecleada no tiene ni validación ni simulacro

Una regla con el signo o la magnitud equivocados reprecia de golpe todas las variantes de una
marca o categoría —al doble o a cero— sin que nadie vea antes qué va a pasar.
Hoy solo se valida el tipo de ajuste, no el valor
(`internal/store/precios.go:92-95`), la migración no pone restricción sobre el valor
(`migrations/003_pricing.sql:74-75`), la regla se aplica multiplicando sin más
(`internal/pricing/resolucion.go:239-243`) y, a diferencia de la edición masiva y de la
plantilla, no hay vista previa.
Falta validar el rango del ajuste y enseñar su efecto sobre una muestra antes de guardar.

### 68. La vista previa enseña un precio que no es el que se publica

La pantalla de «así va a quedar en MercadoLibre» muestra un precio y el canal recibe otro en
cuanto ese producto tiene override, regla de marca o promoción: se aprueba una ficha creyendo
que se revisó el precio.
Hoy la proyección calcula el precio solo con la comisión
(`internal/api/server.go:317` → `internal/store/canales.go:61-67`), sin consultar
`effective_prices`, ni los overrides, ni las reglas, ni las ofertas vigentes, que son
justamente lo que usa la publicación real (`internal/store/publicacion.go:69`).
Falta que la proyección lea el precio efectivo de la cuenta en vez de recalcular la comisión.

### 69. Un stock negativo en una bodega se publica tal cual

MercadoLibre y Falabella rechazan la cantidad negativa y el producto se queda con la
cantidad anterior a la venta —sigue vendiéndose—, WooCommerce la acepta y muestra un stock
absurdo, y además el negativo de una bodega resta del total de las demás.
Hoy la lectura suma sin recortar (`internal/sync/catalogo.go:519-523`), el guardado conserva
el negativo (`internal/store/store.go:404-406`) y la publicación no aplica ningún mínimo
(`internal/store/publicacion.go:70`, `:91`).
Falta recortar a cero el stock publicable, bodega por bodega.

### 70. El sync puede pisar un descuento de stock en curso

Una venta puntual pierde su reserva y sus unidades siguen a la venta en los cuatro canales
hasta que alguien lo note; la ventana es estrecha, pero cae en el momento de más trabajo
—sync nocturno con webhooks entrando—.
Hoy el reemplazo de stock (`internal/store/store.go:384-416`) y la reaplicación de reservas
(`internal/store/ordenes.go:732-751`) no comparten transacción con el descuento, así que si
el reemplazo confirma mientras el descuento espera, este no ve filas donde apartar.
Falta que el reemplazo y la reaplicación ocurran en una sola transacción, y que el descuento
reintente si se quedó sin filas.

### 71. El colchón de seguridad por canal existe y no lo lee nadie

No hay forma de decir «no publiques las últimas dos unidades en MercadoLibre», que es justo
la cantidad que más se sobrevende porque es la que más tarda en actualizarse en los otros
canales.
Hoy `channel_accounts.stock_buffer` está en el esquema desde el principio
(`migrations/001_core.sql:121`) y el comentario de `variant_stock` da por hecho que se resta
(`migrations/002_catalog.sql:102-104`), pero el nombre solo aparece en esas dos migraciones.
Falta restarlo al calcular el stock publicable y poder editarlo por cuenta.

### 72. Se aparta en unas bodegas y Odoo despacha desde otra

Odoo descuenta de la bodega principal mientras Integra tiene apartadas unidades de las de
Falabella: durante los siete días del asiento el stock publicable de una bodega está por
debajo y el de la otra por encima, así que se deja de vender donde hay y se sigue ofreciendo
donde ya no.
Hoy el descuento reparte entre todas las bodegas de la cuenta de mayor a menor
(`internal/store/ordenes.go:600-663`) mientras el pedido de Odoo lleva una sola
(`ordenes.go:476-503`), que hoy es siempre ninguna porque la asignación está vacía.
Falta apartar en la misma bodega que se manda en el `sale.order`, y rellenar la asignación
por cuenta (punto 3).

### 73. Un pedido sin líneas crea un `sale.order` en cero

Queda en Odoo un pedido de venta vacío, marcado como creado correctamente y contado en «En
Odoo», por una venta que el canal sí cobró: no hay nada que despachar ni que facturar y nada
en el panel dice que esté mal.
Hoy el bucle que valida las líneas no se ejecuta con cero líneas
(`internal/ordenes/ordenes.go:353-360`), el pedido viaja a Odoo con `order_line` vacío y la
verificación posterior compara cero contra cero sin ver descuadre (`:580-599`); Falabella
propaga el error si la consulta de líneas falla, pero no si devuelve una lista vacía
(`falabella.go:493-500`).
Falta marcar el pedido como fallido cuando llega sin líneas.

### 74. El identificador del feed de Falabella no se guarda

Cuando Falabella dice «ese producto no entró», no hay forma desde Integra de saber en qué
feed viajó ni qué contestó: hay que buscarlo a mano en el Seller Center, y con 452 productos
eso cuesta una tarde de una persona.
Hoy el contrato transporta el `FeedID` (`internal/channel/adapter.go:229-231`), Falabella lo
rellena (`falabella.go:1278`) y hasta expone una consulta pública de estado
(`falabella.go:1026`), pero los manejadores solo miran si la operación fue bien y descartan
el resto (`internal/publicar/manejadores.go:234-255`).
Falta persistir el `FeedID` junto a la publicación y enseñarlo en el panel.

### 75. El cupo se cuenta por método, no por petición

Con el límite por defecto de 2 por segundo, el canal recibe hasta ocho peticiones por
segundo, cuatro veces lo declarado: la publicación inicial de las 452 fichas es justo el
momento en que se dispara el 429 en cadena que el limitador venía a evitar.
Hoy el limitador está en el sitio correcto y se comparte por cuenta
(`internal/conectores/limite.go:104-120`), pero consume una ficha por método del contrato
(`limite.go:130-208`) y un solo `Publish` de MercadoLibre hace cuatro llamadas HTTP;
además el mapa de limitadores es de proceso, así que dos réplicas no comparten cupo.
Falta aplicar la ficha en la capa HTTP de cada adaptador, no en el envoltorio de métodos.

### 76. No hay registro de lo que respondió el canal

El panel de llamadas a la API sale siempre vacío: cuando MercadoLibre bloquea la aplicación
o Falabella rechaza una tanda, no hay forma de reconstruir qué se mandó ni qué contestaron,
ni para arreglarlo ni para reclamarle al canal.
Hoy la tabla está lista desde la migración 005 con códigos, cuerpos, duración y cabeceras de
cupo (`migrations/005_sync.sql:130-153`) y el store tiene escritor y lector
(`internal/store/observabilidad.go:27-80`), pero `RegistrarLlamadaAPI` no tiene ni un
invocador; y los avisos que devuelve el canal —«la publicación se creó pero la descripción
no»— solo se escriben al log mientras el hash queda sellado como publicado
(`internal/publicar/manejadores.go:139-141`, `:187-189`).
Falta enchufar el registro en la capa HTTP de los cuatro adaptadores y persistir esos avisos
junto a la publicación.

### 77. No se puede saber por qué un producto dejó de venderse

Cuando alguien nota que un producto se cayó de un marketplace, no hay ni fecha ni motivo ni
distinción entre «lo archivaron» y «lo borraron»: se acaba mirando en Odoo producto por
producto, y diagnosticar una baja masiva accidental puede tardar días.
Hoy las columnas `odoo_baja_at` y `odoo_baja_motivo` se crearon justo para responder a esto
(`migrations/019_publicacion_por_variante.sql:57-71`) y no las escribe nada: el sync solo
actualiza `active` (`internal/sync/catalogo.go:566-571`) y el producto desaparece de la
interfaz por el filtro de activos (`internal/store/consultas.go:135`).
Falta rellenar esas dos columnas —el sync ya distingue archivado de borrado— y una vista de
bajas recientes.

### 78. Una categoría sin mapear se ve sana hasta que el trabajo falla

El producto deja de recibir cambios de ficha en MercadoLibre y Falabella y acumula errores
hasta agotar reintentos; la ficha viva conserva la categoría antigua, que es lo prudente,
pero se pierde tiempo diagnosticando porque en pantalla el producto figura como listo.
Hoy la categoría sí se resincroniza y el mapeo exige confirmación humana
(`internal/store/publicacion.go:99-101`), pero no entra en el cálculo de «listo»
(`publicacion.go:128-129`), así que el candidato se encola y el fallo salta ya dentro del
manejador (`internal/publicar/manejadores.go:104-106`), que además bloquea también la
actualización de una ficha viva.
Falta incluir la categoría en «listo» cuando el canal la exige, para que el bloqueo se vea en
la cola de atención antes de encolar.

### 79. Títulos con HTML o emojis salen tal cual

En el mejor caso la ficha se ve mal en la tienda; en MercadoLibre, que rechaza títulos con
marcado, el alta se cae y el producto no llega a estar a la venta hasta que alguien encuentre
el carácter culpable.
Hoy no hay ningún saneamiento en el camino de publicación: el título sale directo del nombre
de Odoo (`internal/store/publicacion.go:63`) y lo único que se le hace es recortarlo al
límite del canal (`internal/publicar/manejadores.go:262-268`), recorte que además puede
partir un emoji compuesto; `internal/content/limpiar.go` sí normaliza, pero solo al generar
contenido.
Falta sanear título y descripción antes de publicar y avisar en la cola de atención cuando el
nombre traiga caracteres que un canal no acepta.

### 80. Borrar el SKU en Odoo bloquea bien, pero deja la ficha viva

El producto deja de recibir precio y stock y el bloqueo se ve en la pantalla de atención, que
es lo correcto; pero la publicación sigue abierta en el canal con la última cantidad, así que
se puede vender algo cuyo stock ya no se actualiza.
Hoy el sync pone el SKU a nulo (`internal/store/store.go:333-337`), la publicación lo excluye
(`internal/store/publicacion.go:104`) y la cola de atención levanta el motivo bloqueante
`missing_sku` (`internal/store/edicion.go:209-212`), con la referencia al canal a salvo por
el `channel_sku` guardado.
Falta lo mismo que en el punto 15: encolar `Pause()` cuando una variante publicada deja de ser
candidata.

### 81. Con dos destinos de correo, el segundo no recibe nada

Gerencia cree estar suscrita a los avisos críticos y no recibe ninguno: un token caído se
comunica solo a la primera dirección de la lista, que puede ser justo la del empleado que
está de vacaciones.
Hoy el despacho recorre los destinos en bucle y, tras enviar al primero, sella las alertas
globalmente (`internal/notificaciones/notificaciones.go:100-146` →
`internal/store/notificaciones.go:107-108`), de modo que el segundo consulta y encuentra la
lista vacía.
Falta que el sello de notificado sea por destino y no por alerta.

### 82. Si el correo no sale, nadie ve el error

El aviso no se pierde —se reintenta— pero el sistema se queda callado creyendo que ya
avisará: no hay ninguna ruta que lea los destinos, ni alerta por «llevo tres días sin poder
mandar correo».
Hoy las alertas se sellan solo tras un envío correcto
(`internal/notificaciones/notificaciones.go:140-146`), el fallo se anota en `last_error`
(`internal/store/notificaciones.go:115-125`) y se exige STARTTLS de forma explícita, pero ese
campo no se muestra en ninguna parte.
Falta que un destino con varios intentos fallidos se vea en el panel y en el log como error
recurrente.

### 83. Un operador puede cambiar credenciales y borrar catálogo

Puede sustituir las credenciales de MercadoLibre o vaciar el catálogo de una conexión antigua
sin ser administrador y sin que quede constancia de quién fue; la recuperación pasa por la
copia nocturna, con lo que se pierde el día.
Hoy el middleware solo distingue lectura de escritura y acepta operador
(`internal/api/middleware_auth.go:169-171`), ni la edición de cuentas
(`internal/api/server.go:696`) ni el borrado de conexiones
(`internal/api/integraciones.go:259`) exigen admin, y aunque no se puede borrar la conexión
activa, borrar una inactiva arrastra en cascada productos, variantes y con ellos los precios
propios de Integra.
Falta exigir rol admin para credenciales y borrado de conexiones, y auditar ambas
operaciones.

### 84. Dos personas editando el mismo producto se pisan

Uno corrige la descripción y el otro sube el precio, y el que guarda después revierte el
trabajo del otro sin que ninguno se entere; como tampoco se audita el cambio de precio, no
queda forma de reconstruir qué pasó.
Hoy `editarProducto` (`internal/api/server.go:866`) aplica los campos uno a uno sin leer
ninguna versión previa, y no hay columna de versión ni cabecera de concurrencia en ninguna
ruta.
Falta un número de versión por producto que haga fallar el guardado si cambió desde que se
cargó la pantalla.

### 85. Cuando un envío de stock agota sus reintentos, la alerta es un número

El desajuste no se da por bueno y alguien recibe el aviso, que es lo correcto; lo que falta es
que ese aviso diga qué SKU y qué canal en vez de un contador, porque hasta la siguiente
corrida el canal sigue ofreciendo la cantidad vieja.
Hoy el trabajo de stock lleva la prioridad más alta y el hash solo se guarda tras un envío
correcto (`internal/publicar/motor.go:107-112`,
`internal/publicar/manejadores.go:249-256`), así que la siguiente planificación lo reencola, y
la vigilancia levanta las alertas de trabajos fallidos y de publicados sin stock
(`internal/planificador/planificador.go:265-275`).
Falta detallar en la alerta qué publicaciones se quedaron sin stock enviado.

---

## Lo menor

### 86. El stock fraccionario se redondea hacia arriba

Con 0,6 unidades reales el canal ofrece 1 y la vende: hay que cancelar o despachar de menos;
al revés, con 1,4 se publica 1 y se deja de vender fracción disponible. Hoy el catálogo de
MDV es de piezas enteras, así que casi nunca ocurre; el día que entre un producto por peso o
por metro, ocurre en cada variante.
Hoy `variant_stock` guarda cuatro decimales (`migrations/002_catalog.sql:91-93`) pero la
publicación convierte con un cast a entero (`internal/store/publicacion.go:70`), y en
PostgreSQL ese cast redondea en vez de truncar; el campo de unidad de medida está en el
esquema y no lo escribe ni lo mira nadie.
Falta truncar hacia abajo al calcular la cantidad publicable.

---

## Lo que la mesa comprobó que sí está cubierto

No hace falta rehacer nada de esto.

- **El `list_price` de Odoo no se publica.** Se guarda solo para auditoría y el precio
  publicable sale del precio propio de Integra (`migrations/002_catalog.sql:56-58`,
  `internal/store/publicacion.go:68-69`); el motor de tarifas avisa cuando ninguna regla
  aplica.
- **MercadoLibre ignorando el precio en silencio.** Se manda solo el campo de precio, se
  busca el aviso `item.price.not_modifiable` y se compara el precio devuelto con tolerancia
  de medio centavo; si no coincide, el hash no se guarda
  (`internal/conectores/mercadolibre/mercadolibre.go:294-329`).
- **Pedido en una moneda que la tarifa de Odoo no usa.** Se busca la tarifa antes de crear
  nada y se falla con un mensaje que dice qué hacer, y después se relee el pedido para
  comparar total y moneda con alerta de descuadre (`internal/ordenes/ordenes.go:387-402`,
  `:568-629`).
- **Promociones solapadas o plantillas a medias.** Gana la decisión más reciente que ya haya
  empezado (`internal/store/precios.go:175-199`) y la carga rechaza la fila entera si falta el
  canal o el precio, si la rebaja no es menor, o si termina antes de empezar, sin escribir
  nada ante cualquier problema (`internal/api/plantillas.go:190-195`, `:300-345`).
- **Dos canales vendiendo la última unidad a la vez.** El descuento bloquea las filas de
  stock y filtra por existencias positivas, de modo que el segundo pedido no aparta nada ni
  deja el stock en negativo (`internal/store/ordenes.go:600-613`), con índice único e
  idempotencia por asientos.
- **Un sync completo justo después de una venta.** El reemplazo de stock va seguido de la
  reaplicación de todas las reservas abiertas, con prueba contra la base real
  (`internal/sync/catalogo.go:267-284`, `internal/store/ordenes_bodega_test.go:245`), y el
  borrado se acota por conexión.
- **El mismo pedido llegando dos veces.** Hay idempotencia en cuatro capas: el webhook solo
  encola, el guardado usa conflicto por pedido externo, el encolado tiene clave única y antes
  de crear en Odoo se adopta el `sale.order` con la misma referencia; el descuento de stock
  tampoco se repite.
- **Cancelación en WooCommerce o MercadoLibre después del montaje.** Se devuelve el stock, se
  saca el pedido de la cola de montaje y se crea alerta nombrando el `sale.order`, solo la
  primera vez (`internal/ordenes/ordenes.go:220-227`, `:272-312`); el `sale.order` no se toca,
  por decisión de producto.
- **Bodegas de Falabella para los pedidos de Falabella.** Se elige la bodega que más unidades
  del pedido cubre, con desempate reproducible, y si la cuenta no tiene bodegas decide Odoo
  (`internal/store/ordenes.go:476-504`).
- **Shopify recortando la respuesta de pedidos tras una parada larga.** Se comprueba si la
  marca de agua se salió de la ventana de 60 días y se devuelve error si el token no tiene el
  permiso, en vez de aceptar una respuesta incompleta (`shopify.go:494-545`); la marca solo
  avanza si la lectura fue bien.
- **El canal desactivando la suscripción de webhooks.** Están documentados los umbrales de
  cada canal y la regla de que un error nuestro no se paga con el cupo del canal; se responde
  antes de encolar por el presupuesto de medio segundo de MercadoLibre
  (`internal/api/webhooks.go:40-172`, `:314-317`).
- **Odoo cayéndose a mitad de una sincronización.** La marca de agua se escribe lo último,
  cada producto con su variante va en una transacción propia, el reemplazo de stock es
  atómico y el cliente se ata al contexto para que apagar el worker corte de verdad
  (`internal/sync/catalogo.go:89-92`, `:290-294`, `internal/store/store.go:303-343`).
- **Mejorar el nombre de un producto en Odoo.** El nombre sí se sigue, con el arreglo
  documentado de la ventana incremental sobre la plantilla y tres pruebas dedicadas
  (`internal/sync/catalogo.go:110-143`); la descripción es propiedad de Integra por decisión
  explícita desde la migración 012.
- **Revocar el acceso de alguien hoy mismo.** El middleware contrasta contra la base en cada
  petición con caché de 30 segundos, el rol vigente sustituye al firmado, desactivar tira la
  caché en el acto y el cierre de sesión revoca el token
  (`internal/api/middleware_auth.go:102-124`, `internal/api/auth.go:270`).
- **Actualizar Integra mientras se publica.** Los trabajos corren con un contexto desligado
  del apagado, se esperan 30 segundos a lo que esté en vuelo, el compose da 45 segundos de
  gracia y lo que no termine vuelve a la cola sin duplicarse
  (`internal/jobs/worker.go:92-99`, `:149-162`, `docker-compose.yml:66-70`).
