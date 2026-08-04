# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bitrix-worker ./cmd/bitrix-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yookassa-worker ./cmd/yookassa-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/expire-holds ./cmd/expire-holds \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/reconcile-bitrix ./cmd/reconcile-bitrix

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY --chown=app:app migrations ./migrations
RUN mkdir -p /app/storage && chown app:app /app/storage

USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
