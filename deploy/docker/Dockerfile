# Build stage — runs natively on the build host, Go cross-compiles for TARGETARCH
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

RUN apk --no-cache add tzdata ca-certificates

ARG SERVICE
ARG VERSION=dev
ARG TARGETARCH
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Install the orchestrion CLI at exactly the version pinned in go.mod. orchestrion
# refuses to build when the installed CLI version differs from the go.mod tool
# directive, so we derive the version from go.mod instead of hardcoding it here —
# keeping go.mod the single source of truth as Dependabot bumps the dependency.
RUN go install github.com/DataDog/orchestrion@"$(go list -m -f '{{.Version}}' github.com/DataDog/orchestrion)"

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
