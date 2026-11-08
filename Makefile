.PHONY: build run dev docker-dev clean

APP_NAME = vpsmon

# Local development only: log in with admin / changeme unless MONITOR_PASS_HASH is set.
DEV_PASS_HASH = MONITOR_PASS_HASH="$${MONITOR_PASS_HASH:-$$(go run ./cmd/vpsmon -hash changeme)}"

build:
	@echo "==> Building ${APP_NAME}..."
	@go build -ldflags="-s -w" -o ${APP_NAME} ./cmd/vpsmon

run: build
	@echo "==> Running ${APP_NAME} locally on :8088..."
	@${DEV_PASS_HASH} ./${APP_NAME}

dev:
	@echo "==> Starting dev server with air (hot-reload)..."
	@if ! command -v air > /dev/null; then \
		echo "air is not installed. Installing it via 'go install github.com/air-verse/air@latest'..."; \
		go install github.com/air-verse/air@latest; \
	fi
	@${DEV_PASS_HASH} air

docker-dev:
	@echo "==> Starting Docker dev server with hot-reload on http://localhost:8089..."
	@${DEV_PASS_HASH} docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
