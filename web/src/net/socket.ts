import { ClientKind, ServerKind, type CraftRequest, type InputFrame, type LoadoutSet, type ServerMessage } from "../types";
import { decodeServer, encodeAmmunitionEnvelope, encodeCraftEnvelope, encodeInputEnvelope, encodeJoin, encodeLoadoutEnvelope, encodeRideableEnvelope, encodeSimple } from "./protobuf";

export interface SocketEvents {
  message(message: ServerMessage): void;
  status(state: "connecting" | "connected" | "reconnecting" | "failed", detail?: string): void;
}

export class GameSocket {
  private socket?: WebSocket;
  private stopped = false;
  private attempts = 0;
  private pingTimer = 0;
  constructor(private readonly token: string, private readonly characterID: string, private readonly events: SocketEvents) {}

  connect(): void {
    this.stopped = false;
    this.events.status(this.attempts ? "reconnecting" : "connecting");
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${scheme}//${location.host}/ws`);
    this.socket = socket; socket.binaryType = "arraybuffer";
    socket.onopen = () => { this.attempts = 0; socket.send(encodeJoin(this.token, this.characterID)); this.events.status("connected"); this.schedulePing(); };
    socket.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      try { const message = decodeServer(event.data); if (message.kind === ServerKind.Error) { this.stopped = true; this.events.status("failed", message.error); socket.close(); } else this.events.message(message); }
      catch { this.events.status("failed", "The server sent an unreadable world update."); }
    };
    socket.onclose = () => { window.clearTimeout(this.pingTimer); if (!this.stopped) this.retry(); };
    socket.onerror = () => socket.close();
  }

  sendInput(input: InputFrame): void { if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(encodeInputEnvelope(input)); }
  respawn(): void { if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(encodeSimple(ClientKind.Respawn)); }
  /** Requests an equipped set. The server answers with a Loadout message either way. */
  setLoadout(set: LoadoutSet): boolean { if (this.socket?.readyState !== WebSocket.OPEN) return false; this.socket.send(encodeLoadoutEnvelope(set)); return true; }
  /** Requests one build. The server answers with a Craft message either way. */
  craft(request: CraftRequest): boolean { if (this.socket?.readyState !== WebSocket.OPEN) return false; this.socket.send(encodeCraftEnvelope(request)); return true; }
  /** Requests one batch of special ammunition. It answers on the Craft message. */
  craftAmmunition(recipe: string): boolean { if (this.socket?.readyState !== WebSocket.OPEN) return false; this.socket.send(encodeAmmunitionEnvelope(recipe)); return true; }
  /** Requests one rideable. It answers on the Craft message; the ride arrives in the world. */
  craftRideable(recipe: string): boolean { if (this.socket?.readyState !== WebSocket.OPEN) return false; this.socket.send(encodeRideableEnvelope(recipe)); return true; }
  close(): void { this.stopped = true; window.clearTimeout(this.pingTimer); this.socket?.close(1000, "player exit"); }

  private retry(): void {
    if (++this.attempts > 5) { this.events.status("failed", "Could not reconnect. Return home and try again."); return; }
    this.events.status("reconnecting", `Attempt ${this.attempts} of 5`);
    window.setTimeout(() => { if (!this.stopped) this.connect(); }, Math.min(5000, 500 * 2 ** this.attempts));
  }

  private schedulePing(): void {
    window.clearTimeout(this.pingTimer);
    this.pingTimer = window.setTimeout(() => { if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(encodeSimple(ClientKind.Ping, Date.now())); this.schedulePing(); }, 3000);
  }
}
