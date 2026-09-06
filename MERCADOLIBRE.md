# Mercado Libre — referencia de API para el conector de Integra

Resumen de la documentación oficial (portal `developers.mercadolibre.com.co`,
páginas consultadas el 5 de septiembre de 2026) recortado a lo que necesita el
adaptador `channel.Adapter` de Integra: publicar, actualizar y leer pedidos.

Base de la API: `https://api.mercadolibre.com`. Site de Colombia: `MCO`.
Moneda: `COP`. Los IDs de ítem tienen forma `MCO123456789`; los de User Product,
`MCOU123456789`.

Fuentes:

- Autenticación: https://developers.mercadolibre.com.co/es_ar/autenticacion-y-autorizacion
- Publicar productos: https://developers.mercadolibre.com.co/es_ar/publica-productos
- Precio por variación (User Products): https://developers.mercadolibre.com.co/es_ar/precio-variacion
- User Products (conceptos): https://developers.mercadolibre.com.co/es_ar/user-products
- Sincronizar y modificar: https://developers.mercadolibre.com.co/es_ar/producto-sincroniza-modifica-publicaciones
- API de precios: https://developers.mercadolibre.com.co/es_ar/api-de-precios
- Descripción: https://developers.mercadolibre.com.co/es_ar/descripcion-de-articulos
- Imágenes: https://developers.mercadolibre.com.co/es_ar/trabajar-con-imagenes
- Stock distribuido: https://developers.mercadolibre.com.co/es_ar/stock-distribuido
- Órdenes: https://developers.mercadolibre.com.co/es_ar/gestiona-ventas
- Notificaciones: https://developers.mercadolibre.com.co/es_ar/productos-recibe-notificaciones
- Usuarios de prueba: https://developers.mercadolibre.com.co/es_ar/realiza-pruebas
- Rate limit: https://developers.mercadolibre.com.co/es_ar/rate-limit-error-429

---

## 1. Autenticación (OAuth 2.0, Authorization Code, server side)

Hay que crear una aplicación en el portal (App ID + Secret Key) con una
`redirect_uri` **exacta y sin partes variables**. Lo que varía se manda en
`state`.

### 1.1 Autorizar

El vendedor abre (dominio del país):

```
https://auth.mercadolibre.com.co/authorization?response_type=code&client_id=$APP_ID&redirect_uri=$REDIRECT_URI&state=$STATE
```

Si la app tiene PKCE activado, `code_challenge` y `code_challenge_method=S256`
pasan a ser obligatorios. El usuario debe ser la cuenta **principal** del
vendedor; un colaborador/operador falla con `invalid_operator_user_id`.
Vuelve a `$REDIRECT_URI?code=...&state=...`.

### 1.2 Cambiar el code por tokens

```bash
curl -X POST https://api.mercadolibre.com/oauth/token \
  -H 'accept: application/json' \
  -H 'content-type: application/x-www-form-urlencoded' \
  -d 'grant_type=authorization_code' \
  -d 'client_id=$APP_ID' \
  -d 'client_secret=$SECRET_KEY' \
  -d 'code=$CODE' \
  -d 'redirect_uri=$REDIRECT_URI'
```

Respuesta:

```json
{
  "access_token": "APP_USR-...",
  "token_type": "bearer",
  "expires_in": 10800,
  "scope": "offline_access read write",
  "user_id": 1234567,
  "refresh_token": "TG-..."
}
```

`user_id` es el `seller_id` que se usa después en búsquedas de ítems y órdenes.

### 1.3 Refrescar

```bash
curl -X POST https://api.mercadolibre.com/oauth/token \
  -d 'grant_type=refresh_token' -d 'client_id=$APP_ID' \
  -d 'client_secret=$SECRET_KEY' -d 'refresh_token=$REFRESH_TOKEN'
```

Reglas que condicionan el diseño del almacenamiento de credenciales:

- El `access_token` dura **6 horas**. Renovar solo cuando expira.
- El `refresh_token` es de **un solo uso**: cada refresh devuelve uno nuevo que
  hay que guardar de inmediato. Solo vale el último generado. Dura 6 meses.
- El token se invalida antes de tiempo si el vendedor cambia contraseña,
  revoca la app, la app rota el secret, o pasan 4 meses sin llamadas.
- `invalid_grant` = code/refresh vencido, ya usado, o `redirect_uri` distinto.
  Ante eso hay que pedir al vendedor que reautorice.
- Siempre `Authorization: Bearer $ACCESS_TOKEN` en header. Nunca en query.

Para Integra: guardar `client_id`, `client_secret`, `access_token`,
`refresh_token`, `expires_at` y `user_id` cifrados en `channel_accounts`, y
serializar el refresh (un solo proceso a la vez) porque dos refresh
concurrentes invalidan uno al otro.

---

## 2. Publicar (`Adapter.Publish`)

### 2.1 Dos modelos que conviven

Mercado Libre está migrando a **User Products (UP)**. Cuál aplica depende del
vendedor, y se detecta con `GET /users/$SELLER_ID`: si `tags` contiene
`user_product_seller`, el vendedor está en el modelo nuevo y publicar con el
modelo viejo devuelve **400**.

| | Modelo legacy | Modelo User Products |
|---|---|---|
| `title` | lo manda el vendedor | **no se envía**, lo genera ML |
| `family_name` | no existe | **obligatorio**: nombre genérico del producto ("Apple iPhone 256GB") |
| `variations[]` | array con las variantes | **desaparece**: cada variante es un ítem distinto con el mismo `family_name` y atributos PARENT_PK |
| `user_product_id` | 1:1 con el ítem | lo asigna ML; agrupa ítems de la misma variante |

Los atributos que pueden variar dentro de una familia son los `CHILD_PK` del
dominio (color, talla, etc.). Los `PARENT_PK` deben coincidir en toda la
familia. Máximo 30 ítems por User Product.

Recomendación para Integra: detectar el tag al conectar la cuenta y guardarlo;
implementar el modelo UP como camino principal porque los vendedores nuevos ya
entran así, y el legacy solo como fallback.

### 2.2 Antes de publicar

1. **Categoría**: `Predictor.Predecir` ya existe en `internal/mercadolibre`.
   Publicar en una categoría distinta a la sugerida provoca moderación.
2. **Atributos de la categoría**: `GET /categories/$CAT/attributes` (ya
   implementado en `AtributosDeCategoria`). Los marcados `required` son
   obligatorios; `ITEM_CONDITION` va aquí (valores `2230284` nuevo,
   `2230581` usado, `2230582` reacondicionado). `max_title_length` viene de la
   categoría.
3. **Condiciones de venta**: `GET /categories/$CAT/sale_terms` para
   `WARRANTY_TYPE` y `WARRANTY_TIME`. Reacondicionado exige garantía ≥ 90 días.
4. **Tipo de publicación** (`listing_type_id`): `gold_special`, `gold_pro`,
   `free`, etc. Para pruebas nunca `gold` ni `gold_premium`.
5. **Pago inmediato**: `GET /sites/MCO/categories/$CAT` →
   `immediate_payment: required|optional`.

### 2.3 POST /items (modelo User Products)

```bash
curl -X POST https://api.mercadolibre.com/items \
  -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Content-Type: application/json' -d '{
  "family_name": "Impresora térmica 80mm",
  "category_id": "MCO12345",
  "price": 189900,
  "currency_id": "COP",
  "available_quantity": 6,
  "buying_mode": "buy_it_now",
  "listing_type_id": "gold_special",
  "condition": "new",
  "sale_terms": [
    { "id": "WARRANTY_TYPE", "value_name": "Garantía del vendedor" },
    { "id": "WARRANTY_TIME", "value_name": "12 meses" }
  ],
  "pictures": [
    { "source": "https://integra.midominio.com/imagenes/abc123.jpg" }
  ],
  "attributes": [
    { "id": "BRAND", "value_name": "Xprinter" },
    { "id": "MODEL", "value_name": "XP-80C" },
    { "id": "GTIN", "value_name": "7898095297749" },
    { "id": "SELLER_SKU", "value_name": "SKU-ODOO-001" },
    { "id": "COLOR", "value_name": "Negro" }
  ],
  "shipping": { "mode": "me2", "local_pick_up": false, "free_shipping": false }
}'
```

En el modelo legacy es igual pero con `"title"` en vez de `"family_name"` y,
si hay variantes, un array `variations` con `attribute_combinations`,
`price`, `available_quantity` y `picture_ids`.

Puntos clave:

- `price` y `currency_id` son obligatorios al crear.
- **SKU**: ML solo reconoce el atributo `SELLER_SKU`. Es lo que vuelve en las
  órdenes como `order_items[].item.seller_sku`, así que es la clave para
  mapear a la variante de Integra/Odoo.
- `available_quantity: 0` crea el ítem en `paused` / `out_of_stock`.
- `condition` (top level) sigue aceptándose por compatibilidad, pero la
  recomendación es `ITEM_CONDITION` dentro de `attributes`.
- `exclusive_channel` ya no existe; el canal se declara en `channels: ["marketplace"]`.
- Respuesta: `id` (item_id), `user_product_id`, `family_name`, `permalink`,
  `status`, `warnings[]`. Guardar `id` y `user_product_id` en `listings`.
- Puede devolver **206** con header `X-Content-Missing` (faltó
  `seller_address` o `geolocation`); el ítem se creó igual.

### 2.4 Descripción (paso aparte, después del POST)

```bash
curl -X POST https://api.mercadolibre.com/items/$ITEM_ID/description \
  -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Content-Type: application/json' \
  -d '{ "plain_text": "Texto plano. Solo saltos de línea, sin HTML ni emojis." }'
```

Si ya existe, el POST da 400 y hay que usar `PUT .../description?api_version=2`.
Con `api_version=2` el error `item.description.type.invalid` indica en
`references` la posición del carácter inválido.

### 2.5 Imágenes

Dos vías:

- **`source`**: URL pública. ML la descarga desde sus IPs
  (`216.33.196.4`, `216.33.196.25`, `54.88.218.97`, `18.215.140.160`,
  `18.213.114.129`, `18.206.34.84`). Sin redirecciones, `Content-Type`
  correcto, y si el certificado TLS da problemas usar `http`. Para Integra
  esto exige que `INTEGRA_PUBLIC_BASE_URL` sea alcanzable desde Internet.
- **Subida directa** (recomendada, no depende de exponer el banco):

```bash
curl -X POST https://api.mercadolibre.com/pictures/items/upload \
  -H 'Authorization: Bearer $ACCESS_TOKEN' \
  -H 'content-type: multipart/form-data' -F 'file=@foto.jpg'
```

Devuelve `id` (picture_id) que se usa como `{ "id": "..." }` en `pictures`.
Para reemplazar imágenes de un ítem se hace `PUT /items/$ID` con el array
completo `pictures` (ids a conservar + sources/ids nuevos, en el orden
deseado); lo que no se manda se borra. Reutilizar el mismo `source` con
contenido distinto **no** actualiza la imagen.

Límites: JPG/PNG, hasta 10 MB, ideal 1200×1200, mín. 500×500, máx. 1920×1920.
Máximo de fotos por categoría en `max_pictures_per_item`. Diagnóstico de
fallos: `GET /pictures/$PICTURE_ID/errors`.

---

## 3. Actualizar (`Update`, `UpdateStock`, `UpdatePrice`, `Pause`, `Resume`)

Todo va por `PUT /items/$ITEM_ID` con solo los campos que cambian, **salvo el
precio** (ver 3.2).

Qué se puede cambiar:

- Siempre: `available_quantity`, `pictures`, `video`, `shipping`, `sale_terms`,
  `attributes`, descripción (endpoint aparte), `status`.
- Solo si `sold_quantity == 0`: `title` (legacy) / `family_name` (UP).
- Nunca con ventas: `buying_mode`, métodos de pago.
- `listing_type_id` solo una vez.
- Modelo UP: `title`, `family_name`, `attributes`, `pictures`, `condition`,
  `available_quantity` se replican asíncronamente a todos los ítems del mismo
  `user_product_id`. No se pueden crear `variations` por POST ni PUT.

### 3.1 Stock

```bash
curl -X PUT https://api.mercadolibre.com/items/$ITEM_ID \
  -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Content-Type: application/json' \
  -d '{ "available_quantity": 6 }'
```

- `0` → `paused` con `sub_status: ["out_of_stock"]`; volver a >0 lo reactiva
  solo. Solo aplica a `condition: new` y `listing_type != free`.
- Legacy con variantes: `{"variations":[{"id":174497701554,"available_quantity":3}]}`.
- Vendedores con **multiorigen** (tag `warehouse_management` en `/users`) o con
  stock distribuido usan `GET /user-products/$UP_ID/stock` y
  `PUT /user-products/$UP_ID/stock/type/seller_warehouse` (o
  `selling_address`), enviando el header `x-version` que devolvió el GET; si no
  coincide responde 409 y hay que releer. Sin multiorigen, el PUT a `/items`
  sigue siendo el camino y ML sincroniza el UP.
- Stock de Fulfillment (`meli_facility`) no se puede editar por API.

### 3.2 Precio — cambio desde el 18/03/2026 para ítems automatizados

`PUT /items/$ID {"price": ...}` sigue funcionando para los ítems normales.
Lo que cambió el 18 de marzo de 2026 es que los ítems con **automatización
de precios activa** (reglas `INT` / `INT_EXT`, página "Automatizaciones de
precios") ya no aceptan precio por API:

- solo `price` en el body → **400** con `"error": "item.price.not_modifiable"`;
- `price` junto a otros campos → 200, los demás campos se aplican, el precio
  se ignora y llega `warnings[].code = "item.price.not_modifiable"`.

Para saber de antemano qué ítems están automatizados:
`GET /pricing-automation/users/$SELLER_ID/items` (paginado, máx. 100) o
`GET /pricing-automation/items/$ITEM_ID/automation`.

El adaptador de Integra manda solo `price`, y comprueba en la respuesta que
el precio aplicado sea el pedido: así un 200 sin efecto no se anota como
precio publicado, y el 400 se traduce a un error que explica que hay que
desactivar la automatización en Seller Central.

A medio plazo el reemplazo es la API de precios:

```bash
# Leer precios vigentes (standard y promotion)
curl -H 'Authorization: Bearer $ACCESS_TOKEN' https://api.mercadolibre.com/items/$ITEM_ID/prices

# Precio de venta ganador en el canal
curl -H 'Authorization: Bearer $ACCESS_TOKEN' \
  'https://api.mercadolibre.com/items/$ITEM_ID/sale_price?context=channel_marketplace'
```

Escritura anunciada (la página dice "aún no disponible, próximamente
reemplazará al PUT de ítems"):

```bash
curl -X POST https://api.mercadolibre.com/items/$ITEM_ID/prices/standard \
  -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Content-Type: application/json' -d '{
  "prices": [
    { "conditions": { "context_restrictions": ["channel_marketplace"] },
      "amount": 189900, "currency_id": "COP" }
  ]
}'
```

Cuando ese endpoint se active, `UpdatePrice` debe pasar a usarlo. Los campos
`price`, `base_price` y `original_price` de `/items` serán eliminados
progresivamente; para leer precios conviene ir migrando a `/prices`.

También hay que suscribirse al tópico `items_prices` para enterarse de cambios
hechos desde Seller Central o promociones.

### 3.3 Estado

```bash
curl -X PUT .../items/$ITEM_ID -d '{ "status": "paused" }'   # Pause
curl -X PUT .../items/$ITEM_ID -d '{ "status": "active" }'   # Resume
curl -X PUT .../items/$ITEM_ID -d '{ "status": "closed" }'   # cerrar (irreversible)
curl -X PUT .../items/$ITEM_ID -d '{ "deleted": "true" }'    # eliminar (tras closed)
```

Valores en minúscula. Un ítem `paused_by_seller` no se reactiva al reponer
stock; hay que mandar `status: active`. Tras `closed`, el segundo PUT puede dar
`409 optimistic locking`; reintentar unos segundos después.

Estados y subestados que debe entender `FetchStatus`:

| status | sub_status | Significado |
|---|---|---|
| `active` | | visible |
| `paused` | `out_of_stock` | sin stock; se reactiva solo al reponer |
| `paused` | `paused_by_seller` | pausado a mano; no se reactiva solo |
| `paused` | `picture_downloading_pending` | esperando descarga de imágenes |
| `under_review` | `warning`, `waiting_for_patch`, `held`, `pending_documentation`, `forbidden` | moderación; `forbidden` solo se puede eliminar |
| `payment_required` | | deuda del vendedor |
| `closed` | `expired`, `deleted`, `suspended`, `freezed`, ... | final; solo republicar |
| `inactive` | | no corrigió la moderación |

### 3.4 Leer ítems (`FetchStatus`, `ListRemote`)

```bash
curl -H 'Authorization: Bearer $ACCESS_TOKEN' https://api.mercadolibre.com/items/$ITEM_ID
curl -H 'Authorization: Bearer $ACCESS_TOKEN' 'https://api.mercadolibre.com/items?ids=MCO1,MCO2,...&attributes=id,status,sub_status,available_quantity'
curl -H 'Authorization: Bearer $ACCESS_TOKEN' 'https://api.mercadolibre.com/users/$SELLER_ID/items/search?status=active&offset=0&limit=50'
```

`available_quantity`, `channels` e `initial_quantity` solo aparecen con el
token del dueño. Para listados grandes, `items/search` acepta
`search_type=scan` con `scroll_id`; no mezclar scroll con offset/limit y
consumir el scroll completo antes de que expire. Para agrupar por familia:
`GET /users/$SELLER_ID/items/search?user_product_id=MCOU1,MCOU2`.

---

## 4. Pedidos (`FetchOrders`, `AckOrder`)

### 4.1 Buscar órdenes

`/orders/search` no hace nada sin filtro; el filtro base es `seller`.

```bash
curl -H 'Authorization: Bearer $ACCESS_TOKEN' \
  'https://api.mercadolibre.com/orders/search?seller=$SELLER_ID&order.status=paid&order.date_created.from=2026-09-01T00:00:00.000-00:00&order.date_created.to=2026-09-05T00:00:00.000-00:00&sort=date_desc&offset=0&limit=50'
```

Filtros útiles: `order.status` (varios separados por coma),
`order.date_created.from/to`, `order.date_last_updated.from/to`,
`order.date_closed.from/to`, `tags`, `tags.not`, `q` (id de orden, id de ítem,
título o nickname), `item`. En las fechas usar solo hasta la hora. Paginación
`offset`/`limit` con `paging.total` en la respuesta. Orden por defecto
`date_asc` sobre `date_closed` (para vendedores).

Se guardan órdenes de los últimos **12 meses** y, buscando como vendedor, las
canceladas quedan filtradas.

Para `FetchOrders(desde, cursor)`: filtrar por
`order.date_last_updated.from=desde` (no por creación, para captar cambios de
estado), `sort=date_desc`, y usar `offset` como cursor.

### 4.2 Detalle

```bash
curl -H 'Authorization: Bearer $ACCESS_TOKEN' https://api.mercadolibre.com/orders/$ORDER_ID
```

Campos que mapean a `channel.Order` / `channel.OrderLine`:

```json
{
  "id": 2000003508419013,
  "status": "paid",
  "date_created": "2026-01-05T10:30:00.000-05:00",
  "date_closed": "2026-01-05T10:32:15.000-05:00",
  "date_last_updated": "...",
  "pack_id": null,
  "order_items": [{
    "item": { "id": "MCO123", "title": "...", "category_id": "...",
              "variation_id": null, "variation_attributes": [],
              "seller_sku": "SKU-ODOO-001", "seller_custom_field": null },
    "quantity": 2, "unit_price": 440.0, "sale_fee": 14.29,
    "discounts": [{ "amounts": { "full": 341.0, "seller": 341.0 } }],
    "gross_price": 1562.0, "currency_id": "COP"
  }],
  "total_amount": 880.0, "paid_amount": 880.0, "currency_id": "COP",
  "taxes": { "amount": null, "currency_id": null },
  "buyer": { "id": 123, "nickname": "...", "first_name": "...", "last_name": "..." },
  "seller": { "id": 987 },
  "payments": [{ "id": 1, "status": "approved", "transaction_amount": 880.0,
                 "payment_method_id": "master", "installments": 1 }],
  "shipping": { "id": 43210987654321 },
  "tags": ["paid", "not_delivered"],
  "context": { "channel": "marketplace", "site": "MCO", "flows": [] }
}
```

Notas:

- `seller_sku` es el `SELLER_SKU` que se publicó; si el ítem no lo tiene, viene
  `null` y hay que resolver por `item.id` contra `listings`.
- `sale_fee` es la comisión, calculada al acreditarse el pago.
- `gross_price = (unit_price + discounts.full) × quantity`; `unit_price` ya
  viene con descuento.
- Puede responder **206** con `X-Content-Missing: buyer, shipping, ...`.
- Total con envío = `total_amount + taxes.amount + lead_time.cost`
  (este último de `/shipments`).
- Compras de carrito: varias órdenes comparten `pack_id`; ver `/packs/$PACK_ID`.
- Tag `fraud_risk_detected`: no despachar y cancelar.
- Con `buying_mode: buy_it_now` la orden solo se muestra al vendedor con el
  pago aprobado, así que en la práctica lo que llega ya está en `paid`.

### 4.3 Estados de la orden

| status | Significado |
|---|---|
| `confirmed` | creada, sin pago (o vendedor la marcó como no concretada) |
| `payment_required` | falta confirmar pago |
| `payment_in_process` | pago pendiente de acreditar |
| `partially_paid` | pago insuficiente |
| `paid` | **pagada, es la que hay que montar en Odoo** |
| `partially_refunded` | devoluciones parciales |
| `pending_cancel` | cancelando, pendiente de reembolso |
| `cancelled` | cancelada |

### 4.4 Envío

```bash
# Envíos de una orden (vista nueva; siempre devuelve array)
curl -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'X-New-Domain: true' \
  https://api.mercadolibre.com/orders/$ORDER_ID/shipments
# Detalle con dirección y nombre/teléfono del receptor
curl -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'X-Api-Version: 2' \
  'https://api.mercadolibre.com/shipments/$SHIPMENT_ID?views=origin,destination'
```

Iterar y filtrar `type == "forward"` (los `return` y `return_to_buyer` son
devoluciones). La vista vieja de `/orders/$ID/shipments` sin `list_all` se
deprecará a finales de septiembre de 2026. El envío se asocia a la orden de
forma asíncrona: puede tardar unos segundos en aparecer (204).

Estados de envío: `pending`, `handling`, `ready_to_ship`, `shipped`,
`delivered`, `not_delivered`, `not_verified`, `cancelled`.

`receiver_address` (con `?views=destination` y `X-Api-Version: 2`) trae
`receiver_name`, `receiver_phone`, calle, ciudad, departamento y zip, que es
lo que necesita `channel.Address` para crear el contacto en Odoo.

### 4.5 Confirmar / despachar (`AckOrder`)

ML no tiene un "acknowledge" de orden. Lo que existe es actuar sobre el envío
según la logística (`shipping.mode` / `logistic_type`): imprimir etiqueta ME2
(`GET /shipment_labels?shipment_ids=...&response_type=pdf`), marcar como
enviado con guía propia en envíos `custom`/`not_specified`, etc. Eso vive en
la guía "Envíos" y se decide cuando se defina la operación logística de MDV.

---

## 5. Notificaciones (webhooks)

Se configuran en la app del portal: **Callback URL pública** + tópicos. Llega
un `POST` JSON:

```json
{
  "_id": "f9f08571-...",
  "resource": "/orders/2195160686",
  "user_id": 468424240,
  "topic": "orders_v2",
  "application_id": 5503910054141466,
  "attempts": 1,
  "sent": "2019-10-30T16:19:20.129Z",
  "received": "2019-10-30T16:19:20.106Z"
}
```

Tópicos que interesan a Integra:

| Tópico | Cuándo | Qué consultar después |
|---|---|---|
| `orders_v2` | creación y cambios de una venta | `GET /orders/$ID` |
| `shipments` | cambios en el envío | `GET /shipments/$ID` |
| `items` | cualquier cambio en un ítem (moderación, pausas, precio) | `GET /items/$ID` |
| `items_prices` | precio creado/actualizado/eliminado | `GET /items/$ID/sale_price` |
| `stock-location` | cambio de stock del UP | `GET /user-products/$UP/stock` |
| `user_products` | UP creado/actualizado/purgado (migración UPtin) | `GET /user-products/$UP` |
| `payments` | pago creado o cambia de estado | `GET /collections/$ID` |
| `questions` | preguntas de compradores | `GET /questions/$ID` |
| `post_purchase` (claims) | reclamos | recurso del `resource` |

Reglas duras:

- Responder **HTTP 200 en menos de 500 ms**. Si no, ML reintenta durante 1
  hora (8 intentos) y luego desactiva el tópico; las notificaciones de ese
  período se pierden y hay que resuscribirse.
- Por eso: el handler solo encola (tabla `jobs`) y devuelve 200; la consulta
  al recurso la hace el worker. Idempotencia por `_id` o por
  `(topic, resource, user_id)`.
- La notificación no trae datos, solo el `resource`; siempre hay que hacer el
  GET y comparar con lo guardado.
- Fechas en UTC.
- Notificaciones perdidas: `GET /missed_feeds?app_id=$APP_ID&topic=orders_v2&offset=0&limit=50`
  (solo 2 días hacia atrás; para `topic=items` es obligatorio `site_id=MCO`).
- Si se filtra por IP, la lista de orígenes está en la página de
  notificaciones (unas 100 IPs de AWS/GCP).

Diseño para Integra: un endpoint `POST /api/webhooks/mercadolibre` que valide
`application_id`, inserte el job y responda 200. Como respaldo, un job
periódico que haga `orders/search` por `date_last_updated` y `missed_feeds`.

---

## 6. Pruebas

No hay sandbox: se prueba en producción con **usuarios de test**.

```bash
curl -X POST https://api.mercadolibre.com/users/test_user \
  -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Content-type: application/json' \
  -d '{ "site_id": "MCO" }'
# → { "id": 120506781, "nickname": "TEST0548", "password": "qatest328", "site_status": "active" }
```

- Hasta 10 usuarios de test por cuenta; guardar las credenciales porque no hay
  forma de listarlos. Se borran tras 60 días sin actividad.
- Crear al menos un vendedor y un comprador de test. Solo pueden operar entre
  sí y sobre ítems de test.
- Título obligatorio en pruebas: "Item de Prueba - Por favor, NO OFERTAR".
  Categoría "Otros", nunca `gold` ni `gold_premium`.
- Para probar User Products hay que pedir la "ambientación" del usuario de
  test por formulario (se activan cada 7 días).
- Pagos de prueba con tarjetas de prueba de Mercado Pago; el titular
  `APRO APRO` aprueba el pago.
- Para las notificaciones hay una colección Postman en la página de
  notificaciones que dispara un POST de ejemplo contra la callback.

---

## 7. Límites y errores

- **Rate limit por Client ID** (app), por endpoint. 429 → backoff exponencial
  con jitter, menos concurrencia, agrupar llamadas (multiget de `/items?ids=`).
  Encaja con el backoff que ya tiene el worker de `jobs`.
- `403 forbidden`: token de otro usuario, IP bloqueada o faltan scopes.
- `seller.unable_to_list`: el vendedor tiene algo pendiente en su cuenta;
  mirar `cause` y hacer una primera publicación manual desde la web.
- `item.category_id.invalid`, `body.invalid_fields`: categoría o campo no
  válido para esa categoría.
- Códigos de orden: `not_owned_order` / `caller.id.invalid` (token que no es
  del vendedor ni del comprador), `order_not_found`.
- Las respuestas 206 con `X-Content-Missing` son parciales pero utilizables.

---

## 8. Mapeo al contrato `channel.Adapter`

| Método | Llamadas ML |
|---|---|
| `Publish` | `GET /users/$SELLER` (modelo) → `POST /items` → `POST /items/$ID/description` |
| `Update` | `PUT /items/$ID` (atributos, fotos, sale_terms) + `PUT .../description?api_version=2` |
| `UpdateStock` | `PUT /items/$ID {available_quantity}`; multiorigen: `PUT /user-products/$UP/stock/type/seller_warehouse` con `x-version` |
| `UpdatePrice` | `PUT /items/$ID {price}` verificando el precio de la respuesta; error claro si `item.price.not_modifiable` (automatización activa). Migrar a `POST /items/$ID/prices/standard` cuando ML lo active |
| `Pause` / `Resume` | `PUT /items/$ID {status: paused|active}` |
| `FetchStatus` | `GET /items?ids=...&attributes=id,status,sub_status,available_quantity,price` |
| `ListRemote` | `GET /users/$SELLER/items/search` (+ multiget de `/items?ids=`) |
| `FetchOrders` | `GET /orders/search?seller=&order.date_last_updated.from=&offset=` (la búsqueda ya trae el detalle) → por orden con envío, `GET /shipments/$ID` con `X-Api-Version: 2` para la dirección |
| `AckOrder` | depende de la logística; ver guía "Envíos" |
| Webhook (nuevo) | `POST /api/webhooks/mercadolibre` → job → GET del `resource` |

Datos nuevos que hay que persistir por listing: `item_id`, `user_product_id`,
`family_name`, `listing_type_id`, `status`/`sub_status`, `permalink`. Por
cuenta: `client_id`, `client_secret`, `access_token`, `refresh_token`,
`expires_at`, `seller_id`, flag `user_product_seller`, `site_id`.
