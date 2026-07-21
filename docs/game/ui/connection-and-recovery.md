# Connecting, loading, and recovery

Entering play replaces or overlays the Home play panel with:

- a clear connection/loading state;
- short text for materially distinct stages;
- cancel/return when safe;
- actionable failure and retry;
- reconnect behavior for an interrupted session.

Never simulate precise progress. Use a determinate indicator only for a real measured download or synchronization step.

After an unexpected disconnect, keep the last world frame only as a visibly inactive backdrop, block gameplay input, and show a reconnecting overlay. Successful recovery resumes the authoritative session; terminal failure returns Home with a reason.

The exact reconnect grace period is **Open**. Offline transaction behavior follows [`shared-states.md`](shared-states.md).

