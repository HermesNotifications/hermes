# Build stage
FROM golang:1.25-alpine AS builder

ARG SERVICE
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /service ./cmd/${SERVICE}/

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /service /service
COPY migrations/ /migrations/

EXPOSE 8080
ENTRYPOINT ["/service"]
