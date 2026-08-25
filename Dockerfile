# syntax=docker/dockerfile:1

FROM golang:1.26.4-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/credential-service ./cmd/api

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -H -u 65532 appuser

WORKDIR /app

COPY --from=builder /out/credential-service /usr/local/bin/credential-service

EXPOSE 8080

USER appuser

ENTRYPOINT ["credential-service"]
