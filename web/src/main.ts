import { API } from "./api";
import { Predictor } from "./game/prediction";
import { GameView } from "./game/view";
import { GameSocket } from "./net/socket";
import { damageBandFor, dangerBandAt, resourceMax, spells, starterWeapon, world } from "./tuning";
import { Buttons, ServerKind, type Character, type CharacterClass, type Entity, type ServerMessage } from "./types";

function element<T extends HTMLElement>(id: string): T {
  const value = document.getElementById(id);
  if (!value) throw new Error(`Missing #${id}`);
  return value as T;
}

class SpellFire {
  private api = new API();
  private characters: Character[] = [];
  private authMode: "login" | "register" = "login";
  private socket?: GameSocket;
  private view?: GameView;
  private predictor?: Predictor;
  private inputTimer = 0;
  private pressed = new Set<number>();
  private aim = { x: 1, y: 0 };
  private noticeTimer = 0;
  private lastBand = "";

  async init(): Promise<void> {
    this.bindHome(); this.bindDialogs(); this.bindControls(); this.bindSettings();
    if (this.api.token) await this.loadCharacters().catch(() => undefined);
    this.renderHome();
    fetch("/api/health").catch(() => { element("service-status").textContent = "World service unavailable"; element("service-status").classList.remove("good"); });
    void this.renderBuildInfo();
  }

  // Shows when the deployed server binary was built, so a stale deployment is
  // obvious at a glance. The build time is stamped into the binary at compile
  // time and served from /api/version.
  private async renderBuildInfo(): Promise<void> {
    const target = element("build-info");
    let info;
    try {
      info = await this.api.version();
    } catch {
      return;
    }
    if (!info.time) return;
    const built = new Date(info.time);
    if (Number.isNaN(built.getTime())) return;
    const absolute = built.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
    const commit = info.commit ? ` · ${info.commit}` : "";
    target.textContent = `Build ${relativeTime(built)} — ${absolute}${commit}`;
    target.title = built.toISOString();
    const ageDays = (Date.now() - built.getTime()) / 86_400_000;
    target.classList.toggle("stale", ageDays > 7);
  }

  private bindHome(): void {
    element("account-button").addEventListener("click", () => {
      if (this.api.token) void this.signOut(); else this.openAuth();
    });
    element("new-character-button").addEventListener("click", () => element<HTMLDialogElement>("character-dialog").showModal());
    element("play-button").addEventListener("click", () => void this.play());
    element("settings-button").addEventListener("click", () => element<HTMLDialogElement>("settings-dialog").showModal());
    element<HTMLSelectElement>("character-select").addEventListener("change", (event) => sessionStorage.setItem("spellfire-character", (event.currentTarget as HTMLSelectElement).value));
  }

  private bindDialogs(): void {
    element("auth-switch").addEventListener("click", () => { this.authMode = this.authMode === "login" ? "register" : "login"; this.renderAuth(); });
    element<HTMLFormElement>("auth-form").addEventListener("submit", (event) => void this.submitAuth(event));
    element<HTMLFormElement>("character-form").addEventListener("submit", (event) => void this.createCharacter(event));
    element("menu-button").addEventListener("click", () => { this.renderMenu("character"); element<HTMLDialogElement>("menu-dialog").showModal(); });
    element("menu-tabs").addEventListener("click", (event) => { const button = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-tab]"); if (button) this.renderMenu(button.dataset.tab ?? "character"); });
    element("exit-button").addEventListener("click", () => { if (confirm("Exit to Home? Your character disconnects immediately.")) this.exitGame(); });
    element("connection-cancel").addEventListener("click", () => this.exitGame());
    element("respawn-button").addEventListener("click", () => this.socket?.respawn());
  }

  private bindControls(): void {
    const keyMap: Record<string, number> = { KeyW: Buttons.Up, ArrowUp: Buttons.Up, KeyS: Buttons.Down, ArrowDown: Buttons.Down, KeyA: Buttons.Left, ArrowLeft: Buttons.Left, KeyD: Buttons.Right, ArrowRight: Buttons.Right, Space: Buttons.Dash, KeyR: Buttons.Reload };
    window.addEventListener("keydown", (event) => { const button = keyMap[event.code]; if (button && !isFormField(event.target)) { event.preventDefault(); this.pressed.add(button); } });
    window.addEventListener("keyup", (event) => { const button = keyMap[event.code]; if (button) this.pressed.delete(button); });
    window.addEventListener("pointermove", (event) => { if (!this.view) return; this.aim = this.view.pointerWorld(event.clientX, event.clientY); });
    element("canvas-host").addEventListener("pointerdown", (event) => { if ((event as PointerEvent).button === 0) this.pressed.add(Buttons.Fire); });
    window.addEventListener("pointerup", (event) => { if ((event as PointerEvent).button === 0) this.pressed.delete(Buttons.Fire); });
    element("canvas-host").addEventListener("contextmenu", (event) => event.preventDefault());
    const touchMap: Record<string, number> = { up: Buttons.Up, down: Buttons.Down, left: Buttons.Left, right: Buttons.Right, fire: Buttons.Fire, dash: Buttons.Dash };
    for (const button of document.querySelectorAll<HTMLButtonElement>("#touch-controls button")) {
      const bit = touchMap[button.dataset.button ?? ""];
      if (!bit) continue;
      button.addEventListener("pointerdown", (event) => { event.preventDefault(); button.setPointerCapture(event.pointerId); this.pressed.add(bit); });
      const release = () => this.pressed.delete(bit);
      button.addEventListener("pointerup", release); button.addEventListener("pointercancel", release); button.addEventListener("lostpointercapture", release);
    }
  }

  private bindSettings(): void {
    const scale = element<HTMLInputElement>("ui-scale"), reduced = element<HTMLInputElement>("reduced-motion");
    scale.value = localStorage.getItem("spellfire-ui-scale") ?? "100";
    reduced.checked = localStorage.getItem("spellfire-reduced-motion") === "true";
    const apply = () => { document.documentElement.style.setProperty("--ui-scale", String(Number(scale.value) / 100)); document.documentElement.classList.toggle("reduced-motion", reduced.checked); };
    apply(); scale.addEventListener("input", () => { localStorage.setItem("spellfire-ui-scale", scale.value); apply(); }); reduced.addEventListener("change", () => { localStorage.setItem("spellfire-reduced-motion", String(reduced.checked)); apply(); });
  }

  private openAuth(): void { this.authMode = "login"; this.renderAuth(); element<HTMLDialogElement>("auth-dialog").showModal(); queueMicrotask(() => element<HTMLInputElement>("auth-email").focus()); }

  private renderAuth(): void {
    const registering = this.authMode === "register";
    element("auth-title").textContent = registering ? "Create account" : "Sign in";
    element("auth-submit").textContent = registering ? "Create account" : "Sign in";
    element("auth-switch").textContent = registering ? "Already registered? Sign in" : "Need an account? Register";
    element<HTMLInputElement>("auth-password").autocomplete = registering ? "new-password" : "current-password";
    element("auth-error").textContent = "";
  }

  private async submitAuth(event: SubmitEvent): Promise<void> {
    event.preventDefault(); const submit = element<HTMLButtonElement>("auth-submit"); submit.disabled = true; element("auth-error").textContent = "";
    try {
      await this.api.authenticate(this.authMode, element<HTMLInputElement>("auth-email").value, element<HTMLInputElement>("auth-password").value);
      await this.loadCharacters(); element<HTMLDialogElement>("auth-dialog").close(); this.renderHome();
      if (!this.characters.length) element<HTMLDialogElement>("character-dialog").showModal();
    } catch (error) { element("auth-error").textContent = messageOf(error); } finally { submit.disabled = false; }
  }

  private async createCharacter(event: SubmitEvent): Promise<void> {
    event.preventDefault(); element("character-error").textContent = "";
    const selected = document.querySelector<HTMLInputElement>('input[name="class"]:checked');
    try {
      const character = await this.api.createCharacter(element<HTMLInputElement>("character-name").value, (selected?.value ?? "gunslinger") as CharacterClass);
      this.characters.push(character); this.renderHome(character.id); element<HTMLDialogElement>("character-dialog").close();
    } catch (error) { element("character-error").textContent = messageOf(error); }
  }

  private async loadCharacters(): Promise<void> { this.characters = await this.api.characters(); this.renderHome(); }

  private renderHome(preferred?: string): void {
    const authenticated = Boolean(this.api.token);
    element("guest-state").hidden = authenticated; element("character-state").hidden = !authenticated;
    element("account-button").textContent = authenticated ? "Sign out" : "Sign in";
    const select = element<HTMLSelectElement>("character-select"), remembered = preferred ?? sessionStorage.getItem("spellfire-character") ?? "";
    select.replaceChildren(...this.characters.map((character) => { const option = document.createElement("option"); option.value = character.id; option.textContent = `${character.name} · ${titleCase(character.class)} · Level ${character.level}`; return option; }));
    if (this.characters.some((character) => character.id === remembered)) select.value = remembered;
    const play = element<HTMLButtonElement>("play-button"); play.disabled = !authenticated || !this.characters.length; play.textContent = !authenticated ? "Sign in to play" : this.characters.length ? "Enter world" : "Create a character";
    element("home-error").textContent = "";
  }

  private async signOut(): Promise<void> { await this.api.logout().catch(() => undefined); this.characters = []; this.renderHome(); }

  private async play(): Promise<void> {
    const character = this.selectedCharacter(); if (!character) return;
    sessionStorage.setItem("spellfire-character", character.id); element("home").hidden = true; element("game").hidden = false;
    element("connection-overlay").hidden = false; element("connection-title").textContent = "Connecting"; element("connection-message").textContent = "Joining the shared world…";
    this.predictor = new Predictor(); this.view = new GameView(); await this.view.init(element("canvas-host")); this.view.bindPredictor(this.predictor);
    this.socket = new GameSocket(this.api.token, character.id, { message: (message) => this.receive(message, character), status: (state, detail) => this.connectionStatus(state, detail) });
    this.socket.connect(); window.clearInterval(this.inputTimer); this.inputTimer = window.setInterval(() => this.simulateInput(), 1000 / 60);
  }

  private receive(message: ServerMessage, character: Character): void {
    if (message.kind === ServerKind.Pong) { element("latency").textContent = `${Math.max(0, Date.now() - message.echoedClientTimeMS)} ms`; return; }
    this.predictor?.setColliders(message.colliders); this.view?.apply(message);
    const local = message.entities.find((entity) => entity.id === message.playerID);
    if (!local || !this.predictor) return;
    if (message.kind === ServerKind.Welcome) this.predictor.initialize(local); else this.predictor.reconcile(local);
    this.updateHUD(local, character); element("connection-overlay").hidden = true; element("death-overlay").hidden = local.alive;
  }

  private simulateInput(): void {
    if (!this.predictor || element("game").hidden) return;
    const blocked = element<HTMLDialogElement>("menu-dialog").open;
    let buttons = 0; if (!blocked) for (const value of this.pressed) buttons |= value;
    const input = this.predictor.step(buttons, this.aim.x, this.aim.y, performance.now()); this.socket?.sendInput(input);
  }

  private connectionStatus(state: "connecting" | "connected" | "reconnecting" | "failed", detail?: string): void {
    const overlay = element("connection-overlay");
    if (state === "connected") { element("connection-message").textContent = "Synchronizing world state…"; return; }
    overlay.hidden = false; element("connection-title").textContent = state === "failed" ? "Connection failed" : state === "reconnecting" ? "Reconnecting" : "Connecting"; element("connection-message").textContent = detail ?? (state === "reconnecting" ? "Gameplay input is paused." : "Contacting the world server…");
  }

  private updateHUD(entity: Entity, character: Character): void {
    const health = Math.max(0, entity.health / Math.max(1, entity.maxHealth)); element("health-bar").style.width = `${health * 100}%`; element("health-label").textContent = `${Math.ceil(entity.health)} / ${Math.ceil(entity.maxHealth)}`;
    const { label, max } = resourceMax(character.class), resource = entity.mana; element("resource-label").innerHTML = `${label} <span>${Math.floor(resource)} / ${max}</span>`; element("resource-bar").style.width = `${Math.max(0, resource / max) * 100}%`;
    const band = dangerBandAt(Math.hypot(entity.x, entity.y));
    element("danger-text").textContent = `${band.name} · ${band.summary}`; element("danger-shape").textContent = band.shape;
    if (band.name !== this.lastBand && this.lastBand) this.notice(`${band.name}: ${band.summary}`); this.lastBand = band.name;
  }

  private notice(message: string): void { const notice = element("world-notice"); notice.textContent = message; notice.classList.add("visible"); window.clearTimeout(this.noticeTimer); this.noticeTimer = window.setTimeout(() => notice.classList.remove("visible"), 2600); }

  private renderMenu(tab: string): void {
    for (const button of document.querySelectorAll<HTMLButtonElement>("#menu-tabs button")) button.classList.toggle("active", button.dataset.tab === tab);
    const character = this.selectedCharacter(); const content = element("menu-content");
    const pages: Record<string, string> = {
      character: `<h3>${escapeHTML(character?.name ?? "Character")}</h3><p>${titleCase(character?.class ?? "gunslinger")} · Level ${character?.level ?? 1}</p><p>Progression unlocks options, never raw combat power.</p>`,
      loadout: `<h3>Starter loadout</h3><p>${escapeHTML(describeLoadout(character?.class ?? "gunslinger"))}</p><p>Loadouts are editable only inside the central safe zone. Expanded crafting and affinity validation are not available in this foundation.</p>`,
      inventory: "<h3>Materials</h3><p>No carried materials. Material harvesting and death drops are not available in this foundation.</p>",
      world: `<h3>Known world</h3><p>${world.danger_bands.map((band) => escapeHTML(band.name)).join(" → ")}. The circular world is contiguous; trees are authoritative static cover.</p>`,
      reference: `<h3>Field reference</h3><p>WASD/Arrows move · pointer aims · primary pointer fires · Space dashes · R reloads. The hub is safe. Combat is server-authoritative and raw time-to-kill is about ${damageBandFor(starterWeapon(character?.class ?? "gunslinger")).target_ttk_seconds} seconds.</p>`,
      settings: "<h3>Settings</h3><p>Accessibility and interface-scale controls remain available on Home. Opening this menu does not pause the shared world.</p>",
    };
    content.innerHTML = pages[tab] ?? pages.character!;
  }

  private selectedCharacter(): Character | undefined { const id = element<HTMLSelectElement>("character-select").value; return this.characters.find((character) => character.id === id); }

  private exitGame(): void {
    window.clearInterval(this.inputTimer); this.socket?.close(); this.socket = undefined; this.view?.destroy(); this.view = undefined; this.predictor = undefined; this.pressed.clear(); this.lastBand = "";
    const menu = element<HTMLDialogElement>("menu-dialog"); if (menu.open) menu.close(); element("game").hidden = true; element("home").hidden = false; element("death-overlay").hidden = true; element("connection-overlay").hidden = true;
  }
}

// The menu names what the character actually carries, read from the same
// tuning tables the simulation fires with.
function describeLoadout(characterClass: CharacterClass): string {
  const weapon = starterWeapon(characterClass);
  const parts = [weapon.name];
  const spell = weapon.spell ? spells[weapon.spell] : undefined;
  if (spell) parts.push(spell.name);
  if (weapon.magazine_size) parts.push(`${weapon.magazine_size}-round magazine`);
  parts.push("Universal dash");
  return parts.join(" · ");
}

function isFormField(target: EventTarget | null): boolean { return target instanceof HTMLInputElement || target instanceof HTMLSelectElement || target instanceof HTMLTextAreaElement; }
function messageOf(error: unknown): string { return error instanceof Error ? error.message : "Something went wrong."; }
function titleCase(value: string): string { return value.charAt(0).toUpperCase() + value.slice(1); }
function escapeHTML(value: string): string { const span = document.createElement("span"); span.textContent = value; return span.innerHTML; }
function relativeTime(date: Date): string {
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [["year", 31_536_000], ["month", 2_592_000], ["day", 86_400], ["hour", 3600], ["minute", 60]];
  for (const [unit, span] of units) {
    if (Math.abs(seconds) >= span) return formatter.format(Math.round(seconds / span), unit);
  }
  return formatter.format(seconds, "second");
}

void new SpellFire().init();
