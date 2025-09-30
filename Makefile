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

.PHONY: help build run clean test test-verbose test-coverage test-race test-coverage-summary test-ci fmt vet deps gen-mocks
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

fmt: ## Format Go code
	@echo "$(GREEN)Formatting Go code...$(NC)"
	@go fmt ./...
	@echo "$(GREEN)✓ Code formatted$(NC)"

vet: ## Run go vet for static analysis
	@echo "$(GREEN)Running go vet...$(NC)"
	@go vet ./...
	@echo "$(GREEN)✓ Static analysis passed$(NC)"

gen-mocks: ## Generate mocks using mockery
	@echo "$(GREEN)Generating mocks...$(NC)"
	@if ! command -v mockery >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing mockery...$(NC)"; \
		go install github.com/vektra/mockery/v2@latest; \
	fi
	@mockery --all --dir internal/interfaces --output internal/mocks --case underscore
	@echo "$(GREEN)✓ Mocks generated$(NC)"

## Testing Targets

test: ## Run all tests
	@echo "$(GREEN)Running tests...$(NC)"
	@go test ./...
	@echo "$(GREEN)✓ All tests passed$(NC)"

test-verbose: ## Run tests with verbose output
	@echo "$(GREEN)Running tests (verbose)...$(NC)"
	@go test -v ./...

test-race: ## Run tests with race detector
	@echo "$(GREEN)Running tests with race detector...$(NC)"
	@go test -race ./...
	@echo "$(GREEN)✓ Tests passed with no race conditions$(NC)"

test-coverage: ## Generate test coverage report (HTML) - excludes interfaces/mocks/testutil
	@echo "$(GREEN)Generating test coverage report...$(NC)"
	@go test \
		$$(go list ./internal/... | grep -v -e '/interfaces$$' -e '/mocks$$' -e '/testutil$$') \
		-short -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✓ Coverage report generated: coverage.html$(NC)"
	@go tool cover -func=coverage.out | grep total

test-coverage-summary: ## Show test coverage percentage - excludes interfaces/mocks/testutil
	@echo "$(GREEN)Generating coverage summary...$(NC)"
	@go test \
		$$(go list ./internal/... | grep -v -e '/interfaces$$' -e '/mocks$$' -e '/testutil$$') \
		-short -coverprofile=coverage.out > /dev/null 2>&1
	@go tool cover -func=coverage.out | grep total | awk '{print "$(GREEN)Total Coverage: " $$3 "$(NC)"}'
	@rm coverage.out

test-ci: fmt vet test ## Run all pre-commit checks (format, vet, test)
	@echo "$(GREEN)✓ All pre-commit checks passed$(NC)"

## Docker Targets

docker-build: ## Build Docker image for linux/amd64 (Unraid)
	@echo "$(GREEN)Building Docker image ${DOCKER_IMAGE}:${DOCKER_TAG} for linux/amd64...$(NC)"
	@docker build --platform linux/amd64 -t ${DOCKER_IMAGE}:${DOCKER_TAG} .
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

deploy: docker-build ## Deploy to remote server (Unraid)
	@echo "$(GREEN)Deploying to ${REMOTE_HOST}...$(NC)"
	@echo "$(YELLOW)Copying config files to remote...$(NC)"
	@scp docker-compose.yml ${REMOTE_USER}@${REMOTE_HOST}:/mnt/user/appdata/grabarr/
	@scp config.yaml ${REMOTE_USER}@${REMOTE_HOST}:/mnt/user/appdata/grabarr/
	@echo "$(YELLOW)Transferring Docker image (this may take a few minutes)...$(NC)"
	@docker save ${DOCKER_IMAGE}:${DOCKER_TAG} | gzip | ssh ${REMOTE_USER}@${REMOTE_HOST} "gunzip | docker load"
	@echo "$(YELLOW)Starting service on remote...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /mnt/user/appdata/grabarr && docker-compose up -d"
	@echo "$(GREEN)✓ Deployment complete$(NC)"

deploy-logs: ## View remote deployment logs
	@echo "$(GREEN)Viewing logs from ${REMOTE_HOST}...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /mnt/user/appdata/grabarr && docker-compose logs -f"

deploy-restart: ## Restart remote service
	@echo "$(GREEN)Restarting service on ${REMOTE_HOST}...$(NC)"
	@ssh ${REMOTE_USER}@${REMOTE_HOST} "cd /mnt/user/appdata/grabarr && docker-compose restart"
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
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^(build|run|fmt|vet|gen-mocks):' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Testing:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^test' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Docker:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^docker-' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Deployment:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^deploy' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Utilities:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '^(clean|deps|setup-config):' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(GREEN)Build Info:$(NC)"
	@echo "  Go Version: $(GO_VERSION)"
	@echo "  Git Commit: $(GIT_COMMIT)"
	@echo "  Build Time: $(BUILD_TIME)"