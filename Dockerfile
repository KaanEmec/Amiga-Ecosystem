# Container image for Coolify (or any Docker host). Runtime data lives in the
# /data volume; the seed is baked in and read once when the database is empty.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/amiga ./cmd/amiga
# Seed files keep their checkout modes; make them world-readable for the runtime user.
COPY data/amiga-data.json /app/data/amiga-data.json
COPY images /app/images
RUN chmod -R a+rX /app

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -S -D -H -h /data -u 10001 amiga \
 && install -d -o amiga -m 750 /data
COPY --from=build /out/amiga /usr/local/bin/amiga
COPY --from=build /app /app
ENV AMIGA_ENV=production \
    AMIGA_ADDR=:8080 \
    AMIGA_DATA_DIR=/data \
    AMIGA_SEED_JSON=/app/data/amiga-data.json \
    AMIGA_SEED_IMAGES=/app/images
USER amiga
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["amiga"]
CMD ["serve"]
