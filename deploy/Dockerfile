# --- Build stage ---
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/td-sync ./cmd/td-sync

# --- Litestream stage ---
FROM litestream/litestream:0.3.13 AS litestream

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
COPY --from=builder /out/td-sync /usr/local/bin/td-sync
COPY deploy/litestream.yml /etc/litestream.yml
COPY deploy/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
