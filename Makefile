.PHONY: up down migrate seed lint test test-db build check

up:
	docker compose build migrate
	docker compose up -d --wait

down:
	docker compose down

migrate:
	docker compose build migrate
	docker compose run --rm migrate

seed:
	@bash -ec 'read -rsp "Contraseña para demo (mínimo 12 bytes): " RETAIL_SEED_PASSWORD; printf "\n"; export RETAIL_SEED_PASSWORD; docker compose run --rm -e RETAIL_SEED_PASSWORD api seed-demo'

lint:
	cd apps/api && test -z "$$(gofmt -l cmd internal migrations)" && go vet ./...
	cd apps/web && npm run lint

test:
	cd apps/api && go test ./...
	cd apps/web && npm test

test-db:
	@test -n "$$TEST_DATABASE_URL" || (echo "Configurá TEST_DATABASE_URL" && exit 1)
	cd apps/api && go test -count=1 -v ./...

build:
	cd apps/api && go build ./...
	cd apps/web && npm run build

check: lint test build
