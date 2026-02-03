# set mac-only linker flags only for go test (not global)
UNAME_S := $(shell uname -s)
TEST_ENV :=
ifeq ($(UNAME_S),Darwin)
  TEST_ENV = CGO_LDFLAGS=-w
endif

TEST_FLAGS := -race -count=1

.PHONY: build-ui
build-ui:
	cd ui && npm ci && npm run build
	rm -rf internal/app/ui/dist
	cp -r ui/dist internal/app/ui/

.PHONY: ui-dev
ui-dev:
	cd ui && VITE_API_URL=http://localhost:8080 npm run dev

.PHONY: ui-dev-local
ui-dev-local:
	cd ui && VITE_API_URL=http://localhost:3010 npm run dev

.PHONY: build-lambda
build-lambda: build-ui
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./var/dist/bootstrap ./cmd/lambda

.PHONY: build-server
build-server: build-ui
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./var/dist/server ./cmd/server

.PHONY: build-server-no-ui
build-server-no-ui:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ./var/dist/server ./cmd/server

.PHONY: test
test:
	$(TEST_ENV) go test $(TEST_FLAGS) ./...

.PHONY: test-unit
test-unit:
	$(TEST_ENV) go test $(TEST_FLAGS) ./internal/...

.PHONY: test-verify
test-verify:
	go run ./cmd/verify

.PHONY: test-verify-verbose
test-verify-verbose:
	go run ./cmd/verify -verbose

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -rf .var/dist/

# ==============================================================================
# Docker Compose Commands
# ==============================================================================

.PHONY: docker-clean
docker-clean:
	@docker compose --profile demo --profile demo-fast --profile server down --rmi local --volumes
	@echo "Docker resources cleaned up"

# --- Local Server -------------------------------------------------------------

.PHONY: server
server:
	@docker compose --profile server up --build -d
	@echo ""
	@echo "=============================================="
	@echo "  RDS Maintenance Machine - SERVER"
	@echo "=============================================="
	@echo ""
	@echo "  Server:  http://localhost:3010"
	@echo ""
	@echo "  Run 'make server-logs' to view logs"
	@echo "  Run 'make server-stop' to stop"
	@echo "=============================================="

.PHONY: server-logs
server-logs:
	@docker compose --profile server logs -f

.PHONY: server-stop
server-stop:
	@docker compose --profile server down
	@echo "Server stopped"

# --- Demo Mode ----------------------------------------------------------------

.PHONY: demo
demo:
	@docker compose --profile demo up --build -d
	@echo ""
	@echo "=============================================="
	@echo "  RDS Maintenance Machine - DEMO MODE"
	@echo "=============================================="
	@echo ""
	@echo "  Web UI:      http://localhost:8080"
	@echo "  Mock RDS:    http://localhost:9080"
	@echo "  Mock State:  http://localhost:9080/mock/state"
	@echo ""
	@echo "  Run 'make demo-logs' to view logs"
	@echo "  Run 'make demo-stop' to stop"
	@echo "=============================================="

.PHONY: demo-logs
demo-logs:
	@docker compose --profile demo logs -f

.PHONY: demo-stop
demo-stop:
	@docker compose --profile demo down
	@echo "Demo stopped"

