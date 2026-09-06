.DEFAULT_GOAL := help
.PHONY: help env dev run build test check tidy

ENV_FILE := ./.env    # local override only; the shared values arrive exported from the root
LOAD_ENV := set -a; [ -f $(ENV_FILE) ] && . $(ENV_FILE); set +a;

help:  ## Show targets
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/' | column -t -s"$$(printf '\t')"

env:   ## what this tree actually sees — root export, plus any local override
	@$(LOAD_ENV) printf '  %s\n' "PORT_SERVER=$$PORT_SERVER" "DATABASE_URL=$$DATABASE_URL" "LOG_LEVEL=$$LOG_LEVEL"

dev:   ## run with reload, ambient env plus any local .env
	@command -v air >/dev/null || { echo "air not found -> go install github.com/air-verse/air@latest"; exit 1; }
	@$(LOAD_ENV) air

run:   ## run once, no reload
	@$(LOAD_ENV) go run ./cmd/server

build: ## compile to ./tmp/server
	@go build -o ./tmp/server ./cmd/server

test:  ## run the tests
	@go test ./...

check: ## vet, then test under the race detector
	@go vet ./...
	@go test -race ./...

tidy:  ## go mod tidy
	@go mod tidy
