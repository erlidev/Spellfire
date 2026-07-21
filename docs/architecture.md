# SpellFire Architecture

SpellFire is a playable browser-based multiplayer foundation: a Pixi.js/TypeScript client connects to a server-authoritative Go simulation over binary WebSockets. Account and character metadata is persisted in SQLite. All in-world visuals are built at runtime with Pixi geometry primitives; there are no bitmap assets in the play space.

This document describes what is implemented in version 0.1. [`game/design/`](game/design/README.md) is authoritative for game rules, and [`game/ui/`](game/ui/README.md) is authoritative for player-facing presentation.

## Repository layout

```text
proto/game.proto                    Canonical WebSocket schema
data/tuning/                        Versioned balance tables, read by both server and client
data/data.go                        Embeds the tables into the Go binary
server/cmd/spellfire/               Process entry point and static-client hosting
server/internal/api/                JSON account and character HTTP API
server/internal/auth/               Password hashing, opaque sessions, authentication
server/internal/config/             Environment configuration
server/internal/game/               Fixed-tick world, physics, combat, AOI, rewind
server/internal/model/              Persistent domain types
server/internal/protocol/           Protobuf wire codec
server/internal/store/              Persistence interface and SQLite implementation
server/internal/transport/          WebSocket lifecycle and origin enforcement
server/internal/tuning/             Tuning-table schema, loader, and validation
web/src/api.ts                      Typed account/character API client
web/src/tuning.ts                   Client-side view of the same tuning tables
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
- `GET /api/version` returns `{time, commit}` describing how the running binary was built. The values are stamped at link time via `-ldflags -X spellfire/server/internal/build.{Time,Commit}` (the `make build` target and the Dockerfile set these; the Docker commit comes from the `BUILD_COMMIT` build arg). For an unstamped local `go build`/`go run` in the checkout, `build.Get` falls back to Go's embedded VCS metadata. The home screen fetches this and shows the build age, so a stale deployment is visible at a glance.
- The raw session token is shown only once to the client. SQLite stores a SHA-256 digest, so a database disclosure does not directly disclose live bearer tokens. The browser keeps the token in `sessionStorage`, preserving refreshes in the current tab without creating an indefinitely persistent browser credential.
- A WebSocket must send a Protobuf `JOIN` as its first binary message. The server authenticates its token and verifies character ownership before inserting the player into the world.
- A later connection for the same character replaces the active transport. The old connection cannot delete the replacement when it eventually closes.
- An account gets one body in the world at a time. `Engine.Join` refuses a join with `ErrAccountInWorld` when the account already occupies the world through a *different* character, and the socket answers with an error and closes. See [one body per account](#one-body-per-account).

Production deployment must terminate TLS in front of the process, rate-limit authentication endpoints, back up the database, and set an explicit trusted-origin policy if the frontend and backend are split across hostnames. Email verification and password recovery are not present in this rudimentary account implementation.

## Persistence and migration

Two independent versions govern a save, and neither one is the tuning tables'.

`PRAGMA user_version` records how far the *database schema* has been migrated. `store.migrations` is an append-only list whose index+1 is the version it leaves behind; opening a database applies every migration it has not seen, each inside a transaction with its own version bump, so an interrupted upgrade is never recorded as complete. A database written by a newer build is refused rather than downgraded. A v0.1 database predates the version counter, reports 0, and migrates forward in place because migration 1 is written idempotently.

`characters.schema_version` records the *record* shape, which changes independently of the table layout. `model.Character.Migrate` carries an older row forward through sequential steps inside the store's scan path, so nothing above the store ever sees an older shape, and `SaveCharacterState` stamps the current version when the record is next written. Version 1 is the original name/class/level/xp record; version 2 adds saved world position, carried materials, and unlocked outposts; version 3 adds the last-seen stamp that decides whether that position is still honoured. A record from a newer build is an error, because writing it back would truncate fields this build cannot see.

A character keeps its world position, carried raw materials, and unlocked outposts across a disconnect: position in nullable scalar columns (unplaced and placed-at-the-origin are different states), materials and outposts as JSON on the same row, since the access pattern is always whole-character. Crafted items live in `crafted_items` as a blueprint ID plus a slot → component ID map — references only, never a stat snapshot, so a balance edit retunes every owned item in place. Crafting itself arrives in Phase 2.3; the record contract does not wait for it.

### Logout, linger, and recall

Dropping the connection does not remove the body. `Engine.Leave` unregisters the client and starts a logout window — `session.logout_linger_seconds`, 10 as shipped — during which the body stands in the world: it takes damage and can be killed, but it cannot move, dash, or fire, because `stepPlayer` treats a lingering player the way it treats a dead one. Disconnecting is therefore not an escape from a fight. Reconnecting inside the window resumes that same body wherever the fight has since moved it, which doubles as the reconnect path. Resuming clears the body's input state: the input sequence numbers the connection, not the character, so a fresh client counting from one again would otherwise have every input rejected as stale by `ApplyInput` and every predicted input discarded against a stale acknowledgement. Only a living body is resumed — a body killed inside its window is dropped on reconnect and the character enters at the hub, so being killed while logged out costs the position exactly as it would had the window closed first and the unplaced save decided the entry. When the window closes, the tick loop reaps the body, saves its final position, and removes it.

### One body per account

An account may have only one character in the world. `World` indexes bodies by account — `occupants`, maintained by `AddPlayer` and `RemovePlayer` — and `Engine.Join` refuses with `ErrAccountInWorld` when that index already holds a character other than the one joining. Rejoining as the *same* character is untouched: that is the reconnect and replacement path above.

The occupancy follows the body, not the connection, so it outlasts a disconnect for the length of the logout window. This is deliberate. The alternative — evicting the other character on demand — would hand every player a combat-log escape: drop the connection mid-fight, join a second character, and the body that was supposed to stand there for ten seconds disappears. Refusing the join instead makes the linger window the only way out, and the cost is bounded by that same window: after a normal exit, the second character can enter as soon as the first body is reaped. The refusal reaches the player as the transport's `SERVER_ERROR`, which the client shows on its terminal connection overlay with the return-home action.

Characters with no account ID are never indexed, so the simulation tests, which build characters without one, do not contend for a single shared slot.

A saved position is honoured only while it is fresh. Every save is stamped, and on join a stamp older than `session.position_expiry_seconds` — 30 minutes as shipped — stops being trusted: the character is recalled to the safe fixture nearest to where it logged out, choosing between its unlocked outposts and the central hub. `data/tuning/outposts.json` holds those positions and ships empty, so today every recall resolves to the hub; Phase 3 adds rows, not structure. An unstamped position (a record migrated from schema version 2) counts as expired, because there is no way to tell how old it is. A position is also refused when the world can no longer accept it — outside the rim, or inside cover that did not exist when it was written — and falls back to the deterministic hub spawn ring.

The engine owns save timing through the narrow `game.Persister` interface, so the simulation still has no database dependency. Writes leave the tick loop through a buffered channel into a single writer goroutine, so a slow database can never delay a tick; an overflowing queue writes on its own goroutine rather than dropping the save. Saves happen when a logout window closes, every 15 seconds for everyone present, and once more for everyone present at shutdown, lingering bodies included. A connection that has already been replaced touches nothing, or it would strand or overwrite the live session. A dead player is saved unplaced and re-enters at the hub, matching the current instant respawn until Phase 4.2 replaces both.

Because a save stores references, content can only be withdrawn, never deleted. `data/tuning/retired.json` maps every retired ID to a live replacement or a material refund, validated for dangling and circular chains at load, and `Tables.Resolve` follows it. Carried materials pass through it on join, so a retired material arrives as its replacement or its refund instead of vanishing. The client does not bundle this table: retirement resolves persisted references, and the client holds none.

## Simulation and world rules

`game.World` owns all mutable gameplay state and advances on a fixed 60 Hz tick. `game.Engine` serializes joins, leaves, inputs, respawns, and simulation steps with a mutex. The default world is a 3,000-unit-radius contiguous circle with a 430-unit central service-safe hub, a PvP-protected fringe out to 1,000 units, and deterministic procedural trees outside the hub.

Implemented authoritative rules include:

- normalized eight-direction movement at a capped speed;
- a timed dash that carries the player at high speed along the direction locked in at the press (movement input, or aim when standing still) for a whole number of ticks, overriding steering for its duration and colliding normally;
- circular player/world bounds and static circular tree collision, resolved independently on each axis;
- server-authoritative aim, universal dash, dash cooldown, health, Mage mana/regen, Gunslinger magazine/reload, fire cadence, projectile lifetime, damage, death, and central-hub respawn;
- projectile-only starter attacks for both classes: a fast narrow bullet and a larger, slower fireball;
- approximately three-second uncontested raw TTK from the shared 100-health/10-damage/300-ms-cadence tuning band;
- no PvP damage inside the protected hub/fringe, while projectiles may still resolve visibly;
- deterministic ordering for player and projectile processing and deterministic tree generation.

Balance values live in the [tuning tables](#tuning-tables), never in the simulation source. `game.Tuning` is a derived runtime view of them: `game.FromTables` fills it, and only the process-level rates (tick, send, AOI, rewind) are overridden afterwards from configuration. Persistent records store references and progression fields, not computed combat values, matching the [progression persistence contract](game/design/progression-and-crafting.md#persistence-and-versioning).

## Tuning tables

`data/tuning/*.json` holds every balance number as versioned data. The Go server embeds the directory through `data/data.go` and parses it in `server/internal/tuning`; the Vite client imports the same files from `web/src/tuning.ts`. One directory, two consumers, no duplicated literal — a balance edit moves the authoritative simulation, client prediction, and the renderer together. `data/tuning/README.md` documents the file-by-file schema and the editing rules.

The tables cover simulation rates, session windows, world geometry and danger bands, the player body and universal dash, damage bands, elements, weapons, spells, blueprint/component layouts, material grades, mobs, biomes, outposts, and retired IDs. Rows exist only where a design document has settled the content: the two starter items are live, while component, material, and biome-placement rows arrive with the phases that consume them.

Damage is the clearest expression of the contract. No weapon or spell carries a damage number; each points at a `combat.damage_bands` row, and a staff carries no combat numbers at all — it delegates cadence, cost, damage band, and projectile to the spell it casts. Editing `damage_bands.standard.damage_per_hit` therefore retunes both classes at once, and a persisted character — which stores only IDs — needs no migration. `server/internal/game` has a test that replays stored character records against an edited copy of the tables to hold that invariant.

Loading validates rather than trusts. Unknown JSON fields are rejected, every cross-table reference is resolved, danger bands must run outward from the hub to the rim with contiguous PvP protection, projectile kinds must be unique across tables so the renderer can resolve a silhouette from a snapshot alone, and every damaging row must declare both a shared damage band and a recognised dodge vector — a projectile with zero travel speed is rejected as instant point-and-click damage. All problems are reported at once. `manifest.json` carries a content `version`, bumped on any balance edit and intended to drive the global respec/refund, and a `schema_version` that must match `tuning.SchemaVersion`; a mismatch fails the load with a request for the forward migration.

Because the tables are bundled into the client at build time, the server and the client must ship from the same build — which `make build` and the Dockerfile both do. Delivering tables to an already-running client, so that simulation constants can move without redeploying, is the separate versioned welcome/tuning message tracked in [`TODO.md`](../TODO.md) Phase 8.

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

The client applies local movement and dash immediately at the same fixed rate as the server and retains each input sequence plus its predicted motion. Because the dash is spread over a fixed tick count instead of a single displacement, the client counts down the same dash ticks at 60 Hz and records one motion per input, so replay reproduces the server path exactly. Every authoritative local-player snapshot carries `acknowledged_input`. The client resets to that position, removes acknowledged motions, and replays the remainder through the same circle/tree collision rules. New AOI colliders are merged before reconciliation.

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

This preserves the [projectile dodge requirement](game/design/invariants.md) while compensating for reasonable network delay. It does not turn ordinary weapons into hitscan. Equal-timestamp history samples prefer the latest state, which is important for respawns and authoritative repositioning.

### Backpressure and connection lifecycle

Each client has a two-message outbound queue. If a slow client fills it, an older snapshot is discarded for the newer authoritative state rather than blocking the world tick. WebSocket reads are limited to 2 KiB, joins have a deadline, ping/pong detects dead peers, writes have deadlines, and reconnect attempts are exponentially backed off and capped at five. During reconnect the rendered world remains visible under an input-blocking overlay.

## Rendering and interface

Pixi.js is the committed 2D backend. Its WebGL batching of `Graphics` primitives covers the 100-player density target, and the design's [procedural visual language](game/design/visual-direction.md) needs no texture pipeline, so nothing in the effect ceiling argues for a heavier engine. The renderer stays behind `view.ts`, so this is a replaceable dependency rather than an architectural commitment.

Pixi owns only the play space; DOM elements own forms, dialogs, HUD, status, and accessibility semantics. The world renderer uses `Graphics` geometry for the grid, safety/world rings, angular Gunslingers, round Mages, held procedural guns/staffs, projectiles, health bars, names, and collidable trees. Fill communicates class/element identity while outlines communicate self/hostility, with silhouette providing the required redundant non-color channel. Because the server serves a strict `Content-Security-Policy` with `script-src 'self'` (no `unsafe-eval`), `view.ts` imports `pixi.js/unsafe-eval` for its side effect, which swaps Pixi's runtime code generation for eval-free polyfills before the renderer is created.

Desktop controls are WASD/arrows, pointer aim/fire, Space dash, and R reload. Touch-first layouts expose directional, fire, and dash buttons in safe-area-aware thumb zones. The HUD exposes health, class resource, danger/PvP state, actions, latency, reconnect state, death, and respawn. Home authentication and character creation remain in modal context. The in-game menu explicitly states that the world does not pause.

## Configuration

| Environment variable | Default | Meaning |
|---|---|---|
| `SPELLFIRE_ADDRESS` | `:8080` | HTTP/WebSocket listen address |
| `SPELLFIRE_DATABASE` | `spellfire.db` | SQLite path; use `:memory:` only for one-connection tests |
| `SPELLFIRE_WEB_ROOT` | `dist` | Built frontend directory |
| `SPELLFIRE_TICK_RATE` | `tick_rate` | Authoritative ticks per second |
| `SPELLFIRE_SEND_RATE` | `send_rate` | Snapshots per second |
| `SPELLFIRE_AOI_RADIUS` | `aoi_radius` | Per-player interest radius |
| `SPELLFIRE_MAX_REWIND_MS` | `max_rewind_ms` | Maximum accepted lag-compensation age |

The last four default to the matching field in `data/tuning/simulation.json` (60, 20, 1200, and 200 as shipped), so an unset environment reproduces exactly what the client bundled. Tick and send rates must remain evenly compatible for predictable snapshot pacing; the loader rejects a table whose tick rate is not a whole multiple of its send rate. Overriding either through the environment moves the server away from the client's compiled-in prediction constants, so production should expose those values through a versioned welcome/tuning message before making them live-configurable.

## Container deployment

`Dockerfile` is a three-stage build: a `node:20-alpine` stage runs `npm ci && npm run build` to produce the client bundle, a `golang:1.22-alpine` stage compiles the server with `CGO_ENABLED=0` (the SQLite driver is pure Go, so no C toolchain is needed), and an `alpine:3.20` runtime stage carries only the static binary and the built assets. The result runs as a non-root user (`spellfire`, uid 10001) and is ~21 MB.

Inside the image the binary is `/app/spellfire-server`, static assets live at `/app/web` (`SPELLFIRE_WEB_ROOT`), and the SQLite database is written to `/data/spellfire.db` (`SPELLFIRE_DATABASE`) on a mounted volume so account/character data survives container replacement. A `HEALTHCHECK` polls `/api/health`. The build stage stamps the build timestamp from the build host's clock and, if the `BUILD_COMMIT` arg is supplied, the git commit; pass it with `BUILD_COMMIT=$(git rev-parse --short HEAD) docker compose build` (the `.git` directory is not in the build context, so the commit cannot be read inside the image).

`compose.yaml` builds the image, persists `/data` in the `spellfire-data` named volume, and exposes the tuning environment variables (overridable through a sibling `.env` file). Run with `docker compose up --build -d`.

The service publishes no host port. It attaches to an external Docker network named `proxy` (create it once with `docker network create proxy`) and only `expose`s port 8080 on that network, so a reverse proxy sharing the `proxy` network reaches the server at `http://spellfire:8080`. The proxy must forward both plain HTTP (`/`, `/api/...`) and WebSocket upgrades (`/ws`) to that address. To run standalone without a proxy, add a `ports:` mapping back to the service.

## Testing and verification

Backend tests cover SQLite constraints and account isolation, forward migration of a pre-1.2 database and refusal of a newer one, character-state and crafted-item round trips, saved-position restore and its fallbacks, retirement resolution for carried materials, position expiry and nearest-destination recall, the logout linger's inability to act and continued vulnerability, reconnect resuming the same body and accepting the new connection's restarted input sequence, reconnect to a body killed while logged out entering at the hub, reap-and-save at window close, shutdown flushing, engine autosave behaviour including a replaced connection and a failing writer, an end-to-end disconnect that returns a player to where they left off and leaves a body behind, password/session lifecycle, HTTP validation and non-disclosing credential errors, Protobuf parsing and unknown/truncated fields, fixed-step movement, normalized diagonals, tree/world collision, dash edges/cooldowns, PvP protection, projectile damage, rewind, death/respawn, resources/reload, AOI, engine replacement/backpressure behavior, WebSocket origin rules, tuning-table validation and rejection cases, the starter kit's raw time-to-kill against its declared band, and the one-row-rebalances-everything invariant. Frontend tests cover wire compatibility, malformed frames, movement/dash prediction, reconciliation replay, predicted tree collision, and the client's derivation of radii, starter items, resources, and projectile silhouettes from the shared tables.

Run `make test` for Go tests, frontend tests, and strict TypeScript checking. Run `make build` to produce `dist/` and compile the Go server.

## Deliberate initial limitations

The foundation implements the requested accounts, multiplayer transport, authoritative combat/netcode, both player classes, procedural world, and static collidable trees. The following GDD systems are represented in UI/reference language but are not falsely presented as functional: crafting recipes and safe-zone loadout mutation, harvesting/material persistence and death drops, squads and squad loot, Sentry mobs, outposts/travel, marketplace, and world bosses. Their rules or values remain open in the source specifications, and each should be added as a separate authoritative module rather than embedded in `World`.

Other known limits are one world process, no spatial index beyond AOI distance checks, and no distributed session cache. The logout linger closes the combat-logging hole, but a lingering body is not marked as such on the wire, so an attacker sees a motionless player rather than one that is visibly logging out; the protocol field belongs with the Phase 1.5 per-entity state expansion, and the surrounding exit and reconnect UX with Phase 7. The current O(players + projectiles + colliders) per-client snapshot path is appropriate for development and modest concurrency; reaching the [100+ player design target](game/design/world.md) under battle density requires load testing, a spatial hash/quadtree, snapshot deltas, and bandwidth budgets before production claims are made.
