.PHONY: help \
	dev-backend dev-frontend \
	build build-backend build-frontend \
	test test-backend test-frontend \
	lint lint-backend lint-frontend \
	fmt vet \
	docker-build docker-run docker-clean \
	qa-up qa-down qa-reset qa-status

DOCKER_IMAGE := calich-server
DATA_DIR := ./data

# The build label reported by /api/version and shown beside the wordmark
# (#256, ADR-0072). Overridable per-invocation — `make build-backend
# VERSION=v1.2.3` — which is how the release pipeline will set it from the
# tag. Left as "dev" it matches the package default, so an ordinary local
# build is indistinguishable from `go run`.
VERSION ?= dev
VERSION_LDFLAGS := -X github.com/XiovV/calich/server/internal/version.Version=$(VERSION)

help:
	@echo "Backend (run from a separate terminal, needs Go 1.26+):"
	@echo "  make dev-backend    run the Go backend on :8080, storing data in $(DATA_DIR)"
	@echo "  make build-backend  compile the backend binary (VERSION=v1.2.3 to stamp a build label)"
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
	cd server && go build -ldflags "$(VERSION_LDFLAGS)" -o bin/calich-server ./cmd/server

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
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

docker-run:
	mkdir -p $(DATA_DIR)
	docker run -d -p 8080:8080 -v $(abspath $(DATA_DIR)):/data --name $(DOCKER_IMAGE) $(DOCKER_IMAGE)

docker-clean:
	docker rm -f $(DOCKER_IMAGE)

# --- Browser QA ---
# A disposable backend+frontend pair on their own ports and their own SQLite
# database under .qa/, so browser QA never touches $(DATA_DIR) and never
# fights dev-backend/dev-frontend for a port. See docs/agents/browser-qa.md.

qa-up:
	scripts/qa-env.sh up

qa-down:
	scripts/qa-env.sh down

qa-reset:
	scripts/qa-env.sh reset

qa-status:
	scripts/qa-env.sh status
