# Integra

Middleware de sincronización de catálogo entre Odoo y los canales de venta
(MercadoLibre, Falabella Seller Center, WooCommerce y Shopify), con las órdenes
de vuelta hacia Odoo.

De Odoo solo se sincronizan **SKU, nombre y stock por almacén**. El resto del
producto (precio, marca, descripción, imágenes, categorías, exclusión) se
administra en Integra y las sincronizaciones nunca lo sobrescriben.

**Estado: Fase 0 completa.** Modelo de datos, cifrado, contrato de canales y
despliegue local funcionando. El conector de catálogo es la Fase 1.

## Puesta en marcha

Requisitos: Go 1.25+, Docker Desktop.

```bash
docker compose up -d postgres
```

Genera la clave maestra de cifrado y guárdala en `.env`:

```bash
go run ./cmd/integra genkey
```

`.env` necesita al menos:

```
INTEGRA_MASTER_KEY=<lo que devolvió genkey>
INTEGRA_DATABASE_URL=postgres://integra:integra_dev@localhost:5544/integra?sslmode=disable
```

Aplica el esquema:

```bash
go run ./cmd/integra migrate up
```

Guarda la conexión a Odoo (la clave se cifra antes de tocar disco) y trae el catálogo:

```bash
go run ./cmd/integra conectar-odoo
```

```bash
go run ./cmd/integra sync
```

Arranca la API y, en otra terminal, el frontend:

```bash
go run ./cmd/integra serve
```

```bash
cd web && npm install && npm run dev
```

**La interfaz queda en http://127.0.0.1:5580**

> **Puertos poco habituales, a propósito.** Windows reserva para Hyper-V los
> rangos donde caen 5432 y 5433 (PostgreSQL) y 5173 (el puerto por defecto de
> Vite). Por eso se usan **5544** para la base de datos y **5580** para el
> frontend. Compruébalo con
> `netsh interface ipv4 show excludedportrange protocol=tcp`.

## Comandos

| Comando | Qué hace |
|---|---|
| `integra genkey` | Genera una clave maestra AES-256 |
| `integra migrate up` | Aplica las migraciones pendientes |
| `integra migrate down` | Revierte la última migración |
| `integra migrate status` | Muestra qué está aplicado |
| `integra ia-probar` | Redacta una ficha de prueba y estima el lote completo |
| `integra serve` | Servidor HTTP (Fase 5) |
| `integra worker` | Procesador de trabajos (Fase 2) |
| `odoo-explorer` | Diagnostica una instancia de Odoo |

`worker` procesa la cola de trabajos (tabla `jobs`): reintentos con backoff
exponencial, idempotencia por clave y recuperación de trabajos huérfanos. Los
manejadores por tipo se registran según van existiendo los conectores; el plan
de fases está en `PLAN.md`.

## Redacción con IA

Integra redacta las fichas (títulos por canal, descripción y specs) y comprueba
que cada foto muestre su producto. Por defecto lo hace con un **modelo local
servido por Ollama**: el catálogo se redacta una vez y se retoca a mano
después, así que pagar por token cada vez que se reescriben cuatrocientas
fichas no compensa.

```bash
# 1. Instalar Ollama (https://ollama.com/download) y arrancarlo
ollama serve

# 2. Traer los modelos: uno de texto y uno multimodal para las imágenes
ollama pull qwen2.5:7b
ollama pull qwen2.5vl:3b

# 3. Comprobar la instalación antes de lanzar el lote
integra ia-probar
```

`ia-probar` redacta una ficha de ejemplo, enseña los títulos con su longitud y
**estima cuánto tardaría el catálogo entero** — que es lo que decide si el lote
cabe en una tarde o en una noche.

| Variable | Por defecto | Para qué |
|---|---|---|
| `IA_PROVEEDOR` | `ollama` | `ollama`, `anthropic` o `auto` (local si responde, si no la API) |
| `IA_MODELO` | `qwen2.5:7b` | Modelo de texto |
| `IA_MODELO_VISION` | `qwen2.5vl:3b` | Modelo multimodal para verificar fotos |
| `OLLAMA_HOST` | `http://localhost:11434` | Si Ollama corre en otra máquina |
| `ANTHROPIC_API_KEY` | — | Solo con `IA_PROVEEDOR=anthropic` |

### Por qué el 7B y no algo más pequeño

Se midieron tres modelos con los mismos cuatro productos del catálogo real
(un scooter, una impresora térmica, una alfombrilla y un anillo inteligente):

| Modelo | Fichas válidas | Por ficha | 450 fichas | Qué hizo mal |
|---|---|---|---|---|
| `qwen2.5:3b` | 1 de 4 | 4 s | 32 min | Copia el nombre del ERP en los cuatro canales e inventa specs |
| `gemma3:4b` | 4 de 4 | 44 s | 5,5 h | Leyó «SEG GEN US» como **«usado»** y tradujo «Rainbow 6 Siege» por «diseño arcoíris» |
| `qwen2.5:7b` | 4 de 4 | 54 s | 6,8 h | El único que respeta nombres propios y no inventa cifras |

El 3B es cuatro veces más rápido y no sirve: publicar un producto nuevo como
usado, o una alfombrilla como «disco de ratón», cuesta más que una noche de
cómputo. En una GPU de 4 GB el 7B se parte 58/42 entre CPU y GPU; el lote entero
es una noche y se corre una vez.

Con más VRAM el 7B va entero en GPU y baja a pocos segundos por ficha.

Se pide el JSON con un **esquema** (`format` de Ollama), no rogándole al modelo
que responda solo JSON. Es lo que hace usable a un modelo pequeño: el servidor
restringe la generación en vez de confiar en que obedezca. Aun así, las fichas
sin descripción o sin títulos se rechazan antes de guardarse — una ficha vacía
cerraría el aviso «sin descripción» y nadie volvería a mirar ese producto.

La API de Claude sigue disponible con `IA_PROVEEDOR=anthropic`, para un lote
donde la redacción importe de verdad. Los prompts son los mismos en los dos
proveedores, así que comparar la calidad de uno contra otro significa algo.

## Tienda WooCommerce de pruebas

De los cuatro canales, WooCommerce es el único que se puede levantar entero en
local. Es la única forma de ejercitar el ciclo completo —publicar, vender,
ingerir el pedido y montarlo en Odoo— sin depender de que un marketplace nos
dé credenciales.

```bash
docker compose -f docker-compose.woocommerce.yml up -d
```

```bash
docker compose -f docker-compose.woocommerce.yml run --rm woo-init
```

El segundo comando instala WordPress y WooCommerce, deja la tienda en pesos
colombianos, crea las claves de la API y las imprime. **La tienda queda en
http://localhost:8090** y su administración en `/wp-admin` con `admin`/`admin`.
Volver a ejecutarlo es inofensivo: cada paso comprueba antes si ya está hecho.

Las claves se generan al azar en cada instalación, así que se copian de la
salida a la pantalla de *Cuentas* de Integra.

> **Dos cosas fijadas a propósito.** La versión de WooCommerce (10.9.4) porque
> la última exige un WordPress que todavía no existe como estable, y porque una
> tienda que instala otra versión en cada arranque convierte una prueba que
> pasa hoy en una que falla mañana por un cambio del plugin y no del código.

## Odoo de pruebas

Un Odoo 18 local contra el que probar el ciclo completo sin tocar el del
cliente. Va en su propio compose porque no es parte de Integra: es la
contraparte.

```bash
docker compose -f docker-compose.odoo.yml up -d odoo-db
```

Crear la base la primera vez (tarda un par de minutos; los datos de
demostración traen 43 productos, 2 almacenes y 43 clientes):

```bash
docker compose -f docker-compose.odoo.yml run --rm odoo odoo -d integra_pruebas -i sale_management,stock --stop-after-init
```

```bash
docker compose -f docker-compose.odoo.yml up -d
```

**La interfaz queda en http://localhost:8069** (usuario `admin`, clave
`admin`). Apunta `.env` a esa instancia y conecta:

```
ODOO_URL=http://localhost:8069
ODOO_DB=integra_pruebas
ODOO_USER=admin
ODOO_API_KEY=admin
```

```bash
go run ./cmd/integra conectar-odoo
```

> **Por qué sirve esta instancia y la de Odoo Online no.** La API externa
> (XML-RPC) está habilitada por defecto en la edición comunitaria
> autoalojada, mientras que en Odoo Online solo la traen los planes Custom.
> Aquí se puede probar la lectura del catálogo y la creación de pedidos de
> venta sin depender del plan del cliente.

## Replicar un catálogo real al Odoo de pruebas

`odoo-replicar` copia categorías, productos, almacenes y existencias de una
instancia a otra, para probar con datos que se parezcan a los de producción.

```bash
ORIGEN_URL=https://mi-empresa.odoo.com ORIGEN_DB=nombre-real ORIGEN_USER=usuario ORIGEN_API_KEY=clave go run ./cmd/odoo-replicar
```

Por defecto escribe en el Odoo local (`integra_pruebas`); se cambia con
`DESTINO_URL`, `DESTINO_DB`, `DESTINO_USER` y `DESTINO_API_KEY`.

Copia, en este orden: compañía y moneda, impuestos de venta, tarifas,
categorías, almacenes, productos, existencias y clientes.

> **La moneda del pedido sale de la TARIFA, no de la compañía.** Una base nueva
> de Odoo nace como «My Company» en dólares con una tarifa en dólares; poner la
> compañía en COP y dejar la tarifa en USD produce pedidos que se ven bien y
> están valorados en la moneda equivocada. Por eso la réplica corrige ambas.

**No es una restauración de copia de seguridad.** Copia datos, no la
instalación: módulos, contabilidad, usuarios y permisos no viajan. Y como el
origen suele ser Enterprise y el destino Community, los campos de módulos
Enterprise —`l10n_co_edi_brand`, donde MDV guarda la marca— no existen en el
destino y se omiten. No es una pérdida: Integra ya no lee la marca de Odoo.

Es idempotente por referencia interna: volver a ejecutarlo actualiza en vez de
duplicar. Los productos sin `default_code` se omiten, porque sin referencia no
hay forma de emparejarlos en la siguiente pasada.

> Para replicar hacen falta varias bases a la vez, así que `datos/odoo-config/odoo.conf`
> va sin `db_name` ni `dbfilter`. Ese fichero lo lee Odoo como UTF-8 y falla con
> cualquier otra codificación: se mantiene sin acentos a propósito.

## Cambiar de instancia de Odoo sin perder el trabajo

Los productos se identifican por **(conexión, id de plantilla en Odoo)**, así
que apuntar Integra a otra instancia —una réplica, una base restaurada, un
Odoo nuevo— crea filas nuevas y deja huérfano todo lo que es propiedad de
Integra: precios, marcas, descripciones, imágenes y atributos. El SKU sí es
estable entre instancias, y es la clave del traslado.

```bash
go run ./cmd/integra conexiones
```

```bash
go run ./cmd/integra conexiones migrar 1 4
```

Eso simula. Para aplicarlo, `--aplicar`. Nunca sobrescribe lo que el destino
ya tenga, así que repetirlo es inofensivo. Después, la conexión vieja se puede
borrar (destructivo: se lleva sus productos por delante):

```bash
go run ./cmd/integra conexiones borrar 1
```

## Promociones

Una promoción es un precio rebajado **por canal** con fecha de inicio y de fin.
Se define en la ficha del producto, pestaña *Promoción*: canal, precio,
`empieza` y `termina` (vacío = sin caducidad).

El estado se calcula contra el reloj, no se guarda:

| Estado | Qué significa |
|---|---|
| Programada | Aún no llega su hora de inicio; el canal sigue con el precio normal |
| Vigente | Está rebajado ahora mismo |
| Terminada | Pasó su fecha de fin y ya se devolvió el precio normal |
| Cancelada | Se detuvo a mano antes de tiempo |

Quien la aplica es el planificador, no la pantalla: en cada pasada busca las
promociones que cruzaron una frontera de vigencia, recalcula los precios
efectivos de esa cuenta y encola el envío al canal
(`Planificador.moverPromociones`). Cancelar una promoción ya aplicada también
encola la reversión — desactivarla en la base sin más dejaría el precio
rebajado publicado para siempre.

## Suelo de coste

Ningún precio sale a un canal si no cubre el coste de Odoo con el margen mínimo
de esa cuenta. El suelo se configura en *Cuentas de canal*, bajo cada cuenta:

| Campo | Qué hace |
|---|---|
| Margen mínimo | Porcentaje sobre el coste que hay que ganar. `0` = que al menos lo cubra |
| No publicar | Marcado, el envío se retiene; sin marcar, solo se avisa |

El margen se mide sobre lo que **queda tras la comisión del canal**, no sobre
el precio de escaparate: publicar a $100.000 donde el canal cobra el 20 % deja
$80.000, y comparar los $100.000 contra el coste daría por bueno vender a
pérdida.

Cuando el precio no llega al suelo pasan tres cosas: el producto entra en la
cola de atención con el motivo *Precio por debajo del coste* y el detalle de
las cifras, la vigilancia levanta la alerta `precio_bajo_costo` (sale por
correo como cualquier otra), y —si la cuenta lo pide— el envío se retiene. En
una publicación que ya existe se retiene solo el precio, así que el canal se
queda con el precio bueno anterior en lugar de que lo pise el malo; el stock se
sigue sincronizando, porque dejar de hacerlo por un problema de precio haría
vender lo que no hay. Un alta nueva se retiene entera: el alta lleva el precio
dentro.

El precio **calculado** nunca baja del suelo, se ajusta solo. Los precios
manuales y las promociones no se tocan —son decisiones de una persona— pero sí
avisan y sí se retienen.

Un coste desconocido (cero en Odoo, lo normal mientras el producto no se haya
comprado nunca) no cuenta como pérdida y no frena nada.

## Actualización masiva por plantilla

Para cambiar cientos de precios o programar muchas promociones a la vez, en
*Productos* → **Actualizar por plantilla**:

1. **Descargar** — genera un `.xlsx` con el catálogo (respeta los filtros que
   estén puestos en pantalla), con el precio actual y la promoción vigente ya
   rellenados. Las columnas editables llevan encabezado verde; el resto son de
   referencia. Una segunda hoja, *Instrucciones*, explica el formato.
2. **Subir** — Integra valida el archivo entero y devuelve un simulacro: qué va
   a cambiar y qué filas tienen error, numeradas como en Excel. **No escribe
   nada.**
3. **Confirmar** — aplica. Si queda un solo error, no se aplica nada: se
   corrige el archivo y se vuelve a subir.

Se acepta lo que de verdad sale de un Excel colombiano: `129900`, `129.900`,
`$ 129.900`, `1.234,56` y `1,234.56` se leen todos bien, y las fechas admiten
`dd/mm/aaaa hh:mm` además del formato ISO. Una casilla vacía significa «no
tocar», nunca «borrar».

## Explorador de Odoo

Inspecciona una instancia y produce un informe con versión, compañías, campos
personalizados, candidatos a marca, variantes, tarifas, almacenes y un
diagnóstico de calidad del catálogo. **No escribe nada en Odoo.**

```bash
go run ./cmd/odoo-explorer -probe -url https://tu-empresa.odoo.com
```

Para el informe completo hace falta una API key (*Mi perfil → Seguridad de la
cuenta → Nueva clave de API*) y el nombre real de la base de datos, que se
obtiene escribiendo `odoo.info` en la consola del navegador — en las bases
duplicadas para pruebas casi nunca coincide con el subdominio.

Rellena `ODOO_*` en `.env` y ejecuta `go run ./cmd/odoo-explorer`.

## Estructura

```
cmd/integra/           Binario principal: genkey, migrate, serve, worker
cmd/odoo-explorer/     Diagnóstico de instancias Odoo
internal/channel/      Contrato ChannelAdapter + registro
internal/config/       Configuración por entorno
internal/crypto/       AES-256-GCM para credenciales
internal/migrate/      Migrador con anotaciones compatibles con goose
internal/odoo/         Conector: cliente, tipos de registro
internal/odoo/xmlrpc/  Códec XML-RPC propio
migrations/            Esquema versionado, embebido en el binario
```

## Decisiones de diseño

**Un códec XML-RPC propio.** Odoo representa "campo vacío" con el booleano
`false`, sea cual sea el tipo declarado: un `default_code` sin rellenar llega
como `<boolean>0</boolean>`, no como cadena vacía. Las librerías de Go
decodifican hacia structs tipados y fallan ahí. Este códec decodifica hacia
`interface{}` y los accesores de `odoo.Record` absorben la unión
`false | valor`. Se descartó JSON-RPC: el endpoint `/jsonrpc` existe pero no
está documentado en Odoo 18 y no conviene apoyar producción en una ruta no
soportada.

**Tres hashes por publicación, no uno.** `content_hash`, `price_hash` y
`stock_hash` se calculan por separado. Con un hash único, cambiar una unidad de
stock invalidaría todo y obligaría a reenviar título, descripción e imágenes,
quemando cupo de API en el endpoint más caro.

**Stock desglosado por almacén.** `qty_available` de Odoo suma todos los
almacenes internos, incluidos muestras, garantías y el stock consignado en las
bodegas de Falabella. `variant_stock` lo guarda por bodega para que cada cuenta
sume lo que le corresponde. Sin filas en `channel_account_warehouses`, una
cuenta ve todos los almacenes.

**Odoo solo aporta SKU, nombre y stock.** Todo lo demás —precio, marca,
descripción, categoría, exclusión, peso, barcode, imágenes— es propiedad de
Integra: se edita en la interfaz (`PATCH /api/productos/{id}`) y el sync jamás
lo sobrescribe. El motivo es que el dato comercial de Odoo no es publicable:
`list_price` mezcla monedas (USD sobre costes en COP), la marca vive en un
campo libre con duplicados y las descripciones no existen. El precio se sembró
una única vez desde el cálculo de tarifas de Odoo (`computed_price`, que queda
congelado como referencia) y desde entonces es manual.

**Capacidades, no condicionales por canal.** El núcleo consulta
`Capabilities()` en vez de preguntar "¿eres MercadoLibre?". Cuando un canal no
soporta ofertas programadas, el planificador genera dos trabajos —aplicar y
revertir— sin una línea de código específica del canal.

**Migrador propio.** Las migraciones viajan embebidas en el binario, así que
desplegar es copiar un ejecutable. El formato de anotaciones se mantiene
compatible con goose por si conviene cambiar de herramienta.

## Cómo añadir un canal

1. Crea `internal/channel/<nombre>/`.
2. Implementa `channel.Adapter` y devuelve unas `Capabilities` honestas: el
   núcleo se fía de ellas para decidir cómo tratar ofertas, lotes y variantes.
3. Regístralo desde `init()` con `channel.Register`.
4. Añade la fila en la tabla `channels`.

No hay que tocar `internal/sync` ni `internal/api`.

## Tests

```bash
go test ./...
```

Los tests de `internal/migrate` verifican que las migraciones reales del
proyecto se trocean bien y que todas tienen sección `Down`.
