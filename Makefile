# Grabarr Makefile
# Go-based service for managing downloads from remote seedboxes

# Variables
BINARY_NAME=grabarr
DOCKER_IMAGE=grabarr
DOCKER_TAG=latest
REMOTE_HOST=millions.local
REMOTE_USER=root
GO_VERSION=$(shell go version | cut -d' ' -f3)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse --short HEAD)

# Build flags
LDFLAGS=-ldflags "-X main.Version=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}"

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[0;33m
BLUE=\033[0;34m
NC=\033[0m # No Color

.PHONY: help build run dev clean test test-verbose test-coverage test-sanitizer fmt vet lint deps
.PHONY: docker-build docker-run docker-stop docker-logs docker-shell
.PHONY: deploy deploy-logs deploy-restart setup-config

# Default target
all: fmt vet test build

## Development Targets

build: ## Build the Go binary locally
	@echo "$(GREEN)Building ${BINARY_NAME}...$(NC)"
	@CGO_ENABLED=1 go build ${LDFLAGS} -o ${BINARY_NAME} ./cmd/grabarr
	@echo "$(GREEN)✓ Build complete: ${BINARY_NAME}$(NC)"

run: build ## Run the application locally (requires config.yaml)
	@if [ ! -f config.yaml ]; then \
		echo "$(RED)✗ config.yaml not found. Run 'make setup-config' first$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Starting ${BINARY_NAME}...$(NC)"
	@./${BINARY_NAME}

dev: ## Run with file watching for development (requires entr)
	@if ! command -v entr >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing entr for file watching...$(NC)"; \
		echo "$(YELLOW)On macOS: brew install entr$(NC)"; \
		echo "$(YELLOW)On Linux: sudo apt-get install entr$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Starting development mode with auto-reload...$(NC)"
	@find . -name "*.go" | entr -r sh -c 'make build && ./${BINARY_NAME}'

fmt: ## Format Go code
	@echo "$(GREEN)Formatting Go code...$(NC)"
	@go fmt ./...
	@echo "$(GREEN)✓ Code formatted$(NC)"

vet: ## Run go vet for static analysis
	@echo "$(GREEN)Running go vet...$(NC)"
	@go vet ./...
	@echo "$(GREEN)✓ Static analysis passed$(NC)"

lint: ## Run golint (install if not available)
	@if ! command -v golint >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golint...$(NC)"; \
		go install golang.org/x/lint/golint@latest; \
	fi
	@echo "$(GREEN)Running golint...$(NC)"
	@golint ./...

## Testing Targets

test: ## Run all tests
	@echo "$(GREEN)Running tests...$(NC)"
	@go test ./...
	@echo "$(GREEN)✓ All tests passed$(NC)"

test-verbose: ## Run tests with verbose output
	@echo "$(GREEN)Running tests (verbose)...$(NC)"
	@go test -v ./...

test-coverage: ## Generate test coverage report
	@echo "$(GREEN)Generating test coverage report...$(NC)"
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✓ Coverage report generated: coverage.html$(NC)"

test-sanitizer: ## Run only sanitizer module tests
	@echo "$(GREEN)Running sanitizer tests...$(NC)"
	@go test -v ./internal/sanitizer/...

## Docker Targets

docker-build: ## Build Docker image
	@echo "$(GREEN)Building Docker image ${DOCKER_IMAGE}:${DOCKER_TAG}...$(NC)"
	@docker build -t ${DOCKER_IMAGE}:${DOCKER_TAG} .
	@echo "$(GREEN)✓ Docker image built$(NC)"

docker-run: docker-build ## Run container locally (detached)
	@echo "$(GREEN)Starting Docker container...$(NC)"
	@docker-compose up -d
	@echo "$(GREEN)✓ Container started. View logs with 'make docker-logs'$(NC)"

docker-stop: ## Stop running container
	@echo "$(GREEN)Stopping Docker container...$(NC)"
	@docker-compose down
	@echo "$(GREEN)✓ Container stopped$(NC)"

docker-logs: ## View container logs
	@docker-compose logs -f

docker-shell: ## Open shell in running container
	@echo "$(GREEN)Opening shell in ${DOCKER_IMAGE} container...$(NC)"
	@docker exec -it grabarr /bin/sh

## Deployment Targets

deploy: docker-build ## Deploy to remote server
	@echo "$(GREEN)Deploying to ${REMOTE_HOST}...$(NC)"
	@echo "$(YELLOW)Copying files to remote server...$(NC)"
	@scp docker-compose.yml config.yaml .env ${REMOTE_USER}@${REMOTE_HOST}:/opt/grabarr/
	@echo "$(YELLOW)Saving and loading Docker image on remote...$(NC)"
	@docker save ${DOCKER_IMAGE}:${DOCKER_TAG} | ssh ${REMOTE_USER}@${REMOTE_HOST} "docker load"
	@echo "$(YELLOW)Starting service on remote...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /opt/grabarr && docker-compose up -d"
	@echo "$(GREEN)✓ Deployment complete$(NC)"

deploy-logs: ## View remote deployment logs
	@echo "$(GREEN)Viewing logs from ${REMOTE_HOST}...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /opt/grabarr && docker-compose logs -f"

deploy-restart: ## Restart remote service
	@echo "$(GREEN)Restarting service on ${REMOTE_HOST}...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /opt/grabarr && docker-compose restart"
	@echo "$(GREEN)✓ Service restarted$(NC)"

## Utility Targets

clean: ## Clean build artifacts and temporary files
	@echo "$(GREEN)Cleaning build artifacts...$(NC)"
	@rm -f ${BINARY_NAME}
	@rm -f coverage.out coverage.html
	@docker system prune -f
	@echo "$(GREEN)✓ Clean complete$(NC)"

deps: ## Download and verify dependencies
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	@go mod download
	@go mod verify
	@go mod tidy
	@echo "$(GREEN)✓ Dependencies updated$(NC)"

setup-config: ## Copy example configs to working configs
	@echo "$(GREEN)Setting up configuration files...$(NC)"
	@if [ ! -f config.yaml ]; then \
		cp config.example.yaml config.yaml; \
		echo "$(YELLOW)→ Created config.yaml from example$(NC)"; \
	else \
		echo "$(YELLOW)→ config.yaml already exists$(NC)"; \
	fi
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "$(YELLOW)→ Created .env from example$(NC)"; \
		echo "$(RED)⚠ Don't forget to edit .env with your credentials!$(NC)"; \
	else \
		echo "$(YELLOW)→ .env already exists$(NC)"; \
	fi
	@if [ ! -f rclone.conf ]; then \
		cp rclone.conf.template rclone.conf; \
		echo "$(YELLOW)→ Created rclone.conf from template$(NC)"; \
		echo "$(RED)⚠ Don't forget to edit rclone.conf with your settings!$(NC)"; \
	else \
		echo "$(YELLOW)→ rclone.conf already exists$(NC)"; \
	fi

## Help

help: ## Show this help message
	@echo "$(BLUE)Grabarr Makefile$(NC)"
	@echo "$(BLUE)===============$(NC)"
	@echo ""
	@echo "$(GREEN)Usage:$(NC) make [target]"
	@echo ""
	@echo "$(GREEN)Development:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^(build|run|dev|fmt|vet|lint):' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Testing:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^test' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Docker:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^docker-' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Deployment:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^deploy' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Utilities:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^(clean|deps|setup-config):' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Build Info:$(NC)"
	@echo "  Go Version: $(GO_VERSION)"
	@echo "  Git Commit: $(GIT_COMMIT)"
	@echo "  Build Time: $(BUILD_TIME)"