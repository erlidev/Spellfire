# Shared interface states

Every network-backed surface defines:

- **Loading:** keep the existing context stable while data resolves.
- **Empty:** explain the surface and, when useful, how to populate it.
- **Unavailable/locked:** state the rule or prerequisite in player language.
- **Error:** name the failed action and offer safe retry or recovery.
- **Stale/conflict:** refresh authoritative state before allowing a contradictory action.
- **Offline/reconnecting:** block unconfirmable actions and never claim a spend, craft, pickup, or loadout change succeeded.

Confirm destructive or spend actions once. Retries are idempotent, and success appears only after server confirmation. Routine reversible actions should not collect needless confirmations.

Topic files own state-specific copy and recovery; this file owns the common behavior.

