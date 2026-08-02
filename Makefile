.PHONY: help \
	dev-backend dev-frontend \
	build build-backend build-frontend \
	test test-backend test-frontend \
	lint lint-backend lint-frontend \
	fmt vet \
	docker-build docker-run docker-clean

DOCKER_IMAGE := calendar-server
DATA_DIR := ./data

help:
	@echo "Backend (run from a separate terminal, needs Go 1.26+):"
	@echo "  make dev-backend    run the Go backend on :8080, storing data in $(DATA_DIR)"
	@echo "  make build-backend  compile the backend binary"
	@echo "  make test-backend   run backend tests"
	@echo "  make lint-backend   go vet the backend"
	@echo "  make fmt            check backend formatting (gofmt)"
	@echo "  make vet            alias for lint-backend"
	@echo
	@echo "Frontend (run from a separate terminal):"
	@echo "  make dev-frontend   run the Vite dev server (proxies /api to :8080)"
	@echo "  make build-frontend build the production frontend bundle"
	@echo "  make test-frontend  run frontend tests"
	@echo "  make lint-frontend  eslint the frontend"
	@echo
	@echo "Combined:"
	@echo "  make build          build both backend and frontend"
	@echo "  make test           run both test suites"
	@echo "  make lint           lint both"
	@echo
	@echo "Docker (single-container deploy):"
	@echo "  make docker-build   build the production image"
	@echo "  make docker-run     run it on :8080, persisting data to $(DATA_DIR)"
	@echo "  make docker-clean   stop and remove the container"

# --- Backend ---

dev-backend:
	cd server && DATA_DIR=$(abspath $(DATA_DIR)) PORT=8080 go run ./cmd/server

build-backend:
	cd server && go build -o bin/calendar-server ./cmd/server

test-backend:
	cd server && go test ./...

lint-backend vet:
	cd server && go vet ./...

fmt:
	cd server && gofmt -l .

# --- Frontend ---

dev-frontend:
	yarn dev

build-frontend:
	yarn build

test-frontend:
	yarn test

lint-frontend:
	yarn lint

# --- Combined ---

build: build-backend build-frontend

test: test-backend test-frontend

lint: lint-backend lint-frontend

# --- Docker ---

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	mkdir -p $(DATA_DIR)
	docker run -d -p 8080:8080 -v $(abspath $(DATA_DIR)):/data --name $(DOCKER_IMAGE) $(DOCKER_IMAGE)

docker-clean:
	docker rm -f $(DOCKER_IMAGE)
