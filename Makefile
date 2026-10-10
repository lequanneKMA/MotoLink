.PHONY: build test test-int lint up down infra migrate tidy
SERVER=server

build:   ; cd $(SERVER) && go build ./...
test:    ; cd $(SERVER) && go test ./... -race -count=1
# Needs a Postgres where the user can CREATE DATABASE, e.g. the compose one.
test-int: ; cd $(SERVER) && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://motolink:motolink_dev_password@localhost:5432/postgres?sslmode=disable} go test ./... -race -count=1
lint:    ; cd $(SERVER) && golangci-lint run ./... && go vet ./...
tidy:    ; cd $(SERVER) && go mod tidy
infra:   ; docker compose -f deploy/docker-compose.yml up -d postgres redis kafka kafka-init
up:      ; docker compose -f deploy/docker-compose.yml up -d --build
down:    ; docker compose -f deploy/docker-compose.yml down
migrate: ; cd $(SERVER) && go run ./cmd/server migrate up
