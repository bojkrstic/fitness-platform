.PHONY: dev local up build build-linux test docker-build docker-push clean

GOFLAGS ?= -mod=mod
GOCACHE ?= /tmp/gocache
DOCKER_IMAGE ?= fitness-platform
DOCKER_TAG ?= latest

dev:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go run ./cmd/fitness-platform

local: dev

up:
	@docker compose up --build

build:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go build -buildvcs=false -o fitnes-api ./cmd/fitness-platform

build-linux:
	@GOOS=linux GOARCH=amd64 GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go build -buildvcs=false -o fitnes-api ./cmd/fitness-platform

test:
	@GOCACHE=$(GOCACHE) GOFLAGS=$(GOFLAGS) go test -buildvcs=false ./...

docker-build:
	@docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-push: docker-build
	@docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

clean:
	@rm -f fitnes-api
