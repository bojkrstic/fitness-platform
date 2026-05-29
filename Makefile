.PHONY: dev up build test

dev:
	@go run .

up:
	@docker compose up --build

build:
	@go build -buildvcs=false ./...

test:
	@go test -buildvcs=false ./...
