.PHONY: dev server client test build

BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_LDFLAGS := -X spellfire/server/internal/build.Time=$(BUILD_TIME) -X spellfire/server/internal/build.Commit=$(BUILD_COMMIT)

dev:
	npm run dev

server:
	go run ./server/cmd/spellfire

client:
	npm run dev

test:
	go test ./...
	npm test
	npm run check

build:
	npm run build
	go build -ldflags "$(BUILD_LDFLAGS)" -o dist/spellfire-server ./server/cmd/spellfire
