# Plan de ejecución — completar, auditar y probar Integra

Fecha: 2026-09-05. Objetivo: llevar la plataforma de "código escrito" a
"ciclo completo verificado": publicar en un canal real, recibir el pedido y
verlo en Odoo, con pruebas que lo demuestren y sin agujeros de seguridad.

## Medición de partida

| Qué | Valor |
|---|---|
| Código Go (sin pruebas) | 23.300 líneas en 31 paquetes |
| Frontend TypeScript | 4.300 líneas en 23 archivos, `strict` activado |
| Rutas HTTP | 67 |
| Comandos CLI | 20 |
| Paquetes **sin ninguna prueba** | 9: `ordenes`, `planificador`, `sync`, `odoo` (cliente), `conectores/shopify`, `conectores/woocommerce`, `conectores/falabella`, `webimagenes`, `config` |
| Tablas del esquema sin código detrás | 9: `attribute_mappings`, `attribute_value_mappings`, `channel_category_cache`, `notification_destinations`, `odoo_pricelist_items`, `odoo_pricelists`, `product_images`, `sync_run_items`, `sync_runs` |
| Adaptadores probados contra un canal real | 0 de 4 |
| Control de versiones | ninguno (no es repositorio git) |

Lo que ya funciona verificado: el ciclo Odoo local → sync → `sale.order`
en borrador, la cola de trabajos, el planificador, la autenticación, y el
adaptador de Mercado Libre contra un servidor simulado.

## Cómo se ejecuta

Cinco olas. Cada ola es uno o varios *workflows* de agentes en paralelo con
una fase de verificación cruzada: ningún hallazgo ni cambio se acepta sin
que un segundo agente lo confirme. Entre olas hay una puerta: yo consolido
los resultados, tú decides qué entra en la siguiente.

Puertas automáticas que corren en toda ola con cambios de código:

```
go build ./... && go vet ./... && go test ./...
cd web && npx tsc -b && npm run build
```

Con base de datos: `go test ./...` con `INTEGRA_DATABASE_URL` (Postgres del
compose) y con el Odoo local (`docker-compose.odoo.yml`) arriba.

Los agentes que **escriben código** trabajan cada uno en su *worktree* de
git y entregan un parche; eso exige convertir la carpeta en repositorio
(ola 0). Los agentes que **solo leen** (auditoría) corren sobre la copia
principal.

---

## Avance

### Ola 0 — hecha (2026-09-05)

Repositorio git creado con commit de línea base, `datos/` (409 MB) excluido,
finales de línea fijados. Puertas verificadas en limpio: `go build`, `go vet`,
`go test ./...` (20 paquetes), `tsc -b` y `vite build` del frontend.

### Ola 1 — auditoría, en curso

Los 10 auditores salieron; 7 devolvieron 79 hallazgos (2 críticos, 11 altos,
34 medios, 32 bajos) antes de que el límite de sesión matara la verificación.
Se recuperaron del diario y se reanudó con el esfuerzo graduado por
severidad: dos lentes más desempate para lo crítico y alto, una lente para lo
medio, revisión manual para lo bajo. Faltaban tres dimensiones por correr
(núcleo, WooCommerce, Falabella).

### Arreglos ya aplicados y verificados a mano

Cuatro hallazgos se confirmaron con evidencia propia (no solo del auditor) y
se corrigieron sin esperar a la verificación:

| Commit | Qué estaba roto | Prueba de que lo estaba |
|---|---|---|
| `2bd652b` | Los pedidos ingeridos nunca se montaban solos en Odoo: el manejador estaba registrado y nadie encolaba el trabajo. Solo llegaban con `integra ordenes` a mano | `grep` de `TrabajoAOdoo`: dos apariciones, la constante y el registro |
| `77e5dae` | `products.categ_path` no lo escribía nadie: sin categoría, MercadoLibre y Falabella rechazan toda publicación | 545 productos, 0 con categoría; los 55 mapeos casaban con 0. Tras el arreglo: 545 con categoría, 55 mapeos casando, 241 productos publicables |
| `6fee68d` | Contraseñas en claro en `audit_logs`, servidas a cualquier rol de solo lectura | El cuerpo de la petición entero iba a `RegistrarAuditoria`; el endpoint exigía solo `viewer` |
| `2ece854` | El banco de imágenes vivía en disco efímero; faltaba `INTEGRA_PUBLIC_BASE_URL`, `restart` y rotación de logs; no había `.dockerignore` | `docker compose config` sin volumen ni esas variables |

Todo con `go test ./...` en verde y prueba nueva en `internal/ordenes` que
vigila el cableado que faltaba.

---

## Ola 0 — Preparación (yo, sin agentes)

1. `git init` + commit de línea base. Sin esto no hay trabajo en paralelo
   seguro ni forma de revertir lo que un agente rompa.
2. Verificar que las puertas pasan en limpio antes de tocar nada.
3. Arrancar Postgres y el Odoo local para las pruebas de integración.
4. Recoger de ti las decisiones de la sección final.

## Ola 1 — Auditoría (solo lectura, ~10 agentes en paralelo)

Cada agente revisa una dimensión y devuelve hallazgos con archivo, línea,
severidad y escenario de fallo. Una segunda tanda de agentes verifica cada
hallazgo de forma adversarial: intenta reproducirlo o demostrar que es
falso. Solo los confirmados pasan.

| Dimensión | Qué busca |
|---|---|
| Seguridad | rutas públicas, `/api/webhooks/*` sin handler ni firma, manejo de secretos y cifrado, inyección SQL, control de roles, sesiones, cabeceras, límites de tamaño de subida |
| Corrección del núcleo | `jobs` (carreras, leases, idempotencia), `planificador` (zonas horarias, marcas de agua), `publicar` (hashes, diff), `ordenes` (duplicados, reintentos), `sync` (borrados en Odoo, variantes) |
| Integridad de datos | esquema vs código: las 9 tablas huérfanas (¿sobran o faltan por implementar?), migraciones `Down`, índices, restricciones que el código asume y la base no impone |
| Adaptador Shopify | contra la documentación oficial actual, como se hizo con Mercado Libre |
| Adaptador WooCommerce | ídem |
| Adaptador Falabella | ídem (Seller Center, feeds asíncronos, firma) |
| Odoo | cliente XML-RPC, `sale.order`, bodega por cuenta, moneda de la tarifa, permisos del usuario API |
| Frontend | `tsc` estricto, rutas que la UI llama y la API no expone (y al revés), estados de error, flujos rotos en cada pantalla |
| Operación | configuración, logs, Docker, ausencia de backups, `INTEGRA_PUBLIC_BASE_URL`, tiempos de espera, cupos por canal |
| Documentación | README/PLAN/MODULOS frente a lo que el código hace de verdad |

Entregable: `AUDITORIA.md` con los hallazgos confirmados, ordenados por
severidad, y una lista de tareas derivada. Puerta: tú eliges cuáles entran.

## Ola 2 — Pruebas de cimientos (~9 agentes, uno por paquete)

Objetivo: que ningún paquete quede sin pruebas y que las pruebas de
integración cubran el ciclo real.

- Pruebas unitarias con servidor HTTP simulado para Shopify, WooCommerce y
  Falabella, al estilo de las de Mercado Libre.
- Pruebas de `ordenes` (ingesta paginada, marca de agua, pedido repetido,
  línea sin SKU), `planificador` (zonas horarias, ticks perdidos), `sync`
  (producto borrado en Odoo, cambio de SKU), cliente `odoo`, `config`.
- Pruebas de integración contra el Odoo local: sync completo y creación de
  `sale.order` con cliente, líneas y bodega.
- Corrección de todo hallazgo de la ola 1 marcado como crítico o alto.

Puerta: `go test ./...` verde con y sin base de datos, informe de cobertura
por paquete.

## Ola 3 — Completar el producto (~12 agentes, por funcionalidad)

Lo que falta para el objetivo, en orden de dependencia. Cada funcionalidad
la implementa un agente, la prueba otro y la revisa un tercero.

Pilar 2 — pedidos:
- **Webhooks** `POST /api/webhooks/{canal}`: Mercado Libre (validar
  `application_id`, encolar, responder en <500 ms), Shopify (HMAC),
  WooCommerce (firma), Falabella (si la ofrece; si no, sondeo). Respaldo con
  `missed_feeds` en Mercado Libre.
- **Despacho** `AckOrder` en los cuatro adaptadores: número de guía y
  transportadora de vuelta al canal.
- **Cancelaciones y devoluciones**: cambio de estado del pedido y reversión
  del stock publicado.
- **Descuento inmediato de stock** al ingerir un pedido, sin esperar al sync.

Pilar 3 — Odoo:
- **Bodega por cuenta** en el `sale.order` (`channel_account_warehouses`).
- **Reglas contables** según lo que decidas: impuestos, cliente genérico por
  canal, diario, confirmar o dejar en borrador.

Operación:
- **OAuth de Mercado Libre desde la interfaz** (redirección, callback,
  guardado del token), en vez de pegar el refresh token a mano.
- **Notificaciones**: canal de salida de las alertas (correo o Telegram; la
  tabla `notification_destinations` ya existe).
- **Conciliación** diaria canal ↔ Integra: precio movido a mano en el canal,
  publicación pausada por el marketplace, stock desviado.

Puerta: cada funcionalidad con pruebas, puertas automáticas verdes, y una
revisión de código por agente sobre el parche completo.

## Ola 4 — Pruebas de extremo a extremo (~6 agentes)

Contra sistemas reales, con las credenciales que tengas:

- **Mercado Libre**: usuarios de test (vendedor y comprador), publicar un
  producto real de Integra, comprarlo entre usuarios de test, ver llegar el
  pedido por webhook y por sondeo, verlo en el Odoo local.
- **Shopify**: tienda de desarrollo; mismo ciclo.
- **WooCommerce**: instancia local en Docker; mismo ciclo.
- **Falabella**: lo que permita el Seller Center de pruebas.
- **Interfaz**: recorrido por navegador de cada pantalla: login, catálogo,
  edición, publicación, cuentas, pedidos, automatización, con captura y
  comprobación de errores de consola.
- **Carga**: publicar los 452 productos × 4 canales contra servidores
  simulados para medir la cola, los cupos y el tiempo total.

Puerta: un pedido real de cada canal disponible aparece en Odoo con sus
líneas correctas y sin duplicados tras reintentos. Es el criterio de
"hecho" de la fase 5 del PLAN.

## Ola 5 — Despliegue y operación (~4 agentes)

- Compose de producción: API + worker + Postgres + Caddy con HTTPS, backups
  automáticos de Postgres, logs persistentes, reinicio automático.
- Guía de despliegue probada por un agente en un entorno limpio.
- Revisión de seguridad final sobre la configuración de producción.
- Actualización de README, PLAN y MODULOS al estado real.

Puerta: dos usuarios operan por HTTPS en una instalación hecha siguiendo la
guía (criterio de la fase 6 del PLAN).

---

## Decisiones tomadas (Luis, 2026-09-05)

1. **Git.** Sí: la carpeta pasa a ser repositorio con un commit de línea
   base antes de que escriba ningún agente.
2. **Credenciales de canal.** Ninguna disponible todavía. La ola 4 se hace
   contra servidores simulados y un WooCommerce local en Docker; las pruebas
   contra Mercado Libre, Shopify y Falabella quedan preparadas (guion y
   datos de prueba) para ejecutarse en cuanto haya credenciales.
3. **Contabilidad.** El pedido queda en **borrador**, con el **comprador
   real** como cliente y **sin tocar impuestos**: Odoo aplica los suyos y una
   persona confirma. Es el comportamiento actual; se mantiene y se prueba.
4. **Notificaciones.** Por **correo (SMTP)**.
5. **Ritmo.** Ola por ola, con revisión de Luis entre medias.
