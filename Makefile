# ResearchSphere: project commands. Run `make help` to list them.
# Recipe lines in a Makefile must start with a TAB character, not spaces.

SHELL := /bin/bash

# Load settings from .env when it exists, and pass them to every command.
-include .env
export

POSTGRES_USER ?= researchsphere
POSTGRES_DB   ?= researchsphere
MINIO_API_PORT ?= 9000
S3_BUCKET     ?= researchsphere
DATABASE_URL  ?= postgres://researchsphere:devpassword@localhost:5433/researchsphere?sslmode=disable
# Tests use their own database and Redis DB, never the development ones. The tests empty
# them, so they refuse a database name that does not end in _test and Redis DB 0.
TEST_POSTGRES_DB  ?= researchsphere_test
TEST_DATABASE_URL ?= postgres://researchsphere:devpassword@localhost:5433/researchsphere_test?sslmode=disable
TEST_REDIS_URL    ?= redis://localhost:6379/15
AI_SERVICE_PORT   ?= 8090
# The compile sandbox image (slice 23); make compile-image builds it under this name.
COMPILE_IMAGE     ?= researchsphere-compile:dev
MIGRATIONS_DIR := backend/db/migrations

.DEFAULT_GOAL := help
.PHONY: help up down logs ps psql check migrate migrate-down migrate-new admin unadmin seed-ui-compare seed-dev sample-data sample-purge sample-venues-purge purge-unverified send-digests venue-sync calibrate-recs eval-recs bench-recs eval-search bench-search embed-backfill backfill-main-tex purge-compile-output update-disposable-domains api web stop web-clean ai-build ai-up ai-fetch-model ai-logs sync sync-build sync-load compile-image compile-worker bench-compile compile-trace generate check-generated test test-prepare test-db test-go test-integration test-one test-web test-ai test-sync test-compile test-compile-sandbox lint lint-go lint-web lint-ai lint-sync lint-compile

help: ## List the available commands
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN {FS = ":.*## "} {printf "  make %-16s %s\n", $$1, $$2}'

up: ## Start PostgreSQL, Redis, Mailpit and MinIO, wait until healthy, create the bucket; start ClamAV and the AI service without waiting (CLAMAV=0, AI=0 leave them out)
	docker compose up -d --wait postgres redis mailpit minio
	docker compose run --rm minio-init
ifneq ($(CLAMAV),0)
	docker compose up -d clamav
endif
ifneq ($(AI),0)
	docker compose up -d ai
endif

down: ## Stop the containers (data, including stored files, is kept)
	docker compose down

logs: ## Follow the container logs (Ctrl+C to stop)
	docker compose logs -f

ps: ## Show the status of the containers
	docker compose ps

psql: ## Open a psql prompt in the database
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

check: ## Check that PostgreSQL, pgvector, Redis and storage respond, the bucket is private and takes uploads from the web client, and the compile sandbox's Landlock works (when its image is built); show whether ClamAV answers
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -tAc "SELECT 'postgres ok: ' || current_setting('server_version')"
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -tAc "SELECT 'pgvector available: ' || default_version FROM pg_available_extensions WHERE name = 'vector'"
	@echo "redis: $$(docker compose exec -T redis redis-cli ping)"
	@curl -fsS -o /dev/null "http://127.0.0.1:$(MINIO_API_PORT)/minio/health/live" || (echo "storage: not responding" && exit 1)
	@status=$$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$(MINIO_API_PORT)/$(S3_BUCKET)/"); \
	if [[ "$$status" != 403 ]]; then echo "storage: anonymous listing of $(S3_BUCKET) returned $$status, want 403 (bucket must be private)"; exit 1; fi
	@echo "storage ok (bucket $(S3_BUCKET) is private)"
	@cors=$$(curl -s -o /dev/null -D - -X OPTIONS "http://127.0.0.1:$(MINIO_API_PORT)/$(S3_BUCKET)/" -H "Origin: http://localhost:3000" \
		-H "Access-Control-Request-Method: POST" -H "Access-Control-Request-Headers: content-type" | tr -d '\r' | grep -i '^access-control-allow-origin:'); \
	if [[ "$$cors" != *"http://localhost:3000"* ]]; then echo "storage: the web client (http://localhost:3000) may not upload figures (CORS); run make up again"; exit 1; fi
	@echo "storage CORS ok (the web client may upload figures)"
	@echo "clamav: $$(docker compose exec -T clamav clamdscan --ping 1 >/dev/null 2>&1 && echo ready || echo 'not ready yet (attachments stay pending until it answers)')"
	@echo "ai: $$(curl -fsS -o /dev/null "http://127.0.0.1:$(AI_SERVICE_PORT)/ready" 2>/dev/null && echo ready || echo 'not ready yet (loading or downloading its model, or not started; GET /health says degraded)')"
	@if docker image inspect "$(COMPILE_IMAGE)" >/dev/null 2>&1; then \
		out=$$(docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true --user 10001:10001 \
			--tmpfs /work:rw,size=1m,mode=0700,uid=10001,gid=10001 --tmpfs /tmp:rw,size=1m,mode=1777 \
			--entrypoint /usr/local/bin/rs-landlock "$(COMPILE_IMAGE)" --probe 2>&1); \
		if [[ "$$out" != "landlock abi="*" ok" ]]; then echo "compile sandbox: $$out"; echo "compile sandbox: this kernel cannot enforce the sandbox's file rules (Landlock ABI 8 needed, docs/deploy.md); no compile will run"; exit 1; fi; \
		echo "compile sandbox ok ($$out)"; \
	else echo "compile sandbox: image $(COMPILE_IMAGE) not built (make compile-image); Landlock not checked"; fi

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

seed-ui-compare: ## Fill the local dev database with the fake accounts ui:compare signs in with (needs make migrate and UI_COMPARE_PASSWORD)
	@cd backend && go run ./cmd/seedcompare

seed-dev: ## Create or repair the dev accounts test1..test4@example.com and admin@example.com (needs make migrate and DEV_SEED_PASSWORD)
	@cd backend && go run ./cmd/seeddev

# Sample profiles for development (slice 16): n is how many there should be (default 1000; a
# second run tops up), wait=1 waits until the running API has embedded them, projects=1 also
# makes one sample project per 10 profiles (slice 19). Never in production.
sample-data: ## Load deterministic sample profiles into the dev database: make sample-data [n=10000] [wait=1] [projects=1] [venues=1] (needs make migrate and SAMPLE_PASSWORD)
	@cd backend && go run ./cmd/seedsample -n "$(or $(n),1000)" $(if $(filter 1 true yes,$(wait)),-wait) $(if $(filter 1 true yes,$(projects)),-projects) $(if $(filter 1 true yes,$(venues)),-venues)

sample-purge: ## Delete every sample account (@sample.researchsphere.local) through the account deletion, and every sample venue
	@cd backend && go run ./cmd/seedsample -purge

sample-venues-purge: ## Delete only the sample venues (sample_venues), with their embeddings and jobs; keeps sample accounts and projects
	@cd backend && go run ./cmd/seedsample -purge-venues

purge-unverified: ## Run the FR-17.4 purge of old unverified accounts once, now (settings from .env), then exit
	cd backend && go run ./cmd/api -purge-unverified

send-digests: ## Send the FR-16.11 unread message digests now due once (settings from .env), then exit
	cd backend && go run ./cmd/api -send-digests

# The source names reach the command through the environment, never pasted into its text.
venue-sync: ## Run the FR-14.5 automatic venue import once now and print each source's counts: dry=1 writes nothing, sources=ccfddl,openalex limits it
	cd backend && go run ./cmd/api -venue-sync $(if $(filter 1,$(dry)),-dry-run) -sources "$$sources"

calibrate-recs: ## Fit the AI-2.2 score map on the sample TUNING split and print RECS_SCORE_*, then check it on profile-project pairs and print RECS_PROJECT_SCORE_* (dev sample data)
	@cd backend && go run ./cmd/recseval calibrate

eval-recs: ## Measure NFR-6 collaborator matching and project matching both ways (recall@10, precision@10) on the held-out sample TEST split
	@cd backend && go run ./cmd/recseval eval

bench-recs: ## Measure NFR-1: people and project recommendations for 200 random sample users and projects (p50, p95, max) and EXPLAIN ANALYZE
	@cd backend && go run ./cmd/recseval bench

eval-search: ## Measure AI-5.2: hybrid vs keyword researcher search precision@10 on the sample TEST split (needs the AI service)
	@cd backend && go run ./cmd/recseval eval-search

bench-search: ## Measure NFR-1: hybrid researcher search p50, p95, max with a cold and a cached query vector (needs the AI service)
	@cd backend && go run ./cmd/recseval bench-search

backfill-main-tex: ## Give the starting main.tex to every project without files (FR-10.4); once after migration 000035, idempotent
	cd backend && go run ./cmd/backfillmaintex

purge-compile-output: ## Delete PDFs and logs of compiles that started before=<RFC 3339 time> (after a sandbox fix); lists only unless apply=1
	@test -n "$(before)" || { echo "Usage: make purge-compile-output before=2026-11-23T21:10:27+06:00 [apply=1]"; exit 1; }
	cd backend && go run ./cmd/purgecompileoutput -before "$(before)" $(if $(filter 1,$(apply)),-apply,)

embed-backfill: ## Queue every verified, not suspended profile, every project and every venue for its AI embedding once (AI-1.5); the running API embeds them
	cd backend && go run ./cmd/api -embed-backfill

# The list of disposable email domains (FR-17.2), CC0, from github.com/disposable-email-domains.
# The download goes to a .download file and replaces the list only when it looks whole.
DISPOSABLE_DOMAINS_URL := https://raw.githubusercontent.com/disposable-email-domains/disposable-email-domains/main/disposable_email_blocklist.conf
DISPOSABLE_DOMAINS_FILE := backend/internal/auth/adapter/emaildomain/disposable_domains.txt

update-disposable-domains: ## Download the latest list of disposable email domains into the backend (review and commit the result)
	curl -fsSL --max-time 60 -o "$(DISPOSABLE_DOMAINS_FILE).download" "$(DISPOSABLE_DOMAINS_URL)"
	@count=$$(grep -c . "$(DISPOSABLE_DOMAINS_FILE).download"); \
	if [[ "$$count" -lt 1000 ]]; then echo "the download has only $$count lines; the list was not replaced"; exit 1; fi
	mv "$(DISPOSABLE_DOMAINS_FILE).download" "$(DISPOSABLE_DOMAINS_FILE)"
	@echo "$$(grep -c . $(DISPOSABLE_DOMAINS_FILE)) domains in $(DISPOSABLE_DOMAINS_FILE)"

api: ## Run the API server (needs make up; settings from .env)
	cd backend && go run ./cmd/api

web: ## Run the web client dev server on port 3000 (needs npm ci in frontend/)
	cd frontend && npm run dev

stop: ## Stop this project's API (port 8080) and Next.js dev servers (3000-3002); leaves Docker running
	@scripts/stop-dev.sh

web-clean: ## Delete the web client build cache (frontend/.next); stop make web first
	rm -rf frontend/.next

# The AI service (ai/, AI-1). The image holds no model: the first start downloads it into the
# aimodels volume (about 1.1 GB, pinned revision), or ai-fetch-model does it ahead of time.
ai-build: ## Build the AI service image (CPU only, without the model)
	docker compose build ai

ai-up: ## Start the AI service and wait until its model is loaded (the first start downloads it)
	docker compose up -d --wait --wait-timeout 900 ai

ai-fetch-model: ## Download the AI model at its pinned revision into the aimodels volume, then exit
	docker compose run --rm --no-deps ai python -m ai_service fetch

ai-logs: ## Follow the AI service's log (Ctrl+C to stop)
	docker compose logs -f ai

# The sync server (sync/, slice 22, docs/sync.md). It needs its packages (cd sync && npm ci),
# Redis (make up) and the API's internal listener (make api with SYNC_SERVICE_TOKEN set).
sync: ## Run the real-time editing server on SYNC_PORT (8091); needs make up, make api and SYNC_SERVICE_TOKEN
	cd sync && node src/main.ts

sync-build: ## Build the sync server's image
	docker compose --profile sync build sync

seconds ?= 60
sync-load: ## Measure NFR-2 and NFR-10.1: 10 files x 4 simulated editors typing for seconds=60 (needs make up; empties TEST_REDIS_URL)
	cd sync && LOAD_SECONDS="$(seconds)" node scripts/load.ts

compile-image: ## Build the compile sandbox image (TeX Live full, pinned; about 9 GB on disk, the first pull downloads 2.8 GB)
	docker build -t "$(COMPILE_IMAGE)" compile/image
	@docker image inspect "$(COMPILE_IMAGE)" --format 'Built $(COMPILE_IMAGE): {{.Id}}, {{.Size}} bytes'

compile-worker: ## Run the compile worker on this host with your Docker (needs make compile-image, make api and COMPILE_SERVICE_TOKEN)
	cd compile && go run ./cmd/worker

compile-trace: ## Trace one project folder in the sandbox (dev only): every path refused, every write outside /work and /tmp; dir=<folder> [main=main.tex] [out=<folder>]
	@test -n "$(dir)" || { echo "Usage: make compile-trace dir=<project folder> [main=main.tex] [out=<folder for the raw trace>]"; exit 1; }
	docker build -q --build-arg BASE="$(COMPILE_IMAGE)" -t researchsphere-compile:debug compile/image/debug >/dev/null
	cd compile && go run ./cmd/trace -dir "$(abspath $(dir))" -main "$(or $(main),main.tex)" $(if $(out),-out "$(abspath $(out))",)

bench-compile: ## Measure NFR-3: compile the 10-page sample paper in the real sandbox runs=5 times (needs make compile-image)
	cd compile && go run ./cmd/bench -runs "$(or $(runs),5)"

generate: ## Regenerate Go code (oapi-codegen, sqlc) and TypeScript types
	cd backend && go generate ./...
	cd frontend && npm run generate

GENERATED := backend/internal/api backend/internal/compile/adapter/postgres/sqlcgen backend/internal/auth/adapter/postgres/sqlcgen backend/internal/profile/adapter/postgres/sqlcgen backend/internal/media/adapter/postgres/sqlcgen backend/internal/notification/adapter/postgres/sqlcgen backend/internal/moderation/adapter/postgres/sqlcgen backend/internal/event/adapter/postgres/sqlcgen frontend/src/lib/api

check-generated: generate ## Fail if generated code is out of date
	@if [[ -n "$$(git status --porcelain -- $(GENERATED))" ]]; then \
		git status --short -- $(GENERATED); \
		echo "Generated code is out of date: run make generate and commit the result."; \
		exit 1; \
	fi

test: test-prepare test-db test-go test-integration test-web test-ai test-sync test-compile ## Run all tests (needs make up)

# The database name reaches psql as a variable (:'db'), never pasted into the SQL.
test-prepare: ## Create the test database (TEST_POSTGRES_DB) if missing and apply all migrations to it
	@if [[ "$(TEST_POSTGRES_DB)" != *_test ]]; then echo "TEST_POSTGRES_DB=$(TEST_POSTGRES_DB) must end in _test"; exit 1; fi
	@exists=$$(echo "SELECT 1 FROM pg_database WHERE datname = :'db'" | \
		docker compose exec -T postgres psql -U $(POSTGRES_USER) -d postgres -X -tA -v ON_ERROR_STOP=1 -v db="$(TEST_POSTGRES_DB)"); \
	if [[ "$$exists" != 1 ]]; then \
		echo "Creating database $(TEST_POSTGRES_DB)"; \
		docker compose exec -T postgres createdb -U $(POSTGRES_USER) "$(TEST_POSTGRES_DB)"; \
	fi
	migrate -path $(MIGRATIONS_DIR) -database "$(TEST_DATABASE_URL)" up

test-db: test-prepare ## Run the database constraint tests on the test database (needs make up)
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(TEST_POSTGRES_DB) -X -v ON_ERROR_STOP=1 < backend/db/tests/constraints.sql

test-go: ## Run the backend Go tests
	cd backend && go test -race -count=1 ./...

# -p 1: the packages share the test database and each test empties it, so they run one
# package at a time.
test-integration: test-prepare ## Run the Go repository and end-to-end tests against the test database, test Redis DB and MinIO (needs make up)
	cd backend && go test -race -count=1 -p 1 -tags=integration ./internal/... ./cmd/...

# One integration test, set up as test-integration is. run is a go test -run pattern; pkg is
# relative to backend/, for example ./internal/messaging/adapter/postgres/.
count ?= 1
test-one: test-prepare ## Run one integration test: make test-one pkg=./internal/x/ run=TestName [count=1]
	@if [[ -z "$(pkg)" || -z "$(run)" ]]; then echo "usage: make test-one pkg=./internal/x/ run=TestName [count=1]" >&2; exit 2; fi
	cd backend && go test -race -count="$(count)" -p 1 -tags=integration -run "$(run)" "$(pkg)"

test-web: ## Run the frontend tests (Jest)
	cd frontend && npm test

lint: lint-go lint-web lint-ai lint-sync lint-compile ## Run all linters and the frontend and sync type checks

lint-go: ## Run golangci-lint for the backend
	cd backend && golangci-lint run ./...

lint-web: ## Run ESLint and the TypeScript type check for the frontend
	cd frontend && npm run lint && npm run typecheck

# The AI targets use the stub model: they install neither torch nor the model (needs uv).
test-ai: ## Run the AI service tests with a stub model (no download)
	cd ai && uv sync --locked && uv run pytest

lint-ai: ## Run ruff (lint and format check) for the AI service
	cd ai && uv sync --locked && uv run ruff check . && uv run ruff format --check .

# The sync tests run real Hocuspocus servers and clients against Redis DB TEST_REDIS_URL (never
# DB 0 or REDIS_URL's), which they empty, and a fake of the API's internal operations.
test-sync: ## Run the sync server tests (needs make up and cd sync && npm ci)
	cd sync && npm test

test-compile: ## Run the compile worker's tests (a fake Docker daemon; no image needed)
	cd compile && go test -race ./...

test-compile-sandbox: ## Run the compile sandbox tests against real Docker and the image (NFR-4, FR-13.5; needs make compile-image; about 90 seconds)
	cd compile && go test -race -count=1 -tags sandbox -timeout 10m ./internal/sandbox/

lint-compile: ## Run golangci-lint for the compile worker
	cd compile && golangci-lint run ./...

lint-sync: ## Run ESLint and the TypeScript type check for the sync server
	cd sync && npm run lint && npm run typecheck
