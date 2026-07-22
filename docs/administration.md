# Administration

SpellFire supports administrator accounts and a server-authoritative developer
mode for privileged world inspection and fixture placement. Each future
privileged HTTP feature must opt into the server's admin authorization wrapper.

## Configure administrators

Edit [`data/tuning/admins.json`](../data/tuning/admins.json):

```json
{
  "emails": [
    "operator@example.com"
  ]
}
```

The identifiers are account emails. Matching trims surrounding whitespace and
ignores case, exactly like registration and login. The loader rejects malformed
or duplicate emails. An entry may be added before its account is registered;
it becomes effective when an account with that normalized email exists.

Tuning files are embedded in the Go binary, so changing the list requires a new
build and server restart. Existing sessions do not need to be revoked: the
server derives the role from the stored account email on every authenticated
HTTP request, so they receive the new authorization state after restart.

## Authorization contract

- Registration and login responses contain the authenticated account's `email`
  and `is_admin` status alongside the opaque session token.
- `GET /api/account` returns the same account view for a current session.
- The Home screen identifies administrators, but that display is informational.
- The server's admin wrapper returns `401 Unauthorized` without a valid session
  and `403 Forbidden` for an authenticated non-admin account.
- Privileged handlers must use the wrapper. They must never accept an
  `is_admin`, role, or email claim from request JSON or the browser.

## Developer mode

Administrators see an **Admin** tab in the floating Field menu. Four pointer
modes are available while ordinary movement remains active:

- **Off** restores primary fire.
- **Spawn** repeatedly places the selected spawnable archetype.
- **Select** targets any visible entity and loads its live editable values.
- **Delete** forces the targeted entity through graceful removal.

The catalog and form schemas live on the archetypes in
[`data/tuning/entities.json`](../data/tuning/entities.json). `admin.spawnable`
controls catalog visibility. Each field declares a stable
`component.attribute` binding, a `spawn`, `edit`, or `both` scope, and a
number/text/select/position/rotation input definition. `transform.position` is
one `[x,y]` vector rendered as adjacent axis inputs plus a pointer-based world
picker. `transform.heading_degrees` uses an angle slider and rotating indicator
rather than a freeform number box. Numeric HTML controls intentionally carry no
browser min/max/step attributes; tuning bounds remain authoritative on the
server. The browser renders this metadata without attribute-specific UI code;
the server validates it and uses
an explicit attribute adapter registry. Adding a field requires tuning plus a
registry adapter, and the adapter is the only layer that must change when
runtime storage moves to ECS components.

Spawn currently supports player, projectile, telegraph, tree, and wall
archetypes. Selection can inspect and edit any of those families, including a
connected player. Position is checked against world bounds as a complete pair
before mutation. Speed and view distance remain temporary in-memory player
values; view distance changes that player's AOI and can increase snapshot size.

Delete is idempotent. A target immediately leaves gameplay/collision and fades
over 350 ms, remains for a short snapshot-delivery grace, then its non-player
store reaps it. Connected players are not removed from the session: they enter
the existing dead state, fade, and can respawn normally. Disposable admin
players are reaped after fading.

Material grants remain available for any live material. Their count is bounded
by `materials.admin_grant`; grants are persisted inventory and therefore a real
economy mutation, unlike temporary entity overrides.

Spawned training players are non-persistent fixtures: they are visible,
collidable, and damageable like a player but never occupy an account slot or
become a saved character. Developer overrides reset when the administrator's
body leaves the world. Granted materials do not: they are ordinary carried
inventory from the moment they land, persisted and spendable like any other, so
a grant on a live account is a real economy change rather than a fixture. The
HTTP commands are `POST /api/admin/spawn`, `POST /api/admin/entity/inspect`,
`/edit`, `/delete`, and `POST /api/admin/materials`; the legacy
`POST /api/admin/attributes` remains compatible. Each verifies the
session's admin role and that the selected in-world character belongs to that
account.

The admin list is configuration, not a secret. Do not put passwords, session
tokens, API keys, or other credentials in the tuning file.
