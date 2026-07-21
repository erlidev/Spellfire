# Application flow

The application has three primary states:

1. **Home:** branding, account/session controls, character context, and Play.
2. **Connecting/loading:** an in-place transition with status and recovery.
3. **In game:** world, HUD, contextual interfaces, and a menu that returns Home.

Authentication updates Home in place. Routes may change internally, but entering and leaving play must feel like one continuous application. Refresh, reconnect, and repeated actions cannot duplicate sessions or lose server-confirmed account and character changes.

See [`home.md`](home.md), [`connection-and-recovery.md`](connection-and-recovery.md), and [`game-menu.md#exit-and-session-actions`](game-menu.md#exit-and-session-actions).

