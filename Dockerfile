# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o vps-monitor .

# Stage 2: Create the minimal runtime image
FROM alpine:latest

WORKDIR /app

# Install procps so we have the standard 'ps' command for the Top Processes feature.
# The busybox 'ps' does not support the flags we need.
RUN apk add --no-cache procps

COPY --from=builder /app/vps-monitor .

EXPOSE 8080

CMD ["./vps-monitor"]
