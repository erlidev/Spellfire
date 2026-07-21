# SpellFire Architecture

SpellFire is a playable browser-based multiplayer foundation: a Pixi.js/TypeScript client connects to a server-authoritative Go simulation over binary WebSockets. Account and character metadata is persisted in SQLite. All in-world visuals are built at runtime with Pixi geometry primitives; there are no bitmap assets in the play space.

This document describes what is implemented in version 0.1. The GDD remains authoritative for game rules and `user-facing-specification.md` remains authoritative for presentation.

## Repository layout

```text
proto/game.proto                    Canonical WebSocket schema
server/cmd/spellfire/               Process entry point and static-client hosting
server/internal/api/                JSON account and character HTTP API
server/internal/auth/               Password hashing, opaque sessions, authentication
server/internal/config/             Environment configuration
server/internal/game/               Fixed-tick world, physics, combat, AOI, rewind
server/internal/model/              Persistent domain types
server/internal/protocol/           Protobuf wire codec
server/internal/store/              Persistence interface and SQLite implementation
server/internal/transport/          WebSocket lifecycle and origin enforcement
web/src/api.ts                      Typed account/character API client
web/src/net/                        Protobuf codec and reconnecting socket
web/src/game/prediction.ts          Local prediction and server reconciliation
web/src/game/view.ts                Procedural Pixi renderer and interpolation
web/src/main.ts                     Application state, inputs, HUD, and menus
web/src/style.css                   Responsive UI and touch-safe layout
```

Module dependencies point inward: transport calls authentication, persistence, and the game engine; the world simulation has no HTTP, WebSocket, database, or Pixi dependency. This keeps authoritative rules directly testable.

## Runtime topology

```text
Browser DOM UI ──JSON/HTTP──> account + character API ──> Store interface ──> SQLite
      │
      └─Pixi renderer ──binary Protobuf/WebSocket──> connection handler ──> game engine
                                                               │
                                                     authoritative World
```

The Go process serves the built SPA, API, WebSocket endpoint, and one shared world. SQLite was selected for the initial implementation because it has no external runtime dependency and gives deterministic integration tests. It is isolated behind `store.Store`; a PostgreSQL implementation can replace it without changing account, API, transport, or game logic. Horizontal world sharding is deliberately not claimed by this single-process foundation.

## Application and account flow

- `POST /api/auth/register` validates the email/password, hashes passwords with bcrypt, creates an account, and returns a new opaque session token.
- `POST /api/auth/login` returns the same generic failure for unknown accounts and incorrect passwords.
- `POST /api/auth/logout` revokes only the presented session.
- `GET|POST /api/characters` lists or creates account-owned characters. Names are 3–20 characters, both GDD classes are accepted, and the initial account limit is four characters.
- The raw session token is shown only once to the client. SQLite stores a SHA-256 digest, so a database disclosure does not directly disclose live bearer tokens. The browser keeps the token in `sessionStorage`, preserving refreshes in the current tab without creating an indefinitely persistent browser credential.
- A WebSocket must send a Protobuf `JOIN` as its first binary message. The server authenticates its token and verifies character ownership before inserting the player into the world.
- A later connection for the same character replaces the active transport. The old connection cannot delete the replacement when it eventually closes.

Production deployment must terminate TLS in front of the process, rate-limit authentication endpoints, back up the database, and set an explicit trusted-origin policy if the frontend and backend are split across hostnames. Email verification and password recovery are not present in this rudimentary account implementation.

## Simulation and world rules

`game.World` owns all mutable gameplay state and advances on a fixed 60 Hz tick. `game.Engine` serializes joins, leaves, inputs, respawns, and simulation steps with a mutex. The default world is a 3,000-unit-radius contiguous circle with a 430-unit central service-safe hub, a PvP-protected fringe out to 1,000 units, and deterministic procedural trees outside the hub.

Implemented authoritative rules include:

- normalized eight-direction movement at a capped speed;
- circular player/world bounds and static circular tree collision, resolved independently on each axis;
- server-authoritative aim, universal dash, dash cooldown, health, Mage mana/regen, Gunslinger magazine/reload, fire cadence, projectile lifetime, damage, death, and central-hub respawn;
- projectile-only starter attacks for both classes: a fast narrow bullet and a larger, slower fireball;
- approximately three-second uncontested raw TTK from the shared 100-health/10-damage/300-ms-cadence tuning band;
- no PvP damage inside the protected hub/fringe, while projectiles may still resolve visibly;
- deterministic ordering for player and projectile processing and deterministic tree generation.

Balance values live in `game.Tuning`; process-level rates and AOI/rewind values can be overridden through configuration. Persistent records store references and progression fields, not computed combat values, matching GDD §6.3.

## Network model

`proto/game.proto` is the protocol contract. The game socket accepts and emits only binary Protobuf frames; REST uses JSON because those low-frequency account surfaces benefit from ordinary HTTP semantics. The repository includes small schema-specific codecs in Go and TypeScript instead of requiring `protoc` at runtime. Golden-wire tests detect schema-number or wire-type drift.

Defaults:

| Concern | Value | Behavior |
|---|---:|---|
| Simulation rate | 60 Hz | Fixed authoritative physics and client prediction |
| Snapshot send rate | 20 Hz | One AOI-filtered snapshot per client every third tick |
| Client input rate | 60 Hz | Sequence-numbered current input state |
| Interpolation delay | 100 ms | Remote entities render between buffered snapshots |
| Rewind window | 200 ms | Client fire time is clamped to this server window |
| AOI radius | 1,200 units | Players, projectiles, and trees outside it are culled |

### Prediction and reconciliation

The client applies local movement and dash immediately at the same fixed rate as the server and retains each input sequence plus its predicted motion. Every authoritative local-player snapshot carries `acknowledged_input`. The client resets to that position, removes acknowledged motions, and replays the remainder through the same circle/tree collision rules. New AOI colliders are merged before reconciliation.

The server never accepts client position, health, damage, cooldown, ammo, or mana values. It accepts buttons, aim direction, input sequence, and client fire time only. Invalid/stale sequences are dropped.

### Entity interpolation

Remote players and projectiles retain up to eight received samples. Rendering occurs 100 ms behind receipt time and linearly interpolates position and aim between the two surrounding samples. The local actor uses its predicted position instead. Entities absent long enough to leave the AOI are destroyed client-side.

### Lag compensation and server rewind

The world retains a short timestamped position history for every player. When firing, the server:

1. clamps the claimed client time to `[server_now - max_rewind, server_now]`;
2. samples the shooter at that historical time;
3. creates the projectile at that historical muzzle position;
4. fast-forwards it in fixed substeps, testing against interpolated historical target positions and current static cover;
5. inserts a surviving projectile into the live simulation.

This preserves the GDD’s projectile dodge vector while compensating for reasonable network delay. It does not turn ordinary weapons into hitscan. Equal-timestamp history samples prefer the latest state, which is important for respawns and authoritative repositioning.

### Backpressure and connection lifecycle

Each client has a two-message outbound queue. If a slow client fills it, an older snapshot is discarded for the newer authoritative state rather than blocking the world tick. WebSocket reads are limited to 2 KiB, joins have a deadline, ping/pong detects dead peers, writes have deadlines, and reconnect attempts are exponentially backed off and capped at five. During reconnect the rendered world remains visible under an input-blocking overlay.

## Rendering and interface

Pixi owns only the play space; DOM elements own forms, dialogs, HUD, status, and accessibility semantics. The world renderer uses `Graphics` geometry for the grid, safety/world rings, angular Gunslingers, round Mages, held procedural guns/staffs, projectiles, health bars, names, and collidable trees. Fill communicates class/element identity while outlines communicate self/hostility, with silhouette providing the required redundant non-color channel.

Desktop controls are WASD/arrows, pointer aim/fire, Space dash, and R reload. Touch-first layouts expose directional, fire, and dash buttons in safe-area-aware thumb zones. The HUD exposes health, class resource, danger/PvP state, actions, latency, reconnect state, death, and respawn. Home authentication and character creation remain in modal context. The in-game menu explicitly states that the world does not pause.

## Configuration

| Environment variable | Default | Meaning |
|---|---|---|
| `SPELLFIRE_ADDRESS` | `:8080` | HTTP/WebSocket listen address |
| `SPELLFIRE_DATABASE` | `spellfire.db` | SQLite path; use `:memory:` only for one-connection tests |
| `SPELLFIRE_WEB_ROOT` | `dist` | Built frontend directory |
| `SPELLFIRE_TICK_RATE` | `60` | Authoritative ticks per second |
| `SPELLFIRE_SEND_RATE` | `20` | Snapshots per second |
| `SPELLFIRE_AOI_RADIUS` | `1200` | Per-player interest radius |
| `SPELLFIRE_MAX_REWIND_MS` | `200` | Maximum accepted lag-compensation age |

Tick and send rates must remain evenly compatible for predictable snapshot pacing. Changing simulation speed requires coordinating the client prediction constants; production should expose those values through a versioned welcome/tuning message before making them live-configurable.

## Testing and verification

Backend tests cover SQLite constraints and account isolation, password/session lifecycle, HTTP validation and non-disclosing credential errors, Protobuf parsing and unknown/truncated fields, fixed-step movement, normalized diagonals, tree/world collision, dash edges/cooldowns, PvP protection, projectile damage, rewind, death/respawn, resources/reload, AOI, engine replacement/backpressure behavior, and WebSocket origin rules. Frontend tests cover wire compatibility, malformed frames, movement/dash prediction, reconciliation replay, and predicted tree collision.

Run `make test` for Go tests, frontend tests, and strict TypeScript checking. Run `make build` to produce `dist/` and compile the Go server.

## Deliberate initial limitations

The foundation implements the requested accounts, multiplayer transport, authoritative combat/netcode, both player classes, procedural world, and static collidable trees. The following GDD systems are represented in UI/reference language but are not falsely presented as functional: crafting recipes and safe-zone loadout mutation, harvesting/material persistence and death drops, squads and squad loot, Sentry mobs, outposts/travel, marketplace, and world bosses. Their rules or values remain open in the source specifications, and each should be added as a separate authoritative module rather than embedded in `World`.

Other known limits are one world process, no spatial index beyond AOI distance checks, no database-backed character position, no combat-log grace period, and no distributed session cache. The current O(players + projectiles + colliders) per-client snapshot path is appropriate for development and modest concurrency; reaching the GDD’s 100+ player target under battle density requires load testing, a spatial hash/quadtree, snapshot deltas, and bandwidth budgets before production claims are made.
