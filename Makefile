.PHONY: dev up build build-linux test clean

GOFLAGS ?= -mod=mod
GOCACHE ?= /tmp/gocache

dev:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go run ./cmd/fitness-platform

up:
	@docker compose up --build

build:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go build -buildvcs=false -o fitnes-api ./cmd/fitness-platform

build-linux:
	@GOOS=linux GOARCH=amd64 GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go build -buildvcs=false -o fitnes-api ./cmd/fitness-platform

test:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go test -buildvcs=false ./...

clean:
	@rm -f fitnes-api
