# Home and authentication

Home is a full-viewport, low-friction entrance to play.

## Layout

Home has three conceptual layers:

- **Background:** lightweight, non-interactive procedural branding that preserves contrast.
- **Utility:** account/session access and secondary links at the edges.
- **Primary:** a compact centered play panel with title, character context, prerequisites, and Play.

The play panel is the strongest focus. News, promotion, legal, and social content cannot displace or overpower it.

## Play panel

Reading and focus order is:

1. logo or wordmark;
2. service or blocking maintenance state, when relevant;
3. signed-in identity or guest state;
4. character selection/creation;
5. selected name and class;
6. **Play**;
7. adjacent validation or connection errors.

Play is disabled only for an explained blocking requirement. Activation moves in place to [connection feedback](connection-and-recovery.md) and prevents duplicate attempts.

The signed-in identity uses the account email. Administrator accounts append a
plain “Administrator” label so privileged surfaces added later have visible
role context. The label reflects server-provided account state and never acts
as an authorization check; the server still gates every privileged operation.

**Open:** guest play, character slots, naming, region/server selection, and where first-time class choice occurs. Reserve one compact prerequisite control without implying every system exists.

## Account flows

Account actions live in an unobtrusive utility control. Normal sign-in, registration, sign-out, recovery, verification, expired-session reauthentication, and profile settings use a modal, popover, or mobile sheet over Home rather than a full-page redirect.

All account overlays:

- preserve selected character and Home state;
- move focus inside, trap it, and restore it on close;
- support Escape and an explicit close for non-blocking dialogs;
- expose pending submission and reject duplicates;
- place field errors by fields and service errors at form level;
- update Home without redirect after success;
- separate explicitly confirmed destructive actions from routine sign-in.

External identity providers may require a new window or redirect. Preserve pending context and return the player to it after success or cancellation.

## Secondary surfaces

Settings, credits, privacy/legal, accessibility, and patch/news content use secondary overlays where practical. Long legal documents may use dedicated pages but never interrupt the normal play or authentication path.

Responsive rules live in [`responsive-and-mobile.md#home-and-overlays`](responsive-and-mobile.md#home-and-overlays); generic remote-data states live in [`shared-states.md`](shared-states.md).
