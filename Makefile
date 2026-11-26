VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all web build test run docker clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/datafluxor-seo ./cmd/datafluxor-seo

test:
	go vet ./...
	go test ./...
	cd web && npm run typecheck

run: all
	./bin/datafluxor-seo

docker:
	docker build --build-arg VERSION=$(VERSION) -t datafluxor-seo:$(VERSION) .

clean:
	rm -rf bin
	find internal/server/dist -mindepth 1 ! -name .gitkeep -delete
