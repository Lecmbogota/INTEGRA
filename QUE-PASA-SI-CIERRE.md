# Cierre de «¿Y qué pasa si...?»

De los 86 huecos de [QUE-PASA-SI.md](QUE-PASA-SI.md) se cerraron **9**: los
números 2 a 11, salvo el 1 y el 8. Son los más caros de la lista, todos de la
sección «lo que rompe el negocio».

No se cerraron los otros 77 porque el taller que los repartía —un agente por
hueco— se quedó sin cuota a mitad: de 519 agentes terminaron 9. Lo que quedó a
medias se descartó y se limpió; `main` no se tocó en ningún momento. Todo esto
vive en la rama `qps/integracion`.

**Cómo leer esto.** Cada hueco cerrado tiene una prueba que falla con el código
anterior y pasa con el nuevo. La suite completa está en verde contra la base de
datos real y la interfaz compila. Pero solo el hueco 3 se ha visto funcionar
contra un canal de verdad; los demás están probados contra servidores falsos,
que es lo único posible mientras no haya credenciales de MercadoLibre, Falabella
y Shopify.

---

## Antes de desplegar: lee esto

**Toda cuenta de canal deja de publicar hasta que le asignes bodegas.** Es el
hueco 3 y es deliberado: hasta ahora, sin asignación, Integra publicaba la suma
de las siete bodegas, incluidas Muestras, Garantías y las tres de consignación
en Falabella. Al desplegar, cada cuenta existente queda parada, con pastilla
roja en «Cuentas» y alerta crítica por correo, hasta que alguien entre y elija.

En la instancia de pruebas ya se hizo: se le asignó **solo Bodega Oficina** a la
cuenta de WooCommerce, y el efecto se vio en la tienda:

| SKU | antes | después | por qué |
|---|---|---|---|
| AS6806T | 2 | **0** | sus dos unidades estaban enteras en Muestras |
| AO-SC-1012 | 32 | 30 | dos eran muestras |
| AS6706T-V2 | 2 | 1 | una era muestra |

Eso es exactamente lo que se buscaba: dejar de vender lo que no se puede
despachar. Si Bodega Oficina no es la elección correcta para algún canal,
cámbiala en «Cuentas»; el cambio queda registrado en la auditoría.

---

## Lo que se cerró

**2. Una venta baja el stock en los otros canales en el acto.** Antes, la última
unidad vendida en un canal se seguía ofreciendo en los otros tres hasta la
corrida del día siguiente. Ahora la venta —y también la cancelación— encola el
envío de stock a las demás cuentas donde esa variante está publicada.

**3. Cada cuenta publica solo el stock de sus bodegas.** Ver arriba.

**4. Un SKU repetido en Odoo ya no despacha otra mercancía.** Antes, dos
variantes vivas con la misma referencia se resolvían eligiendo una al azar: la
ficha de una pisaba a la otra y un pedido podía montarse contra el producto
equivocado. Ahora se bloquean las dos hasta corregirlo en Odoo, y la colisión
sale en la cola de atención.

**5. Un SKU renombrado en Odoo ya no pierde los pedidos.** El canal manda el SKU
con el que se publicó, y renombrarlo en Odoo dejaba cada pedido de ese producto
sin variante: el montaje se agotaba en cinco intentos y la venta cobrada nunca
llegaba a Odoo. Ahora se busca primero por el SKU con el que esa cuenta publicó.

**6. Cambiar la comisión de un canal rehace los precios.** Antes se guardaba el
número y no cambiaba ni un precio publicado.

**7. Un precio manual o una regla de canal salen al canal.** Antes se guardaban
y solo llegaban si alguien acertaba a pulsar «Planificar envíos». Ahora se
recalculan al guardar y antes de cada horario.

**9. Falabella: se lee el estado del pedido y se baja lo modificado.** Antes solo
se traían los pedidos creados, así que una cancelación en el Seller Center era
invisible para Integra.

**10. Reintentar a mano un pedido que agotó sus intentos.** Antes, un pedido que
fallaba cinco veces al montarse en Odoo se quedaba fuera para siempre, sin
forma de recuperarlo desde la interfaz.

**11. El refresh token de MercadoLibre se canjea con la cuenta bloqueada.** Dos
trabajos simultáneos podían canjear el mismo token de un solo uso y dejar la
cuenta sin acceso.

---

## Roces que solo aparecieron al juntarlos

Tres cosas que ningún agente podía ver trabajando solo:

- Los huecos 4 y 5 chocaban de frente en la misma consulta. El 4 dice «no
  emparejes si el SKU vive en dos variantes»; el 5 dice «empareja por el SKU con
  el que se publicó». Se resolvió dejando las dos: primero lo publicado por esa
  cuenta, después el SKU actual, y ninguno de los dos si hay ambigüedad.
- Las pruebas de los huecos 6 y 7 creaban cuentas sin bodegas y pasaron a fallar
  por la regla del hueco 3, no por lo suyo.
- El recálculo de precios del hueco 7 insertaba en bloque: una variante borrada
  a mitad tiraba el lote entero por clave foránea. Ahora se salta esa variante.

---

## Decisiones que deberías confirmar

Los agentes no se pararon a preguntar: implementaron la opción más segura para
el negocio y la anotaron. Son 38. Las que más te conviene mirar:

- **Hueco 3:** una cuenta sin bodegas no publica *nada* (ni ficha, ni precio, ni
  stock), no solo el stock. Y no se sembró ninguna asignación automática, para
  no perpetuar la mentira de ofrecer la consignación de Falabella.
- **Hueco 2:** la cuenta que hizo la venta queda fuera del envío inmediato,
  porque el marketplace ya descontó por su cuenta. No se añadió sondeo de
  pedidos cada N minutos: la inmediatez depende de que los webhooks estén
  registrados.
- **Hueco 4:** se bloquean *los dos* productos que comparten SKU, incluso uno
  que ya estuviera publicado.
- **Hueco 5:** ante un renombrado se avisa, no se bloquea. Integra sigue sin
  renombrar el SKU en el canal.
- **Huecos 6 y 7:** el precio nuevo sale en la siguiente planificación, no en el
  mismo guardado.

La lista completa, hueco por hueco:

**Hueco 2**

- La cuenta que hizo la venta queda FUERA del envío inmediato: se manda el stock solo a las otras cuentas activas donde la variante está publicada con identificador del canal. Motivo: el marketplace que vendió ya descontó la unidad por su cuenta, y reescribirle el stock en ese instante podría pisar una segunda venta todavía sin ingerir; el horario la reconcilia después por hash, como siempre. Alternativa: incluirla también (una llamada más por venta, corrige un canal que no descuente solo).
- Las cancelaciones se tratan igual que las ventas: cuando el canal cancela y la reserva vuelve a la base, se encola el stock devuelto a las demás cuentas (una unidad que vuelve y no se publica es una venta que no se hace). Alternativa: solo ventas, dejando la reposición al horario diario.
- NO se añadió sondeo de pedidos cada N minutos ni horarios medidos en minutos (la segunda alternativa de la entrada). La inmediatez depende de que los webhooks de los cuatro canales estén registrados (pantalla Cuentas, campo webhook_secret; los cuatro tienen manejador en internal/api/webhooks.go); sin webhook, la venta se conoce en la ingesta del horario diario y el stock sale en esa misma pasada. Un sondeo frecuente es barato en cupo, pero hoy amplificaría dos cosas que hay que arreglar antes: la marca de agua de pedidos retrocede un minuto por cada sondeo vacío (AUDITORIA.md §5, abierta) y una cuenta con credencial rota generaría un trabajo fallido por sondeo, y la alerta «N trabajos agotaron sus reintentos» cambia de texto con cada N, es decir, un correo por cada fallo. Si se quiere el sondeo, las opciones son un horario de alcance «stock» con campo «cada N minutos» (migración + store + API + UI) o un intervalo fijo en el planificador por variable de entorno (más pequeño, menos visible).
- El envío inmediato llega a cualquier publicación viva con identificador aunque la variante ya no esté «lista» (sin descripción o sin foto); el planificador, en cambio, se salta esas variantes. Se hizo así porque el stock de una ficha viva en el canal debe ser correcto pase lo que pase con su descripción; el trabajo de stock no comprueba «Listo». Alternativa: restringir a variantes listas, igual que Planificar.

**Hueco 3**

- Una cuenta sin bodegas asignadas no publica NADA (ni ficha, ni precio, ni stock), no solo el stock. Alternativa: bloquear únicamente los envíos de stock y dejar pasar ficha y precio; se eligió bloquear todo porque el alta (Publish) lleva el stock en el mismo cuerpo y porque asignar bodegas es un paso de configuración que debe hacerse antes de la primera publicación.
- No se sembró ninguna asignación para las cuentas que ya existan: al desplegar, toda cuenta existente (hoy la de la tienda WooCommerce local de pruebas) deja de publicar hasta que alguien le asigne bodegas en «Cuentas», con alerta crítica por correo y pastilla roja en la pantalla. Alternativa descartada: una migración que asignara «todas» a las cuentas existentes, porque perpetuaría explícitamente la misma mentira (WooCommerce ofreciendo la consignación de Falabella).
- No se permite guardar una asignación vacía (la API responde 422 y la hoja no deja pulsar Guardar sin marcar una): dejar de publicar en un canal no puede ser el resultado de desmarcar casillas. Alternativa: permitir vaciarla como forma de «pausar» un canal.
- El descuento inmediato de stock por venta (DescontarStockPublicado) conserva «todas las bodegas si la cuenta no tiene asignación»: bajar stock nunca sobrevende y la venta pudo entrar por una ficha publicada antes. Alternativa: no descontar nada en cuentas sin asignación.
- La alerta cuenta_sin_bodegas se levanta con severidad «critical» (misma que la de conexión caída), así que llega a cualquier destino de correo configurado. Alternativa: «error» o «warning».
- Asignar bodegas lo puede hacer cualquier rol con escritura (operator o admin), igual que reemplazar credenciales; queda registrado en audit_logs con la lista anterior y la nueva. Alternativa: exigir admin.
- Solo se pueden asignar bodegas activas de conexiones de Odoo activas; una conexión apagada ya no recibe sync y publicar desde sus bodegas sería publicar stock congelado.

**Hueco 4**

- Una línea de pedido cuyo SKU tienen dos variantes vivas se deja SIN emparejar (el pedido espera en la cola de montaje y el rescate lo reactiva solo cuando el sync trae las referencias separadas). La entrada pedía «un orden determinista»; se descartó porque un orden fijo (p. ej. la variante más antigua) hace reproducible la elección equivocada, no correcta. Alternativas: emparejar con la variante más antigua (ORDER BY v.id), o con la que tenga publicación viva en esa cuenta (variant_channel_listings.channel_sku), que acierta a partir de este cambio pero no para fichas que ya se hubieran pisado antes.
- Se bloquean los DOS productos que comparten SKU, incluido uno que ya estuviera publicado: su ficha, precio y stock dejan de enviarse hasta corregir el SKU en Odoo, exactamente como pasa hoy cuando se borra el default_code (entrada 80); la ficha viva sigue abierta en el canal porque pausarla es la entrada 15. Alternativas: bloquear solo al que no tiene publicación previa, o encolar además una pausa (stock 0) de la ficha viva mientras dure la colisión.
- El SKU repetido solo cuenta dentro de la MISMA conexión de Odoo: el mismo SKU en dos conexiones se trata como el mismo producto visto desde dos instancias (así lo asume `conexiones migrar`, que empareja por SKU). Con detección global, mientras conviven dos conexiones el catálogo entero quedaría bloqueado. El emparejamiento de pedidos, en cambio, sí exige unicidad global, porque no puede saber cuál de las dos conexiones es la buena.
- La comparación de SKUs no distingue mayúsculas (igual que el emparejamiento de pedidos, que ya usaba lower()) y no recorta espacios.
- El motivo se llama duplicate_sku (en inglés, como los demás motivos de la cola, que el código declara «claves estables del dominio») y se muestra como «Referencia repetida en otro producto»; la entrada lo llamaba «sku_duplicado». No hay migración: attention_queue.reason es texto libre.

**Hueco 5**

- Avisar en vez de bloquear: tras un renombrado la ficha se sigue republicando (título, fotos, descripción) y el canal conserva el SKU viejo; la divergencia queda como aviso ('warning') en la cola de atención y como alerta por correo. Alternativa: marcarla 'blocking' y no republicar hasta que alguien la resuelva. No se hizo porque hoy Integra no ofrece ninguna acción para resolverla y bloquear congelaría el resto del contenido de ese producto sin salida.
- El SKU viejo solo se reconoce en la cuenta que publicó esa variante con él (mismo channel_account_id). Un pedido con el SKU viejo desde una cuenta donde el producto nunca se publicó vía Integra sigue quedando sin variante, como hasta ahora, y sale en la alerta de «líneas con SKU que no existe». Alternativa: aceptar el SKU publicado en cualquier cuenta; recuperaría listados hechos a mano en otros canales, a costa de equivocarse de producto si el SKU viejo se reutiliza.
- Cuando el SKU de un pedido coincide a la vez con el SKU publicado de una variante y con el SKU actual de otra (reutilización del código), gana la publicada en esa cuenta: es lo que el comprador vio. Alternativa: preferir el catálogo actual.
- Integra sigue sin renombrar el SKU en el canal. MercadoLibre (atributo SELLER_SKU en PUT /items/{id}), Shopify (PUT /variants/{id}.json) y WooCommerce (PUT /products/{id}) lo permitirían y harían desaparecer la divergencia; Falabella no (SellerSku es la identidad del producto). No se hizo porque la entrada no lo pedía y son tres adaptadores con sus pruebas contra servidor falso; si se quiere que el marketplace muestre el código nuevo, esa es la vía.
- La alerta nueva sale con severidad 'warning', igual que la de «publicados sin existencias». Si los destinos de correo tienen severidad mínima 'error', no se enviará y solo se verá en el panel.

**Hueco 6**

- El precio nuevo llega al marketplace en la siguiente planificación (el horario o el botón «planificar» de la cuenta), no en el mismo guardado: es lo que ya hacen la edición de precio de la ficha y la masiva, y lo que pide la entrada. Alternativa: llamar además a publicar.Planificar sobre las cuentas del canal dentro del PATCH, con lo que los trabajos de precio se encolarían al instante y el worker los mandaría en minutos; se descartó por no ampliar el alcance y porque planificar recorre el catálogo entero dentro de una petición HTTP. Con horarios de una vez al día, entre guardar la comisión y la corrida siguiente el margen se sigue perdiendo salvo que alguien pulse «planificar».
- El recálculo abarca solo las cuentas globales activas del canal editado (las que devuelve ListarCuentas), no todas las cuentas como hace la edición de precio. Alternativa: rehacer todas; sería inofensivo (el recálculo es idempotente) pero trabajo por nada en los otros tres canales.
- Si el recálculo falla después de guardar la comisión, el endpoint sigue respondiendo «guardado» y el fallo queda solo en el log, igual que en la edición de precio; la pantalla no lo ve. Alternativa: devolver un aviso en la respuesta (y mostrarlo en Canales.tsx) o levantar una alerta del panel.

**Hueco 7**

- Al guardar un override o una regla se rehacen los precios efectivos, pero el envío al canal sale en el siguiente horario (price/full/stock), igual que hoy al editar un precio en la ficha; no se encola la publicación en la misma petición. Alternativa: encolar también `publicar.Planificar` de esa cuenta al guardar, para que salga en minutos y no en horas.
- El recálculo previo a planificar corre en TODOS los horarios, también los de alcance «stock» (el motor ya empujaba diferencias de precio en esos horarios; ahora además las detecta). Coste: una consulta y un lote de UPSERT sobre el catálogo por cuenta y horario, lo mismo que el botón «recalcular precios». Alternativa: limitarlo a los alcances price y full.
- Si el recálculo de una cuenta falla dentro de un horario, el horario se aborta para esa cuenta y las siguientes (y se levanta la alerta `horario_fallido`), en vez de planificar con precios viejos; es el mismo comportamiento que ya tenía cualquier otro error dentro de `planificarTodas`.
- Si el recálculo falla dentro de la petición HTTP, el override/regla se dan por guardados (200 ok) y el error queda en el log, con el mismo criterio que el refresco tras editar un precio; el horario lo recoge. Alternativa: responder 500 y que el operador reintente.
- Editar una regla por id desde la ruta de otra cuenta ahora falla («no existe la regla N en la cuenta M») en lugar de editar la regla ajena en silencio.

**Hueco 9**

- Cancelación parcial en un pedido NUEVO (Falabella cancela por artículo): se dejó fuera la unidad anulada, así que el sale.order de Odoo nace solo con lo que hay que despachar, y el estado del pedido queda como «pending,canceled» (los estados distintos de las líneas, separados por coma) para que la anulación se vea sin dar el pedido por muerto. Alternativas: conservar todas las líneas y confiar en que alguien lo compare con Seller Center, o tratar cualquier cancelación parcial como pedido entero cancelado (deja sin montar unidades que sí se vendieron). Nota: total_amount en Integra sigue siendo el Price de cabecera de Falabella, que puede incluir la unidad anulada; la comprobación de descuadre contra Odoo usa la suma de líneas, así que no dispara alertas falsas.
- Primera ingesta de una cuenta de Falabella: al filtrar por UpdatedAfter (ventana inicial de 7 días que fija el núcleo), además de los pedidos creados esa semana entran los creados antes pero modificados en ella (entregados, devueltos), que se crearían como borradores en Odoo aunque ya estén atendidos a mano. MercadoLibre y WooCommerce ya se comportan así; los borradores se revisan antes de confirmar. Alternativa: que el núcleo use fecha de creación solo en la primera pasada sin marca (cambio en internal/ordenes, fuera de este hueco), o acortar la ventana inicial.
- Un pedido cancelado que YA estaba montado en Odoo se queda como estaba en el núcleo: alerta pedido_cancelado (correo) y decisión humana sobre el sale.order; no se cancela nada en Odoo automáticamente. Es la política previa del núcleo y este cambio solo hace que Falabella la active.

**Hueco 10**

- El botón y el endpoint aceptan pedidos en 'failed' y también en 'received'/'mapped' (sirve para volver a encolar un montaje cuyo trabajo se perdió); alternativa: ofrecerlo solo para 'failed'. Lo que ya está en Odoo (odoo_sale_order_id o 'created_in_odoo') y lo cancelado por el canal ('ignored') se rechaza con 409 y el motivo.
- Antes de reiniciar, el reintento vuelve a emparejar por SKU las líneas de ESE pedido y, si alguna sigue sin variante, NO reactiva nada y responde 409 nombrando los SKU que faltan, sin gastar intentos (las líneas que sí emparejó se quedan emparejadas). Alternativa: reintentar igual y dejar que falle con el mismo error.
- Cada reintento manual concede otros cinco intentos con el mismo backoff (30s, 1m, 2m, 4m); no se tocaron los topes de sync_attempts ni max_attempts ni se hicieron configurables. Alternativa: subir el tope o hacerlo configurable por variable de entorno.
- Si todavía hay un trabajo orden_a_odoo vivo para ese pedido (pendiente con backoff, como mucho 4 minutos), la clave única lo deduplica y el reintento no adelanta su run_at: el montaje corre cuando venza ese backoff. Alternativa: añadir a la cola una operación para adelantar run_at a now().
- Puede reintentar cualquier rol con permiso de escritura (operator y admin; viewer recibe 403 por el middleware). El reintento queda en audit_logs con action 'retry', entity 'channel_orders', before {intentos, error} y after {intentos: 0, estado: 'received'}.
- La interfaz se verificó solo con tsc -b && vite build: no se pudo abrir en el navegador porque haría falta iniciar sesión con credenciales que no debo manejar.

---

## Lo que sigue abierto

Los 77 huecos restantes de QUE-PASA-SI.md, empezando por los dos críticos que no
llegaron a tocarse:

- **1. MDV despacha y el canal nunca se entera.** Los cuatro conectores
  implementan `AckOrder` con detalle y no lo llama nadie. Es el más caro de
  todos: MercadoLibre y Falabella cancelan, reembolsan y bajan la reputación
  pasado el plazo de despacho.
- **8. Nada impide publicar por debajo del coste.**

Y del 12 al 86, en el orden en que están escritos allí.

Recuerda además que el contraste de la mesa original no refutó ni uno solo de
los 86, lo que dice más del contraste que de los huecos. De los 9 cerrados aquí
sí se comprobó el diagnóstico antes de tocar el código. De los 77 restantes,
solo tres se han verificado a mano: el 1, el 3 y el 7 (este último resultó
exagerado en su redacción original). Los otros 74 siguen siendo hipótesis.
