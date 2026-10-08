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
.PHONY: help up down logs ps psql check migrate migrate-down migrate-new api generate check-generated test test-db test-go lint

help: ## List the available commands
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*## "} {printf "  make %-16s %s\n", $$1, $$2}'

up: ## Start PostgreSQL and Redis and wait until both are healthy
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

api: ## Run the API server (needs make up; settings from .env)
	cd backend && go run ./cmd/api

generate: ## Regenerate Go code from docs/openapi.yaml
	cd backend && go generate ./...

check-generated: generate ## Fail if generated code differs from docs/openapi.yaml
	@if [[ -n "$$(git status --porcelain -- backend/internal/api)" ]]; then \
		git status --short -- backend/internal/api; \
		echo "Generated code is out of date: run make generate and commit the result."; \
		exit 1; \
	fi

test: test-db test-go ## Run all tests (needs make up and make migrate)

test-db: ## Run the database constraint tests (needs make up and make migrate)
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -X -v ON_ERROR_STOP=1 < backend/db/tests/constraints.sql

test-go: ## Run the backend Go tests
	cd backend && go test -race -count=1 ./...

lint: ## Run all linters (golangci-lint for the backend)
	cd backend && golangci-lint run ./...
