# SpellFire

A playable foundation for a 2D top-down open-world combat MMORPG in the `.io` tradition. The client is framework-free TypeScript rendered with Pixi.js; the backend is Go with an authoritative fixed-tick world, binary Protobuf WebSockets, SQLite accounts, client prediction, interpolation, and server rewind.

## Run locally

Requirements: Go 1.22+, Node 20+, and npm.

```sh
npm install
npm run build
go run ./server/cmd/spellfire
```

Open `http://localhost:8080`, register, create a Gunslinger or Mage, and enter the world. For frontend hot reload, run `go run ./server/cmd/spellfire` and `npm run dev` in separate terminals, then open Vite’s URL.

```sh
make test   # backend tests, frontend tests, strict TypeScript
make build  # production frontend and Go binary
```

## Run with Docker

Requirements: Docker with Compose v2. No local Go/Node toolchain needed — the image builds the client and server itself.

```sh
docker network create proxy   # once, if it doesn't exist
docker compose up --build -d
```

The container publishes no host port; it joins the external `proxy` network and exposes port 8080 there, so a reverse proxy on the same network reaches it at `http://spellfire:8080` (forward both HTTP and the `/ws` WebSocket upgrade). To run standalone without a proxy, add a `ports:` mapping to the service in `compose.yaml`. Account and character data persists in the `spellfire-data` volume; simulation tuning is configurable via the environment variables in `compose.yaml`. Stop with `docker compose down` (add `-v` to also drop the database volume).

See [the architecture](./docs/architecture.md), [game design](./docs/game/design/README.md), and [user-facing specification](./docs/game/ui/README.md).
