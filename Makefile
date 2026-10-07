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
.PHONY: help up down logs ps psql check migrate migrate-down migrate-new test lint

help: ## List the available commands
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*## "} {printf "  make %-14s %s\n", $$1, $$2}'

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

test: ## Run all tests (each service adds its tests in its own slice)
	@echo "No tests yet. Slice 2 adds the CI pipeline and the first checks."

lint: ## Run all linters (each service adds its linter in its own slice)
	@echo "No linters yet. Slice 2 adds them."
