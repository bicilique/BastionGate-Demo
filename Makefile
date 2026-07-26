.PHONY: build run test test-go test-web test-e2e test-live verify docker-build docker-config

build:
	go build -trimpath -o bin/acme-people ./cmd/acme-people

run:
	go run ./cmd/acme-people

test: test-go test-web

test-go:
	go test ./... -count=1

test-web:
	npm test

test-e2e:
	npm run test:e2e

test-live:
	BASTIONGATE_LIVE_TEST=1 go test ./internal/live -count=1 -v

verify:
	gofmt -w cmd/acme-people/main.go internal/app/*.go web/embed.go
	go vet ./...
	go test ./... -count=1
	npm test

docker-build:
	docker build -t bastiongate/acme-people-demo:local .

docker-config:
	docker compose config --quiet
