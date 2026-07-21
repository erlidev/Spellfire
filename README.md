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

See [the architecture](./docs/architecture.md), [game design](./docs/gdd.md), and [user-facing specification](./docs/user-facing-specification.md).
