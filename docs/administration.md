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

Administrators see an **Admin** tab in the in-game Field menu. It provides a
searchable catalog, configuration fields for the selected item, and temporary
overrides for the administrator's connected character:

- Movement speed multiplier: 0.25–4×.
- View distance: 300–2,000 world units. This only changes that administrator's
  snapshot/AOI and can increase their received snapshot size.

Enable Developer Mode after configuring an item, close the menu, and click the
world to place it. While enabled, primary clicks place the selected item rather
than firing; the compact developer HUD names the active selection and can exit
the mode. Placement repeats until the mode is disabled.

The spawn catalog and panel schema live in
[`data/tuning/admin_tools.json`](../data/tuning/admin_tools.json). It currently
contains the entity families the world really runs: disposable training players,
projectiles, and telegraphs. Each catalog row declares its type, source class or
ability, optional element, and typed bounded fields. The browser renders the
same catalog but the server resolves the row, rejects unknown fields/out-of-range
values, checks the requested coordinate, and creates the world entity. New rows
for existing kinds are data-only; a new entity kind requires a world executor
and validation rule before it is permitted.

Spawned training players are non-persistent fixtures: they are visible,
collidable, and damageable like a player but never occupy an account slot or
become a saved character. Developer overrides reset when the administrator's
body leaves the world. The HTTP commands are `POST /api/admin/spawn` and
`POST /api/admin/attributes`; both verify the session's admin role and that the
selected in-world character belongs to that account.

The admin list is configuration, not a secret. Do not put passwords, session
tokens, API keys, or other credentials in the tuning file.
