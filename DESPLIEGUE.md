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
BACKUP_RETENCION_IMAGENES_DIAS=7
# Solo si se sacan las copias fuera de la máquina (ver más abajo).
BACKUP_REMOTO_DESTINO=
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
curl -sS https://integra.tudominio.com/estado
```

La primera debe responder `{"estado":"ok"}` con certificado válido: dice que la
API está en pie. La segunda dice algo distinto y más importante —si Integra
está *haciendo* su trabajo— y es la que hay que vigilar desde fuera (ver
«Vigilancia»). Después, abre el dominio en el navegador: sale la pantalla de
acceso.

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
| `worker` | Publica en los canales, ingiere pedidos y corre el planificador. Si está caído no ocurre nada solo, pero deja de latir y la API avisa |
| `postgres` | La base. No publica puerto al exterior |
| `backup` | Volcado diario de la base y del banco de imágenes en `datos/backups` |
| `backup-remoto` | Sube esas copias fuera de la máquina. Opcional, perfil `remoto` |

## Copias de seguridad

El contenedor `backup` vuelca una vez al día, comprueba cada archivo antes de
darlo por bueno y borra los que pasan de la retención. Vuelca **dos** cosas,
y las dos hacen falta para volver a operar:

- **PostgreSQL**, que tiene el catálogo comercial —precios propios de Integra,
  marcas, descripciones y atributos—, que no está en Odoo.
- **El banco de imágenes**, que son fotos buscadas, recortadas y verificadas
  una a una. Las de Odoo son miniaturas que los marketplaces rechazan, así que
  perderlas es rehacer semanas de trabajo a mano.

Además deja un latido en la base cada vez que la pasada entera sale bien. Es lo
que hace que una copia que dejó de hacerse llegue por correo en vez de
descubrirse el día que hay que restaurar (ver «Vigilancia»).

### Sacarlas fuera de la máquina

Un disco que muere se lleva la base y sus copias a la vez. El contenedor
`backup-remoto` sube `datos/backups` a donde diga el cliente —S3, Backblaze,
Drive, otro servidor por SFTP: `rclone` habla con todos— una vez al día.

Va en un perfil aparte porque necesita dos cosas que solo tiene el cliente, el
destino y sus credenciales. Se enciende en tres pasos:

```bash
# 1. Crear la configuración de rclone (pide el tipo de destino y sus claves).
docker run --rm -it -v "$PWD/datos":/config/rclone rclone/rclone:1 config
# deja el archivo en datos/rclone.conf y lo llama, por ejemplo, "copias"

# 2. Apuntar el destino en .env
echo 'BACKUP_REMOTO_DESTINO=copias:mdv-integra' >> .env

# 3. Levantar con el perfil
docker compose --profile remoto -f docker-compose.yml -f docker-compose.prod.yml up -d
```

El archivo `datos/rclone.conf` **tiene que existir antes** de levantar el
perfil: si no, Docker crea un directorio con ese nombre y rclone arranca sin
configuración.

Cuando una subida sale bien deja la fecha en `datos/backups/.remoto-ok`, y el
contenedor `backup` la publica como latido `copias_remotas`. Si las subidas
empiezan a fallar, ese latido envejece y sale el aviso: una copia remota que
lleva un mes rota se ve igual que una que va bien hasta que alguien mira.

### Restaurar

```bash
# La base
gunzip -c datos/backups/integra-AAAAMMDD-HHMMSS.sql.gz |   docker compose exec -T postgres psql -U integra -d integra

# El banco de imágenes
docker run --rm -v integra_imagenes_data:/datos -v "$PWD/datos/backups":/copias   alpine tar xzf /copias/imagenes-AAAAMMDD-HHMMSS.tar.gz -C /datos
```

> Prueba una restauración antes de dar el despliegue por bueno. Una copia que
> nadie ha restaurado nunca no es una copia, es una suposición.

## Vigilancia

Todo lo que Integra vigila lo vigila el planificador, y el planificador corre
dentro del worker. Cuando el que se para es el worker no queda nadie ahí dentro
para contarlo: durante días no se sincroniza Odoo, no sale un precio y no entra
un solo pedido de los cuatro canales —se sigue vendiendo con el stock congelado
del último sync— mientras el panel se ve en verde.

Por eso el worker y el contenedor de copias dejan un latido en la base, y quien
los echa en falta es la **API**, que es otro proceso:

- Levanta una alerta crítica y **manda el correo ella misma**, sin depender del
  proceso muerto. Los destinos se configuran en la pantalla **Avisos**.
- `GET /estado` responde `503` mientras algún proceso siga sin latir, y `200`
  cuando todos laten. Es una ruta pública, sin sesión, pensada para un servicio
  de uptime externo:

```bash
curl -sS https://integra.tudominio.com/estado
{"estado":"ok","procesos":[{"componente":"copias",...},{"componente":"worker",...}]}
```

Se tolera perder tres latidos seguidos antes de dar un proceso por muerto: un
reinicio o un despliegue se saltan uno, y avisar por eso enseñaría a ignorar
los avisos. En la práctica el worker se declara caído a los tres minutos y las
copias a los tres días.

**Apunta un servicio de uptime a `/estado`.** El correo sale de la API, así que
si lo que se cae es la máquina entera —o la API— no queda nadie dentro para
avisar. Esa última red tiene que estar fuera.

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
- [ ] Encender el perfil `remoto` con un destino real y ver el primer
      `.remoto-ok` en `datos/backups`.
- [ ] Apuntar un servicio de uptime a `/estado`: es el único aviso que no
      depende de que la máquina siga viva.
- [ ] Guardar la clave maestra donde el cliente pueda recuperarla sin ti.
- [ ] Configurar el destino de alertas por correo, o nadie se entera de que un
      token caducó ni de que hay pedidos sin montar en Odoo.
