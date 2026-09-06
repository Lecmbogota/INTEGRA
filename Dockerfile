# Compilación
FROM golang:1.25-alpine AS build

WORKDIR /src

# Las dependencias se copian primero para que su descarga quede cacheada y no
# se repita cada vez que cambia una línea de código.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO desactivado: binario estático, sin dependencias del sistema.
# Las migraciones viajan embebidas, así que la imagen final no necesita nada más.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-s -w" \
        -o /out/integra \
        ./cmd/integra

RUN CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-s -w" \
        -o /out/odoo-explorer \
        ./cmd/odoo-explorer

# Ejecución
FROM alpine:3.20

# ca-certificates para hablar HTTPS con Odoo y las APIs de los canales.
# tzdata porque los horarios de sincronización son en hora local de Bogotá y
# sin la base de zonas time.LoadLocation falla.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 integra

COPY --from=build /out/integra        /usr/local/bin/integra
COPY --from=build /out/odoo-explorer  /usr/local/bin/odoo-explorer

USER integra
WORKDIR /home/integra

ENTRYPOINT ["integra"]
CMD ["serve"]
