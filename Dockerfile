# syntax=docker/dockerfile:1

FROM golang:1.26.4-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/credential-service ./cmd/api

# --- Debug stages (docker-compose.debug.yml, see RUNNING.md) -------------------------
# Must stay above the runtime stage: the default build target is the LAST stage, so
# moving these would make `docker compose build` produce the debug image.

FROM golang:1.26.4-alpine AS builder-debug

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

RUN go install github.com/go-delve/delve/cmd/dlv@latest

COPY . .

# -N -l keeps stepping on the source; -s -w is dropped because it strips the symbols
# breakpoints resolve against.
RUN CGO_ENABLED=0 GOOS=linux go build -gcflags="all=-N -l" -o /out/credential-service ./cmd/api

FROM golang:1.26.4-alpine AS debug

WORKDIR /app

COPY --from=builder-debug /go/bin/dlv /usr/local/bin/dlv
COPY --from=builder-debug /out/credential-service /usr/local/bin/credential-service
COPY --from=builder-debug /src/configs /app/configs

EXPOSE 8080 2345

# No USER line: Delve needs ptrace. --continue starts the service without waiting for a
# client; --accept-multiclient allows repeated attach and detach.
ENTRYPOINT ["dlv", "exec", "/usr/local/bin/credential-service", \
            "--headless", "--listen=:2345", "--api-version=2", \
            "--accept-multiclient", "--continue", "--log"]

# --- Production runtime (default target) --------------------------------------------

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -H -u 65532 appuser

WORKDIR /app

COPY --from=builder /out/credential-service /usr/local/bin/credential-service
COPY --from=builder /src/configs /app/configs

EXPOSE 8080

USER appuser

ENTRYPOINT ["credential-service"]
