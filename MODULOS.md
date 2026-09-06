# Mapa de módulos

Análisis del 2026-08-22 contra el objetivo declarado:

> **publicar productos, recibir pedidos y montarlos automáticamente en Odoo.**

El objetivo tiene tres pilares. Hoy el primero está al 60 %, el segundo no
existe y el tercero es imposible con el código actual (el cliente de Odoo solo
sabe leer). Este documento lista los módulos que la plataforma necesita, cuáles
están hechos y cuáles faltan.

Dato que ordena el diagnóstico: **de las 39 tablas del esquema, 14 no tienen
una sola línea de código detrás**. El esquema se diseñó completo desde el
principio; lo que falta es implementación, no rediseño.

---

## Pilar 1 — Publicar productos

| Módulo | Estado | Qué hace / qué falta |
|---|---|---|
| `sync` | ✅ | Trae de Odoo SKU, nombre y stock por bodega. Nada más, por diseño. |
| `store` | ✅ | Catálogo propiedad de Integra: precio, marca, descripción, exclusión. |
| `imagen` + `webimagenes` | ✅ | Banco propio, derivadas, búsqueda por SKU, 1.365 fotos aptas para los 4 canales. |
| `content` + `ia` | ✅ | Fichas por reglas y redacción con modelo. El proveedor por defecto es un modelo local sobre Ollama (`qwen2.5:3b` texto, `qwen2.5vl:3b` visión), con la API de Claude disponible en `IA_PROVEEDOR=anthropic`. El JSON se pide con esquema, no rogando. Falta ejecutar el lote de las fichas que quedan. |
| `pricing` | ✅ | Motor de tarifas de Odoo. Congelado: solo sembró el precio inicial. |
| `channel` | ✅ | Contrato `Adapter` + `Capabilities`. Bien diseñado, no necesita cambios. |
| `publicar` | ✅ | Motor de diff por 3 hashes: encola solo lo que cambió, con prioridades. |
| `conectores/shopify` | ✅ | Publicar, precio, stock, pausar, adoptar por SKU, listar, pedidos. |
| `conectores/woocommerce` | ✅ | REST v3. Único con ofertas programadas nativas y lotes de 100. |
| `conectores/mercadolibre` | ✅ | OAuth con refresco; el refresh token rotado se guarda cifrado en la cuenta con la fila bloqueada y releída (`RotateCredentials`), de modo que ocho trabajos a la vez canjean una sola vez y los demás adoptan el token nuevo. Publica según el modelo del vendedor (`title` legacy / `family_name` User Products). Precio verificado contra la respuesta y error explícito si el ítem tiene automatización de precios. Pedidos por `date_last_updated`, paginados, con dirección del envío. Adopción por `seller_sku`, descripción en su propio recurso, publicación pausada. Referencia de la API en `MERCADOLIBRE.md`. Pendiente: flujo OAuth desde la UI, webhooks y `AckOrder`. |
| `conectores/falabella` | ✅ | Firma HMAC compartida, feeds asíncronos con `EstadoFeed`, exige EAN y peso antes de enviar. El SKU es la referencia (no hay id de publicación aparte). |
| `atributos` | ✅ | Multicanal: `FuenteRequisitos` la implementan MercadoLibre (API pública) y Falabella (Seller Center con firma). Deduce valores desde marca, SKU y specs. La ficha muestra solo obligatorios y con valor; los opcionales se añaden desde un buscador. Shopify y WooCommerce quedan fuera a propósito: no exigen atributos. |
| `precios/canal` | ✅ | Hecha la comisión por canal, `channel_price_rules` (reglas por canal/categoría/marca), `price_overrides` (precio fijado a mano para un canal) y `effective_prices` (precio resuelto y auditable integrado en candidatos). |
| `ofertas` | ✅ | Tabla `offers` con resolución de vigencia y precio de oferta (`sale_price`) en `effective_prices`. |

## Pilar 2 — Recibir pedidos

| Módulo | Estado | Qué hace / qué falta |
|---|---|---|
| `ordenes` | ✅ | Ingesta por sondeo con marca de agua, deduplicación por (cuenta, id externo), emparejamiento de líneas por SKU y montaje del `sale.order`. Falta el descuento inmediato del stock publicado. |
| `webhooks` | ❌ | **Falta.** Endpoint público con verificación de firma por canal. Sin él, la ingesta depende de sondeo y un pedido puede tardar minutos en aparecer. |
| `devoluciones` | ❌ | **Falta.** Cancelaciones y devoluciones cambian stock y contabilidad; ignorarlas deja el inventario mintiendo. |
| `despacho` | 🟡 | El contrato tiene `AckOrder(tracking, carrier)`, ningún adaptador lo implementa. Es lo que cierra el ciclo: el canal necesita el número de guía. |

## Pilar 3 — Montarlos automáticamente en Odoo

| Módulo | Estado | Qué hace / qué falta |
|---|---|---|
| `odoo` (lectura) | ✅ | Códec XML-RPC propio, `SearchRead`, `ReadGroup`, `FieldsGet`. |
| `odoo/escritura` | ✅ | `Create`, `Write`, `CallMethod` y `BuscarUno`. El pedido se crea en borrador con `client_order_ref` como clave de idempotencia, y el cliente se resuelve por correo o se crea. Falta elegir bodega según la cuenta. |
| `contabilidad` | ❌ | **Decisión pendiente, no solo código.** Impuestos, cliente genérico por canal, diario de ventas, y si el pedido se confirma o queda en borrador. Hay que acordarlo con quien lleve la contabilidad de MDV antes de escribir la primera línea. |

## Cimientos (transversales)

| Módulo | Estado | Qué hace / qué falta |
|---|---|---|
| `config`, `crypto`, `migrate` | ✅ | Entorno, AES-256-GCM, migraciones embebidas. |
| `jobs` | ✅ | Cola con reintentos, backoff, idempotencia y recuperación de huérfanos. |
| `api` | ✅ | HTTP con la biblioteca estándar. Crece bien. |
| `auth` | ✅ | Módulo `auth` con bcrypt, tokens HMAC-SHA256, roles (`admin`, `operator`, `viewer`), endpoints de sesión, CLI `integra crear-usuario`, `integra usuarios`, `integra desactivar-usuario`, `integra activar-usuario` y `integra borrar-usuario`. El middleware contrasta cada petición contra `users` (caché de 30 s), así que borrar o desactivar a alguien le corta la sesión abierta en vez de esperar a que caduque el token. |
| `auditoría` | ✅ | Registro en `audit_logs` de operaciones clave (logins, modificaciones de usuarios, configuración) y endpoint `GET /api/auditoria`. |
| `alertas` | ✅ | Vigilancia en cada tick del planificador: pedidos fallidos, SKU sin mapear, conexión caída, publicaciones rechazadas, trabajos agotados y publicados sin stock. Deduplica mientras el aviso siga abierto. |
| `notificaciones` | ❌ | **Falta.** El canal de salida de las alertas (correo, WhatsApp, Telegram). Sin esto, las alertas son un panel más que nadie mira. |
| `observabilidad` | ✅ | Registro y consulta de `channel_api_calls` (cupos, estado HTTP, duración y diagnóstico) y endpoint `GET /api/canales/llamadas`. |
| `planificador` | ✅ | Corre dentro del worker: dispara sync, planificación de envíos e ingesta de pedidos por horario, en la zona del horario (no la del servidor). Avanza la marca antes de ejecutar para no repetir en bucle, y ejecuta al arrancar lo que se perdió mientras estaba caído. |
| `conciliación` | ❌ | **Falta.** Comparar periódicamente lo que el canal dice contra lo que Integra cree: precios movidos a mano en el canal, publicaciones pausadas por el marketplace, stock desincronizado. |

---

## Orden recomendado

El criterio es desbloquear el objetivo completo antes que perfeccionar una parte:

1. **`odoo/escritura` + `ordenes`** — cierra el ciclo. Hoy la plataforma puede
   publicar pero no cobrar el resultado. Es lo que convierte a Integra de
   catálogo bonito en herramienta que gana dinero.
2. **`atributos`** — sin él, MercadoLibre y Falabella no publican. Es el
   verdadero bloqueante del Pilar 1, no los adaptadores.
3. **Adaptadores restantes** — Woo, ML, Falabella, en ese orden.
4. **`auth` + `alertas` + `notificaciones`** — lo que hace falta para que
   opere gente que no sea quien lo programó.
5. **`observabilidad` + `conciliación`** — lo que hace falta para que siga
   funcionando el mes tres.
6. **`ofertas`, `precios/canal` completo, `devoluciones`** — refinamiento.

## Lo que NO debería existir

Para que el mapa sea honesto, también hay que decir qué no hace falta:

- **Módulo de facturación electrónica**: lo hace Odoo con la localización
  colombiana. Integra crea el pedido y se aparta.
- **Módulo de envíos/transportadoras**: MercadoLibre y Falabella gestionan su
  propia logística; Woo y Shopify se integran con transportadoras por su
  cuenta. Integra solo pasa el número de guía.
- **CRM de compradores**: los canales no entregan datos de contacto completos
  y lo poco que dan tiene restricciones de uso. No se acumula.
- **Panel de contabilidad o reportes financieros**: vive en Odoo, que ya los
  tiene y es la fuente de verdad contable.
