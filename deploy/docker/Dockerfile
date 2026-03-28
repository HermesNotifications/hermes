# Build stage — runs natively on the build host, Go cross-compiles for TARGETARCH
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

RUN apk --no-cache add tzdata ca-certificates
RUN go install github.com/DataDog/orchestrion@latest

ARG SERVICE
ARG VERSION=dev
ARG TARGETARCH
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    orchestrion go build \
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
