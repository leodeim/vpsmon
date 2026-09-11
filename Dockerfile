# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o vpsmon ./cmd/vpsmon

# Development image: source is bind-mounted by docker-compose.dev.yml and Air
# rebuilds the binary whenever Go or embedded dashboard templates change.
FROM golang:1.25-alpine AS development

WORKDIR /app

RUN apk add --no-cache busybox-extras docker-cli \
    && go install github.com/air-verse/air@v1.63.3

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8088

CMD ["/go/bin/air", "-c", ".air.toml"]

# Stage 2: Create the minimal runtime image
FROM alpine:latest

WORKDIR /app

# Install procps so we have the standard 'ps' command for the Top Processes feature.
# The busybox 'ps' does not support the flags we need.
RUN apk add --no-cache procps docker-cli

COPY --from=builder /app/vpsmon .

EXPOSE 8088

CMD ["./vpsmon"]
