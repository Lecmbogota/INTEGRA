# Despliegue de Integra en producción

Todo va en Docker. Quien despliegue no necesita Go ni Node instalados: la
imagen compila el backend y la interfaz por dentro.

## Lo que hace falta antes de empezar

- Una máquina Linux con Docker y el complemento Compose. Dos núcleos y 4 GB de
  memoria sobran; el disco depende del banco de imágenes, cuenta con 20 GB.
- Un dominio apuntando por DNS a la IP de esa máquina, y los puertos **80 y
  443 abiertos**. Caddy pide el certificado por el 80: sin él no hay HTTPS.
- Las credenciales de los cuatro canales y de Odoo, que se cargan después
  desde la propia interfaz.

## 1. Traer el código y generar la clave

```bash
git clone <repositorio> integra && cd integra
```

La clave maestra cifra en la base las credenciales de Odoo y de los canales.

```bash
docker run --rm golang:1.25-alpine sh -c "cd /tmp && echo ok" >/dev/null && \
docker compose run --rm --no-deps --entrypoint integra api genkey
```

> **Guárdala en un gestor de secretos antes de seguir.** Si se pierde, las
> credenciales cifradas son irrecuperables y hay que volver a introducirlas
> todas a mano.

## 2. Escribir el `.env`

En la raíz del proyecto, junto a los compose:

```
INTEGRA_MASTER_KEY=<lo que devolvió genkey>
POSTGRES_PASSWORD=<una contraseña larga y aleatoria>
INTEGRA_DOMINIO=integra.tudominio.com
BACKUP_RETENCION_DIAS=30
```

`.env` está en `.gitignore` y no debe salir de la máquina.

### Ajustes opcionales

Todo lo demás tiene un valor por defecto pensado para MDV. Estos dos gobiernan
el contraste periódico contra los canales, que es lo que descubre una
publicación borrada o retirada por el marketplace:

```
INTEGRA_CONCILIACION_CADA=24h
INTEGRA_RECREAR_PUBLICACIONES_CAIDAS=true
```

- `INTEGRA_CONCILIACION_CADA` es cada cuánto se vuelve a preguntar al canal por
  la misma publicación. WooCommerce y Shopify cuestan una petición por ficha,
  así que bajarlo mucho se come el cupo de API que necesitan los envíos de
  stock. Mínimo aceptado: `1m`.
- `INTEGRA_RECREAR_PUBLICACIONES_CAIDAS` decide qué hacer con una ficha que ya
  no existe en el canal (borrada a mano en la tienda, por ejemplo): con `true`
  Integra la vuelve a crear sola; con `false` solo la marca y levanta el aviso
  «publicaciones que ya no existen en su canal» para que lo decida una persona.
  Lo que el propio canal retiró de la venta —una revisión, una infracción— no
  se reabre nunca solo, con cualquiera de los dos valores.

## 3. Levantar

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

La primera vez tarda unos minutos: compila la interfaz y el binario. Al
terminar hay cinco contenedores: `integra-caddy`, `integra-api`,
`integra-worker`, `integra-postgres` e `integra-backup`.

## 4. Aplicar el esquema y crear el primer usuario

```bash
docker compose exec api integra migrate up
```

```bash
docker compose exec api integra crear-usuario tu@correo.com "Tu Nombre" "una contraseña larga" admin
```

## 5. Comprobar

```bash
curl -sS https://integra.tudominio.com/healthz
```

Debe responder `{"estado":"ok"}` con certificado válido. Después, abre el
dominio en el navegador: sale la pantalla de acceso.

## 6. Conectar Odoo y los canales

Desde la interfaz, en **Integraciones** se conecta Odoo y en **Cuentas** cada
canal. Después:

```bash
docker compose exec api integra sync
```

Con el sync hecho ya existen las bodegas de Odoo: vuelve a **Cuentas** y
asígnale a cada canal las bodegas de las que despacha (**Asignar bodegas**).
Una cuenta sin bodegas asignadas no publica nada, a propósito: es lo que evita
ofrecer en un canal stock que está en Muestras, en Garantías o consignado en
las bodegas de Falabella.

## Qué hace cada pieza

| Contenedor | Para qué |
|---|---|
| `caddy` | HTTPS con certificado automático. Es lo único expuesto a internet |
| `api` | Sirve la interfaz y la API desde el mismo origen |
| `worker` | Publica en los canales, ingiere pedidos y corre el planificador. **Si está caído, nada ocurre solo** |
| `postgres` | La base. No publica puerto al exterior |
| `backup` | Volcado diario comprimido en `datos/backups` |

## Copias de seguridad

El contenedor `backup` vuelca PostgreSQL una vez al día a `datos/backups`,
comprueba que el archivo se puede leer entero antes de darlo por bueno, y
borra los que pasan de la retención configurada.

**Dos cosas que no cubre y hay que resolver:**

1. **Las copias viven en la misma máquina.** Un disco que muere se lleva la
   base y sus copias. Hay que sacarlas fuera: `datos/backups` a un bucket, a
   otro servidor o a lo que use el cliente.
2. **El banco de imágenes no entra en el volcado.** Está en el volumen
   `integra_imagenes_data` y son cientos de megas de fotos buscadas,
   recortadas y verificadas una a una: rehacerlas cuesta horas de proceso y
   las de Odoo no sirven, son miniaturas que los marketplaces rechazan.

```bash
docker run --rm -v integra_imagenes_data:/datos -v "$PWD/datos/backups":/salida \
  alpine tar czf /salida/imagenes-$(date +%F).tar.gz -C /datos .
```

### Restaurar

```bash
gunzip -c datos/backups/integra-AAAAMMDD-HHMMSS.sql.gz | \
  docker compose exec -T postgres psql -U integra -d integra
```

> Prueba una restauración antes de dar el despliegue por bueno. Una copia que
> nadie ha restaurado nunca no es una copia, es una suposición.

## Actualizar

```bash
git pull && docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
docker compose exec api integra migrate up
```

El worker espera medio minuto a que terminen los envíos que ya están hablando
con un canal antes de apagarse, así que una publicación no se corta a la
mitad.

## Ver qué pasa

```bash
docker compose logs -f worker
docker compose logs -f api
```

Los registros rotan a 10 MB con cinco archivos por contenedor, así que no
llenan el disco.

## Antes de entregar al cliente

- [ ] Restaurar una copia en una máquina aparte y comprobar que el catálogo
      está entero.
- [ ] Sacar las copias fuera de la máquina.
- [ ] Copiar el banco de imágenes.
- [ ] Guardar la clave maestra donde el cliente pueda recuperarla sin ti.
- [ ] Configurar el destino de alertas por correo, o nadie se entera de que un
      token caducó ni de que hay pedidos sin montar en Odoo.
