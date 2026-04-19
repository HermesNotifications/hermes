# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
RUN apk --no-cache add tzdata ca-certificates
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/loadseed ./cmd/loadseed

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/loadseed /loadseed
ENTRYPOINT ["/loadseed"]
