.PHONY: dev server client test build

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
	go build -o dist/spellfire-server ./server/cmd/spellfire
