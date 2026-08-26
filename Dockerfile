FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
      -trimpath \
      -tags timetzdata \
      -ldflags='-s -w' \
      -o /omega ./cmd/omega

RUN adduser -D -u 10001 omega \
 && mkdir -p /data/database /data/storage/app \
 && chown -R 10001:10001 /data


FROM scratch AS api

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /omega /omega
COPY --from=build /src/config /config
COPY --from=build --chown=10001:10001 /data/database /database
COPY --from=build --chown=10001:10001 /data/storage /storage

USER omega
WORKDIR /

VOLUME ["/database", "/storage"]
EXPOSE 3000

ENV APP_ENV=production \
    APP_HOST=0.0.0.0 \
    APP_PORT=3000 \
    DB_SQLITE_PATH=/database/omega.db \
    STORAGE_ROOT=/storage/app

ENTRYPOINT ["/omega"]
CMD ["serve"]
