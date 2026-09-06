# Plan de desarrollo

Decisiones tomadas (2026-08-22, con Luis):

- **Canales:** los cuatro (MercadoLibre, Falabella, WooCommerce, Shopify), con
  una **cuenta por canal** donde publican todas las marcas.
- **Alcance:** publicar y mantener sincronizado (contenido, precio, stock) **y**
  traer las órdenes de vuelta, creando el pedido de venta en Odoo
  automáticamente (sin confirmarlo: contabilidad y despacho siguen en Odoo).
- **Operación:** servidor en la nube multiusuario. El proveedor lo decide el
  cliente después: todo queda preparado sobre Docker.
- **Credenciales:** existen cuentas de vendedor con acceso a API en los cuatro
  canales; se conectarán cuando cada conector esté listo.

## Estado actual

Hecho: modelo de datos completo (16 tablas anticipan cuentas, listings,
órdenes, auditoría), sync estricto con Odoo (SKU + nombre + stock por bodega),
catálogo propiedad de Integra (precio, marca, descripción, imágenes, exclusión)
con edición en la interfaz, generador de contenido, banco de imágenes con
búsqueda web por SKU (individual y masiva), motor de tarifas, proyección pura
de payloads por canal con vista previa, y mapeo de categorías de MercadoLibre
confirmado (55/55).

Bloqueador externo: la base de pruebas de Odoo fue purgada. Para probar
órdenes y stock reales hace falta reconectar (`integra conectar-odoo`) contra
una base viva.

## Fases

### Fase 2 — Motor de trabajos ✅
La base de todo lo que sigue: publicar 452 productos × 4 canales son miles de
llamadas con reintentos, y no pueden vivir en una petición HTTP.
- Tabla `jobs` (estado, prioridad, reintentos con backoff, clave de
  idempotencia, lease por worker).
- Paquete `internal/jobs`: encolar, reclamar con `FOR UPDATE SKIP LOCKED`,
  completar/fallar, recuperación de trabajos huérfanos.
- `integra worker` procesa con la concurrencia de `INTEGRA_WORKER_CONCURRENCY`.

**Hecho cuando:** un trabajo encolado se procesa, reintenta con backoff al
fallar y sobrevive a la muerte del worker.

### Fase 3 — Cuentas de canal y credenciales ✅
- Alta de la cuenta de cada canal desde la interfaz, credencial cifrada
  (AES-GCM ya existe). OAuth de MercadoLibre con refresco de token; API keys
  para Falabella/Woo/Shopify.
- Asignación de bodegas por cuenta (`channel_account_warehouses`): Falabella
  suma sus 3 bodegas FB; los demás, la principal.

**Hecho cuando:** cada canal muestra "conectado" con una llamada de prueba real.

### Fase 4 — Conectores de publicación ✅ (los cuatro canales)
Un motor común y cuatro adaptadores (`channel.Adapter` ya definido).
- Motor común: decide qué publicar (los N publicables), calcula los tres
  hashes (contenido/precio/stock), compara con `variant_channel_listings` y
  encola solo lo que cambió. Todo pasa por la cola de la Fase 2.
- Orden de construcción (de menor a mayor fricción de API):
  1. **Shopify** (REST/GraphQL simple, valida el motor completo)
  2. **WooCommerce** (REST con claves)
  3. **MercadoLibre** (OAuth, categorías + atributos obligatorios — el mapeo ya está)
  4. **Falabella** (Seller Center, el más estricto: EAN, peso, categoría propia)
- Publicación inicial en modo pausado/borrador donde el canal lo permita; se
  activa por producto desde Integra.

**Hecho cuando:** un cambio de precio en Integra llega solo al canal en <5 min
y la interfaz muestra el estado de cada publicación.

### Fase 5 — Órdenes de vuelta
- Ingesta por sondeo (webhooks donde el canal los dé): `channel_orders` +
  `order_ingest_state` ya existen.
- Creación idempotente del `sale.order` en Odoo por XML-RPC (cliente por
  referencia del canal, líneas por SKU, bodega según la cuenta). Una orden que
  no se puede mapear queda en la cola de atención, nunca se pierde.
- Descuento de stock publicado inmediato al ingerir (sin esperar al sync).

**Hecho cuando:** una orden real de cada canal aparece como pedido en Odoo con
sus líneas correctas y sin duplicados tras reintentos.

### Fase 6 — Multiusuario y despliegue
- Login (tabla `users` ya existe), sesiones, roles mínimos (admin/operador).
- Compose de producción: API + worker + Postgres + proxy con HTTPS (Caddy),
  backups automáticos de Postgres, logs persistentes.
- Guía de despliegue para el proveedor que elija el cliente.

**Hecho cuando:** dos usuarios distintos operan por HTTPS en un servidor limpio
instalado siguiendo la guía.

### Fase 7 — Operación continua
- Sincronizaciones programadas (`sync_schedules`), alertas (stock agotado,
  token vencido, órdenes sin mapear), reconciliación diaria canal↔Integra,
  y panel de salud de publicaciones.

## Riesgos conocidos

- **Base de Odoo:** hasta reconectar, órdenes y stock se prueban contra mocks.
- **Falabella:** su API es la más rígida; por eso va última, con el motor ya
  rodado en tres canales.
- **Búsqueda de imágenes:** el endpoint de DuckDuckGo no es oficial; si se
  bloquea, el reemplazo es Google Programmable Search con clave.
- **Órdenes → Odoo:** crear pedidos exige permisos de escritura del usuario
  API de Odoo y decisiones de impuestos/cliente genérico que hay que validar
  con contabilidad al llegar a la Fase 5.
