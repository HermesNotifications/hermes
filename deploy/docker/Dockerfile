# Build stage
FROM golang:1.25-alpine AS builder

ARG SERVICE
ARG VERSION=dev
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /service ./cmd/${SERVICE}/

# Runtime stage — scratch for minimal attack surface
FROM scratch

# TLS certificates for outbound HTTPS (webhooks, managed PG/Redis with TLS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Timezone data for time.LoadLocation
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=builder /service /service
COPY migrations/ /migrations/

USER 65534:65534
ENTRYPOINT ["/service"]
