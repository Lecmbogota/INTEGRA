# Auditoría de Integra — ola 1

Fecha: 2026-09-06. Diez dimensiones auditadas en paralelo por agentes de
solo lectura, partiendo del commit de línea base `5ee62c5`. Cada hallazgo
pasó después por verificación adversarial: una lente que intenta refutarlo
reproduciéndolo y otra que comprueba si es una decisión de diseño ya
documentada, con una tercera de desempate cuando no coincidían. Lo crítico
y alto llevó las tres; lo medio, una. Solo aparece aquí lo que sobrevivió.

> **Por qué faltan aquí cuatro defectos graves que sí existían.** La
> verificación se ejecutó mientras se corregían los hallazgos más urgentes,
> así que leyó el árbol de trabajo ya arreglado y los refutó con razón. Van
> en la sección de descartados con severidad baja, pero eran reales en la
> línea base: los pedidos que nunca se montaban solos en Odoo (`2bd652b`),
> la categoría de Odoo que no se poblaba y bloqueaba toda publicación en
> MercadoLibre y Falabella (`77e5dae`), las contraseñas en claro en el
> registro de auditoría (`6fee68d`) y el banco de imágenes en disco efímero
> (`aea2b7d`). La lección para las siguientes olas: congelar el árbol
> mientras se verifica, o verificar contra el commit auditado.

| | Crítica | Alta | Media | Baja | Total |
|---|---|---|---|---|---|
| Confirmados | 4 | 19 | 34 | 5 | **62** |

De 109 hallazgos brutos, 96 quedaron tras quitar duplicados entre
dimensiones; 74 se verificaron con agentes, 12 se descartaron por falsos o
por ser decisiones de diseño, y 22 de severidad baja quedan sin verificar.

**1 de estos hallazgos ya está corregido** y va marcado abajo; los otros
tres arreglos ya aplicados salen como descartados por lo dicho arriba.

Quedan por tanto **61 defectos confirmados y sin corregir**: 3 críticos,
19 altos, 34 medios y 5 bajos.

---

## Severidad crítica (4)

### 1. Un cambio de contenido nunca llega a una publicación existente y, de paso, se traga el cambio de precio

`internal/publicar/manejadores.go:98` · Núcleo

**Qué falla.** El manejador de `publicar_producto` solo llama a `Publish`, que en todos los adaptadores adopta la publicación existente por SKU sin enviar nada; aun así se guardan los tres hashes como si se hubiera publicado.

**Cuándo se rompe.** Un producto ya está publicado en Shopify a $100.000. El operador sube el precio a $150.000 y edita la descripción. `Planificar` (internal/publicar/motor.go:68) entra por la rama de contenido —que es excluyente— y encola SOLO `publicar_producto`; no encola `actualizar_precio`. El manejador llama a `ad.Publish`, que en shopify.go:76-85 encuentra el SKU, devuelve `Adopted:true` sin hacer ninguna llamada de escritura, y a continuación `GuardarPublicacion` (manejadores.go:107-109) escribe content_hash, price_hash y stock_hash con los valores nuevos. A partir de ahí los tres hashes coinciden con el catálogo, así que ninguna planificación futura volverá a encolar nada: Shopify sigue vendiendo a $100.000 con la descripción vieja, para siempre, y el panel de Integra dice "publicado, sin cambios". Lo mismo con ML (mercadolibre.go:126-133) y Woo/Falabella.

**Arreglo propuesto.** En el manejador, si ya hay `external_id` (o si `res.Adopted`), llamar a `ad.Update` con la ficha y encolar además precio y stock; guardar cada hash solo cuando su envío se haya hecho de verdad (no escribir price_hash/stock_hash en el camino de adopción).

### 2. Contraseñas en claro guardadas en audit_logs y legibles por cualquier viewer — **corregido en `6fee68d`**

`internal/api/auth.go:166` · Seguridad

**Qué falla.** crearUsuario y editarUsuario registran en auditoría el struct `req` completo, que incluye el campo `password` en claro, y GET /api/auditoria lo devuelve a cualquier usuario con rol viewer.

**Cuándo se rompe.** Un admin crea a otro admin desde la interfaz (POST /api/usuarios con {"email":..,"password":"Secreta123","role":"admin"}). La fila de audit_logs guarda after = {"email":..,"password":"Secreta123","role":"admin"}. Cualquier usuario con sesión de solo lectura llama GET /api/auditoria?entity=users y obtiene la contraseña del administrador en claro; con ella inicia sesión como admin y controla usuarios, credenciales de canal y conexión a Odoo. Lo mismo ocurre al cambiar una contraseña por PATCH /api/usuarios/{id} (línea 224). Además el bcrypt de la tabla users queda inútil porque la contraseña ya está en otra tabla sin cifrar.

**Arreglo propuesto.** Registrar en auditoría una copia sin la contraseña (struct aparte o poner Password="" antes de llamar a RegistrarAuditoria) y restringir GET /api/auditoria a admin.

### 3. buscarPorSKU solo mira los primeros 250 productos: la adopción falla y se publican duplicados

`internal/conectores/shopify/shopify.go:377` · Shopify

**Qué falla.** La búsqueda de un SKU existente lista una sola página de productos (limit=250, sin paginar), así que a partir del producto 251 de la tienda la adopción devuelve nil y Publish crea un producto nuevo con un SKU que ya existe.

**Cuándo se rompe.** MDV tiene 452 productos. Se publican los 452 en Shopify (orden por id ascendente). El operador corrige la descripción del producto nº 300 en Integra; Planificar detecta el cambio de content_hash y encola TrabajoPublicar (motor.go:67); Publish llama a buscarPorSKU, que solo ve los 250 primeros por id, no encuentra el SKU y ejecuta POST /products.json: la tienda queda con dos productos con el mismo SKU, con stock e inventory_item distintos. Repetido por cada edición de contenido de la mitad alta del catálogo.

**Arreglo propuesto.** Resolver el SKU con GraphQL (`productVariants(first:1, query:"sku:'<sku>'")`), que sí lo indexa y devuelve id de producto y de variante en una llamada; como mínimo, paginar con el Link header hasta agotar el catálogo antes de concluir que el SKU no existe.

### 4. FetchOrders filtra por `after` en UTC contra un campo que WooCommerce interpreta en la hora del sitio: pedidos que nunca se ingieren

`internal/conectores/woocommerce/woocommerce.go:241` · WooCommerce

**Qué falla.** La marca de agua se envía en UTC al parámetro `after`, que WooCommerce compara contra `post_date` (hora local del sitio) porque no se manda `dates_are_gmt=true`; en una tienda en UTC-5 los pedidos de la ventana de desfase quedan bajo la marca de agua y no se ingieren nunca.

**Cuándo se rompe.** Tienda con zona horaria America/Bogota (UTC-5). (1) Se ingiere el pedido A creado a las 10:00 local; su `date_created_gmt` es 15:00, así que la marca de agua queda en 2026-09-01T15:00:00 (ordenes.go:141-147 usa OrderedAt, que sale de date_created_gmt). (2) A las 10:15 local entra el pedido B. (3) La siguiente ingesta pide `after=2026-09-01T15:00:00`; WooCommerce lo lee como las 15:00 de Bogotá, así que B (10:15 local) no se devuelve. (4) A las 16:00 local entra el pedido C, que sí supera el filtro; al ingerirlo la marca de agua salta a su `date_created_gmt` = 21:00 UTC. B queda definitivamente por debajo de la marca y no se vuelve a pedir jamás: no llega a `channel_orders`, no se crea su `sale.order` y su stock no se descuenta. Una venta cobrada en la tienda que el ERP nunca ve.

**Arreglo propuesto.** Añadir `q.Set("dates_are_gmt", "true")` (existe desde WC 4.1) y, ya puestos, usar `modified_after` en vez de `after` para recoger también los cambios de estado; alternativamente convertir la marca a la zona horaria de la tienda antes de formatearla.

---

## Severidad alta (19)

### 1. La carga masiva por plantilla no es transaccional: un fallo en la fila N deja aplicadas las N-1 anteriores aunque la API responda error

`internal/api/plantillas.go:244` · Integridad de datos

**Qué falla.** El bucle de aplicación llama a ActualizarPrecio y GuardarOferta uno por uno, cada uno en su propia transacción implícita; al primer error devuelve y deja el resto sin aplicar, contradiciendo la garantía documentada de «todo o nada».

**Cuándo se rompe.** El operador reutiliza la plantilla del mes pasado: deja «Promocion empieza» vacío y «Promocion termina» con una fecha ya pasada. La validación de internal/plantilla/plantilla.go:436 solo compara termina contra inicia cuando AMBOS están presentes, así que la fila pasa el simulacro sin problemas. Al confirmar, plantillas.go:255 pone `inicia = time.Now()`, GuardarOferta (internal/store/precios.go:207) rechaza con «la fecha de fin no puede ser anterior a la de inicio» y resolverPlantilla devuelve error. La respuesta HTTP es un fallo, pero todos los ActualizarPrecio de las filas anteriores ya están confirmados en la base: medio catálogo queda reprecificado y el operador cree que no se aplicó nada.

**Arreglo propuesto.** Abrir una transacción en el store para toda la carga (una función AplicarPlantilla que reciba la lista y haga Begin/Commit) y, en paralelo, rechazar en la validación las filas con «termina» sin «empieza» o con «termina» en el pasado.

### 2. Un pedido con un SKU desconocido se pierde de forma definitiva: nadie vuelve a emparejar la línea ni reinicia el contador de intentos

`internal/store/ordenes.go:163` · Integridad de datos

**Qué falla.** channel_order_lines.variant_id se resuelve solo en el INSERT inicial y nunca se recalcula; sync_attempts solo crece y OrdenesPendientesOdoo descarta para siempre lo que llegó a 5, sin ninguna ruta de reproceso en la API.

**Cuándo se rompe.** MercadoLibre entrega un pedido de un SKU que aún no está sincronizado. GuardarOrden lo guarda con variant_id NULL (línea 138). CrearPedido (internal/ordenes/ordenes.go:196) marca fallo «el SKU X del pedido Y no existe en el catálogo» y sube sync_attempts. Tras cinco pasadas del planificador, sync_attempts = 5 y el pedido desaparece de OrdenesPendientesOdoo. El operador ejecuta `integra sync`, el SKU ya existe, pero: (a) la línea sigue con variant_id NULL porque GuardarOrden hace `return id, false` en la línea 122 cuando el pedido ya existía y no toca las líneas; (b) nada pone sync_attempts a 0; (c) no hay endpoint de reproceso (server.go solo expone GET /api/ordenes, GET /api/ordenes/resumen y POST /api/cuentas/{id}/ingerir-ordenes). El pedido cobrado nunca llega a Odoo.

**Arreglo propuesto.** Reemparejar las líneas con variant_id NULL en cada ingesta (o en un job periódico) y añadir una acción de reproceso que ponga status='received' y sync_attempts=0; alternativamente, no contar como intento el fallo por SKU sin mapear.

### 3. GetProducts: el adaptador lee Body.Products como array cuando Seller Center devuelve Body.Products.Product — Publish falla siempre

`internal/conectores/falabella/falabella.go:471` · Falabella

**Qué falla.** Las tres lecturas de GetProducts (existeSKU, FetchStatus, ListRemote) declaran `Products []struct{...}` bajo `Body`, pero la API anida `Body.Products.Product[]` y además el precio, el stock y el estado viven bajo `BusinessUnits.BusinessUnit`, no como campos planos del producto.

**Cuándo se rompe.** Publicar cualquier producto en Falabella: `Publish` (línea 109) llama a `existeSKU`, `llamar` hace `json.Unmarshal` de la respuesta real y devuelve «json: cannot unmarshal object into Go struct field ... of type []struct{...}». `Publish` devuelve error en la línea 110, `publicar` (internal/publicar/manejadores.go:99) anota el error y el trabajo se reintenta hasta agotarse. Ningún producto llega nunca a Falabella. Con el mismo defecto, `ListRemote` devuelve una página vacía (la adopción por SKU nunca encuentra nada) y `FetchStatus` se traga el error con `continue` (línea 241) devolviendo una lista vacía en vez de un fallo. Aunque se corrigiera el anidamiento, `Price`/`Quantity`/`Status` seguirían llegando a cero/vacío porque están dentro de BusinessUnit.

**Arreglo propuesto.** Modelar `Body.Products.Product []productoResp` y leer precio/stock/estado desde `BusinessUnits.BusinessUnit[]` (eligiendo el OperatorCode de la cuenta). Como Seller Center colapsa el array a objeto cuando hay un solo elemento, usar un tipo que acepte objeto o array (json.RawMessage + reintento de decodificación).

### 4. GetOrderItems: se leen las líneas de Body.OrderItems como array (la API anida OrderItem) y el error se descarta en silencio

`internal/conectores/falabella/falabella.go:365` · Falabella

**Qué falla.** `lineasDe` espera `Body.OrderItems []struct{...}` cuando la respuesta documentada es `Body.OrderItems.OrderItem[]`; el fallo se ignora en la línea 350 con `if lineas, err := ...; err == nil`, así que el pedido se ingiere sin líneas.

**Cuándo se rompe.** Una vez corregido el anidamiento de GetOrders, cada pedido de Falabella llama a `lineasDe`, el unmarshal falla, la condición `err == nil` de la línea 350 es falsa y `ord.Lines` queda nil. `ordenes.ingerir` guarda el pedido con cabecera y total pero sin una sola línea (internal/ordenes/ordenes.go:127-133), y el `sale.order` que se monta en Odoo queda vacío: el operador ve un pedido con total 45.990 y ningún producto que despachar, sin ningún error registrado en ninguna parte.

**Arreglo propuesto.** Leer `Body.OrderItems.OrderItem` aceptando objeto o array, y propagar el error de `lineasDe` en vez de descartarlo: un pedido sin líneas debe ir a la cola de atención, nunca a Odoo.

### 5. FetchOrders ignora el cursor y no envía Offset: 40 llamadas idénticas por ronda y los pedidos a partir del 101 no se ingieren

`internal/conectores/falabella/falabella.go:295` · Falabella

**Qué falla.** `FetchOrders` recibe `cur channel.Cursor` y no lo usa: fija `Limit=100`, nunca manda `Offset` y devuelve `OrderPage` con `Next` a cero, de modo que el bucle del núcleo repite la misma primera página.

**Cuándo se rompe.** Una cuenta con 150 pedidos en la ventana (la primera pasada abre 7 días, internal/ordenes/ordenes.go:88): `FetchOrders` devuelve los mismos 100 pedidos, `Done` es falso (100 < 100 es falso), `cur = pagina.Next` es el cursor cero, y el bucle `for i := 0; i < maxPaginas` (ordenes.go:98) repite la llamada 40 veces — 40 peticiones firmadas idénticas contra una API con cupo, y 4.000 pedidos duplicados que la deduplicación tiene que descartar. Los 50 pedidos restantes no se ingieren en esa ronda y, según cómo ordene Seller Center por defecto, la marca de agua avanza hasta la fecha máxima de los 100 devueltos y los deja fuera de forma definitiva.

**Arreglo propuesto.** Enviar `p["Offset"] = strconv.Itoa(cur.Page * limite)` y devolver `Next: channel.Cursor{Page: cur.Page + 1, Size: limite}`, como ya hace `ListRemote` en la línea 261. Añadir `SortBy=created_at&SortDirection=ASC` para que la marca de agua avance por el pedido más viejo y no pueda saltarse ninguno.

### 6. El FeedID se lee de Body.FeedId cuando Falabella lo devuelve en Head.RequestId, y EstadoFeed no lo llama nadie

`internal/conectores/falabella/falabella.go:423` · Falabella

**Qué falla.** `enviarProductos` extrae el identificador del feed de `SuccessResponse.Body.FeedId`, pero la respuesta de ProductCreate/ProductUpdate trae el `Body` vacío y el identificador en `Head.RequestId`; además `EstadoFeed` no se invoca desde ningún punto del código, así que el resultado real de la escritura nunca se comprueba.

**Cuándo se rompe.** Se publica un producto: el feed se acepta (HTTP 200), `feedID` queda en cadena vacía, `Publish` devuelve el aviso «escritura asíncrona: el resultado se confirma en el feed » sin identificador, y `publicar` (internal/publicar/manejadores.go:107) guarda `HashContenido`, `HashPrecio` y `HashStock` como si estuviera publicado. Si Falabella rechaza el feed —atributo obligatorio ausente, EAN duplicado, categoría inválida—, Integra cree que el producto está en venta, el motor de diff no vuelve a encolarlo nunca (los hashes coinciden) y el producto no existe en el Seller Center. El campo `RequestId` sí se parsea en la línea 420 pero no se usa, y `OpResult.FeedID` no lo lee nadie.

**Arreglo propuesto.** Tomar el feed de `Head.RequestId`, persistirlo junto a la publicación y añadir un trabajo de seguimiento que consulte `EstadoFeed` hasta `Finished`; solo entonces guardar los hashes. Mientras el feed no se confirme, la publicación no debe darse por sincronizada.

### 7. Los atributos obligatorios de la categoría se calculan, se validan y se tiran: productoXML no tiene dónde ponerlos

`internal/conectores/falabella/falabella.go:123` · Falabella

**Qué falla.** `publicar` rellena `prod.Attributes` con los atributos obligatorios de la categoría, pero `Publish` nunca lee `p.Attributes` y la estructura `productoXML` (líneas 71-84) no tiene ningún campo donde serializarlos; Falabella los exige dentro de `<ProductData>`.

**Cuándo se rompe.** Un producto de una categoría que exige, por ejemplo, `talla` y `color`: el motor comprueba que ambos tengan valor (internal/publicar/manejadores.go:85-95) y construye `prod.Attributes`. `Publish` arma `productoXML` en la línea 123 sin tocar ese mapa, y el XML enviado no lleva `<ProductData>` en absoluto. Falabella acepta el feed y lo rechaza después con «mandatory attribute missing»; como el resultado del feed no se consulta (hallazgo del FeedID), Integra da el producto por publicado. Toda la validación de atributos que hace el núcleo para Falabella es decorativa.

**Arreglo propuesto.** Añadir a `productoXML` un campo `ProductData` que serialice `ConditionType`, las medidas de paquete y cada par nombre/valor de `req.Product.Attributes` como elemento propio (`<talla>M</talla>`), que es como Seller Center identifica los atributos: por nombre.

### 8. Precio, stock y estado se envían como campos planos del producto; el modelo actual de Falabella los pide dentro de BusinessUnits

`internal/conectores/falabella/falabella.go:77` · Falabella

**Qué falla.** `Price`, `SalePrice`, `Quantity` y `Status` se serializan como hijos directos de `<Product>`; el ejemplo vigente de ProductCreate y la respuesta de GetProducts los sitúan dentro de `<BusinessUnits><BusinessUnit>` con un `OperatorCode` (faco para Colombia), campo que no existe en ninguna parte del proyecto.

**Cuándo se rompe.** Un cambio de precio dispara `UpdatePrice`, que manda `<Product><SellerSku>X</SellerSku><Price>129900.00</Price></Product>`. Falabella procesa el feed sin actualizar el precio del operador, porque el precio vive por unidad de negocio; el mismo feed devuelve 200, Integra guarda `HashPrecio` (internal/publicar/manejadores.go:130) y el precio publicado queda congelado en el valor viejo sin que ninguna alerta lo note. Lo mismo con el stock (`UpdateStock`, línea 197) y con `Pause`/`Resume`, que mandan `<Status>inactive</Status>` fuera de la unidad de negocio: un producto que se quiere pausar sigue vendiéndose.

**Arreglo propuesto.** Añadir `<BusinessUnits>` a `productoXML` con el OperatorCode de la cuenta (guardarlo junto a user_id/api_key) y mover ahí Price/SpecialPrice/SpecialFromDate/SpecialToDate/Stock/Status. Confirmar contra la cuenta real antes de tocar: las páginas heredadas de la documentación todavía muestran la forma plana, así que conviene mandar un feed de prueba con las dos formas y mirar el FeedStatus.

### 9. Un cambio de contenido en un producto ya publicado en Falabella no se envía nunca, pero se marca como sincronizado

`internal/conectores/falabella/falabella.go:109` · Falabella

**Qué falla.** `Publish` corta en cuanto el SKU existe en el Seller Center y devuelve `Adopted` sin mandar nada; el motor solo llama a `Publish` (nunca a `Update`) y acto seguido guarda el `HashContenido` nuevo, así que la edición se da por aplicada.

**Cuándo se rompe.** Un producto ya está publicado en Falabella. El operador corrige la descripción o cambia la foto principal en Integra. `Planificar` detecta `c.ContentHash != hContenido` y encola `publicar_producto` (internal/publicar/motor.go:67). `publicar` llama a `ad.Publish`; `existeSKU` devuelve true, `Publish` retorna en la línea 113 con `Adopted: true` y sin enviar un solo feed. `publicar` guarda entonces el hash nuevo (manejadores.go:107-109). En la siguiente planificación los hashes coinciden y el cambio no se vuelve a encolar jamás: Falabella conserva la descripción y la foto viejas para siempre, y la interfaz muestra la publicación como al día. El método `Update` de la línea 158 —el único que mandaría ProductUpdate— no lo llama nadie en todo el repositorio.

**Arreglo propuesto.** Cuando el SKU ya existe, enviar un ProductUpdate con la ficha completa en vez de devolver la adopción vacía (o hacer que el motor llame a `Update` si ya hay `ExternalID`). Y no guardar `HashContenido` cuando `res.Adopted` sea true y no se haya enviado nada.

### 10. PackageWeight se envía como hijo directo de Product; Falabella solo lo lee dentro de ProductData

`internal/conectores/falabella/falabella.go:81` · Falabella

**Qué falla.** El adaptador exige el peso antes de publicar (línea 102) y luego lo escribe en `<Product><PackageWeight>`, pero el ejemplo vigente de ProductCreate lo sitúa en `<Product><ProductData><PackageWeight>` junto a PackageWidth/Length/Height y ConditionType.

**Cuándo se rompe.** Se publica un producto con peso 0,850 kg. El XML lleva `<PackageWeight>0.850</PackageWeight>` fuera de `<ProductData>`, Falabella lo ignora y el producto queda sin peso de despacho — exactamente lo que la comprobación de la línea 102-104 pretendía evitar («Falabella necesita el peso para calcular el envío»). El vendedor descubre el problema cuando el marketplace no puede cotizar el envío o cobra una tarifa equivocada.

**Arreglo propuesto.** Mover PackageWeight (y añadir ConditionType, que también es obligatorio) al bloque `<ProductData>` del mismo `productoXML`.

### 11. La edición masiva ignora el filtro de categoría que manda la pantalla

`internal/api/server.go:329` · Frontend

**Qué falla.** La pantalla Productos envía el filtro con la clave `categoria`, pero el struct que decodifica POST /api/productos/masivo no la declara y `IDsDeFiltro` tampoco la aplica, así que la operación se resuelve sobre un conjunto mucho mayor que el que el operador está viendo.

**Cuándo se rompe.** El operador filtra por una categoría (p. ej. «Cómputo / Tabletas», 12 productos), no marca ninguna casilla, abre «Editar en masa» → alcance «Todos los 12 del filtro actual» → «Precio = coste × 1.35» y aplica. El cuerpo enviado es {filtro:{q:'',marca:'',categoria:'Cómputo / Tabletas',problemas:false,excluidos:false,sin_precio:false},operacion:{...}}. Go descarta el campo desconocido `categoria`, IDsDeFiltro devuelve TODO el catálogo activo (hasta 2000 ids) y se reescribe el precio de los 450 productos en vez de 12. El precio es propiedad de Integra y no hay deshacer. La única señal es que la simulación devuelve un `afectados` mayor, pero el diálogo sigue rotulando «12 productos afectados» en su cabecera.

**Arreglo propuesto.** Añadir `Categoria string `json:"categoria"`` al struct de editarMasivo, pasarlo a store.FiltroProductos y replicar en IDsDeFiltro la condición `p.categ_path = $n` que ya tienen ListarProductos y FilasParaPlantilla.

### 12. Editar el precio en la interfaz no invalida `effective_prices`: el motor no detecta el cambio y publica el precio viejo

`internal/api/server.go:796` · Núcleo

**Qué falla.** `editarProducto` escribe `product_variants.price` pero no recalcula los precios efectivos, y `CandidatosPublicacion` prefiere el valor guardado en `effective_prices` para el precio y para el hash.

**Cuándo se rompe.** Una promoción cualquiera hace que el planificador llame a `RecalcularPreciosCuenta` (planificador.go:118), que llena `effective_prices` para toda la cuenta. Días después el operador cambia el precio de un SKU de $100.000 a $150.000 desde la ficha. `ActualizarPrecio` toca solo `product_variants.price`; `effective_prices.regular_price` sigue en $100.000. `CandidatosPublicacion` hace `COALESCE(ep.sale_price, ep.regular_price, 0)` y solo cae al cálculo por comisión si eso da 0, así que `PrecioCanal` sigue siendo el viejo: `HashPrecio` no cambia, no se encola `actualizar_precio`, y si por otra razón se republicara la ficha, saldría con el precio antiguo. La interfaz muestra $150.000 y el canal vende a $100.000. Ninguna pantalla del frontend llama al endpoint de recálculo (`grep -rn recalcular web/src` no devuelve nada), así que no hay forma de arreglarlo desde la UI.

**Arreglo propuesto.** Invalidar/recalcular la fila de `effective_prices` de la variante en `ActualizarPrecio` (y en los cambios de comisión, reglas y overrides), o hacer que `Planificar` recalcule la cuenta antes de comparar hashes.

### 13. Un producto archivado o borrado en Odoo sigue publicado y a la venta en los canales

`internal/sync/catalogo.go:66` · Núcleo

**Qué falla.** El sync solo hace upsert de lo que Odoo devuelve; no marca inactivo lo que dejó de venir, y nada en el código pone `products.active` o `product_variants.active` en false.

**Cuándo se rompe.** Se archiva un producto descatalogado en Odoo. El dominio de `SearchRead` sobre product.product usa el filtro activo por defecto de Odoo, así que deja de aparecer en la lectura. En Integra su fila conserva `active = true` y `excluded_reason IS NULL`, de modo que `CandidatosPublicacion` (publicacion.go:91) lo sigue devolviendo como candidato `Listo` y el motor lo sigue publicando, actualizándole precio y stock. Si el producto se borra de verdad, sus `stock.quant` desaparecen, `ReemplazarStock` lo deja en 0 y se le empuja stock 0 a los cuatro canales, pero la publicación nunca se pausa ni se retira. Ninguna sincronización posterior lo corrige.

**Arreglo propuesto.** En el sync completo, leer también con `active_test=false` (o comparar el conjunto de ids leídos) y marcar `active=false` lo que Odoo ya no devuelve activo; y encolar `Pause` de sus publicaciones.

### 14. El cupo por canal (rate_limit_rps / rate_limit_burst) está en el esquema pero ningún código lo lee

`migrations/001_core.sql:117` · Operación

**Qué falla.** No existe ningún limitador de ritmo: el worker lanza tantas llamadas simultáneas al canal como concurrencia tenga configurada, ignorando el cupo declarado por cuenta.

**Cuándo se rompe.** Se planifica la publicación inicial (452 productos × 4 canales) con la concurrencia por defecto de 8. El worker reclama 8 trabajos por tick de 2 s y dispara 8 peticiones simultáneas contra la misma cuenta de Shopify, cuyo límite documentado en el propio código es 2 req/s. Shopify responde 429 en cadena, MercadoLibre puede bloquear la aplicación temporalmente, y como todos esos 429 vuelven a la cola con backoff, la tormenta se repite. La columna `rate_limit_rps NUMERIC DEFAULT 2.0` que la interfaz permitiría ajustar no tiene efecto alguno.

**Arreglo propuesto.** Añadir un limitador por cuenta (golang.org/x/time/rate) construido a partir de rate_limit_rps/burst en el punto donde se obtiene el adaptador (internal/conectores.AdaptadorDeCuenta), y hacer que el worker no reclame más trabajos de una cuenta de los que su cupo permite.

### 15. consumer_key/consumer_secret de WooCommerce acaban en mensajes de error persistidos y devueltos por la API

`internal/conectores/probar.go:96` · Seguridad

**Qué falla.** Las credenciales de WooCommerce viajan en la query string; cuando la petición falla a nivel de transporte, el error de Go incluye la URL completa con el secreto, y ese texto se guarda en channel_accounts.config.probada_msg y se devuelve en GET /api/cuentas a cualquier viewer.

**Cuándo se rompe.** Un operador da de alta la cuenta de WooCommerce con una URL mal escrita, o la tienda está caída/DNS falla, y pulsa Probar. hacer() devuelve el error crudo de cliente.Do (probar.go:262-265) → server.go:649 `msg = err.Error()` → server.go:670 AnotarPrueba guarda `probada_msg = Get "https://tienda/wp-json/wc/v3/products?consumer_key=ck_…&consumer_secret=cs_…&per_page=1": dial tcp…` en claro en la base (al lado de credentials_enc cifrado) → store/cuentas.go:74-76 ListarCuentas lo devuelve como ProbadaMsg a cualquier sesión, incluidos viewers. El mismo err.Error() se guarda en product_channel_listings.last_error y jobs.last_error desde el adaptador (woocommerce.go:377 `Message: err.Error()`, publicar/manejadores.go:100) en cada caída de la tienda durante una publicación.

**Arreglo propuesto.** Enviar las claves de Woo por Basic Auth (Woo lo admite sobre HTTPS) o, como mínimo, envolver los errores de transporte con un mensaje propio que no incluya la URL (usar url.Error.Err) antes de guardarlos o devolverlos.

### 16. Publish adopta y devuelve éxito sin enviar el contenido; Adapter.Update no lo llama nadie

`internal/conectores/shopify/shopify.go:78` · Shopify

**Qué falla.** Cuando encuentra el SKU, Publish devuelve Adopted sin hacer ninguna escritura, y el manejador guarda el content_hash nuevo: el cambio de ficha se marca como sincronizado sin haber llegado a Shopify.

**Cuándo se rompe.** Producto ya publicado y dentro de los 250 primeros. Se corrige el título (o se cambia una foto) en Integra. Planificar encola TrabajoPublicar porque ContentHash != hContenido (motor.go:67-73; el hash de contenido cubre título, descripción e imágenes). Publish encuentra el SKU y devuelve {Adopted:true} sin llamar a la API. manejadores.go:108 guarda HashContenido(*c) en variant_channel_listings. Resultado: Shopify conserva el título viejo para siempre —ninguna pasada posterior volverá a encolar nada porque el hash ya coincide— y la interfaz muestra la publicación como sincronizada. `Adapter.Update`, que es quien sabría mandar title/body_html/vendor, no tiene ni un solo llamador en todo el repositorio.

**Arreglo propuesto.** En el motor, encolar TrabajoActualizar (Update) cuando ya existe ExternalID y solo cambió el contenido, y reservar Publish para la primera publicación; en el adaptador, que la rama de adopción haga el PUT de contenido antes de devolver Adopted.

### 17. El stock del Publish nunca llega: inventory_quantity es de solo lectura en la REST

`internal/conectores/shopify/shopify.go:102` · Shopify

**Qué falla.** Publish manda el stock en variants[0].inventory_quantity, campo que la Admin API documenta como de solo lectura; Shopify lo ignora y el producto nace con 0 disponibles, pero Integra guarda el stock_hash como si se hubiera enviado.

**Cuándo se rompe.** Se publica un SKU con 40 unidades. Shopify crea el producto en borrador con available=0. manejadores.go:108 llama a GuardarPublicacion con HashStock(*c) = hash de 40, así que en la siguiente pasada Planificar compara 40 contra 40, no encuentra cambio y no encola TrabajoStock (motor.go:83). El día que una persona activa el producto en Shopify, aparece agotado y no vende; el desajuste solo se corrige cuando el stock cambia en Odoo, que en un SKU de rotación lenta puede no pasar en meses.

**Arreglo propuesto.** Tras crear el producto, llamar a inventory_levels/set con el inventory_item_id de la variante recién creada (viene en la respuesta del POST) antes de devolver el PublishResult; o no guardar el stock_hash en el manejador de publicar, para que la siguiente pasada encole el trabajo de stock.

### 18. El precio de línea de Shopify es antes de descuentos: el sale.order de Odoo queda por encima de lo cobrado

`internal/conectores/shopify/shopify.go:340` · Shopify

**Qué falla.** OrderLine.UnitPrice se toma de line_items[].price, que la documentación define como el precio antes de descuentos, y se ignoran total_discount y discount_allocations.

**Cuándo se rompe.** Un comprador usa un código de descuento del 20 % sobre un artículo de 129.900 COP y paga 103.920. FetchOrders guarda UnitPrice=129.900 y TotalPrice=129.900; ordenes.go:237 crea la línea del sale.order con price_unit=129.900. En Odoo queda un pedido por 129.900 que no cuadra con lo cobrado en Shopify ni con total_price del pedido; si alguien lo confirma, factura de más.

**Arreglo propuesto.** Leer también total_discount (o sumar discount_allocations) y enviar UnitPrice = (price*quantity − total_discount)/quantity, dejando TotalPrice = price*quantity − total_discount; y contrastar la suma de líneas contra total_price antes de montar el pedido.

### 19. Un cambio de ficha ya publicada nunca llega a WooCommerce, pero el motor lo da por sincronizado

`internal/conectores/woocommerce/woocommerce.go:70` · WooCommerce

**Qué falla.** Publish devuelve `Adopted` sin escribir nada cuando el SKU ya existe, y como el motor solo llama a Publish (nunca a Update), toda edición de título, descripción, imágenes, marca o peso se pierde mientras el hash de contenido se guarda como enviado.

**Cuándo se rompe.** Un producto ya está publicado en la tienda. Se corrige su título desde la interfaz (`PATCH /api/productos/{id}`). En la siguiente pasada, publicar.Planificar (motor.go:68) ve `ContentHash != hContenido` y encola TrabajoPublicar. El manejador llama `ad.Publish` (manejadores.go:98); el adaptador busca el SKU, lo encuentra, y devuelve Adopted=true sin hacer ninguna escritura. manejadores.go:107 guarda entonces `HashContenido(*c)` como publicado, así que el motor no volverá a encolar nada: la tienda conserva el título viejo para siempre y la interfaz muestra la publicación al día. Lo mismo con una foto nueva o una descripción reescrita, que también entran en HashContenido (motor.go:110-129).

**Arreglo propuesto.** Que el manejador de contenido llame a `Update` cuando ya hay `ExternalID`, y que el `Update` de WooCommerce mande el cuerpo completo (name, description, images, weight, atributo Marca); si se prefiere seguir entrando por Publish, que la rama de adopción haga el PUT con el contenido nuevo en vez de devolver sin escribir.

---

## Severidad media (34)

### 1. `integra migrate down` destruye sin aviso datos propiedad de Integra que no se pueden reconstruir

`cmd/integra/main.go:230` · Integridad de datos

**Qué falla.** El comando revierte la última migración sin confirmación ni copia de seguridad, y varias secciones Down eliminan columnas y tablas cuyo contenido es trabajo humano irrecuperable (Odoo ya no es su fuente desde la migración 012).

**Cuándo se rompe.** Un operador aplica de más y ejecuta `integra migrate down` dos o tres veces para «volver atrás». El primer down borra notification_destinations (inocuo). El segundo ejecuta el Down de 017 y elimina products.condicion, garantia_meses, garantia_tipo, video_url, nota_interna y product_variants.largo_cm/ancho_cm/alto_cm. El tercero ejecuta el Down de 016 y hace DROP TABLE producto_atributos (619 filas de atributos deducidos y escritos a mano) y channel_category_attributes (2168 filas). Un cuarto llegaría al Down de 012, que borra product_variants.price: el PVP de todo el catálogo, que desde esa misma migración es un dato manual sin origen en Odoo. Nada avisa ni pide confirmación, y ningún Down repone el contenido.

**Arreglo propuesto.** Exigir confirmación interactiva o un flag `--si-de-verdad` en `migrate down`, y avisar en pantalla de qué tabla o columna con datos se va a perder (contando filas antes de revertir).

### 2. El emparejamiento de líneas de pedido por SKU no tiene desempate ni índice utilizable

`internal/store/ordenes.go:138` · Integridad de datos

**Qué falla.** La subconsulta usa `lower(v.sku) = lower($4) ... LIMIT 1` sin ORDER BY y sin filtrar por conexión de Odoo; product_variants.sku no tiene unicidad y el único índice está sobre sku, no sobre lower(sku).

**Cuándo se rompe.** Escenario documentado en el README («Cambiar de instancia de Odoo sin perder el trabajo»): se conecta la instancia nueva, el sync crea filas nuevas y quedan dos variantes con el mismo SKU —una por conexión— hasta que se borre la vieja. Llega un pedido de MercadoLibre; el LIMIT 1 sin orden elige una fila arbitraria, y si toca la de la conexión antigua, OdooProductIDDeVariante (ordenes.go:333) devuelve el odoo_product_id de la instancia vieja: la línea del sale.order se crea con un product_id que en la instancia actual es otro producto o no existe, y Odoo la crea mal o el Create falla. Con el mismo mecanismo, ResolverSKUs (internal/store/plantilla.go:139) se queda con una de las dos variantes al construir el mapa y la plantilla masiva reprecifica la equivocada.

**Arreglo propuesto.** Resolver el SKU acotando a la conexión activa de Odoo y con ORDER BY determinista (p. ej. el producto de la conexión activa primero); añadir `CREATE INDEX ... ON product_variants (lower(sku)) WHERE sku IS NOT NULL` y un índice único parcial por (odoo_connection_id, lower(sku)) para que la ambigüedad sea imposible dentro de una instancia.

### 3. ReemplazarStock borra el stock de todas las conexiones de Odoo, no solo el de la que se está sincronizando

`internal/store/store.go:375` · Integridad de datos

**Qué falla.** El DELETE no lleva WHERE, pero las filas que se reinsertan solo son las de la conexión sincronizada; las consultas de publicación y de precios tampoco acotan por odoo_connection_id.

**Cuándo se rompe.** Durante el traslado documentado entre instancias (`integra conexiones migrar 1 4`) conviven dos conexiones con el mismo catálogo. El sync de la conexión 4 ejecuta `DELETE FROM variant_stock` (todas las filas) y reinserta solo las variantes de la 4: los productos de la conexión 1 quedan con stock 0. Como CandidatosPublicacion (internal/store/publicacion.go:70-92) y RecalcularPreciosCuenta (internal/store/precios.go:464-470) no filtran por conexión, esos productos siguen siendo candidatos —con precio e imágenes ya migrados— y el motor de publicación crea una SEGUNDA publicación en el canal para el mismo SKU, esta vez con stock 0, mientras la buena sigue viva.

**Arreglo propuesto.** Acotar el DELETE a las variantes de la conexión (`DELETE FROM variant_stock vs USING product_variants v JOIN products p ... WHERE p.odoo_connection_id = $1`) y filtrar CandidatosPublicacion, RecalcularPreciosCuenta y Resumen por la conexión activa.

### 4. variant_stock.qty_free y channel_accounts.stock_buffer/pause_on_zero se escriben o se declaran pero nunca se leen

`migrations/002_catalog.sql:93` · Integridad de datos

**Qué falla.** El sync calcula y guarda qty_free (existencias menos reservadas) y el esquema define stock_buffer y pause_on_zero como reserva anti-sobreventa, pero todas las rutas de publicación usan qty_on_hand en bruto.

**Cuándo se rompe.** Un producto tiene 3 unidades en la bodega y las 3 reservadas para una entrega de Odoo ya confirmada. El sync guarda qty_on_hand = 3 y qty_free = 0 (internal/sync/catalogo.go:225-228 resta reserved_quantity). CandidatosPublicacion publica 3 en MercadoLibre. Se vende una unidad que ya estaba comprometida con otro pedido. Lo mismo con stock_buffer: aunque un operador quisiera reservar 2 unidades de colchón, no hay ni lectura de la columna ni interfaz para fijarla.

**Arreglo propuesto.** Publicar `GREATEST(sum(qty_free) - stock_buffer, 0)` en lugar de sum(qty_on_hand), o quitar del esquema y de la documentación las columnas que no se van a usar para que nadie confíe en una protección inexistente.

### 5. El README declara «Fase 0 completa» cuando las fases 2, 3 y 4 están hechas

`README.md:11` · Documentación

**Qué falla.** La cabecera de estado del README describe un proyecto sin conectores ni API, mientras el código tiene los cuatro adaptadores registrados, 67 rutas HTTP, autenticación e ingesta de pedidos.

**Cuándo se rompe.** Alguien que evalúa Integra (o un agente de la ola 1 que se apoya en el README) lee «Estado: Fase 0 completa. […] El conector de catálogo es la Fase 1» y concluye que no hay conectores de canal ni servidor; planifica o presupuesta trabajo que ya está hecho. La misma tabla de comandos rotula `integra serve` como «(Fase 5)» y `integra worker` como «(Fase 2)», numeración que además choca con PLAN.md, donde la Fase 5 es «Órdenes de vuelta» y la Fase 2 es el motor de trabajos.

**Arreglo propuesto.** Sustituir la línea de estado por el estado real (fases 2-4 completas, fase 5 parcial, fase 6 con auth hecha) y quitar las etiquetas de fase de la tabla de comandos, que ya solo aportan ruido y contradicen PLAN.md.

### 6. La firma usa url.QueryEscape, que codifica el espacio como '+'; Falabella firma con rawurlencode (%20)

`internal/conectores/probar.go:128` · Falabella

**Qué falla.** `FirmarFalabella` documenta «codificados RFC 3986» pero usa `url.QueryEscape`, que codifica el espacio como `+`. Seller Center recalcula la firma con `rawurlencode`, que produce `%20`: cualquier parámetro con espacio da firmas distintas y la llamada se rechaza con E007.

**Cuándo se rompe.** 31 de los 545 SKU del catálogo actual contienen un espacio (por ejemplo «RingConn POS Display A version - Power Version»). Al publicarlos, `existeSKU` manda `SkuSellerList=["RingConn POS Display A version - Power Version"]`; la cadena firmada lleva `+` donde el servidor pone `%20`, la firma no coincide y Falabella responde «E007: Login failed. Signature mismatch». Esos 31 productos no se pueden consultar ni publicar nunca, y el error que ve el operador («Login failed») apunta a la credencial, no al SKU. El mismo fallo alcanza a `AckOrder` con cualquier transportadora de nombre compuesto («Servientrega Express»).

**Arreglo propuesto.** Escapar con una función RFC 3986 (`strings.ReplaceAll(url.QueryEscape(v), "+", "%20")` o `url.PathEscape` ajustado) tanto para construir la cadena firmada como para la consulta enviada, y añadir una prueba con un valor que contenga espacio contra un vector de firma conocido.

### 7. Los errores de la carga de plantilla llegan a la pantalla como «HTTP 400»

`internal/api/plantillas.go:113` · Frontend

**Qué falla.** cargarPlantilla responde con http.Error (text/plain) mientras el cliente `pedir` solo sabe leer `{error: "..."}`; el mensaje concreto que el backend se molesta en redactar se pierde y el operador ve el código HTTP pelado.

**Cuándo se rompe.** En Productos → «Actualizar por plantilla» → paso 2, el operador sube un .xls antiguo o un CSV renombrado a .xlsx. El servidor responde 400 con el cuerpo de texto «el archivo no se pudo abrir como Excel (.xlsx): …». En api.ts, `r.json()` lanza, se cae al `catch` y queda `detalle = "HTTP 400"`, así que PlantillaMasiva muestra la caja de aviso con «HTTP 400» y nada más. Lo mismo con «el archivo es demasiado grande» (413) y «falta el archivo (campo «archivo»)». El operador no puede saber qué corregir. Idéntico efecto en Login con una cuenta desactivada: el servidor manda 403 «el usuario está inactivo» y la pantalla dice «No se pudo entrar (HTTP 403)».

**Arreglo propuesto.** Sustituir los http.Error de internal/api por `escribir(w, code, map[string]string{"error": …})`, que es la forma que el resto de la API ya usa y la única que la interfaz sabe leer.

### 8. «Cerrar sesión» no cierra la sesión en el servidor: la cookie integra_token sigue viva 24 h

`web/src/App.tsx:33` · Frontend

**Qué falla.** El login deja una cookie HttpOnly `integra_token` válida 24 h que el middleware acepta como credencial, pero ninguna pantalla llama a POST /api/auth/logout: salir solo borra el localStorage del navegador.

**Cuándo se rompe.** Un operador entra en un equipo compartido y pulsa el icono de salir de la barra lateral. La UI vuelve al login (se borró `integra_sesion` de localStorage), pero la cookie `integra_token` sigue en el navegador y sigue siendo válida hasta 24 h después del login. Quien use ese equipo puede abrir http://127.0.0.1:5580/api/productos (o /api/cuentas, /api/ordenes) en la barra de direcciones y ver el catálogo completo, o ejecutar peticiones desde la consola del navegador con la sesión del usuario anterior: extraerClaims usa la cookie cuando no hay cabecera Authorization. El endpoint que limpiaría la cookie existe y no lo llama nadie.

**Arreglo propuesto.** Que onSalir llame a POST /api/auth/logout (ignorando el fallo de red) antes de borrar el localStorage. Como el token es HMAC sin estado, conviene además no emitir la cookie si la UI no la usa, o llevar una lista de revocación.

### 9. Guardar un atributo de canal que falla no muestra ningún error

`web/src/Atributos.tsx:123` · Frontend

**Qué falla.** PanelAtributos.guardar envuelve la llamada en try/finally sin catch: si el PUT falla, la promesa se rechaza sin manejar, no se pinta ningún mensaje y el valor tecleado sigue en pantalla como si se hubiera guardado.

**Cuándo se rompe.** Un usuario con rol `viewer` (o cualquiera, ante un 422 del store o un 503 de base caída) abre la vista previa de un producto, escribe el valor de un atributo obligatorio de MercadoLibre y pulsa «Guardar». El middleware responde 403 {"error":"tu rol es de solo lectura"}; `pedir` lanza, el `finally` quita el indicador «…», no hay catch, no se llama a cargar() y la fila sigue mostrando el valor escrito con el botón «Guardar» visible. El operador cierra la ficha convencido de que el atributo quedó puesto; al reabrirla está vacío y la publicación en ML seguirá rechazándose.

**Arreglo propuesto.** Añadir `catch` con un estado de error propio en PanelAtributos (como hacen Imagenes.tsx y Promocion.tsx) y pintarlo en una caja de aviso.

### 10. Pausar, borrar un horario o marcar un aviso como visto falla en silencio

`web/src/Automatizacion.tsx:34` · Frontend

**Qué falla.** Las tres acciones de la pantalla Automatización (`alternar`, `borrar`, `reconocer`) llaman a la API sin try/catch: cualquier fallo deja una promesa rechazada sin manejar y la pantalla no cambia ni avisa.

**Cuándo se rompe.** El operador pulsa «Pausar» sobre la sincronización nocturna. Si la petición falla —403 por rol de solo lectura, 422 de GuardarHorario (zona/hora/alcance inválidos guardados antes por otra vía), 503 del middleware con Postgres caído— no se ejecuta `cargar()`, la fila sigue rotulada «Activo» y no aparece ninguna caja de error: la única señal es un «Uncaught (in promise)» en la consola. El operador cree que dejó pausado un horario que va a seguir disparando sincronizaciones y publicaciones a las 02:00. Igual con «Borrar» y con «Visto» sobre una alerta.

**Arreglo propuesto.** Envolver las tres funciones en try/catch reutilizando el setError que ya existe, como hace `probar` en Cuentas.tsx:53-64.

### 11. Un producto con más de una variante se republica en bucle en cada planificación

`internal/publicar/motor.go:68` · Núcleo

**Qué falla.** `HashContenido` incluye campos de variante (SKU, código de barras, peso) pero el hash se guarda en `product_channel_listings`, que es una fila por producto y cuenta: dos variantes se pisan el hash mutuamente sin fin.

**Cuándo se rompe.** Un producto con dos variantes (talla S y M, SKUs distintos). Planificación 1: la variante S tiene ContentHash del producto vacío o ajeno → se publica → pcl.content_hash = hash(S). Planificación 2: la variante M lee ese mismo `pcl.content_hash` (el LEFT JOIN de publicacion.go:89 es por product_id) y no coincide con hash(M) → se publica → pcl.content_hash = hash(M). Planificación 3: S vuelve a no coincidir. El ciclo no termina nunca: en cada corrida se encola un `publicar_producto` por variante y por cuenta, que es el endpoint más caro de los cuatro canales y el que más cerca está de los cupos. Hoy no se dispara porque el catálogo sincronizado no tiene productos multivariante (0 de 545), pero el esquema y el sync (que lee product.product) los admiten.

**Arreglo propuesto.** Sacar del hash de contenido los campos de variante (o guardar un content_hash por variante en `variant_channel_listings`) y calcular el hash de producto una sola vez por producto, no por variante.

### 12. Un cambio de SKU en Odoo deja la publicación inalcanzable y hace fallar los pedidos de ese producto

`internal/store/publicacion.go:178` · Núcleo

**Qué falla.** `GuardarPublicacion` escribe siempre `channel_sku = NULL` y `RefDePublicacion` devuelve el SKU actual de la variante, no el que se publicó, así que tras renombrar el SKU las actualizaciones apuntan a una referencia que el canal no conoce.

**Cuándo se rompe.** Se corrige el `default_code` de un producto en Odoo (de ABC-123 a ABC-123-N). El sync actualiza `product_variants.sku`. En Falabella, donde el seller SKU ES la referencia de la publicación, el siguiente `actualizar_precio`/`actualizar_stock` viaja con `SellerSku = ABC-123-N` (falabella.go:181), que no existe en el Seller Center: el feed falla o crea basura, y la publicación real de ABC-123 se queda congelada con el precio y el stock viejos. Además, un pedido que llegue con el SKU antiguo no empareja: `GuardarOrden` busca `lower(v.sku) = lower($4)` contra el SKU nuevo, la línea queda con `variant_id` nulo y `CrearPedido` marca el pedido como fallido con "el SKU no existe en el catálogo".

**Arreglo propuesto.** Rellenar `channel_sku` con el SKU realmente publicado y que `RefDePublicacion` lo devuelva; detectar el cambio de SKU en el sync para encolar una republicación o una pausa, y emparejar las líneas de pedido también contra `channel_sku` histórico.

### 13. El cliente XML-RPC ignora el context y espera hasta 180 s por llamada: puede superar el lease del worker y duplicar el pedido

`internal/odoo/xmlrpc/xmlrpc.go:48` · Odoo

**Qué falla.** `Call` construye la petición con `http.NewRequest` sin contexto y con timeout de 180 s por llamada, mientras el lease de un trabajo es de 5 minutos y `CrearPedido` encadena hasta cinco llamadas.

**Cuándo se rompe.** Odoo responde muy lento (backup, instancia saturada). `CrearPedido` hace authenticate + BuscarUno(sale.order) + BuscarUno(res.partner) + Create(res.partner) + Create(sale.order); con respuestas cercanas al timeout se superan los 5 minutos del lease. `RecuperarHuerfanos` devuelve el trabajo a 'pending' mientras la primera ejecución sigue viva, otro worker lo reclama, ambos pasan el `BuscarUno` por `client_order_ref` (que aún no existe) y ambos crean el pedido: dos sale.order con la misma referencia, porque Odoo no impone unicidad sobre `client_order_ref`. Además, cancelar el worker no aborta la llamada en curso: el apagado se queda esperando hasta 180 s por llamada.

**Arreglo propuesto.** Propagar el `context.Context` hasta `xmlrpc.Call` (http.NewRequestWithContext) para que el apagado y el lease corten de verdad, bajar el timeout por llamada a algo por debajo del lease, y crear el sale.order con `client_order_ref` protegido —índice único en Odoo o releer por referencia inmediatamente después del create— para que el duplicado sea imposible y no solo improbable.

### 14. El sale.order se crea sin moneda ni tarifa: un pedido en COP puede quedar valorado en USD

`internal/ordenes/ordenes.go:244` · Odoo

**Qué falla.** El mapa de valores del `sale.order` no fija `pricelist_id` ni `currency_id`, y la moneda que trajo el canal (`o.Moneda`) no se usa ni se verifica, así que el pedido hereda la moneda de la tarifa del cliente o de la compañía.

**Cuándo se rompe.** Llega una venta de MercadoLibre por 129.900 COP. Integra crea el partner nuevo (sin tarifa asignada) y el sale.order sin `pricelist_id`; Odoo calcula `currency_id` desde la tarifa y, a falta de ella, desde la compañía. En la instancia de pruebas actual eso es USD: el pedido queda como 129.900 USD (unos 500 millones de pesos) y quien lo confirma ve «$ 129,900.00», numéricamente idéntico a lo esperado. El propio README y la memoria del proyecto documentan la trampa («la moneda del pedido sale de la TARIFA, no de la compañía»), pero el código de creación no la aplica ni comprueba el resultado.

**Arreglo propuesto.** Resolver la tarifa/moneda de forma explícita: buscar `res.currency` por el código de `o.Moneda`, pasar `pricelist_id` (o `currency_id`) coherente con él y, tras crear, releer `currency_id` del pedido y marcar el pedido como fallido si no coincide con la moneda del canal.

### 15. El comprador se resuelve solo por correo: duplica partners en cada reintento y puede colgar el pedido del cliente equivocado

`internal/ordenes/ordenes.go:275` · Odoo

**Qué falla.** `resolverCliente` busca `res.partner` por email y, si no hay email o no hay coincidencia, crea uno nuevo; el documento del comprador se lee de la base pero nunca se usa.

**Cuándo se rompe.** (a) MercadoLibre no entrega el correo real del comprador. Con `datos.Email == ""` no hay búsqueda posible y se crea un partner nuevo en cada llamada; si la creación del sale.order falla después (Odoo caído, permisos), cada uno de los cinco reintentos deja otro `res.partner` huérfano con el mismo nombre en el ERP. (b) Si el canal devuelve un alias compartido o el correo coincide con un contacto ya existente en Odoo (una dirección genérica de ventas, o el contacto hijo de una empresa), `BuscarUno` con `limit 1` y sin orden devuelve un partner arbitrario y el pedido —con su dirección de entrega y su facturación— queda colgado de otro cliente.

**Arreglo propuesto.** Dar al partner una clave de idempotencia propia del canal: buscar primero por `vat`/documento y, si no hay, por una `ref` determinista tipo «ML-<buyer_id>» que se escriba al crearlo; restringir la búsqueda por correo a `[('email','=',x),('customer_rank','>',0),('parent_id','=',False)]` y volcar documento, país, departamento y código postal en el partner.

### 16. No hay .dockerignore: el contexto de compilación arrastra 500 MB y mete .env con la clave maestra en la imagen intermedia — **corregido en `aea2b7d`**

`Dockerfile:11` · Operación

**Qué falla.** `COPY . .` copia todo el directorio —incluidos datos/ (409 MB), web/node_modules (69 MB), los .exe y el .env con INTEGRA_MASTER_KEY y ODOO_API_KEY— dentro de la etapa de compilación.

**Cuándo se rompe.** Se ejecuta `docker compose build` en la máquina de desarrollo o en un CI que tenga el .env: el demonio recibe más de medio giga de contexto (cada build tarda minutos y se invalida la caché de capas con cualquier cambio en datos/), y la capa de la etapa `build` contiene /src/.env con la clave maestra AES y la API key de Odoo en claro. Esa imagen intermedia queda en el almacén local del constructor y viaja entera si alguien la etiqueta o si el builder usa una caché remota compartida (`--cache-to`), momento en el que la clave que descifra todas las credenciales de canal sale de la máquina.

**Arreglo propuesto.** Añadir un .dockerignore que excluya al menos .env, datos/, web/node_modules, *.exe, .git y bin/; el .gitignore ya lista casi lo mismo y sirve de base.

### 17. `integra migrate` es el único comando que no lee .env: el primer paso del README falla

`cmd/integra/main.go:196` · Operación

**Qué falla.** cmdMigrate consulta os.Getenv directamente en vez de config.Load(), así que ignora el fichero .env que el README manda rellenar.

**Cuándo se rompe.** Un operador sigue el README al pie de la letra: crea .env con INTEGRA_MASTER_KEY e INTEGRA_DATABASE_URL y ejecuta `go run ./cmd/integra migrate up`. El comando falla con «INTEGRA_DATABASE_URL es obligatoria» pese a que la variable SÍ está en .env; el mensaje apunta al sitio equivocado y el operador no tiene forma de saber que este subcomando —y solo este— exige exportar la variable en el shell. Es el primer paso de la instalación y del cambio de esquema en cada actualización.

**Arreglo propuesto.** Llamar a config.Load() (o al menos a cargarDotEnv) al principio de cmdMigrate, igual que hacen el resto de subcomandos.

### 18. `docker compose up` deja api y worker muertos: nadie aplica las migraciones y no hay política de reinicio — **corregido en `aea2b7d`**

`docker-compose.yml:26` · Operación

**Qué falla.** Ningún servicio del compose ejecuta `integra migrate up`, y tanto serve como worker abortan si hay migraciones pendientes; sin `restart:` los contenedores quedan parados para siempre.

**Cuándo se rompe.** En un servidor limpio se hace `INTEGRA_MASTER_KEY=... docker compose up -d`. Postgres arranca vacío y pasa su healthcheck; api y worker arrancan, llaman a comprobarEsquema, encuentran las N migraciones sin aplicar y salen con código 1 y el mensaje «hay N migraciones sin aplicar... Ejecuta: integra migrate up». Como no hay `restart: unless-stopped` ni healthcheck para api (pese a existir GET /healthz), ambos quedan en Exited y el despliegue queda muerto sin que nada lo señale. Lo mismo ocurre tras cada actualización que añada una migración: el redespliegue mata api y worker en vez de arrancar.

**Arreglo propuesto.** Añadir un servicio de un solo uso (`command: ["migrate","up"]`) del que dependan api y worker con `condition: service_completed_successfully`, y poner `restart: unless-stopped` + healthcheck sobre /healthz en ambos.

### 19. La clasificación de errores del canal (EsReintentable y RetryAfter) es código muerto: se reintentan los 4xx definitivos

`internal/channel/adapter.go:334` · Operación

**Qué falla.** El worker manda a reintento cualquier error, sin consultar EsReintentable, y el Retry-After que los adaptadores extraen de los 429 nunca se usa para programar el siguiente intento.

**Cuándo se rompe.** Un producto sale a Falabella sin EAN y el canal responde 400 «EAN requerido». El manejador devuelve el error, worker.procesar llama a Fallar y la cola lo reprograma con backoff (30 s, 1 m, 2 m, 4 m) hasta agotar los 5 intentos: 5 llamadas en vez de 1 para un fallo que nunca se va a arreglar solo. Si el error es sistémico (una regla de validación que afecta a todo el catálogo), son ~2.260 llamadas desperdiciadas contra el cupo del canal, justo lo que el comentario de EsReintentable dice que hay que evitar. En sentido inverso, un 429 con `Retry-After: 60` se reintenta a los 30 s porque el campo RetryAfter que el adaptador rellenó no lo lee nadie.

**Arreglo propuesto.** En worker.procesar, consultar channel.EsReintentable(err) para marcar el trabajo como fallido definitivo sin consumir intentos, y añadir a Cola.Fallar un parámetro de espera mínima alimentado por Error.RetryAfter.

### 20. El apagado del worker no drena: cancela el contexto de los trabajos en vuelo y ni siquiera puede anotar su resultado

`internal/jobs/worker.go:108` · Operación

**Qué falla.** Al recibir SIGTERM el worker pasa el mismo contexto ya cancelado a los manejadores y a Fallar/Completar, así que aborta las llamadas al canal a medias y deja los trabajos colgados en estado 'running'.

**Cuándo se rompe.** Con 8 trabajos de publicación en vuelo se hace `docker compose restart worker` (o el orquestador manda SIGTERM en un despliegue). signal.NotifyContext cancela ctx: las 8 peticiones HTTP a MercadoLibre/Shopify se cortan a mitad —algunas ya llegaron al canal— y los manejadores devuelven «context canceled». El worker llama a `w.cola.Fallar(ctx, ...)` con ESE MISMO contexto cancelado, y pgx devuelve error sin ejecutar el UPDATE: el fallo nunca se registra y la fila sigue con status='running'. El trabajo solo vuelve a la cola cuando expira el lease de 5 minutos y otro worker corre RecuperarHuerfanos, ya con el intento consumido y con last_error «el worker murió con el trabajo en curso». Tres reinicios durante una campaña de publicación agotan los 5 intentos y dejan trabajos en 'failed' sin causa real. Si el POST de creación sí llegó al canal, el reintento crea un ítem duplicado, porque la publicación solo se guarda tras leer la respuesta. El log «worker apagándose: esperando los trabajos en vuelo» describe algo que no ocurre: espera a las goroutines, pero su trabajo ya está abortado.

**Arreglo propuesto.** Separar el contexto de parada del de trabajo: ejecutar los manejadores con un contexto propio (con plazo de drenaje) y usar context.Background() con timeout corto para Fallar/Completar, de modo que el estado del trabajo siempre quede escrito.

### 21. Login sin límite de intentos ni retardo: fuerza bruta en línea contra contraseñas de 6 caracteres

`internal/api/auth.go:44` · Seguridad

**Qué falla.** POST /api/auth/login es público, no tiene limitación por IP ni por cuenta, no bloquea tras fallos y la política mínima de contraseña es de 6 caracteres.

**Cuándo se rompe.** El servidor está expuesto en la nube (PLAN: multiusuario en la nube). Un atacante que conozca un correo de usuario (aparece en audit_logs, en la lista de usuarios o es adivinable) lanza intentos continuos contra /api/auth/login. Con bcrypt cost 10 (~70 ms) obtiene unas 14 pruebas/s por núcleo sin ninguna respuesta 429 ni bloqueo; una contraseña de 6 caracteres del diccionario cae en horas, y de paso consume CPU del servidor en bcrypt.

**Arreglo propuesto.** Limitador por IP y por correo en memoria (por ejemplo 5 fallos → espera exponencial), registrar los fallos en auditoría y subir el mínimo a 8-12 caracteres en HashPassword (la CLI ya exige 8).

### 22. Los tokens no se pueden revocar: cambiar la contraseña o cerrar sesión no expulsa a quien tenga un token robado

`internal/api/auth.go:212` · Seguridad

**Qué falla.** El token es un HMAC sin identificador ni versión; el middleware solo comprueba que el usuario exista y esté activo, así que un cambio de contraseña o un logout dejan válidos todos los tokens ya emitidos hasta 24 h.

**Cuándo se rompe.** Un operador sospecha que le han copiado el token (está en localStorage, sin HttpOnly) y un admin le cambia la contraseña por PATCH /api/usuarios/{id}. ActualizarPassword solo escribe el hash; vigencia() sigue devolviendo existe+activo y el atacante continúa operando con el token viejo hasta que caduque (24 h), editando precios y publicando. POST /api/auth/logout (auth.go:94-103) solo borra la cookie: el Bearer que usa el frontend sigue siendo válido.

**Arreglo propuesto.** Añadir a users una columna `session_version` (o `password_changed_at`) que viaje en los claims y se compare en vigencia(); incrementarla en cambio de contraseña, desactivación y logout.

### 23. PATCH /api/usuarios/{id} permite desactivar o degradar al último administrador (la CLI lo impide, la API no)

`internal/api/auth.go:203` · Seguridad

**Qué falla.** ActualizarUsuario no comprueba que quede al menos un admin activo, a diferencia de usuarioParaBaja en la CLI, y el campo `active` no enviado se decodifica como false.

**Cuándo se rompe.** El único admin edita su propio nombre desde la interfaz y el cliente manda {"name":"Luis","role":"admin"} sin `active` (o marca por error rol operator). ActualizarUsuario escribe active=false / role=operator; `s.sesiones.olvidar(id)` hace que la siguiente petición reciba 401 o 403. Ya no hay ningún admin: nadie puede crear usuarios ni reactivarlo desde la interfaz; hace falta acceso al servidor y a la CLI.

**Arreglo propuesto.** Reutilizar la misma comprobación transaccional de usuarioParaBaja en ActualizarUsuario y usar *bool para `active` (omitido = no tocar).

### 24. El rol operator puede borrar conexiones a Odoo con su catálogo y sustituir credenciales de canal y de Odoo

`internal/api/integraciones.go:259` · Seguridad

**Qué falla.** El único control de roles fuera de /api/usuarios es «viewer no escribe»; cualquier operator ejecuta operaciones destructivas o de configuración de credenciales que no tienen puerta de admin.

**Cuándo se rompe.** Un operator (rol pensado para editar precios y publicar) hace DELETE /api/integraciones/{id} sobre la conexión antigua: borrarIntegracion ejecuta BorrarConexion y elimina todos sus productos (el README avisa: «destructivo: se lleva sus productos por delante»), sin auditoría ni confirmación por rol. También puede PUT /api/cuentas/mercadolibre con credenciales propias, redirigiendo las publicaciones y la ingesta de pedidos a otra cuenta, o POST /api/integraciones apuntando Integra a otro Odoo. Ninguna de estas acciones se registra en audit_logs (solo login y usuarios). Si es una decisión de diseño que operator administre integraciones, queda como deuda de auditoría; el documento PLAN habla de «roles mínimos (admin/operador)» sin asignarles estas operaciones.

**Arreglo propuesto.** Exigir RolAdmin en borrarIntegracion, crearIntegracion, editarIntegracion, activarIntegracion y guardarCuenta, y registrar estas operaciones en audit_logs.

### 25. Subidas multipart sin tope real de cuerpo: ParseMultipartForm no limita el tamaño, solo la memoria

`internal/api/plantillas.go:95` · Seguridad

**Qué falla.** Ni la subida de imágenes ni la de plantillas xlsx envuelven r.Body con http.MaxBytesReader; el argumento de ParseMultipartForm solo decide cuánto se retiene en memoria y el resto se vuelca a ficheros temporales en disco sin límite.

**Cuándo se rompe.** Un usuario con rol operator (o alguien con su token) envía POST /api/plantillas/precios con un multipart de varios GB. ParseMultipartForm(25<<20) acepta el cuerpo entero escribiéndolo en el directorio temporal del contenedor; la comprobación `cab.Size > maxPlantilla` (línea 106) llega después de que todo el fichero ya está en disco. Repetido en paralelo llena el disco del servidor y tumba Postgres/la API. Lo mismo en imagenes.go:69 (ParseMultipartForm(imagen.MaxBytesEntrada)); el io.LimitReader de la línea 82 limita la lectura posterior, no la escritura temporal.

**Arreglo propuesto.** Antes de ParseMultipartForm: `r.Body = http.MaxBytesReader(w, r.Body, maxPlantilla+1<<20)` (y equivalente con imagen.MaxBytesEntrada en subirImagen).

### 26. probarShopify dice «conectado» con un token que no puede publicar

`internal/conectores/probar.go:68` · Shopify

**Qué falla.** La prueba de conexión solo pide shop.json, que responde con cualquier token válido: no comprueba los permisos que de verdad hace falta tener.

**Cuándo se rompe.** El cliente crea la app personalizada en Shopify y marca solo los permisos de lectura (read_products, read_orders), que es el error habitual. La pantalla de cuentas muestra «conectado a la tienda «MDV»» y se da la Fase 3 por buena. En el primer lote de publicación, los 452 trabajos fallan con 403 «requires merchant approval for write_products scope»; como EsReintentable no reintenta un 4xx, los 452 quedan en fallo definitivo y nadie sabe que la causa era el permiso, porque la prueba de conexión sigue diciendo que todo está bien.

**Arreglo propuesto.** Consultar además GET /admin/oauth/access_scopes.json y exigir write_products, write_inventory y read_orders, informando en el mensaje cuáles faltan; y validar que la tienda termine en .myshopify.com, que es el único host donde responde la Admin API.

### 27. La versión de API fijada (2024-10) lleva casi un año fuera de soporte: Shopify sirve otra sin avisar

`internal/conectores/shopify/shopify.go:25` · Shopify

**Qué falla.** El adaptador fija 2024-10 «a propósito para que la versión no flote», pero esa versión ya no está soportada y Shopify aplica fall-forward: responde con el comportamiento de la versión soportada más antigua, que cambia cada trimestre. El pin consigue exactamente lo contrario de lo que pretende.

**Cuándo se rompe.** Hoy (septiembre de 2026) la tabla de versiones de shopify.dev lista como estables 2025-10, 2026-01, 2026-04 y 2026-07 (2025-07 caducó el 16/07/2026); 2024-10 no aparece. Toda llamada a /admin/api/2024-10/... se sirve con el comportamiento de 2025-10, y el 16/10/2026 pasará a servirse con 2026-01 sin que nadie toque el código. Como los endpoints REST de products y variants están deprecados desde 2024-04, el aviso de la propia documentación es literal: lo deprecado deja de funcionar justo cuando una versión vieja se conmuta hacia adelante. El día que esa conmutación retire /products.json, publicar deja de funcionar en los cuatro pasos (Publish, Update, Pause, Resume, FetchStatus) sin ningún cambio en Integra.

**Arreglo propuesto.** Subir a una versión soportada (2026-01 o posterior), poner la constante en un solo sitio y usarla también en probar.go, y planificar la migración de products/variants a GraphQL (productSet / productVariantsBulkUpdate), que además resuelve la búsqueda por SKU y el límite de 100 variantes.

### 28. No se lee el Link header: la paginación de pedidos y de ListRemote no puede avanzar

`internal/conectores/shopify/shopify.go:349` · Shopify

**Qué falla.** llamarCrudo descarta las cabeceras de respuesta, así que nunca se obtiene el page_info que Shopify entrega en el Link header; FetchOrders además ignora el cursor recibido y siempre pide la misma primera página.

**Cuándo se rompe.** Una cuenta con 250 pedidos desde la marca de agua. ordenes.go:99 pagina con `cur = pagina.Next`, pero Shopify devuelve Done=false (100 == 100) y Next vacío, y FetchOrders no usa `cur` para nada: el bucle repite la misma petición 40 veces (maxPaginas), gasta 40 llamadas del cupo por ronda de ingesta y hace ~4.000 upserts redundantes en channel_orders, y solo entran 100 pedidos por ronda. En ListRemote pasa lo mismo: acepta cur.Token pero nunca puede producirlo, así que un llamador quedaría releyendo la página 1.

**Arreglo propuesto.** Devolver también resp.Header desde llamarCrudo, extraer el page_info del Link rel="next" y rellenar RemotePage.Next / OrderPage.Next; en la segunda página mandar únicamente page_info y limit (sin status ni created_at_min, que Shopify rechaza junto a page_info).

### 29. El coste de envío del pedido nunca se lee: Envio queda en 0 aunque Total lo incluye

`internal/conectores/shopify/shopify.go:326` · Shopify

**Qué falla.** El struct de respuesta no declara total_shipping_price_set ni shipping_lines, así que Order.Shipping se queda a cero mientras Total (total_price) sí lleva el envío dentro.

**Cuándo se rompe.** Pedido de 100.000 en producto y 15.000 de envío. Integra guarda total_amount=115.000 y shipping_amount=0 (ordenes.go:117), y el sale.order de Odoo se monta solo con la línea de 100.000. La cabecera del pedido en Integra no cuadra con la suma de sus líneas ni con el pedido de Odoo, y el envío cobrado desaparece de la contabilidad. Los otros tres adaptadores sí lo rellenan (falabella.go:338, woocommerce.go:289, mercadolibre.go:583): Shopify es el único hueco.

**Arreglo propuesto.** Añadir `TotalShippingPriceSet.ShopMoney.Amount` (o sumar shipping_lines[].price) al struct y asignarlo a Order.Shipping; de paso rellenar Order.Raw con el JSON del pedido, que hoy se guarda como '{}' en raw_payload (store/ordenes.go:92) y deja el pedido imposible de reprocesar.

### 30. Se da por hecho que el pedido trae objeto customer: cada pedido de invitado crea un cliente nuevo en Odoo

`internal/conectores/shopify/shopify.go:291` · Shopify

**Qué falla.** El comprador se arma solo con customer.first_name/last_name/email/phone; la documentación advierte que el pedido puede no tener customer, y el correo de contacto del pedido (campos raíz email / contact_email) no se lee nunca.

**Cuándo se rompe.** Compra sin cuenta (guest checkout) o pedido con el cliente ya redactado por privacidad: `customer` llega nulo, así que Buyer.Name, Email y Phone quedan vacíos. En ordenes.go:273 resolverCliente no puede buscar por correo (email vacío) y ejecuta Create de un res.partner llamado «Comprador SHOPIFY» — uno nuevo por cada pedido en esa situación. Odoo se llena de partners genéricos duplicados y ningún pedido se puede atribuir a su comprador real, cuando el dato sí venía en la carga (order.email y shipping_address.name/phone).

**Arreglo propuesto.** Leer los campos raíz `email` y `contact_email` y, como respaldo del nombre y el teléfono, `shipping_address.name` / `shipping_address.phone`; usar el primero que venga con valor.

### 31. Se ingieren pedidos cancelados y pedidos de prueba, y acaban como sale.order en Odoo

`internal/conectores/shopify/shopify.go:278` · Shopify

**Qué falla.** La consulta pide status=any y el struct no declara cancelled_at ni test, así que un pedido cancelado o creado con la pasarela de pruebas entra igual que uno bueno.

**Cuándo se rompe.** El comprador cancela el pedido a los cinco minutos, o el operador hace una compra de prueba con el Bogus Gateway antes de abrir la tienda. En ambos casos orders.json?status=any lo devuelve, Integra lo guarda con EstadoCanal = financial_status («voided», «refunded» o «paid» en el de prueba) y el trabajo de montaje crea el sale.order en Odoo con sus líneas. Como después no se vuelve a consultar el pedido (UpdatedAt no se rellena y la marca de agua avanza por created_at), nada lo cancela: queda un pedido borrador fantasma que descuadra el pipeline de ventas.

**Arreglo propuesto.** Declarar cancelled_at y test, descartar (o marcar como no montables) los pedidos de prueba y los cancelados, y rellenar Order.UpdatedAt con `updated_at` para que un cambio de estado posterior vuelva a entrar en la ventana de ingesta.

### 32. La ubicación de inventario se elige al azar entre las activas y se consulta en cada trabajo de stock

`internal/conectores/shopify/shopify.go:403` · Shopify

**Qué falla.** ubicacionPrincipal devuelve la primera ubicación con active=true del listado, sin criterio ni configuración, y se invoca en cada llamada a UpdateStock.

**Cuándo se rompe.** La tienda tiene dos ubicaciones activas (por ejemplo el punto de venta físico y la bodega). GET /locations.json no garantiza cuál viene primero, así que todo el stock de Integra se fija en la que Shopify liste antes; si es la del punto de venta, la tienda online (que despacha desde la bodega) sigue mostrando 0 y no vende, o vende contra existencias que están en el otro sitio. Además la asignación de bodegas por cuenta (`channel_account_warehouses`, que el README describe como el mecanismo para decidir qué almacenes suma cada cuenta) no interviene en ningún momento. Aparte, cada trabajo de stock gasta 3 llamadas (locations + variants/{id} + inventory_levels/set) para actualizar una sola variante: 452 variantes son ~1.356 llamadas contra un cupo de 2/s.

**Arreglo propuesto.** Guardar el location_id en las credenciales o en la configuración de la cuenta (y ofrecerlo en la interfaz al conectar), cachearlo en el Adaptador, y persistir el inventory_item_id junto a la referencia de la variante para ahorrar la llamada a /variants/{id}.json en cada envío.

### 33. Autenticación por clave en la cadena de consulta sin exigir HTTPS: sobre HTTP WooCommerce solo acepta OAuth 1.0a y el secreto viaja en claro

`internal/conectores/woocommerce/woocommerce.go:357` · WooCommerce

**Qué falla.** Ni el adaptador ni la prueba de conexión ni la interfaz comprueban que la URL de la tienda sea https, y las claves van siempre como parámetros de la URL; con una tienda HTTP todas las llamadas fallan con 401 y el consumer_secret se transmite y se registra en claro.

**Cuándo se rompe.** El operador da de alta la cuenta con `url = http://localhost:8080` (exactamente el WooCommerce local en Docker que la ola 4 de PLAN-EJECUCION.md prevé usar para las pruebas de extremo a extremo). `probarWoo` y todas las llamadas posteriores mandan `?consumer_key=ck_...&consumer_secret=cs_...` sobre HTTP. WooCommerce, cuando `is_ssl()` es falso, no intenta autenticación básica ni por query: exige OAuth 1.0a de una pata. Resultado: 401 en cada llamada (mensaje opaco «HTTP 401: {"code":"woocommerce_rest_cannot_view"...}») y, de paso, la clave y el secreto han quedado escritos en claro en el cable, en el log de acceso de nginx/Apache y en cualquier proxy intermedio. El mismo problema aparece con una tienda https detrás de un proxy que no propaga el esquema, donde `is_ssl()` también es falso.

**Arreglo propuesto.** Rechazar en la factoría y en `probarWoo` cualquier URL que no empiece por `https://` (con un mensaje que lo explique), y mover las claves de la query a la cabecera `Authorization: Basic base64(ck:cs)` para que no queden en los logs; si se quiere soportar tiendas HTTP locales, implementar la firma OAuth 1.0a.

### 34. La oferta programada nativa no se usa nunca, y la rama que la enviaría manda la ventana en UTC a un campo que WooCommerce lee en hora local

`internal/conectores/woocommerce/woocommerce.go:144` · WooCommerce

**Qué falla.** El motor llama siempre a UpdatePrice con SalePrice=0, de modo que toda promoción viaja como `regular_price` rebajado y con `sale_price: ""`; la rama de `sale_price`/`date_on_sale_*` es código muerto y además formatea las fechas en UTC contra campos documentados «in the site's timezone» y nunca limpia las fechas antiguas.

**Cuándo se rompe.** Se programa una promoción en Integra. `moverPromociones` recalcula el precio efectivo, cambia el hash de precio y encola TrabajoPrecio; manejadores.go:121-123 construye el PriceUpdate solo con `RegularPrice` y `Currency`, así que el adaptador entra por el `else` de la línea 149 y envía `{"regular_price":"99900.00","sale_price":""}`. En la tienda el descuento aparece como precio normal: sin precio tachado, sin insignia de oferta y sin el precio anterior, justo lo contrario de lo que MODULOS.md promete («Único con ofertas programadas nativas»). Si algún día se rellena SalePrice, la ventana se enviará desplazada cinco horas en Colombia (se manda 2026-09-10T00:00:00 UTC a `date_on_sale_from`, que WooCommerce interpreta como las 00:00 de Bogotá) y, como el `else` nunca limpia `date_on_sale_from`/`_to`, una promoción posterior sin fechas heredará las de la anterior y `is_on_sale()` la dejará sin aplicar.

**Arreglo propuesto.** Pasar el precio de oferta y su vigencia desde el motor (los datos ya están en `effective_prices`/`offers`), usar `date_on_sale_from_gmt`/`date_on_sale_to_gmt` con la hora en UTC, y en la rama sin oferta enviar también `"date_on_sale_from": ""` y `"date_on_sale_to": ""` para no dejar ventanas huérfanas.

---

## Severidad baja (5)

### 1. La unicidad de attention_queue es inerte y dos recálculos concurrentes duplican la cola de atención

`internal/store/edicion.go:195` · Integridad de datos

**Qué falla.** RecalcularAtencion hace DELETE + INSERT masivo sin bloqueo; el UNIQUE (variant_id, channel_account_id, reason) no protege porque channel_account_id siempre es NULL y PostgreSQL considera distintos los NULL.

**Cuándo se rompe.** El planificador termina el sync nocturno y llama a RecalcularAtencion (internal/sync/catalogo.go:133) mientras un operador pulsa «recalcular» en la interfaz (internal/api/server.go:903) o guarda un precio masivo (internal/store/masivo.go:191). En READ COMMITTED, el DELETE de la segunda transacción no ve las filas que la primera acaba de insertar, así que ambas insertan su juego completo: la cola queda con cada motivo duplicado. El panel muestra «601 en atención» donde hay 300 problemas reales y el array de problemas por producto de internal/store/consultas.go:177 repite cada motivo.

**Arreglo propuesto.** Tomar un advisory lock (pg_advisory_xact_lock) al inicio de la transacción de RecalcularAtencion, o declarar la restricción como UNIQUE ... NULLS NOT DISTINCT y pasar a un upsert con marcado de last_seen_at en vez de borrar y reinsertar.

### 2. channel_api_calls nunca se escribe: el panel de llamadas a las APIs de los canales siempre sale vacío

`internal/store/observabilidad.go:28` · Integridad de datos

**Qué falla.** RegistrarLlamadaAPI es la única función que inserta en channel_api_calls y no la llama nadie; la tabla está vacía y GET /api/canales/llamadas no puede mostrar nada.

**Cuándo se rompe.** Un adaptador empieza a recibir 429 de MercadoLibre y las publicaciones fallan. El operador abre el panel de observabilidad para ver los códigos HTTP, las duraciones y las cabeceras de cupo: la lista sale vacía y no hay forma de diagnosticar desde la interfaz. Las columnas rate_limit_remaining y rate_limit_reset_at, pensadas justo para el panel de cupos, no llegan a existir nunca.

**Arreglo propuesto.** Instrumentar el transporte HTTP común de los adaptadores (o el httpDo de cada conector) para llamar a RegistrarLlamadaAPI, o retirar la tabla y el endpoint del mapa de módulos hasta que exista el instrumentado.

### 3. PLAN-EJECUCION cuenta 9 «tablas del esquema sin código detrás»; cinco de ellas no existen en el esquema

`PLAN-EJECUCION.md:16` · Documentación

**Qué falla.** La medición de partida se hizo contando `CREATE TABLE` en las migraciones sin descontar los `DROP TABLE` ni las secciones `Down`, así que cinco de las nueve tablas listadas no existen en la base real.

**Cuándo se rompe.** El agente al que la ola 1 asigna la dimensión «Integridad de datos» recibe el encargo literal de resolver «las 9 tablas huérfanas (¿sobran o faltan por implementar?)» (PLAN-EJECUCION.md:67). Cinco de esas nueve —`attribute_mappings`, `attribute_value_mappings`, `odoo_pricelists`, `odoo_pricelist_items` y `product_images`— ya fueron eliminadas a propósito (migración 018) o sólo se crean dentro de la sección `Down` de una migración. El agente gasta la ola investigando tablas inexistentes y, en el peor caso, propone «implementarlas», reintroduciendo el caché de tarifas de Odoo que la migración 012 hizo obsoleto. Las huérfanas reales son cuatro: `channel_category_cache`, `notification_destinations`, `sync_runs` y `sync_run_items`.

**Arreglo propuesto.** Recalcular la fila contra `information_schema.tables` de una base migrada, dejarla en «4: channel_category_cache, notification_destinations, sync_runs, sync_run_items» y corregir también el encargo de la línea 67.

### 4. AckOrder invoca una acción que no existe y le pasa el SKU del producto como identificador de línea de pedido

`internal/conectores/falabella/falabella.go:391` · Falabella

**Qué falla.** `AckOrder` llama a `SetStatusToShipped`, acción que no figura en la API de Falabella v500 (las documentadas son SetStatusToPackedByMarketplace, SetStatusToReadyToShip y SetStatusToCanceled), y compone `OrderItemIds` con `ref.ListingID`, que en este adaptador es el SKU del producto, no un OrderItemId.

**Cuándo se rompe.** Cuando se conecte el despacho (ola 3 del plan), confirmar el envío de un pedido llamará a `SetStatusToShipped` y Falabella responderá «E008: Invalid Action». Aunque la acción se corrigiera, el parámetro iría mal: para Falabella `ExternalRef.ListingID` se rellena con el SKU (líneas 112 y 151), de modo que se enviaría `OrderItemIds=[AO-NU-1001]` —ni siquiera una lista de enteros válida— en vez del `OrderItemId` numérico que devuelve GetOrderItems y que el adaptador ya guarda en `OrderLine.ExternalID` (línea 383). Hoy no rompe nada porque ningún punto del núcleo llama a `AckOrder`.

**Arreglo propuesto.** Cambiar la acción a `SetStatusToReadyToShip` y ampliar la firma para recibir los `OrderItemId` de las líneas del pedido (los que ya se guardan en `channel_order_lines.external_id`), no el ref de la publicación.

### 5. La marca de agua de pedidos retrocede un minuto en cada sondeo, aunque no llegue nada

`internal/store/ordenes.go:354` · Núcleo

**Qué falla.** `ActualizarWatermarkOrdenes` resta un minuto siempre, y `ingerir` la llama con `maxFecha`, que arranca valiendo la propia marca guardada: sin pedidos nuevos, cada sondeo deja la marca un minuto más atrás que la anterior.

**Cuándo se rompe.** Cuenta sin ventas nuevas. Sondeo 1: `desde` = marca guardada W; no hay pedidos; `maxFecha` sigue siendo W; se guarda W-1min. Sondeo 2: se guarda W-2min. Con el botón "traer pedidos" de la UI (api/server.go:536) o con horarios frecuentes, la ventana consultada crece sin límite hacia atrás. Cuando la ventana acumula más de 40 páginas de pedidos históricos, el tope `maxPaginas` (ordenes.go:95) corta antes de llegar a los recientes y los pedidos nuevos dejan de entrar; además, tras suficientes sondeos la ventana sobrepasa el guardarraíl de "solo los últimos 7 días" y se empieza a reingerir el histórico. El camino de error (ordenes.go:101) hace lo mismo en cada reintento.

**Arreglo propuesto.** Aplicar el margen de un minuto solo al construir el `desde` de la consulta (o solo cuando `maxFecha` avanzó de verdad), y nunca escribir una marca menor que la ya guardada.

---

## Descartados tras verificar (12)

Hallazgos que no sobrevivieron: o el escenario no puede ocurrir con el
código tal como está, o son decisiones de diseño documentadas.

- `docker-compose.yml:41` — El banco de imágenes vive dentro del contenedor: cualquier redespliegue borra las 1.132 fotos
- `internal/ordenes/ordenes.go:56` — Los pedidos ingeridos nunca se montan en Odoo: nadie encola `orden_a_odoo`
- `internal/store/publicacion.go:81` — El stock publicado suma todas las bodegas: no existe ningún código que llene `channel_account_warehouses`
- `internal/store/store.go:303` — products.categ_path nunca se escribe: el mapeo de categorías y los atributos obligatorios están muertos y MercadoLibre/Falabella no pueden publicar
- `internal/conectores/falabella/falabella.go:304` — GetOrders: la respuesta anida Body.Orders.Order y el adaptador la lee como array — no entra ni un pedido
- `internal/conectores/falabella/atributos.go:41` — AtributosDeCategoria no puede decodificar ninguna respuesta: Body.Attribute, Options.Option e isMandatory como cadena
- `internal/ordenes/ordenes.go:151` — Ningún punto del sistema encola el trabajo que crea el sale.order: el ciclo automático no existe
- `internal/config/config.go:62` — INTEGRA_PUBLIC_BASE_URL vale http://localhost:8080 por defecto y no se valida ni se documenta
- `docker-compose.yml:73` — No hay copia de seguridad de Postgres, que es la única copia de los datos que Integra posee
- `internal/conectores/falabella/falabella.go:334` — La fecha del pedido se interpreta como UTC cuando Falabella la entrega en hora local del vendedor
- `internal/ordenes/ordenes.go:250` — El pedido no fija bodega, así que ignora channel_account_warehouses
- `docker-compose.yml:17` — El compose publica Postgres en todas las interfaces con contraseña fija y la API sin TLS

## Sin verificar: severidad baja (22)

Deuda que no rompe nada hoy. Se dejaron fuera de la verificación con
agentes a propósito, para no gastar en lo que no cambia decisiones.

- `.env.example:1` — .env.example no documenta ninguna variable INTEGRA_*
- `.gitignore:23` — El README documenta datos/odoo-config/odoo.conf, pero .gitignore excluye todo /datos/ del repositorio
- `MODULOS.md:25` — MODULOS.md documenta qwen2.5:3b como modelo de texto por defecto; el código usa 7b y el README explica por qué el 3b no sirve
- `PLAN.md:15` — El «Estado actual» y los riesgos de PLAN.md describen una situación que ya no es la del repositorio
- `README.md:69` — La tabla «Comandos» del README lista 5 de los 20 subcomandos
- `README.md:361` — «Cómo añadir un canal» indica una ruta de paquete que no es donde viven los adaptadores, y la «Estructura» omite 17 de los 23 paquetes
- `internal/api/auth.go:33` — Clave HMAC de sesión de respaldo fija en el código y reutilización de la clave maestra AES como secreto de firma
- `internal/api/auth.go:79` — Cookie de sesión integra_token sin atributo Secure
- `internal/api/auth.go:60` — Enumeración de correos en el login por mensaje y por tiempo de respuesta
- `internal/api/auth.go:24` — Doce rutas de la API no tienen pantalla, entre ellas toda la administración de usuarios
- `internal/api/middleware_auth.go:45` — Prefijo público /api/webhooks/ reservado sin ningún handler detrás
- `internal/api/server.go:987` — Los logs no persisten, no rotan y en el nivel por defecto no hay registro de peticiones
- `internal/conectores/shopify/shopify.go:214` — FetchStatus se traga los errores: una publicación borrada en Shopify desaparece del informe sin ruido
- `internal/conectores/shopify/shopify.go:462` — Ninguna llamada a Shopify se registra ni se lee la cabecera de cupo
- `internal/conectores/shopify/shopify.go:52` — Se declara compare_at_price como capacidad pero no se escribe nunca, y UpdatePrice descarta el precio de oferta
- `internal/conectores/woocommerce/woocommerce.go:312` — AckOrder descarta el número de guía y la transportadora, al revés de lo que dice su propio comentario
- `internal/conectores/woocommerce/woocommerce.go:187` — FetchStatus se traga los errores del canal: una publicación borrada en la tienda desaparece del resultado en vez de reportarse
- `internal/jobs/worker.go:39` — Un trabajo más largo que el lease de 5 minutos se ejecuta dos veces y puede resetear el trabajo de otro worker
- `migrations/005_sync.sql:10` — Cuatro tablas huérfanas reales (no nueve): sync_runs, sync_run_items, channel_category_cache y notification_destinations
- `migrations/005_sync.sql:81` — sync_schedules.weekdays: el esquema documenta 0=domingo y el código usa ISO 1..7, sin CHECK que lo impida
- `migrations/010_mapeo_categorias.sql:31` — category_mappings.channel_account_id y su índice de excepción por cuenta son esquema muerto
- `web/src/Editar.tsx:101` — No se puede borrar la garantía en meses: el campo se descarta sin avisar

## Cobertura por dimensión

**Seguridad.** Leídos README.md, PLAN.md, MODULOS.md y PLAN-EJECUCION.md para separar decisiones de diseño (imágenes públicas, pedido en borrador, propiedad del precio) de defectos. Revisados por completo: internal/api/middleware_auth.go, server.go, auth.go, sesiones.go, imagenes.go, integraciones.go, masivo.go, plantillas.go, precios.go, auditoria.go, observabilidad.go; internal/auth/auth.go; internal/crypto/crypto.go; cmd/integra/main.go (despacho, cmdServe, cmdWorker, comandos de usuarios); internal/config; docker-compose.yml, Dockerfile, .gitignore y claves de .env (solo nombres). En internal/store se buscó SQL construido por concatenación: todas las condiciones dinámicas (consultas.go, masivo.go, plantilla.go) usan marcadores $n con args; no hay inyección. Verificado: el ServeMux redirige rutas no canónicas (`..`, `//`), por lo que el prefijo público no permite evadir el middleware; RutaDeImagen resuelve por base de datos y Almacen.Leer rechaza rutas absolutas o con `..`; HMAC con hmac.Equal (tiempo constante) y bcrypt; AES-GCM con nonce aleatorio y byte de versión; CORS solo para orígenes localhost sin credenciales y el frontend va por proxy de Vite con Bearer, así que no es explotable; no se encontraron secretos en llamadas a slog. Ejecutados `go build ./...` y `go vet ./...` (limpios) y una consulta de solo lectura a audit_logs (sin filas de usuarios todavía). Reproducido con un programa Go en el scratchpad que url.Error incluye la query con consumer_secret. Fuera por tiempo: internal/conectores (salvo probar.go y el builder de peticiones de WooCommerce), internal/mercadolibre (buscador de competencia), webimagenes (descargas de URLs externas / SSRF), cabeceras de seguridad para el frontend servido en producción (aún no existe ese servicio), y los tests de middleware.

**Núcleo.** Revisado en profundidad, con lectura línea a línea: internal/jobs (cola.go, worker.go, cola_test.go y migrations/013_jobs.sql: encolado con ON CONFLICT sobre el índice parcial `jobs_unique_key_vivo_idx`, reclamo con FOR UPDATE SKIP LOCKED, backoff en SQL, recuperación de huérfanos y apagado del worker); internal/planificador (bucle de tick, `dispararVencidos`, `moverPromociones`, `planificarTodas`, `Vigilar`) junto con internal/store/horarios.go (`ProximaEjecucion`, `diaPermitido` —comprobado que la conversión a numeración ISO es correcta—, `HorariosVencidos`, `MarcarHorarioEjecutado`, dedupe de alertas); internal/publicar (motor de tres hashes, prioridades 10/50/100, los tres manejadores) y su contraparte de persistencia internal/store/publicacion.go (`CandidatosPublicacion`, `UnCandidato`, `GuardarPublicacion`, `RefDePublicacion`, `GuardarPrecio/StockPublicado`); internal/ordenes completo (ingesta paginada, marca de agua, deduplicación por (cuenta, id externo) con `xmax=0`, líneas sin SKU, `montarEnOdoo`, `CrearPedido`, `resolverCliente`, `referencia`) con internal/store/ordenes.go; internal/sync/catalogo.go (lectura incremental por write_date, `leerStock` sobre stock.quant, `ReemplazarStock`) con internal/store/store.go (`UpsertIdentidad`, `UpsertAlmacenes`, `VariantesPorOdooID`, watermark). Como apoyo: el cableado en cmd/integra/main.go (cmdWorker, comando manual de pedidos), las rutas de api/server.go que disparan planificación e ingesta, api/precios.go, store/edicion.go y store/precios.go (`RecalcularPreciosCuenta`), el contrato internal/channel/adapter.go y los caminos de `Publish`/`Update`/`UpdatePrice` de los cuatro conectores, más las migraciones 001/002/004/013. Verificado con `go build ./...` y `go vet ./...` (ambos limpios) y con consultas de solo lectura al Postgres del compose (multivariante, contenido de channel_account_warehouses, stock por bodega, effective_prices, sync_schedules).\n\nQuedó fuera por tiempo, y merecería otra pasada: el detalle de cada adaptador frente a la documentación oficial del canal (lo cubre otra dimensión), internal/atributos y internal/proyeccion, el motor de tarifas y ofertas de store/precios.go más allá de su relación con los hashes, internal/imagen/webimagenes, y la ejecución de `go test ./...` contra la base (no lancé la suite de integración para no escribir en la base del usuario; los tests de jobs y store hacen DELETE sobre tablas reales). Tampoco pude reproducir en vivo los hallazgos que exigen una cuenta de canal conectada —no hay ninguna en la base (`select count(*) from channel_accounts` → 0)—, así que los tres hallazgos del motor de publicación están demostrados por lectura de código y por el estado del esquema, no por ejecución."

**Integridad de datos.** Leí README.md, PLAN.md, MODULOS.md y PLAN-EJECUCION.md antes de auditar, y descarté como hallazgo todo lo decidido a propósito (Odoo solo aporta SKU/nombre/stock, el precio es de Integra, el sale.order queda en borrador, «sin filas en channel_account_warehouses = todos los almacenes» como default declarado, computed_price congelado, product_variants.odoo_list_price solo para auditoría).

Revisado a fondo: las 18 migraciones completas (secciones Up y Down, restricciones, índices y comentarios) contrastadas con los 22 ficheros de internal/store, internal/sync/catalogo.go, internal/jobs/cola.go, internal/ordenes/ordenes.go, internal/publicar/manejadores.go, internal/api/plantillas.go, internal/api/server.go (rutas) y cmd/integra/main.go (comandos migrate). Verifiqué contra la base viva (docker exec integra-postgres psql, solo lecturas y EXPLAIN): inventario de tablas (\\dt: 38), \\d de attention_queue, conteos de filas por tabla y por columna (products, product_variants, category_mappings, channel_category_attributes, producto_atributos, channel_api_calls, channel_account_warehouses, effective_prices, listings, jobs), stock agregado por almacén, duplicados de SKU y planes de ejecución. Puertas ejecutadas: `go build ./...`, `go vet ./...` y `go test ./...`, las tres en verde. No modifiqué, creé ni borré ningún archivo del proyecto ni ninguna fila.

Veredicto pedido sobre las 9 tablas «sin código»: la lista de PLAN-EJECUCION.md está desactualizada; 5 de las 9 ya no existen en la base. attribute_mappings y attribute_value_mappings las borró migrations/018 (líneas 44-46) y las sustituyen channel_category_attributes + producto_atributos de la 016. odoo_pricelists y odoo_pricelist_items las borró la misma 018 (líneas 51-53) porque el precio pasó a ser de Integra en la 012. product_images la borró migrations/011 (línea 14) y la sustituyen imagenes + producto_imagenes + imagen_derivadas. De las 4 que sí quedan: sync_runs y sync_run_items SOBRAN (su papel lo cubren hoy la tabla jobs de la 013 y las columnas last_error/error_count/last_published_at de product_channel_listings; arrastran además la FK muerta channel_api_calls.sync_run_id); channel_category_cache SOBRA (el árbol de categorías se consulta en vivo contra el predictor público de MercadoLibre y lo que sí se cachea son los atributos, en channel_category_attributes); notification_destinations es implementación PENDIENTE, la única de las nueve (ola 3 del plan, salida por SMTP).

Cobertura parcial o fuera de alcance por tiempo: no audité en profundidad internal/store/usuarios.go, auditoria.go, contenido.go, preview.go, webimagenes ni el frontend web/src; no revisé el detalle de las cuatro implementaciones de internal/conectores más allá de sus Capabilities; y no comprobé el comportamiento real de las secciones Down ejecutándolas (habría sido escritura sobre la base del proyecto), así que el análisis de las Down es por lectura del SQL.

**Shopify.** Revisado en detalle internal/conectores/shopify/shopify.go (las 499 líneas: Publish, Update, UpdatePrice, UpdateStock, Pause/Resume, FetchStatus, ListRemote, FetchOrders, buscarPorSKU, ubicacionPrincipal, inventarioDeVariante, llamar/llamarCrudo y el manejo de errores 404/429) y probarShopify en internal/conectores/probar.go. Para poder decir qué rompe de verdad seguí las cadenas hacia el núcleo: internal/channel/adapter.go (contrato y EsReintentable), internal/publicar/motor.go y manejadores.go (qué trabajo se encola con cada hash y qué se guarda tras cada envío), internal/ordenes/ordenes.go (bucle de paginación de la ingesta y montaje del sale.order) y internal/store/ordenes.go (raw_payload, dedupe). Comprobé contra la documentación oficial actual de shopify.dev: tabla de versiones (2025-10 … 2026-07 vigentes; 2024-10 fuera y política de fall-forward), deprecación de products/variants desde 2024-04, Product Variant (inventory_quantity de solo lectura, weight/weight_unit vigentes, inventory_management devuelve siempre \"shopify\" desde 2025-01), InventoryLevel (set conecta la ubicación automáticamente, disconnect_if_necessary), Order (ventana de 60 días sin read_all_orders, price antes de descuentos, total_discount/discount_allocations, cancelled_at, «el pedido puede no tener customer»), y paginación por cursor con Link header rel=next (page_info solo admite limit y fields). `go build ./...` y `go vet ./internal/conectores/...` pasan sin salida. No verifiqué nada contra una tienda real ni contra Postgres: no hay credenciales de Shopify en el entorno, así que las respuestas concretas de la API (orden exacto de /locations.json, contenido del Link header) están razonadas desde la documentación, no observadas. Fuera de alcance por dimensión: los adaptadores de WooCommerce, MercadoLibre y Falabella (solo los miré para comparar qué campos de pedido rellenan), la firma HMAC de Falabella y el frontend. Sobre webhooks HMAC: no hay nada que auditar en Shopify — internal/api/middleware_auth.go:45 deja público el prefijo /api/webhooks/ diciendo que «su autenticidad se comprueba con la firma del propio canal», pero no existe ningún handler registrado en esa ruta ni verificación de X-Shopify-Hmac-Sha256 en el repositorio; cuando se escriba, esa exención ya está abierta y la verificación tendrá que llegar con el handler. Comportamiento no verificado más arriesgado, por si se escribe una sola prueba: buscarPorSKU + la rama de adopción de Publish contra un httptest.Server que devuelva un catálogo de más de 250 productos — es lo único que separa «adoptar» de «duplicar el catálogo en una tienda viva», y hoy no hay ni un test que lo toque (internal/conectores/adaptadores_test.go no cubre el camino de adopción).

**WooCommerce.** Revisado línea a línea `internal/conectores/woocommerce/woocommerce.go` (411 líneas: init/factoría, Capabilities, Publish, Update, UpdatePrice, UpdateStock, Pause, Resume, FetchStatus, ListRemote, FetchOrders, AckOrder, buscarPorSKU, imagenesDe, llamar, recortar) y `probarWoo` en `internal/conectores/probar.go:91-111`. Para saber si cada método se usa de verdad seguí también sus llamadores: `internal/publicar/motor.go` y `manejadores.go` (qué encola cada hash y qué método del adaptador se invoca), `internal/ordenes/ordenes.go` (bucle de paginación, marca de agua, montaje en Odoo), `internal/store/ordenes.go` (selección de pedidos pendientes), `internal/planificador` (moverPromociones), `internal/channel/adapter.go` (contrato, Capabilities, EsReintentable) y `web/src/Cuentas.tsx` (alta de credenciales). Comprobé contra la documentación oficial de la REST API v3 y contra el código fuente de WooCommerce en GitHub: autenticación (Basic sobre HTTPS, OAuth 1.0a obligatorio sobre HTTP), `dates_are_gmt` y la columna `post_date`/`post_date_gmt` de `after`/`before`, el valor por defecto `any` de `status` en pedidos y en productos, el filtro `sku` (coincidencia exacta con `meta_query IN`), el máximo de 100 en `per_page`, las cabeceras `X-WP-Total`/`X-WP-TotalPages`, y los campos `date_on_sale_from`/`_to` («in the site's timezone») frente a sus variantes `_gmt`. Verifiqué el comportamiento real levantando un servidor WooCommerce simulado en el sandbox contra una copia del módulo (no toqué el proyecto): capturé los cuerpos y las URL que salen en Publish sobre un SKU existente, en UpdatePrice con y sin oferta, y reproduje el bucle de ingesta de `ordenes.go` con 100 pedidos por página. `go vet ./internal/conectores/...` pasa limpio. Descarté por comprobación explícita dos sospechas que parecían hallazgos: que `buscarPorSKU` no encontrara los productos creados en `draft` (el `status` por defecto de la lista de productos es `any`, no `publish`, así que sí los encuentra y no se duplican publicaciones) y que `line_items.price` llegara como cadena y rompiera el `json.Unmarshal` (WooCommerce lo emite como número). Quedan fuera por tiempo: el comportamiento del endpoint `products/batch` (que el adaptador declara en Capabilities con lote de 100 pero no usa: cada precio y cada stock van en un PUT individual, sin consecuencia funcional hoy porque el núcleo no consume `MaxBatchSize`), la verificación de firma `X-WC-Webhook-Signature` (no hay ningún manejador bajo `/api/webhooks/`, que es dimensión de la auditoría de seguridad), y el comportamiento de la paginación de `ListRemote` cuando el catálogo es múltiplo exacto de 100 (no pude confirmar en la documentación si WooCommerce responde 400 al pedir una página posterior a la última; `ListRemote` además no tiene llamadores). El comportamiento no verificado más arriesgado, y el primero que debería cubrir una prueba con `httptest`, es la ingesta de pedidos: formato y zona horaria del parámetro `after`, avance de la marca de agua y paginación — es donde está el único hallazgo con pérdida de datos, y donde un servidor simulado con la zona horaria de la tienda desplazada respecto a UTC lo habría detectado en la primera ejecución.

**Falabella.** Revisado, solo lectura, contra la referencia oficial del Seller Center de Falabella (developers.falabella.com v500.0.0: getting-started, requests-and-responses, productcreate, productupdate, getproducts, getorders/getordersv2, getorderitems, getmultipleorderitems, feeds, feedstatus, feedlist, setstatustoreadytoship, setstatustopackedbymarketplace, setstatustocanceled, getcategoryattributes; el sitio devuelve 403 a la descarga directa, así que se leyó por el volcado indexado en context7, que cita cada página de origen).

Ficheros auditados línea a línea: internal/conectores/falabella/falabella.go (561 líneas, las 14 operaciones del adaptador), internal/conectores/falabella/atributos.go y las funciones FirmarFalabella/ParamsFalabella/probarFalabella de internal/conectores/probar.go. Para juzgar consecuencias reales se leyeron además internal/channel/adapter.go (contrato y Capabilities), internal/publicar/motor.go y manejadores.go (motor de diff por tres hashes y qué se guarda tras cada envío), internal/ordenes/ordenes.go (bucle de paginación y marca de agua) y internal/atributos/fuentes.go + cmd/integra/main.go:888 (cómo se engancha la fuente de requisitos de Falabella). Antes de todo se leyeron README.md, PLAN.md, MODULOS.md y PLAN-EJECUCION.md; no se reportan como hallazgos las decisiones deliberadas (Odoo aporta solo SKU/nombre/stock, el precio es de Integra, publicación inicial inactiva, pedido en borrador, /imagenes/* público).

Comprobaciones ejecutadas: `go build ./...` y `go vet ./internal/conectores/...` (limpios); un programa Go aparte en el scratchpad que decodifica las respuestas documentadas de GetProducts, GetOrderItems y GetCategoryAttributes con las structs exactas del adaptador (los tres fallan, salida citada en la evidencia) y que compara url.QueryEscape con rawurlencode; y una consulta de solo lectura a Postgres (`select count(*) filter (where sku like '% %'), count(*) from product_variants` -> 31 de 545) para cuantificar el impacto del escapado de la firma.

Qué NO se pudo verificar y es el comportamiento no probado más arriesgado: el paquete no tiene una sola prueba y no hay credenciales del Seller Center, así que ninguna de estas llamadas se ha ejecutado nunca contra Falabella ni contra un servidor simulado. Lo más peligroso no es un fallo ruidoso sino el silencioso: la escritura es asíncrona, `Publish`/`UpdatePrice`/`UpdateStock` dan por buena la aceptación del feed, el motor guarda inmediatamente los tres hashes y nadie consulta nunca `EstadoFeed`, de modo que un feed rechazado por Falabella deja al catálogo entero marcado como publicado y sincronizado sin estarlo, y el diff no lo vuelve a intentar jamás. La prueba que más urge es un servidor simulado que devuelva las respuestas literales de la documentación (con el anidamiento Body.X.Y y los valores como cadena) para cada una de las 14 operaciones, más un vector de firma conocido con un valor que contenga espacio. Tampoco pude verificar el endpoint de imágenes: no encontré en la referencia consultada la página que describa cómo se cargan las fotos, así que no afirmo nada sobre el bloque `<Images>` que arma el adaptador. La forma de escritura con `<BusinessUnits>` (hallazgo 8) procede del ejemplo vigente de ProductCreate y de la respuesta documentada de GetProducts, pero las páginas heredadas todavía muestran la forma plana; conviene confirmarla con un feed de prueba antes de reescribir el payload.

**Odoo.** Revisado en lectura completa: internal/odoo/client.go, internal/odoo/escritura.go, internal/odoo/record.go, internal/odoo/xmlrpc/xmlrpc.go, internal/ordenes/ordenes.go (ingerir, montarEnOdoo, CrearPedido, resolverCliente, productoOdoo, referencia), internal/sync/catalogo.go, internal/store/ordenes.go, internal/store/store.go (UpsertIdentidad, ReemplazarStock, VariantesPorOdooID), internal/store/migracion.go, migrations/001_core.sql y 002_catalog.sql (esquema de products/product_variants/variant_stock/channel_account_warehouses), internal/jobs/cola.go y worker.go (lease, reintentos, huérfanos), internal/planificador/planificador.go (qué encola y qué vigila), cmd/integra/main.go (abrirOdoo, cmdOrdenes), internal/api/server.go (rutas de pedidos), internal/conectores/mercadolibre (FetchOrders y mapeo de comprador/líneas) y web/src/Pedidos.tsx. Leídos antes de auditar README.md, PLAN.md, MODULOS.md y PLAN-EJECUCION.md; se han excluido como decisiones de diseño el pedido en borrador, no tocar impuestos, leer de Odoo solo SKU/nombre/stock y que el precio sea propiedad de Integra.

Comprobaciones ejecutadas: `go build ./...` y `go vet` sobre odoo, ordenes y sync (limpios). Consultas XML-RPC de SOLO LECTURA contra el Odoo local (integra_pruebas, uid 2): res.company (dos compañías, ambas en USD), product.pricelist con active_test=false y allowed_company_ids=[1,2] (cero tarifas), sale.order search_read (los 20 pedidos existentes en USD, ninguno con client_order_ref) y fields_get de sale.order para pricelist_id/currency_id/warehouse_id/company_id. Una llamada con clave equivocada para ver la forma del fault («Access Denied» sin traceback), que confirma que `esErrorDeCredenciales` clasifica bien y NO es un hallazgo. Consultas de solo lectura a Postgres: odoo_connections (una sola, id 4 mdv_replica), duplicados de SKU en product_variants (cero hoy) y channel_orders (vacía).

Fuera de alcance por tiempo o por no haberlo podido provocar sin escribir: no se creó ningún sale.order de prueba (la auditoría es de solo lectura), así que el comportamiento exacto de Odoo 18 al recibir una línea con producto archivado, con `name` vacío o con un product_id inexistente no se verificó en vivo, solo por código y metadatos de campos. Tampoco se auditaron los adaptadores de Shopify, WooCommerce y Falabella salvo en lo que tocan al contrato de pedidos, ni el motor de publicación, ni `cmd/odoo-replicar`, ni el resto de dimensiones (seguridad, frontend, operación).

**Frontend.** Revisado: los 21 componentes de web/src (App, Login, Sidebar, Catalogo, Editar, Promocion, EdicionMasiva, PlantillaMasiva, Preview, Imagenes, Atributos, Mapeos, Canales, Cuentas, Integraciones, Pedidos, Publicacion, Prioridad, Automatizacion, Logo, main) y web/src/api.ts, contrastados uno a uno contra las 67 rutas registradas con mux.HandleFunc en internal/api/*.go (server.go, auth.go, imagenes.go, integraciones.go, masivo.go, observabilidad.go, plantillas.go, precios.go, auditoria.go). Comprobaciones hechas: (1) toda ruta llamada desde la UI existe con el mismo método y forma de cuerpo — se verificó campo a campo contra los structs que decodifican cada handler (editarMasivo, guardarCuenta, editarCanal, guardarHorario, guardarOferta, guardarAtributo, editarProducto, crearIntegracion) y contra los tags JSON de los tipos de respuesta (FilaProducto, DatosPreview, OfertaVista, Horario, Orden, CuentaCanal, Credenciales, ImagenGuardada/decorar, BusquedaMasiva, RespuestaPlantilla); solo apareció un desajuste real, el `categoria` de la edición masiva. (2) Rutas sin pantalla: recuento completo, 12 de 67. (3) Manejo de errores: recorridas todas las llamadas a `api.*` buscando fetch/promesas sin catch y estados de error no pintados. (4) Pérdida de sesión: `pedir` sí reacciona al 401 llamando a alCaducarSesion (api.ts:452-456) y App lo conecta con el login (App.tsx:26-28); lo que falla es el camino inverso, el cierre de sesión. (5) `npx tsc -b` y `npx tsc --noEmit -p tsconfig.json` en web/: ambos salen con código 0, sin errores, con `strict`, `noUnusedLocals` y `noUnusedParameters` activados. (6) Consultas de solo lectura a Postgres (docker exec integra-postgres psql) para confirmar el hallazgo de categ_path: 55 mapeos de categoría, 0 productos que emparejen, 545 productos con categ_path NULL. Fuera de alcance por tiempo: no se ejecutó la interfaz en un navegador (no se comprobaron errores de consola en tiempo real ni el CSS/responsive), no se revisó styles.css ni index.html, no se auditó la accesibilidad ni el rendimiento del listado, y no se validaron los payloads de los adaptadores de canal más allá de lo que la vista previa proyecta. Tampoco se trataron como hallazgos los comportamientos que README/PLAN/MODULOS declaran deliberados (de Odoo solo SKU/nombre/stock, precio propiedad de Integra, /imagenes/* público, pedido en borrador).

**Operación.** Leí primero README.md, PLAN.md, MODULOS.md y PLAN-EJECUCION.md para no reportar decisiones deliberadas (Odoo solo aporta SKU/nombre/stock, pedido en borrador, /imagenes/* público, frontend pendiente, compose de producción y backups previstos para la Fase 6 / Ola 5 — esto último lo reporto igualmente porque la dimensión lo pide, señalando que está planificado).

Revisado en detalle: internal/config/config.go completo (carga, defaults y validar()); cmd/integra/main.go (main y manejo de señales líneas 52-115, uso(), registro(), cmdMigrate, cmdServe y cmdWorker líneas 1121-1234, comprobarEsquema); internal/jobs/worker.go y cola.go completos (lease, reclamo, backoff, huérfanos, apagado); internal/api/server.go (http.Server con sus cuatro timeouts, Escuchar/Shutdown, middlewares cors y registrar, rutas); internal/api/middleware_auth.go (rutas públicas); internal/publicar/manejadores.go y motor.go (uso de PublicBaseURL y opciones de encolado); internal/planificador/planificador.go (bucle, pasada, disparo de horarios); internal/channel/adapter.go (clasificación de errores y RetryAfter); los cuatro adaptadores solo en lo tocante a timeouts HTTP y manejo de 429; internal/imagen/almacen.go (rutas en disco); Dockerfile, docker-compose.yml, docker-compose.odoo.yml, .env.example, .gitignore y la ausencia de .dockerignore; migrations/001_core.sql en la definición de channel_accounts.

Comprobaciones ejecutadas: `go build ./...` y `go vet ./...` (limpios); `env -u INTEGRA_DATABASE_URL go run ./cmd/integra migrate status` para reproducir el fallo de .env; `docker ps` para confirmar el enlace 0.0.0.0:5544; `du -sh datos/imagenes` (409 MB) y consultas de solo lectura a Postgres (`\dt`, `select count(*) from imagenes` → 1132, `select ... from channel_accounts` → 0 filas). Los grep de rate_limit_rps, RetryAfter, EsReintentable, http.Client y .dockerignore están citados en las evidencias.

Fuera de alcance por tiempo o por pertenecer a otra dimensión: el contenido funcional de los adaptadores frente a la documentación de cada canal, la corrección de sync/ordenes/publicar (hashes, marcas de agua, duplicados), el esquema y sus índices, el frontend, y la auditoría de seguridad de la API (roles, tamaño de subidas, inyección). Tampoco levanté el compose para verificar el arranque en un servidor limpio: la conclusión sobre `docker compose up` está deducida del código de comprobarEsquema y del fichero, no de una ejecución real, para no alterar el estado de los contenedores que ya corren.

**Documentación.** Revisé, en modo estrictamente de solo lectura, los cuatro documentos indicados (README.md 376 líneas, PLAN.md 102, MODULOS.md 99, PLAN-EJECUCION.md 173) contra el código real, comprobando: (1) los 20 subcomandos del switch de cmd/integra/main.go y el texto de uso() frente a la tabla «Comandos» del README y frente al conteo de PLAN-EJECUCION; (2) las 10 variables leídas por internal/config/config.go frente a README, .env.example, docker-compose.yml, docker-compose.odoo.yml y uso(), incluida la traza de INTEGRA_PUBLIC_BASE_URL hasta internal/publicar/manejadores.go:165; (3) los pasos de puesta en marcha del README ejecutados mentalmente y, en el caso de .env.example, reproducidos de verdad con un binario compilado en el scratchpad; (4) las 67 rutas registradas en internal/api/*.go y el middleware exigirSesion frente a las pantallas descritas (login, catálogo, pestaña Promoción, Actualizar por plantilla) y frente a web/src/App.tsx y Sidebar; (5) las 42 sentencias CREATE TABLE de migrations/ contra las 38 tablas reales de la base (docker exec psql) y contra las listas de tablas huérfanas de MODULOS.md y PLAN-EJECUCION.md, incluyendo la lectura de las migraciones 011 y 018; (6) el estado ✅/❌ de cada módulo de MODULOS.md contra la existencia de los paquetes y registros correspondientes (channel.Register de los cuatro adaptadores, planificador.moverPromociones, internal/plantilla, internal/pricing, auth, auditoría, observabilidad); (7) las afirmaciones de estado de PLAN.md contra la base (odoo_connections, products, users) y contra go build ./..., go vet ./... y go test ./... , que pasan en limpio y confirman los 9 paquetes sin pruebas que declara PLAN-EJECUCION; (8) la ruta documentada para añadir un canal contra la disposición real de internal/channel e internal/conectores; y (9) el versionado de datos/odoo-config/odoo.conf con git check-ignore y git ls-files.

Quedó fuera por tiempo: MERCADOLIBRE.md (26 KB de referencia de API) frente al adaptador de Mercado Libre; la verificación una a una de las cifras de volumen que cita la documentación (452 productos publicables, 1.365 fotos aptas, mapeo 55/55 de categorías, 4.300 líneas de TypeScript); la comprobación exhaustiva de cada endpoint que consume web/src/api.ts contra las 67 rutas del backend (dimensión «Frontend»); y la revisión del Dockerfile y de los comentarios de docker-compose.yml más allá del servicio web comentado como «llega en la Fase 5», que hoy existe en web/.
