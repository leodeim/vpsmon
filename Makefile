.PHONY: build run dev docker-dev clean

APP_NAME = vpsmon

build:
	@echo "==> Building ${APP_NAME}..."
	@go build -ldflags="-s -w" -o ${APP_NAME} ./cmd/vpsmon

run: build
	@echo "==> Running ${APP_NAME} locally on :8088..."
	@./${APP_NAME}

dev:
	@echo "==> Starting dev server with air (hot-reload)..."
	@if ! command -v air > /dev/null; then \
		echo "air is not installed. Installing it via 'go install github.com/air-verse/air@latest'..."; \
		go install github.com/air-verse/air@latest; \
	fi
	@air

docker-dev:
	@echo "==> Starting Docker dev server with hot-reload on http://localhost:8089..."
	@docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
