# ResearchSphere: project commands. Run `make help` to list them.
# Recipe lines in a Makefile must start with a TAB character, not spaces.

SHELL := /bin/bash

# Load settings from .env when it exists, and pass them to every command.
-include .env
export

POSTGRES_USER ?= researchsphere
POSTGRES_DB   ?= researchsphere
DATABASE_URL  ?= postgres://researchsphere:devpassword@localhost:5433/researchsphere?sslmode=disable
MIGRATIONS_DIR := backend/db/migrations

.DEFAULT_GOAL := help
.PHONY: help up down logs ps psql check migrate migrate-down migrate-new admin unadmin api web web-clean generate check-generated test test-db test-go test-integration test-web lint lint-go lint-web

help: ## List the available commands
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*## "} {printf "  make %-16s %s\n", $$1, $$2}'

up: ## Start PostgreSQL, Redis and Mailpit and wait until they are healthy
	docker compose up -d --wait

down: ## Stop the containers (data is kept)
	docker compose down

logs: ## Follow the container logs (Ctrl+C to stop)
	docker compose logs -f

ps: ## Show the status of the containers
	docker compose ps

psql: ## Open a psql prompt in the database
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

check: ## Check that PostgreSQL, pgvector and Redis respond
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -tAc "SELECT 'postgres ok: ' || current_setting('server_version')"
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -tAc "SELECT 'pgvector available: ' || default_version FROM pg_available_extensions WHERE name = 'vector'"
	@echo "redis: $$(docker compose exec -T redis redis-cli ping)"

migrate: ## Apply all database migrations
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down: ## Undo the most recent migration
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

migrate-new: ## Create a migration pair: make migrate-new name=add_users
	@test -n "$(name)" || (echo "Usage: make migrate-new name=add_users" && exit 1)
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)

# The address reaches the command through the environment (every variable is exported
# above), never pasted into the shell command, so no character in it can change the command.
admin: ## Make an account an administrator: make admin email=ada@example.org
	@test -n "$$email" || (echo "Usage: make admin email=ada@example.org" && exit 1)
	@cd backend && go run ./cmd/setrole -role admin -email "$$email"

unadmin: ## Make an administrator a researcher again: make unadmin email=ada@example.org
	@test -n "$$email" || (echo "Usage: make unadmin email=ada@example.org" && exit 1)
	@cd backend && go run ./cmd/setrole -role researcher -email "$$email"

api: ## Run the API server (needs make up; settings from .env)
	cd backend && go run ./cmd/api

web: ## Run the web client dev server on port 3000 (needs npm ci in frontend/)
	cd frontend && npm run dev

web-clean: ## Delete the web client build cache (frontend/.next); stop make web first
	rm -rf frontend/.next

generate: ## Regenerate Go code (oapi-codegen, sqlc) and TypeScript types
	cd backend && go generate ./...
	cd frontend && npm run generate

GENERATED := backend/internal/api backend/internal/auth/adapter/postgres/sqlcgen frontend/src/lib/api

check-generated: generate ## Fail if generated code is out of date
	@if [[ -n "$$(git status --porcelain -- $(GENERATED))" ]]; then \
		git status --short -- $(GENERATED); \
		echo "Generated code is out of date: run make generate and commit the result."; \
		exit 1; \
	fi

test: test-db test-go test-integration test-web ## Run all tests (needs make up and make migrate)

test-db: ## Run the database constraint tests (needs make up and make migrate)
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -X -v ON_ERROR_STOP=1 < backend/db/tests/constraints.sql

test-go: ## Run the backend Go tests
	cd backend && go test -race -count=1 ./...

test-integration: ## Run the Go repository tests against the database and Redis (needs make up and make migrate)
	cd backend && go test -race -count=1 -tags=integration ./internal/...

test-web: ## Run the frontend tests (Jest)
	cd frontend && npm test

lint: lint-go lint-web ## Run all linters and the frontend type check

lint-go: ## Run golangci-lint for the backend
	cd backend && golangci-lint run ./...

lint-web: ## Run ESLint and the TypeScript type check for the frontend
	cd frontend && npm run lint && npm run typecheck
