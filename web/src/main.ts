import { API, type AdminEntityState } from "./api";
import { buildableAmmunition, componentOf, cost as craftCost, craftable, describe, fitting, itemLabel, lockedCraftable, materialName, recipeOf, resolvedWeapon, resultOf, shortfall, slotsOf } from "./game/crafting";
import { bar, barSlots, contentName, defaultLoadout, equippable, ledgerOf, loadoutProblem, locked, type Ledger, type LockedContent, type SlotKind } from "./game/loadout";
import { Predictor } from "./game/prediction";
import { joystickVector, movementButtons } from "./game/touch";
import { GameView } from "./game/view";
import { GameSocket } from "./net/socket";
import { abilities, ammunition as ammunitionTable, biomes, damageBandFor, entityDefinitions, handlingScale, materials as materialsTable, movementStatus, progression as progressionTable, resourceMax, session, specialAmmunition, weapons, weightOf, world, xpToNext, type AdminField, type EntityDefinition, type Guard, type Weapon } from "./tuning";
import { biomeName, gradeName, materialsAt, worldField } from "./game/worldfield";
import { Buttons, ServerKind, type Character, type CharacterClass, type CraftedItem, type Entity, type LoadoutSet, type ServerMessage } from "./types";

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
  // Each physical control owns one entry, so releasing one finger cannot clear
  // a button that another finger (or a hardware key) is still holding.
  private heldInputs = new Map<string, number>();
  private aim = { x: 1, y: 0 };
  private noticeTimer = 0;
  private lastBand = "";
  private lastBiome = "";
  private localEntity?: Entity;
  private activeCharacter?: Character;
  private adminMode: "off" | "spawn" | "select" | "delete" = "off";
  private adminSpawnID = Object.keys(entityDefinitions).filter((id) => entityDefinitions[id]!.admin.spawnable).sort()[0] ?? "";
  private adminSpawnConfig: Record<string, string> = this.defaultAdminConfig(this.adminSpawnID);
  private adminSelected?: AdminEntityState;
  private adminEditDraft: Record<string, string> = {};
  private adminSearch = "";
  private adminMaterialCount = materialsTable.admin_grant.default;
  private adminLevel = progressionTable.admin_grant.default;
  private adminPositionPick?: string;
  private menuRefreshFrame = 0;
  private menuCollapseTimer = 0;
  private renderedMenuState = "";
  // The authoritative equipped set and the slot the use button acts through.
  // `draft` is the unconfirmed edit in the menu; nothing shows as committed
  // until a Loadout reply confirms it.
  private loadout: LoadoutSet = { weapon: "", gadgets: [], spells: [] };
  private draft?: LoadoutSet;
  // The permanent axis. The ledger decides what the Loadout section may offer;
  // the server enforces the same rule, so this only avoids offering a refusal.
  private ledger: Ledger = ledgerOf([]);
  private level = 1;
  private xp = 0;
  private xpNext = 0;
  private selectedSlot = 0;
  // When each ability's own lockout is over, keyed by ability ID and derived
  // from the remaining time the server sends on every snapshot. It is a readout,
  // never a gate: the input is sent regardless and the server decides.
  private cooldowns: Record<string, number> = {};
  private inSafety = true;
  private respecOwed = false;
  private loadoutStatus = "";
  private menuTab = "character";
  // What the character owns and carries. Both arrive on the welcome and change
  // only on a confirmed craft, so nothing here is ever inferred from a snapshot.
  private items: CraftedItem[] = [];
  private materials: Record<string, number> = {};
  // The unconfirmed build in the Crafting section: a previewed recipe and the
  // component chosen per blank. The complete parts determine the server result.
  private craftWeapon = "";
  private craftChoices: Record<string, string> = {};
  private craftStatus = "";
  private adminMaterialID = Object.keys(materialsTable.materials).sort()[0] ?? "";

  async init(): Promise<void> {
    this.bindViewport(); this.bindHome(); this.bindDialogs(); this.bindControls(); this.bindSettings();
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

  /** Keep the installed-app-like viewport stable on iOS, including Home. */
  private bindViewport(): void {
    for (const type of ["gesturestart", "gesturechange", "gestureend"]) {
      document.addEventListener(type, (event) => event.preventDefault(), { passive: false });
    }
    let lastTouchEnd = -1000;
    document.addEventListener("touchend", (event) => {
      const now = performance.now();
      if (now - lastTouchEnd < 350) event.preventDefault();
      lastTouchEnd = now;
    }, { passive: false });
  }

  private bindDialogs(): void {
    element("auth-switch").addEventListener("click", () => { this.authMode = this.authMode === "login" ? "register" : "login"; this.renderAuth(); });
    element<HTMLFormElement>("auth-form").addEventListener("submit", (event) => void this.submitAuth(event));
    element<HTMLFormElement>("character-form").addEventListener("submit", (event) => void this.createCharacter(event));
    const menu = element<HTMLDialogElement>("menu-dialog");
    bindActivation(element("menu-button"), () => { if (menu.open) menu.close(); else { this.setMenuCollapsed(false); this.renderMenu(this.menuTab); menu.show(); } });
    bindActivation(element("menu-collapse"), () => this.setMenuCollapsed(!menu.classList.contains("collapsed")));
    // Touch contact can synthesize hover transitions on iOS. Only a real fine
    // pointer may auto-expand/collapse; touch uses the explicit toggle alone.
    menu.addEventListener("pointerenter", (event) => {
      if (event.pointerType !== "mouse" || !matchMedia("(hover: hover) and (pointer: fine)").matches) return;
      window.clearTimeout(this.menuCollapseTimer); if (menu.classList.contains("collapsed")) this.setMenuCollapsed(false);
    });
    menu.addEventListener("pointerleave", () => this.scheduleMenuCollapse());
    menu.addEventListener("focusout", () => { this.refreshOpenMenu(); if (!menu.matches(":hover")) this.scheduleMenuCollapse(); });
    menu.addEventListener("close", () => { window.clearTimeout(this.menuCollapseTimer); window.cancelAnimationFrame(this.menuRefreshFrame); this.menuRefreshFrame = 0; });
    bindDelegatedActivation(element("menu-tabs"), "button[data-tab]", (button) => this.renderMenu(button.dataset.tab ?? "character"));
    element("exit-button").addEventListener("click", () => { if (confirm(`Exit to Home? Your body stays in the world for ${session.logout_linger_seconds} seconds after you leave and can still be attacked.`)) this.exitGame(); });
    bindActivation(element("connection-cancel"), () => this.exitGame());
    bindActivation(element("respawn-button"), () => this.socket?.respawn());
    element("developer-mode-exit").addEventListener("click", () => this.setDeveloperMode(false));
  }

  private bindControls(): void {
    const keyMap: Record<string, number> = { KeyW: Buttons.Up, ArrowUp: Buttons.Up, KeyS: Buttons.Down, ArrowDown: Buttons.Down, KeyA: Buttons.Left, ArrowLeft: Buttons.Left, KeyD: Buttons.Right, ArrowRight: Buttons.Right, Space: Buttons.Dash, KeyR: Buttons.Reload, KeyE: Buttons.Interact, ShiftLeft: Buttons.Scope, ShiftRight: Buttons.Scope };
    window.addEventListener("keydown", (event) => {
      // 1–6 select the equipped slot the use button acts through: a Mage's six
      // spells, a Gunslinger's weapon and five gadgets.
      const slot = slotKey(event.code);
      if (slot !== undefined && !isFormField(event.target)) { event.preventDefault(); this.selectSlot(slot); return; }
      const button = keyMap[event.code]; if (button && !isFormField(event.target)) { event.preventDefault(); this.setHeld(`key:${event.code}`, button); }
    });
    window.addEventListener("keyup", (event) => { if (keyMap[event.code]) this.setHeld(`key:${event.code}`, 0); });
    // Mouse aim keeps tracking outside the canvas during a held shot. Touch aim
    // is handled only by the world or shooting stick that owns that pointer.
    window.addEventListener("pointermove", (event) => { if (!this.view || event.pointerType !== "mouse") return; this.aim = this.view.pointerWorld(event.clientX, event.clientY); });
    const canvas = element("canvas-host");
    canvas.addEventListener("pointermove", (event) => {
      if (!this.view || event.pointerType === "mouse") return;
      this.aim = this.view.pointerWorld(event.clientX, event.clientY);
    });
    canvas.addEventListener("pointerdown", (event) => {
      if (this.view) this.aim = this.view.pointerWorld(event.clientX, event.clientY);
      // The secondary button scopes, mirroring Shift, because the committed
      // aiming mode is held rather than toggled.
      if (event.button === 2) { this.setHeld(`pointer:${event.pointerId}:scope`, Buttons.Scope); return; }
      if (event.button !== 0) return;
      if (this.adminPositionPick || this.adminMode !== "off") { void this.useAdminPointer(event); return; }
      event.preventDefault();
      this.setHeld(`pointer:${event.pointerId}:fire`, Buttons.Fire);
    });
    window.addEventListener("pointerup", (event) => {
      this.setHeld(`pointer:${event.pointerId}:fire`, 0);
      this.setHeld(`pointer:${event.pointerId}:scope`, 0);
    });
    window.addEventListener("pointercancel", (event) => {
      this.setHeld(`pointer:${event.pointerId}:fire`, 0);
      this.setHeld(`pointer:${event.pointerId}:scope`, 0);
    });
    window.addEventListener("blur", () => this.heldInputs.clear());
    canvas.addEventListener("contextmenu", (event) => event.preventDefault());
    // The wheel steps through the same slots, wrapping in both directions.
    element("canvas-host").addEventListener("wheel", (event) => { event.preventDefault(); this.selectSlot((this.selectedSlot + (event.deltaY > 0 ? 1 : barSlots - 1)) % barSlots); }, { passive: false });
    bindDelegatedActivation(element("touch-slots"), "button[data-slot]", (button) => this.selectSlot(Number(button.dataset.slot)));
    const touchMap: Record<string, number> = { dash: Buttons.Dash, reload: Buttons.Reload, interact: Buttons.Interact, scope: Buttons.Scope };
    for (const button of document.querySelectorAll<HTMLButtonElement>("#touch-controls button")) {
      const bit = touchMap[button.dataset.button ?? ""];
      if (!bit) continue;
      button.addEventListener("pointerdown", (event) => { event.preventDefault(); button.setPointerCapture(event.pointerId); this.setHeld(`touch:${button.dataset.button}:${event.pointerId}`, bit); });
      const release = (event: PointerEvent) => this.setHeld(`touch:${button.dataset.button}:${event.pointerId}`, 0);
      button.addEventListener("pointerup", release); button.addEventListener("pointercancel", release); button.addEventListener("lostpointercapture", release);
    }
    this.bindJoystick(element("move-joystick"), "move");
    this.bindJoystick(element("shoot-joystick"), "shoot");
  }

  private setHeld(source: string, buttons: number): void {
    if (buttons) this.heldInputs.set(source, buttons); else this.heldInputs.delete(source);
  }

  /** Fixed bases make the controls predictable; pointer capture preserves the drag. */
  private bindJoystick(base: HTMLElement, kind: "move" | "shoot"): void {
    const thumb = base.querySelector<HTMLElement>(".joystick-thumb");
    if (!thumb) return;
    let activePointer: number | undefined;
    const update = (event: PointerEvent) => {
      const bounds = base.getBoundingClientRect();
      const travel = Math.max(1, (Math.min(bounds.width, bounds.height) - thumb.offsetWidth) / 2 - 5);
      const vector = joystickVector(event.clientX, event.clientY, bounds, travel);
      thumb.style.transform = `translate(${vector.knobX}px, ${vector.knobY}px)`;
      if (kind === "move") {
        const direction = movementButtons(vector.x, vector.y);
        let buttons = 0;
        if (direction.up) buttons |= Buttons.Up;
        if (direction.down) buttons |= Buttons.Down;
        if (direction.left) buttons |= Buttons.Left;
        if (direction.right) buttons |= Buttons.Right;
        this.setHeld("joystick:move", buttons);
      } else {
        const length = Math.hypot(vector.x, vector.y);
        if (length > 0.18) this.aim = { x: vector.x / length, y: vector.y / length };
      }
    };
    const release = (event: PointerEvent) => {
      if (event.pointerId !== activePointer) return;
      activePointer = undefined; base.classList.remove("active"); thumb.style.transform = "translate(0px, 0px)";
      this.setHeld(`joystick:${kind}`, 0);
    };
    base.addEventListener("pointerdown", (event) => {
      if (activePointer !== undefined || event.button !== 0) return;
      event.preventDefault(); activePointer = event.pointerId; base.classList.add("active"); base.setPointerCapture(event.pointerId);
      if (kind === "shoot") this.setHeld("joystick:shoot", Buttons.Fire);
      update(event);
    });
    base.addEventListener("pointermove", (event) => { if (event.pointerId === activePointer) update(event); });
    base.addEventListener("pointerup", release); base.addEventListener("pointercancel", release); base.addEventListener("lostpointercapture", release);
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

  private async loadCharacters(): Promise<void> {
    const [characters] = await Promise.all([this.api.characters(), this.api.loadAccount()]);
    this.characters = characters;
    this.renderHome();
  }

  private renderHome(preferred?: string): void {
    const authenticated = Boolean(this.api.token);
    element("guest-state").hidden = authenticated; element("character-state").hidden = !authenticated;
    element("account-button").textContent = authenticated ? "Sign out" : "Sign in";
    const identity = this.api.account;
    element("account-identity").textContent = identity ? `${identity.email}${identity.is_admin ? " · Administrator" : ""}` : "";
    const select = element<HTMLSelectElement>("character-select"), remembered = preferred ?? sessionStorage.getItem("spellfire-character") ?? "";
    select.replaceChildren(...this.characters.map((character) => { const option = document.createElement("option"); option.value = character.id; option.textContent = `${character.name} · ${titleCase(character.class)} · Level ${character.level}`; return option; }));
    if (this.characters.some((character) => character.id === remembered)) select.value = remembered;
    const play = element<HTMLButtonElement>("play-button"); play.disabled = !authenticated || !this.characters.length; play.textContent = !authenticated ? "Sign in to play" : this.characters.length ? "Enter world" : "Create a character";
    element("home-error").textContent = "";
  }

  private async signOut(): Promise<void> { await this.api.logout().catch(() => undefined); this.characters = []; this.renderHome(); }

  private async play(): Promise<void> {
    const character = this.selectedCharacter(); if (!character) return;
    // Shown until the welcome arrives with the authoritative set; the server
    // resolves the same default for a character that has never chosen one.
    this.ledger = ledgerOf(character.unlocks ?? []);
    this.level = character.level; this.xp = character.xp; this.xpNext = xpToNext(character.level);
    this.loadout = defaultLoadout(character.class, this.ledger); this.draft = undefined; this.selectedSlot = 0; this.loadoutStatus = "";
    this.items = []; this.materials = {}; this.craftWeapon = ""; this.craftChoices = {}; this.craftStatus = "";
    sessionStorage.setItem("spellfire-character", character.id); element("home").hidden = true; element("game").hidden = false;
    element("connection-overlay").hidden = false; element("connection-title").textContent = "Connecting"; element("connection-message").textContent = "Joining the shared world…";
    this.predictor = new Predictor(); this.view = new GameView(); await this.view.init(element("canvas-host")); this.view.bindPredictor(this.predictor);
    this.activeCharacter = character;
    this.socket = new GameSocket(this.api.token, character.id, { message: (message) => this.receive(message), status: (state, detail) => this.connectionStatus(state, detail) });
    this.socket.connect(); window.clearInterval(this.inputTimer); this.inputTimer = window.setInterval(() => this.simulateInput(), 1000 / 60);
  }

  private receive(message: ServerMessage): void {
    if (message.kind === ServerKind.Pong) { element("latency").textContent = `${Math.max(0, Date.now() - message.echoedClientTimeMS)} ms`; return; }
    if (message.kind === ServerKind.Welcome || message.kind === ServerKind.Progress) this.applyProgress(message);
    if (message.kind === ServerKind.Progress) return;
    if (message.kind === ServerKind.Welcome || message.kind === ServerKind.Craft) this.applyInventory(message);
    if (message.kind === ServerKind.Craft) return;
    if (message.loadout) this.applyLoadout(message);
    if (message.kind === ServerKind.Loadout) return;
    this.predictor?.setColliders(message.colliders); this.view?.apply(message);
    const local = message.entities.find((entity) => entity.id === message.playerID);
    if (!local || !this.predictor) return;
    this.localEntity = local;
    this.applyCooldowns(message);
    this.updateAdminSelectedFromSnapshot(message.entities);
    if (message.kind === ServerKind.Welcome) this.predictor.initialize(local); else this.predictor.reconcile(local);
    this.updateHUD(local); element("connection-overlay").hidden = true; element("death-overlay").hidden = local.alive;
  }

  private simulateInput(): void {
    if (!this.predictor || element("game").hidden) return;
    let buttons = 0; for (const value of this.heldInputs.values()) buttons |= value;
    // A weapon with no scope ignores the button entirely, so holding it never
    // predicts a slowdown the server is not applying.
    const weapon = resolvedWeapon(this.loadout.weapon, this.items);
    // What the status layer is doing to this body rides its own snapshot, so a
    // slow, root, stun, or knockback is predicted instead of being corrected by
    // every reconciliation. A stun also drops both committed stances.
    const status = movementStatus(this.localEntity?.effectIDs ?? []);
    const scoped = Boolean(weapon?.scope) && !status.stunned && (buttons & Buttons.Scope) !== 0;
    const guard = this.selectedGuard();
    // A broken shield cannot be raised, so predicting its movement penalty while
    // the server has already dropped it would rubber-band every step. The
    // authoritative durability is on the local body's own snapshot.
    const shielded = !guard || !this.localEntity || this.localEntity.maxShield <= 0 || this.localEntity.shield > 0;
    const guarding = Boolean(guard) && shielded && !status.stunned && (buttons & Buttons.Fire) !== 0;
    if (!scoped) buttons &= ~Buttons.Scope;
    const input = this.predictor.step(buttons, this.aim.x, this.aim.y, this.selectedSlot, performance.now(), handlingScale(weapon, guard, scoped, guarding), status);
    this.socket?.sendInput(input);
    this.setScopeView(scoped, weapon?.scope?.view_bonus ?? 0);
    this.view?.setHeavyRecoil(this.firesExplosive(weapon));
  }

  /**
   * Adopts the authoritative lockouts the snapshot carried. Every slot kind can
   * hold one — a gadget's throw and every spell above tier one are both
   * cooldown-gated — and the answer is the server's alone: whether a use was
   * charged at all depends on resources and placement rules the client only
   * learns a snapshot late, so a locally guessed lockout was wrong in both
   * directions. It showed nothing on a cast that landed the instant enough mana
   * had regenerated, because the client's mana was still a snapshot behind, and
   * it showed a lockout on a cast the server had refused.
   */
  private applyCooldowns(message: ServerMessage): void {
    const now = Date.now(), cooldowns: Record<string, number> = {};
    for (const [ability, remainingMS] of Object.entries(message.cooldowns)) cooldowns[ability] = now + remainingMS;
    this.cooldowns = cooldowns;
  }

  /**
   * Whether the selected slot is a weapon that fires an explosive. That is the
   * only thing that knocks the camera: a launcher's shot moves the view, while
   * ordinary gunfire is read from the weapon's own kick and its muzzle flash.
   * A thrown gadget never qualifies — its blast goes off away from the body.
   */
  private firesExplosive(weapon: Weapon | undefined): boolean {
    const slot = bar(this.activeCharacter?.class ?? "gunslinger", this.loadout, this.items)[this.selectedSlot];
    if (!weapon || slot?.kind !== "weapon") return false;
    return Boolean(abilities[weapon.ability ?? ""]?.blast);
  }

  /** The barrier the selected slot holds, or undefined for any other slot. */
  private selectedGuard(): Guard | undefined {
    const slots = bar(this.activeCharacter?.class ?? "gunslinger", this.loadout, this.items);
    const slot = slots[this.selectedSlot];
    if (!slot || slot.kind !== "gadget" || !slot.id) return undefined;
    return abilities[slot.abilityId]?.guard;
  }

  /**
   * The camera exception a scope buys: peripheral vision is blacked out and the
   * view is pushed toward where the weapon is pointed. The server has already
   * widened what it sends by the same bonus, so the extra reach is real rather
   * than a rendering trick.
   */
  private setScopeView(scoped: boolean, bonus: number): void {
    document.body.classList.toggle("scoped", scoped);
    this.view?.setScope(scoped ? bonus : 0, this.aim.x, this.aim.y);
  }

  private selectSlot(slot: number): void {
    if (slot < 0 || slot >= barSlots) return;
    this.selectedSlot = slot; this.renderAbilityBar();
  }

  /** Adopts the authoritative set and reports what the server made of a commit. */
  private applyLoadout(message: ServerMessage): void {
    this.loadout = message.loadout!; this.respecOwed = message.respecOwed;
    if (message.kind === ServerKind.Loadout) {
      this.draft = undefined;
      this.loadoutStatus = message.error || "Loadout committed.";
      if (message.error) this.notice(message.error);
    }
    if (this.selectedSlot >= barSlots) this.selectSlot(0);
    this.renderAbilityBar();
    this.refreshOpenMenu();
    if (this.respecOwed) this.notice("A balance patch re-validated your loadout. Respec is free in any safe zone.");
  }

  /**
   * Adopts the permanent axis the server reports. A level-up is announced and
   * the ledger widens immediately, so the Loadout section offers what was just
   * unlocked without a reconnect.
   */
  private applyProgress(message: ServerMessage): void {
    const levelled = message.level > this.level && this.level > 0;
    this.level = message.level; this.xp = message.xp; this.xpNext = message.xpToNext;
    const before = this.ledger.size;
    this.ledger = ledgerOf(message.unlocks);
    const character = this.selectedCharacter();
    if (character) { character.level = message.level; character.xp = message.xp; character.unlocks = [...message.unlocks]; }
    if (message.kind === ServerKind.Welcome) { this.refreshOpenMenu(); return; }
    if (levelled) {
      const gained = this.ledger.size - before;
      this.notice(gained > 0
        ? `Level ${this.level}. ${gained} new option${gained === 1 ? "" : "s"} unlocked — respec is free in any safe zone.`
        : `Level ${this.level}.`);
    }
    this.refreshOpenMenu();
  }

  private renderAbilityBar(): void {
    const character = this.selectedCharacter(); if (!character) return;
    const slots = bar(character.class, this.loadout, this.items);
    const label = (index: number) => `${index + 1}`;
    element("ability-bar").replaceChildren(...slots.map((slot) => {
      const cell = document.createElement("div");
      const left = Math.max(0, Math.ceil(((this.cooldowns[slot.abilityId] ?? 0) - Date.now()) / 1000));
      cell.className = `${slot.index === this.selectedSlot ? "slot selected" : "slot"}${left ? " cooling" : ""}`;
      cell.innerHTML = `<kbd>${label(slot.index)}</kbd><span>${escapeHTML(slot.name || "Empty")}</span>${left ? `<small>${left}s</small>` : ""}`;
      return cell;
    }));
    // Snapshots redraw cooldowns at 20 Hz. Preserve the actual touch buttons so
    // iOS never loses a pointer target between pointer-down and pointer-up.
    const touchBar = element("touch-slots");
    let touchButtons = [...touchBar.querySelectorAll<HTMLButtonElement>("button[data-slot]")];
    if (touchButtons.length !== slots.length || touchButtons.some((button, index) => Number(button.dataset.slot) !== slots[index]?.index)) {
      touchBar.replaceChildren(...slots.map((slot) => {
        const button = document.createElement("button"); button.type = "button"; button.dataset.slot = String(slot.index); return button;
      }));
      touchButtons = [...touchBar.querySelectorAll<HTMLButtonElement>("button[data-slot]")];
    }
    slots.forEach((slot, index) => {
      const button = touchButtons[index]!;
      button.className = slot.index === this.selectedSlot ? "selected" : "";
      const text = label(slot.index), ariaLabel = `Slot ${slot.index + 1}: ${slot.name || "empty"}`, pressed = String(slot.index === this.selectedSlot);
      if (button.textContent !== text) button.textContent = text;
      if (button.getAttribute("aria-label") !== ariaLabel) button.setAttribute("aria-label", ariaLabel);
      if (button.getAttribute("aria-pressed") !== pressed) button.setAttribute("aria-pressed", pressed);
    });
  }

  private connectionStatus(state: "connecting" | "connected" | "reconnecting" | "failed", detail?: string): void {
    const overlay = element("connection-overlay");
    if (state === "connected") { element("connection-message").textContent = "Synchronizing world state…"; return; }
    overlay.hidden = false; element("connection-title").textContent = state === "failed" ? "Connection failed" : state === "reconnecting" ? "Reconnecting" : "Connecting"; element("connection-message").textContent = detail ?? (state === "reconnecting" ? "Gameplay input is paused." : "Contacting the world server…");
  }

  private updateHUD(entity: Entity): void {
    const health = Math.max(0, entity.health / Math.max(1, entity.maxHealth)); element("health-bar").style.width = `${health * 100}%`; element("health-label").textContent = `${Math.ceil(entity.health)} / ${Math.ceil(entity.maxHealth)}`;
    const { label, max, capped } = resourceMax(resolvedWeapon(this.loadout.weapon, this.items)), resource = entity.mana;
    element("resource-label").innerHTML = `${label} <span>${capped ? `${Math.floor(resource)} / ${max}` : `${Math.floor(resource)} carried`}</span>`;
    element("resource-bar").style.width = `${Math.min(1, Math.max(0, resource / Math.max(1, max))) * 100}%`;
    // A shield is a spendable object, so its durability is a readout rather than
    // a hidden number. The row is only present while one is selected: it is the
    // Gunslinger's gadget, not a universal vital.
    const shieldLabel = element("shield-label"), shieldTrack = element("shield-track");
    const carrying = entity.maxShield > 0;
    shieldLabel.hidden = !carrying; shieldTrack.hidden = !carrying;
    if (carrying) {
      const left = Math.max(0, Math.min(1, entity.shield / entity.maxShield));
      shieldLabel.innerHTML = `Shield <span>${left > 0 ? `${Math.ceil(entity.shield)} / ${Math.ceil(entity.maxShield)}` : "broken"}</span>`;
      element("shield-bar").style.width = `${left * 100}%`;
      shieldTrack.classList.toggle("broken", left <= 0);
    }
    // The zone readout comes from the world field rather than from a radius
    // comparison of its own, so the client and the server are answering the
    // same question with the same code.
    const region = worldField.regionAt(entity.x, entity.y), band = region.danger;
    element("danger-text").textContent = `${band.name} · ${band.summary}`; element("danger-shape").textContent = band.shape;
    if (band.name !== this.lastBand && this.lastBand) this.notice(`${band.name}: ${band.summary}`); this.lastBand = band.name;
    // Biome names the material type, grade names its quality: the two axes the
    // world is built on, stated where a player is standing on them.
    const biome = biomeName(region.biome.id);
    element("region-text").textContent = region.grade.id
      ? `${biome} · ${gradeName(region.grade.id)} ground`
      : biome;
    if (region.biome.id !== this.lastBiome && this.lastBiome) this.notice(`${biome}. ${biomes[region.biome.id]?.summary ?? ""}`.trim());
    this.lastBiome = region.biome.id;
    // Crossing out of safety locks the equipped set. Warn at the crossing, not
    // only when the player later opens the menu and finds the controls dead.
    const safe = worldField.safeAt(entity.x, entity.y);
    if (safe !== this.inSafety) {
      this.notice(safe ? "Safe zone: loadout unlocked." : "You left the safe zone. Your loadout is locked until you return.");
      this.refreshOpenMenu();
    }
    this.inSafety = safe;
    // The bar carries the gadget cooldown readout, so it is redrawn on every
    // snapshot rather than only when the equipped set changes.
    this.renderAbilityBar();
    this.refreshOpenMenu();
  }

  private notice(message: string): void { const notice = element("world-notice"); notice.textContent = message; notice.classList.add("visible"); window.clearTimeout(this.noticeTimer); this.noticeTimer = window.setTimeout(() => notice.classList.remove("visible"), 2600); }

  private scheduleMenuCollapse(): void {
    const menu = element<HTMLDialogElement>("menu-dialog");
    if (!menu.open || !matchMedia("(hover: hover) and (pointer: fine)").matches) return;
    window.clearTimeout(this.menuCollapseTimer);
    this.menuCollapseTimer = window.setTimeout(() => {
      if (!menu.matches(":hover")) this.setMenuCollapsed(true);
    }, 450);
  }

  private setMenuCollapsed(collapsed: boolean): void {
    const menu = element<HTMLDialogElement>("menu-dialog"), toggle = element<HTMLButtonElement>("menu-collapse");
    window.clearTimeout(this.menuCollapseTimer);
    menu.classList.toggle("collapsed", collapsed);
    toggle.textContent = collapsed ? "+" : "−";
    toggle.setAttribute("aria-expanded", String(!collapsed));
    toggle.setAttribute("aria-label", collapsed ? "Expand menu" : "Minimize menu");
    if (!collapsed) this.refreshOpenMenu();
  }

  private refreshOpenMenu(): void {
    const menu = element<HTMLDialogElement>("menu-dialog");
    if (!menu.open || this.renderedMenuState === this.menuStateSignature() || this.menuRefreshFrame) return;
    this.menuRefreshFrame = window.requestAnimationFrame(() => {
      this.menuRefreshFrame = 0;
      if (!menu.open || this.renderedMenuState === this.menuStateSignature()) return;
      if (menu.contains(document.activeElement) && isFormField(document.activeElement)) return;
      this.renderMenu(this.menuTab);
    });
  }

  private menuStateSignature(): string {
    const local = this.localEntity;
    switch (this.menuTab) {
      case "character": return JSON.stringify([this.level, this.xp, this.xpNext, this.ledger.size, local && Math.ceil(local.health), local && Math.ceil(local.maxHealth), local && Math.floor(local.mana), local?.alive]);
      case "world": return `${this.lastBand}|${this.lastBiome}|${local ? Math.round(Math.hypot(local.x, local.y) / 500) : ""}`;
      case "loadout": return JSON.stringify([this.inSafety, this.loadout, this.draft, this.respecOwed, this.loadoutStatus, this.items]);
      case "crafting": return JSON.stringify([this.inSafety, this.materials, this.items, this.craftWeapon, this.craftChoices, this.craftStatus, this.ledger.size]);
      case "inventory": return JSON.stringify([this.materials, this.items, this.loadout.weapon]);
      case "admin": return JSON.stringify([this.adminMode, this.adminSpawnID, this.adminSpawnConfig, this.adminSelected, this.adminEditDraft, this.adminSearch, this.adminMaterialID, this.adminMaterialCount, this.adminLevel, this.adminPositionPick]);
      default: return this.menuTab;
    }
  }

  /**
   * The world section states the two axes a player is standing on: the biome
   * decides which materials the ground can yield, the radius decides their
   * grade. Both come from the shared field, so what this lists is exactly what
   * the server will hand out when Phase 4.1 puts nodes on that ground.
   */
  private renderWorldSection(): string {
    const local = this.localEntity;
    if (!local) return "<h3>Known world</h3><p>Synchronizing…</p>";
    const region = worldField.regionAt(local.x, local.y);
    const biome = biomes[region.biome.id];
    const yielded = materialsAt(region.biome.id, region.grade.tier)
      .map((id) => `${escapeHTML(materialsTable.materials[id]?.name ?? id)} <small>(${escapeHTML(materialsTable.grades[materialsTable.materials[id]?.grade ?? ""]?.name ?? "")})</small>`);
    return `<h3>Known world</h3>
      <p><strong>${escapeHTML(biome?.name ?? region.biome.id)}</strong> · ${escapeHTML(this.lastBand || "")}${region.grade.id ? ` · ${escapeHTML(gradeName(region.grade.id))} ground` : ""}</p>
      <p>${escapeHTML(biome?.summary ?? "")}</p>
      <p>${world.danger_bands.map((band) => escapeHTML(band.name)).join(" → ")}. Biome decides which material this ground yields; distance from the hub decides its grade, on a curve that rewards the rim disproportionately.</p>
      <p>This ground can yield: ${yielded.length ? yielded.join(", ") : "nothing — the hub is worked stone"}.</p>
      <p><small>Harvesting arrives with Phase 4.1; the ground already knows what it holds.</small></p>`;
  }

  private renderMenu(tab: string): void {
    const admin = Boolean(this.api.account?.is_admin);
    const adminTab = element<HTMLButtonElement>("admin-menu-tab"); adminTab.hidden = !admin;
    if (tab === "admin" && !admin) tab = "character";
    this.menuTab = tab;
    this.renderedMenuState = this.menuStateSignature();
    for (const button of document.querySelectorAll<HTMLButtonElement>("#menu-tabs button")) button.classList.toggle("active", button.dataset.tab === tab);
    const character = this.selectedCharacter(); const content = element("menu-content");
    if (tab === "admin") { this.renderAdminMenu(content); return; }
    if (tab === "loadout") { this.renderLoadoutSection(content, character); return; }
    if (tab === "crafting") { this.renderCraftingSection(content, character); return; }
    if (tab === "inventory") { this.renderInventorySection(content); return; }
    const equipped = resolvedWeapon(this.loadout.weapon, this.items);
    const pages: Record<string, string> = {
      character: `<h3>${escapeHTML(character?.name ?? "Character")}</h3><p>${titleCase(character?.class ?? "gunslinger")} · Level ${this.level}</p><p>${this.xpNext ? `${this.xp} / ${this.xpNext} XP to level ${this.level + 1}` : "Level cap reached"} · ${this.ledger.size} unlock${this.ledger.size === 1 ? "" : "s"} owned</p>${this.localEntity ? `<p>Health ${Math.ceil(this.localEntity.health)} / ${Math.ceil(this.localEntity.maxHealth)} · Resource ${Math.floor(this.localEntity.mana)}</p>` : ""}<p>Progression unlocks options, never raw combat power.</p>`,
      world: this.renderWorldSection(),
      reference: `<h3>Field reference</h3><p>WASD/Arrows move · pointer aims · primary pointer fires · Shift or the secondary pointer button scopes a weapon that has a scope · 1–6 or the wheel select an equipped slot · Space dashes · R reloads · E interacts. Every gun kicks in a fixed pattern and spreads while you move; a raised shield covers a frontal arc, slows you, and locks fire. The hub is safe. Combat is server-authoritative and raw time-to-kill is about ${equipped ? damageBandFor(equipped).target_ttk_seconds : 3} seconds.</p>`,
      settings: "<h3>Settings</h3><p>Accessibility and interface-scale controls remain available on Home. Opening this menu does not pause the shared world.</p>",
    };
    content.innerHTML = pages[tab] ?? pages.character!;
  }

  private renderAdminMenu(content: HTMLElement, query = this.adminSearch): void {
    this.adminSearch = query;
    const selected = this.selectedAdminSpawn();
    const search = query.toLowerCase();
    const entries = Object.entries(entityDefinitions).filter(([, definition]) => definition.admin.spawnable && (definition.admin.name.toLowerCase().includes(search))).sort(([, left], [, right]) => left.admin.name.localeCompare(right.admin.name));
    const selectedEditor = this.adminSelected ? this.adminEditorMarkup(this.adminSelected) : "<p>Select mode lets you click any visible entity and edit the fields its archetype exposes.</p>";
    content.innerHTML = `<h3>Developer tools</h3><p>Choose a pointer mode. The floating panel stays interactive while movement and the world remain under your control.</p><div class="admin-modes">${(["off", "spawn", "select", "delete"] as const).map((mode) => `<button data-admin-mode="${mode}" aria-pressed="${this.adminMode === mode}" class="${mode === "delete" ? "danger-button" : ""}">${titleCase(mode)}</button>`).join("")}</div><label>Search spawnables<input id="admin-spawn-search" value="${escapeHTML(query)}" placeholder="Player, projectile, tree…" /></label><div id="admin-spawn-list" class="admin-spawn-list">${entries.map(([id, definition]) => `<button data-admin-spawn="${escapeHTML(id)}" aria-pressed="${id === this.adminSpawnID}"><strong>${escapeHTML(definition.admin.name)}</strong><small>${escapeHTML(id)}</small></button>`).join("") || "<p>No spawnables match.</p>"}</div>${selected ? this.adminConfigMarkup(selected) : "<p class=\"error\">No spawnable is configured.</p>"}<section class="admin-selected"><h4>Selected entity</h4>${selectedEditor}</section><form id="admin-progress-form" novalidate><h4>Set level</h4><p>Levelling is the only thing that grants content, and a player kill is its only trigger until mobs land. This reaches the same grant path.</p>${this.adminFieldMarkup(progressionTable.admin_grant, "admin-level", this.adminLevel)}<button class="secondary" type="submit">Apply to your character</button></form><form id="admin-materials-form" novalidate><h4>Grant materials</h4><label>Material<select id="admin-material-select">${Object.keys(materialsTable.materials).sort().map((id) => `<option value="${escapeHTML(id)}"${id === this.adminMaterialID ? " selected" : ""}>${escapeHTML(materialsTable.materials[id]!.name)}</option>`).join("")}</select></label>${this.adminFieldMarkup(materialsTable.admin_grant, "admin-material-count", this.adminMaterialCount)}<button class="secondary" type="submit">Grant to your character</button></form><p id="admin-notice" class="error" role="status"></p>`;
    for (const button of document.querySelectorAll<HTMLButtonElement>("[data-admin-mode]")) button.addEventListener("click", () => this.setAdminMode(button.dataset.adminMode as typeof this.adminMode));
    element<HTMLInputElement>("admin-spawn-search").addEventListener("input", (event) => { this.adminSearch = (event.currentTarget as HTMLInputElement).value; this.renderAdminMenu(content, this.adminSearch); });
    for (const button of document.querySelectorAll<HTMLButtonElement>("[data-admin-spawn]")) button.addEventListener("click", () => { this.adminSpawnID = button.dataset.adminSpawn ?? ""; this.adminSpawnConfig = this.defaultAdminConfig(this.adminSpawnID); this.renderAdminMenu(content, query); this.updateAdminHUD(); });
    for (const input of document.querySelectorAll<HTMLInputElement>("[data-admin-config]")) input.addEventListener("input", () => { this.adminSpawnConfig[input.dataset.adminConfig ?? ""] = input.value; this.updateRotationDisplay(input); });
    for (const select of document.querySelectorAll<HTMLSelectElement>("select[data-admin-config]")) select.addEventListener("change", () => { this.adminSpawnConfig[select.dataset.adminConfig ?? ""] = select.value; });
    for (const input of document.querySelectorAll<HTMLInputElement>("[data-admin-edit]")) input.addEventListener("input", () => { this.adminEditDraft[input.dataset.adminEdit ?? ""] = input.value; this.updateRotationDisplay(input); });
    for (const select of document.querySelectorAll<HTMLSelectElement>("select[data-admin-edit]")) select.addEventListener("change", () => { this.adminEditDraft[select.dataset.adminEdit ?? ""] = select.value; });
    for (const input of document.querySelectorAll<HTMLInputElement>("[data-admin-vector-axis]")) input.addEventListener("input", () => this.updateAdminVector(input));
    for (const button of document.querySelectorAll<HTMLButtonElement>("[data-admin-position-pick]")) button.addEventListener("click", () => {
      this.adminPositionPick = button.dataset.adminPositionPick;
      this.updateAdminHUD();
      this.setMenuCollapsed(true);
      this.notice("Click the world to choose the new position.");
    });
    element<HTMLInputElement>("admin-material-count").addEventListener("input", (event) => { this.adminMaterialCount = (event.currentTarget as HTMLInputElement).value; });
    document.getElementById("admin-entity-form")?.addEventListener("submit", (event) => void this.applyAdminEntity(event as SubmitEvent));
    element<HTMLSelectElement>("admin-material-select").addEventListener("change", (event) => { this.adminMaterialID = (event.currentTarget as HTMLSelectElement).value; });
    element<HTMLInputElement>("admin-level").addEventListener("input", (event) => { this.adminLevel = (event.currentTarget as HTMLInputElement).value; });
    element<HTMLFormElement>("admin-materials-form").addEventListener("submit", (event) => void this.grantAdminMaterials(event));
    element<HTMLFormElement>("admin-progress-form").addEventListener("submit", (event) => void this.grantAdminLevel(event));
  }

  private adminConfigMarkup(selected: EntityDefinition): string {
    const fields = selected.admin.fields.filter((field) => field.scope === "spawn" || field.scope === "both");
    return `<section class="admin-config"><h4>Place ${escapeHTML(selected.admin.name)}</h4>${fields.map((field) => this.adminFieldMarkup(field, `admin-config-${field.attribute}`, this.adminSpawnConfig[field.attribute] ?? field.default, "data-admin-config")).join("")}</section>`;
  }

  private adminFieldMarkup(field: AdminField, inputID: string, value: string, dataAttribute = ""): string {
    const binding = dataAttribute ? `${dataAttribute}="${escapeHTML(field.attribute)}"` : "";
    if (field.input === "select") return `<label>${escapeHTML(field.label)}<select id="${escapeHTML(inputID)}" ${binding}>${(field.options ?? []).map((option) => `<option value="${escapeHTML(option.value)}"${option.value === value ? " selected" : ""}>${escapeHTML(option.label)}</option>`).join("")}</select></label>`;
    if (field.input === "text") return `<label>${escapeHTML(field.label)}<input id="${escapeHTML(inputID)}" ${binding} type="text" maxlength="${field.max_length ?? 1}" value="${escapeHTML(value)}" /></label>`;
    if (field.input === "position") {
      const [x, y] = adminPositionValue(value);
      return `<div class="admin-vector"><span>${escapeHTML(field.label)}</span><div class="admin-vector-inputs"><label>X<input type="number" data-admin-vector-axis="x" data-admin-vector-field="${escapeHTML(field.attribute)}" value="${escapeHTML(String(x))}" /></label><label>Y<input type="number" data-admin-vector-axis="y" data-admin-vector-field="${escapeHTML(field.attribute)}" value="${escapeHTML(String(y))}" /></label></div><button type="button" class="secondary" data-admin-position-pick="${escapeHTML(field.attribute)}">Pick from world</button><input id="${escapeHTML(inputID)}" type="hidden" ${binding} data-admin-vector-value="${escapeHTML(field.attribute)}" value="${escapeHTML(value)}" /></div>`;
    }
    if (field.input === "rotation") {
      const angle = Number(value) || 0;
      return `<label class="admin-rotation"><span>${escapeHTML(field.label)}</span><div class="admin-rotation-row"><span class="rotation-indicator"><i style="transform:rotate(${angle}deg)"></i></span><input id="${escapeHTML(inputID)}" ${binding} type="range" min="${field.min ?? -180}" max="${field.max ?? 180}" step="${field.step ?? 1}" value="${escapeHTML(String(angle))}" /><output>${Math.round(angle)}°</output></div></label>`;
    }
    return `<label>${escapeHTML(field.label)}<input id="${escapeHTML(inputID)}" ${binding} type="number" value="${escapeHTML(value)}" /></label>`;
  }

  private adminEditorMarkup(state: AdminEntityState): string {
    const definition = entityDefinitions[state.definition_id];
    if (!definition) return `<p class="error">${escapeHTML(state.id)} has no client tuning schema.</p>`;
    const fields = definition.admin.fields.filter((field) => (field.scope === "edit" || field.scope === "both") && state.values[field.attribute] !== undefined);
    return `<form id="admin-entity-form" novalidate><p><strong>${escapeHTML(state.id)}</strong> · ${escapeHTML(definition.admin.name)}</p>${fields.map((field) => this.adminFieldMarkup(field, `admin-edit-${field.attribute}`, this.adminEditDraft[field.attribute] ?? state.values[field.attribute]!, "data-admin-edit")).join("")}<button class="secondary" type="submit">Apply entity attributes</button></form>`;
  }

  private updateAdminSelectedFromSnapshot(entities: Entity[]): void {
    if (!this.adminSelected) return;
    const entity = entities.find((candidate) => candidate.id === this.adminSelected?.id);
    if (!entity) return;
    const values = { ...this.adminSelected.values };
    const update = (attribute: string, value: string) => { if (values[attribute] !== undefined) values[attribute] = value; };
    update("transform.position", JSON.stringify([roundAdminCoordinate(entity.x), roundAdminCoordinate(entity.y)]));
    update("physics.mass", formatAdminNumber(entity.mass));
    update("vitals.health", formatAdminNumber(entity.health));
    update("vitals.max_health", formatAdminNumber(entity.maxHealth));
    update("player.name", entity.name);
    update("player.class", entity.className);
    update("render.element", entity.element || "none");
    if (values["transform.heading_degrees"] !== undefined) {
      const x = entity.vx || entity.aimX, y = entity.vy || entity.aimY;
      update("transform.heading_degrees", formatAdminNumber(Math.atan2(y, x) * 180 / Math.PI));
    }
    if (JSON.stringify(values) !== JSON.stringify(this.adminSelected.values)) this.adminSelected = { ...this.adminSelected, values };
  }

  private updateAdminVector(input: HTMLInputElement): void {
    const field = input.dataset.adminVectorField ?? "", root = input.closest<HTMLElement>(".admin-vector");
    const x = root?.querySelector<HTMLInputElement>('[data-admin-vector-axis="x"]'), y = root?.querySelector<HTMLInputElement>('[data-admin-vector-axis="y"]'), hidden = root?.querySelector<HTMLInputElement>("[data-admin-vector-value]");
    if (!x || !y || !hidden) return;
    hidden.value = `[${x.value},${y.value}]`;
    if (hidden.dataset.adminEdit !== undefined) this.adminEditDraft[field] = hidden.value;
    if (hidden.dataset.adminConfig !== undefined) this.adminSpawnConfig[field] = hidden.value;
  }

  private updateRotationDisplay(input: HTMLInputElement): void {
    if (input.type !== "range") return;
    const row = input.closest<HTMLElement>(".admin-rotation-row");
    const indicator = row?.querySelector<HTMLElement>(".rotation-indicator i"), output = row?.querySelector<HTMLOutputElement>("output");
    if (indicator) indicator.style.transform = `rotate(${Number(input.value) || 0}deg)`;
    if (output) output.value = `${Math.round(Number(input.value) || 0)}°`;
  }

  private selectedAdminSpawn(): EntityDefinition | undefined { return entityDefinitions[this.adminSpawnID]; }

  private defaultAdminConfig(id: string): Record<string, string> {
    const values: Record<string, string> = {};
    for (const field of entityDefinitions[id]?.admin.fields ?? []) if (field.scope === "spawn" || field.scope === "both") values[field.attribute] = field.default;
    return values;
  }

  private setDeveloperMode(enabled: boolean): void {
    this.setAdminMode(enabled ? "spawn" : "off");
  }

  private setAdminMode(mode: typeof this.adminMode): void {
    if (mode !== "off" && !this.api.account?.is_admin) return;
    this.adminMode = mode;
    this.adminPositionPick = undefined;
    document.body.classList.toggle("developer-mode", mode !== "off");
    this.updateAdminHUD();
    const menu = element<HTMLDialogElement>("menu-dialog"); if (menu.open && this.menuTab === "admin") this.renderAdminMenu(element("menu-content"));
  }

  private updateAdminHUD(): void {
    const hud = element("developer-mode-hud"), label = this.selectedAdminSpawn()?.admin.name ?? "entity";
    hud.hidden = this.adminMode === "off" && !this.adminPositionPick;
    element("developer-mode-selection").textContent = this.adminPositionPick ? "Pick a world position" : this.adminMode === "spawn" ? `Placing: ${label}` : this.adminMode === "select" ? "Selecting entity attributes" : this.adminMode === "delete" ? "Deleting entities" : "";
  }

  private async useAdminPointer(event: PointerEvent): Promise<void> {
    if (this.adminPositionPick && this.adminSelected && this.view) {
      event.preventDefault();
      const point = this.view.worldAtPointer(event.clientX, event.clientY);
      this.adminEditDraft[this.adminPositionPick] = JSON.stringify([roundAdminCoordinate(point.x), roundAdminCoordinate(point.y)]);
      this.adminPositionPick = undefined;
      this.updateAdminHUD();
      this.setMenuCollapsed(false);
      this.renderMenu("admin");
      this.notice("World position selected. Apply entity attributes to commit it.");
      return;
    }
    if (this.adminMode === "spawn") { await this.placeAdminEntity(event); return; }
    const character = this.activeCharacter, target = this.view?.entityAtPointer(event.clientX, event.clientY);
    if (!character || !target) { this.notice("No entity at that point."); return; }
    event.preventDefault();
    try {
      if (this.adminMode === "delete") { await this.api.adminEntityDelete(character.id, target.id); this.adminSelected = undefined; this.notice(`Removed ${target.name || target.className || target.id}.`); }
      else { this.adminSelected = await this.api.adminEntityInspect(character.id, target.id); this.adminEditDraft = {}; this.notice(`Selected ${target.name || target.className || target.id}.`); }
      const menu = element<HTMLDialogElement>("menu-dialog"); if (menu.open && this.menuTab === "admin") this.renderAdminMenu(element("menu-content"));
    } catch (error) { this.notice(`Admin action rejected: ${messageOf(error)}`); }
  }

  private async placeAdminEntity(event: PointerEvent): Promise<void> {
    const character = this.activeCharacter, spawnable = this.selectedAdminSpawn();
    if (!character || !spawnable || !this.view) return;
    event.preventDefault();
    const point = this.view.worldAtPointer(event.clientX, event.clientY);
    try {
      await this.api.adminSpawn({ character_id: character.id, spawn_id: this.adminSpawnID, x: point.x, y: point.y, config: this.adminSpawnConfig });
      this.notice(`Placed ${spawnable.admin.name}.`);
    } catch (error) { this.notice(`Placement rejected: ${messageOf(error)}`); }
  }

  /**
   * Grants materials to the developer's own character. The world validates the
   * ID and bounds the count against the same catalog row the form renders, so
   * the browser never decides what a grant may be.
   */
  private async grantAdminMaterials(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!this.activeCharacter) return;
    const count = Number(element<HTMLInputElement>("admin-material-count").value);
    const notice = element("admin-notice"); notice.textContent = "";
    try {
      await this.api.adminMaterials(this.activeCharacter.id, { [this.adminMaterialID]: count });
      notice.textContent = `Granted ${count} ${materialName(this.adminMaterialID)}.`; notice.classList.remove("error");
    } catch (error) { notice.textContent = messageOf(error); notice.classList.add("error"); }
  }

  /**
   * Sets the character's level from developer mode. The server grants whatever
   * the levels unlock and pushes the change back on its own progress path, so
   * nothing here has to guess what a level is worth.
   */
  private async grantAdminLevel(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!this.activeCharacter) return;
    const level = Number(element<HTMLInputElement>("admin-level").value);
    const notice = element("admin-notice"); notice.textContent = "";
    try {
      await this.api.adminProgress(this.activeCharacter.id, level);
      notice.textContent = `Set to level ${level}. New options appear in Loadout and Crafting.`; notice.classList.remove("error");
    } catch (error) { notice.textContent = messageOf(error); notice.classList.add("error"); }
  }

  private async applyAdminEntity(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!this.activeCharacter || !this.adminSelected) return;
    const attributes: Record<string, string> = {};
    for (const input of document.querySelectorAll<HTMLInputElement | HTMLSelectElement>("[data-admin-edit]")) attributes[input.dataset.adminEdit ?? ""] = input.value;
    const notice = element("admin-notice"); notice.textContent = "";
    try { this.adminSelected = await this.api.adminEntityEdit(this.activeCharacter.id, this.adminSelected.id, attributes); this.adminEditDraft = {}; this.notice("Entity attributes applied."); this.renderMenu("admin"); }
    catch (error) { notice.textContent = messageOf(error); notice.classList.add("error"); }
  }

  /**
   * The Loadout section: viewable anywhere, editable only in safety. Editing
   * builds a draft and commits it as one request; nothing is shown as equipped
   * until the server confirms it.
   */
  private renderLoadoutSection(content: HTMLElement, character: Character | undefined): void {
    if (!character) { content.innerHTML = "<h3>Loadout</h3><p>No character selected.</p>"; return; }
    const set = this.draft ?? this.loadout;
    const slots = bar(character.class, set, this.items);
    const editable = this.inSafety;
    const problem = loadoutProblem(character.class, this.ledger, set, this.items);
    content.replaceChildren();
    content.append(heading("Loadout"));
    const lock = document.createElement("p");
    lock.className = editable ? "status good" : "warning";
    lock.textContent = editable
      ? "You are inside a safe zone. Respec is free and takes effect immediately."
      : "Locked: the equipped set can only be changed inside a safe zone. You committed to this kit when you left.";
    content.append(lock);
    if (this.respecOwed) { const patch = document.createElement("p"); patch.className = "status good"; patch.textContent = "A balance patch re-validated this loadout. Rearranging it costs nothing."; content.append(patch); }

    const list = document.createElement("div"); list.className = "loadout-slots";
    list.append(this.slotRow(character, "weapon", 0, set.weapon, editable, "Weapon"));
    const handling = document.createElement("p");
    handling.textContent = weaponHandling(resolvedWeapon(set.weapon, this.items));
    if (handling.textContent) list.append(handling);
    for (const slot of slots) {
      if (slot.kind === "weapon") continue;
      const index = character.class === "gunslinger" ? slot.index - 1 : slot.index;
      const stored = (character.class === "gunslinger" ? set.gadgets : set.spells)[index] ?? "";
      list.append(this.slotRow(character, slot.kind, index, stored, editable, `Slot ${slot.index + 1}`));
    }
    content.append(list);

    const message = document.createElement("p");
    message.className = problem ? "error" : "status good";
    message.textContent = problem ?? this.loadoutStatus;
    content.append(message);

    const save = document.createElement("button");
    save.className = "primary"; save.textContent = this.draft ? "Commit loadout" : "No changes";
    save.disabled = !editable || !this.draft || Boolean(problem);
    save.addEventListener("click", () => this.commitLoadout(set));
    content.append(save);
  }

  /** Adopts what the server says the character owns and carries. */
  private applyInventory(message: ServerMessage): void {
    this.items = message.items; this.materials = message.materials;
    if (message.kind === ServerKind.Craft) {
      this.craftStatus = message.error || "Crafted. The item is in your inventory and can be equipped in the Loadout section.";
      if (message.error) this.notice(message.error); else this.craftChoices = {};
    }
    this.renderAbilityBar();
    this.refreshOpenMenu();
  }

  /**
   * The Crafting section: a blueprint's slots, the components that fit each one,
   * what the build costs against what is carried, and what it changes in plain
   * language. Like the loadout it is viewable anywhere and usable only in
   * safety, because raw materials have to be hauled back before they are worth
   * anything.
   */
  private renderCraftingSection(content: HTMLElement, character: Character | undefined): void {
    if (!character) { content.innerHTML = "<h3>Crafting</h3><p>No character selected.</p>"; return; }
    const buildable = craftable(character.class, this.ledger);
    if (!this.craftWeapon || !buildable.includes(this.craftWeapon)) this.craftWeapon = buildable[0] ?? "";
    content.replaceChildren();
    content.append(heading("Crafting"));

    const lock = document.createElement("p");
    lock.className = this.inSafety ? "status good" : "warning";
    lock.textContent = this.inSafety
      ? "You are inside a safe zone. Crafting spends the materials you are carrying."
      : "Locked: crafting is only available inside a safe zone. Haul your materials back to spend them.";
    content.append(lock);

    if (!this.craftWeapon) {
      const empty = document.createElement("p");
      const next = lockedCraftable(character.class, this.ledger)[0];
      empty.textContent = next
        ? `You have not unlocked a weapon to build from yet. ${next.name} unlocks at level ${next.level}.`
        : "You have not unlocked a weapon to build from yet.";
      content.append(empty);
      return;
    }

    const recipe = recipeOf(this.craftWeapon)!;
    const workbench = document.createElement("div"); workbench.className = "craft-workbench";
    const blueprint = document.createElement("div"); blueprint.className = `craft-blueprint ${recipe.blueprint}`;
    const blueprintTitle = document.createElement("h4");
    blueprintTitle.textContent = recipe.blueprint === "staff" ? "Staff assembly" : "Generic weapon blueprint";
    const blueprintSummary = document.createElement("p"); blueprintSummary.textContent = recipe.summary;
    const silhouette = document.createElement("div"); silhouette.className = "craft-silhouette"; silhouette.setAttribute("aria-hidden", "true");
    blueprint.append(blueprintTitle, blueprintSummary, silhouette);

    const choosePart = (slot: string, id: string): void => {
      const next = { ...this.craftChoices, [slot]: id };
      // Changing one half of a staff may invalidate the other half. Clear the
      // incompatible old half instead of presenting a build the server refuses.
      if (recipe.blueprint === "staff") {
        const other = slot === "crystal" ? "stave" : "crystal";
        if (next[other] && !fitting(this.craftWeapon, other, next).includes(next[other]!)) delete next[other];
      }
      this.craftChoices = next; this.craftStatus = ""; this.renderMenu("crafting");
    };

    const slots = document.createElement("div"); slots.className = "craft-drop-slots";
    for (const slot of slotsOf(this.craftWeapon)) {
      const drop = document.createElement("div"); drop.className = "craft-drop-slot"; drop.dataset.slot = slot;
      const label = document.createElement("strong"); label.textContent = titleCase(slot);
      const selected = componentOf(this.craftChoices[slot] ?? "");
      const value = document.createElement("span"); value.textContent = selected ? `${selected.name} · T${selected.tier}` : "Drop a compatible part here";
      if (selected) drop.classList.add("filled");
      drop.addEventListener("dragover", (event) => {
        if (this.inSafety) event.preventDefault();
      });
      drop.addEventListener("drop", (event) => {
        event.preventDefault();
        const id = event.dataTransfer?.getData("text/plain") ?? "";
        if (fitting(this.craftWeapon, slot, this.craftChoices).includes(id)) choosePart(slot, id);
      });
      if (selected) {
        const clear = document.createElement("button"); clear.className = "craft-clear"; clear.textContent = "Remove";
        clear.disabled = !this.inSafety;
        clear.addEventListener("click", () => { delete this.craftChoices[slot]; this.craftStatus = ""; this.renderMenu("crafting"); });
        drop.append(label, value, clear);
      } else drop.append(label, value);
      slots.append(drop);
    }
    blueprint.append(slots);

    const preview = document.createElement("div"); preview.className = "craft-result";
    const result = resultOf(this.craftChoices);
    preview.innerHTML = result
      ? `<strong>Result: ${weapons[result]?.name ?? result}</strong><span>Recipe complete and ready to craft.</span>`
      : `<strong>Preview: ${weapons[this.craftWeapon]?.name ?? this.craftWeapon}</strong><span>Fill every blank to complete this recipe.</span>`;
    blueprint.append(preview);

    const recipes = document.createElement("aside"); recipes.className = "craft-recipes";
    const recipeTitle = document.createElement("h4"); recipeTitle.textContent = recipe.blueprint === "staff" ? "Staff recipe" : "Craftable gun recipes";
    recipes.append(recipeTitle);
    for (const id of buildable) {
      const candidate = recipeOf(id);
      if (!candidate || candidate.blueprint !== recipe.blueprint) continue;
      const button = document.createElement("button"); button.className = id === this.craftWeapon ? "active" : "";
      button.innerHTML = `<strong>${weapons[id]?.name ?? id}</strong><small>${candidate.summary}</small>`;
      button.addEventListener("click", () => { this.craftWeapon = id; this.craftChoices = {}; this.craftStatus = ""; this.renderMenu("crafting"); });
      recipes.append(button);
    }
    for (const locked of lockedCraftable(character.class, this.ledger)) {
      const candidate = recipeOf(locked.id);
      if (!candidate || candidate.blueprint !== recipe.blueprint) continue;
      const button = document.createElement("button"); button.disabled = true;
      button.innerHTML = `<strong>${locked.name} · level ${locked.level}</strong><small>${candidate.summary}</small>`;
      recipes.append(button);
    }
    workbench.append(blueprint, recipes); content.append(workbench);

    const tray = document.createElement("div"); tray.className = "craft-parts";
    const trayTitle = document.createElement("h4"); trayTitle.textContent = recipe.blueprint === "staff" ? "Crystal and stave recipes" : "Compatible parts";
    tray.append(trayTitle);
    for (const slot of slotsOf(this.craftWeapon)) {
      const group = document.createElement("section");
      const groupTitle = document.createElement("strong"); groupTitle.textContent = titleCase(slot); group.append(groupTitle);
      for (const id of fitting(this.craftWeapon, slot, this.craftChoices)) {
        const part = componentOf(id)!;
        const price = Object.keys(part.cost).sort().map((material) => `${part.cost[material]} ${materialName(material)}`).join(", ");
        const button = document.createElement("button"); button.className = "craft-part"; button.draggable = this.inSafety;
        button.disabled = !this.inSafety;
        button.innerHTML = `<strong>${part.name} · T${part.tier}</strong><small>${part.effect}</small><small class="craft-part-cost">Recipe: ${price}</small>`;
        button.addEventListener("dragstart", (event) => event.dataTransfer?.setData("text/plain", id));
        button.addEventListener("click", () => choosePart(slot, id));
        group.append(button);
      }
      tray.append(group);
    }
    content.append(tray);

    // What this build does in player-facing language rather than raw multipliers.
    const behaviour = describe(this.craftWeapon, this.craftChoices);
    const effects = document.createElement("ul"); effects.className = "craft-effects";
    if (behaviour.length) {
      for (const line of behaviour) { const item = document.createElement("li"); item.textContent = line; effects.append(item); }
    } else {
      const item = document.createElement("li");
      item.textContent = "Stock configuration: this weapon behaves exactly as its category does.";
      effects.append(item);
    }
    content.append(effects);

    const required = craftCost(this.craftWeapon, this.craftChoices);
    const missing = shortfall(required, this.materials);
    content.append(this.materialCostList(required, missing));

    const message = document.createElement("p");
    message.className = Object.keys(missing).length ? "error" : "status good";
    message.textContent = Object.keys(missing).length
      ? `Short ${Object.keys(missing).sort().map((id) => `${missing[id]} ${materialName(id)}`).join(", ")}.`
      : this.craftStatus;
    content.append(message);

    const full = this.items.length >= progressionTable.crafted_item_capacity;
    if (full) {
      const capacity = document.createElement("p"); capacity.className = "warning";
      capacity.textContent = `You are carrying ${this.items.length} of ${progressionTable.crafted_item_capacity} crafted weapons. Nothing more can be built.`;
      content.append(capacity);
    }

    const build = document.createElement("button");
    build.className = "primary";
    build.textContent = Object.keys(required).length ? "Craft — spend materials" : "Craft (no materials required)";
    build.disabled = !this.inSafety || full || !result || result !== this.craftWeapon || Object.keys(missing).length > 0;
    build.addEventListener("click", () => this.commitCraft(required));
    content.append(build);

    this.renderAmmunitionSection(content, character.class);
  }

  /**
   * Special ammunition: a finite crafted resource rather than a weapon, so it
   * has no slots and no capacity — it lands in the carried inventory the weapon
   * that fires it spends from, and a death drops it like any other material.
   */
  private renderAmmunitionSection(content: HTMLElement, characterClass: CharacterClass): void {
    const recipes = buildableAmmunition(characterClass);
    if (!recipes.length) return;
    const heading = document.createElement("h4"); heading.textContent = "Special ammunition";
    content.append(heading);
    const explain = document.createElement("p");
    explain.textContent = "Heavy weapons spend crafted rounds instead of a magazine. There is no reload: when these run out, build more.";
    content.append(explain);
    for (const id of recipes) {
      const recipe = ammunitionTable[id]!;
      const missing = shortfall(recipe.cost, this.materials);
      const row = document.createElement("div"); row.className = "loadout-slot";
      const summary = document.createElement("span");
      const price = Object.keys(recipe.cost).sort().map((material) => `${recipe.cost[material]} ${materialName(material)}`).join(", ");
      summary.textContent = `${recipe.name} ×${recipe.count} — ${price} · ${this.materials[recipe.material] ?? 0} carried`;
      if (Object.keys(missing).length) summary.className = "error";
      const button = document.createElement("button");
      button.className = "secondary"; button.textContent = `Build ${recipe.count}`;
      button.disabled = !this.inSafety || Object.keys(missing).length > 0;
      button.addEventListener("click", () => this.commitAmmunition(id));
      row.append(summary, button);
      content.append(row);
    }
  }

  /** Sends one ammunition build. It answers on the same reply an ordinary craft does. */
  private commitAmmunition(recipe: string): void {
    if (!this.socket?.craftAmmunition(recipe)) {
      this.craftStatus = "Not connected. Nothing was spent."; this.renderMenu("crafting"); return;
    }
    this.craftStatus = "Building ammunition…"; this.renderMenu("crafting");
  }

  /** Owned and required materials side by side, with the shortfall named. */
  private materialCostList(required: Record<string, number>, missing: Record<string, number>): HTMLElement {
    const list = document.createElement("dl"); list.className = "craft-cost";
    if (!Object.keys(required).length) {
      const none = document.createElement("p"); none.textContent = "This build costs no materials.";
      return none;
    }
    for (const id of Object.keys(required).sort()) {
      const term = document.createElement("dt"); term.textContent = materialName(id);
      const value = document.createElement("dd");
      const carried = this.materials[id] ?? 0;
      value.textContent = `${carried} / ${required[id]} carried`;
      if (missing[id]) value.className = "error";
      list.append(term, value);
    }
    return list;
  }

  /**
   * Sends one build and shows it as pending. Nothing is deducted on screen: the
   * server answers with the authoritative materials and items either way, so a
   * refusal never leaves a spend the player did not make.
   */
  private commitCraft(required: Record<string, number>): void {
    const summary = Object.keys(required).sort().map((id) => `${required[id]} ${materialName(id)}`).join(", ");
    if (summary && !confirm(`Craft this ${weapons[this.craftWeapon]?.name ?? "weapon"}? It spends ${summary}.`)) return;
    if (!this.socket?.craft({ weapon: this.craftWeapon, components: { ...this.craftChoices } })) {
      this.craftStatus = "Not connected. Nothing was spent."; this.renderMenu("crafting"); return;
    }
    this.craftStatus = "Crafting…"; this.renderMenu("crafting");
  }

  /** Carried materials and owned crafted items — what crafting draws on and produces. */
  private renderInventorySection(content: HTMLElement): void {
    content.replaceChildren();
    content.append(heading("Inventory"));
    const carried = Object.keys(this.materials).filter((id) => (this.materials[id] ?? 0) > 0).sort();
    const materialsHeading = document.createElement("h4"); materialsHeading.textContent = "Carried materials";
    content.append(materialsHeading);
    if (!carried.length) {
      const empty = document.createElement("p");
      empty.textContent = "No carried materials. Harvesting is not available in this foundation, so materials arrive only through developer tools for now.";
      content.append(empty);
    } else {
      const list = document.createElement("dl"); list.className = "craft-cost";
      for (const id of carried) {
        const term = document.createElement("dt"); term.textContent = materialName(id);
        const value = document.createElement("dd"); value.textContent = String(this.materials[id]);
        list.append(term, value);
      }
      content.append(list);
      const warning = document.createElement("p"); warning.className = "warning";
      warning.textContent = "Carried materials are what a death drops. Crafted gear is kept.";
      content.append(warning);
    }
    const itemsHeading = document.createElement("h4"); itemsHeading.textContent = "Crafted weapons";
    content.append(itemsHeading);
    if (!this.items.length) {
      const empty = document.createElement("p"); empty.textContent = "Nothing crafted yet. A crafted weapon is equipped from the Loadout section like any other.";
      content.append(empty);
      return;
    }
    const owned = document.createElement("ul"); owned.className = "craft-effects";
    for (const item of this.items) {
      const row = document.createElement("li");
      row.textContent = itemLabel(item) + (item.id === this.loadout.weapon ? " · equipped" : "");
      owned.append(row);
    }
    content.append(owned);
  }

  /** One slot row: a select over the content this character may equip there. */
  private slotRow(character: Character, kind: SlotKind, index: number, id: string, editable: boolean, label: string): HTMLElement {
    const row = document.createElement("label"); row.className = "loadout-slot";
    const select = document.createElement("select");
    select.disabled = !editable;
    const options = equippable(character.class, this.ledger, kind, this.items);
    if (kind !== "weapon") select.append(new Option("Empty", ""));
    for (const option of options) select.append(new Option(contentName(kind, option, this.items), option));
    if (!options.length && kind !== "weapon") select.append(new Option(kind === "gadget" ? "No gadgets unlocked yet" : "No spells unlocked yet", "", true, true));
    appendLocked(select, locked(character.class, this.ledger, kind));
    select.value = id;
    select.addEventListener("change", () => { this.editDraft(kind, index, select.value); });
    row.append(document.createTextNode(label), select);
    return row;
  }

  private editDraft(kind: SlotKind, index: number, id: string): void {
    const draft = this.draft ?? { weapon: this.loadout.weapon, gadgets: [...this.loadout.gadgets], spells: [...this.loadout.spells] };
    if (kind === "weapon") draft.weapon = id;
    else if (kind === "gadget") draft.gadgets[index] = id;
    else draft.spells[index] = id;
    this.draft = draft; this.loadoutStatus = "";
    this.renderMenu("loadout");
  }

  private commitLoadout(set: LoadoutSet): void {
    // The server is the authority on both the lock and the rules; this only
    // avoids a request that is already known to be refused.
    if (!this.socket?.setLoadout(set)) { this.loadoutStatus = "Not connected. The change was not sent."; this.renderMenu("loadout"); return; }
    this.loadoutStatus = "Committing…"; this.renderMenu("loadout");
  }

  private selectedCharacter(): Character | undefined { const id = element<HTMLSelectElement>("character-select").value; return this.characters.find((character) => character.id === id); }

  private exitGame(): void {
    window.clearInterval(this.inputTimer); this.socket?.close(); this.socket = undefined; this.view?.destroy(); this.view = undefined; this.predictor = undefined; this.heldInputs.clear(); this.lastBand = ""; this.lastBiome = "";
    this.draft = undefined; this.selectedSlot = 0; this.inSafety = true; this.respecOwed = false; this.loadoutStatus = "";
    this.items = []; this.materials = {}; this.craftWeapon = ""; this.craftChoices = {}; this.craftStatus = "";
    this.ledger = ledgerOf([]); this.level = 1; this.xp = 0; this.xpNext = 0; this.localEntity = undefined; this.cooldowns = {};
    this.adminSelected = undefined; this.adminEditDraft = {}; this.adminPositionPick = undefined; this.renderedMenuState = "";
    element("ability-bar").replaceChildren(); element("touch-slots").replaceChildren();
    const menu = element<HTMLDialogElement>("menu-dialog"); if (menu.open) menu.close(); this.activeCharacter = undefined; this.setDeveloperMode(false); element("game").hidden = true; element("home").hidden = false; element("death-overlay").hidden = true; element("connection-overlay").hidden = true;
  }
}

function heading(text: string): HTMLElement { const node = document.createElement("h3"); node.textContent = text; return node; }

/**
 * iOS can suppress a delayed click after a touch gesture. Activate important
 * game controls on touch pointer-up, while retaining click for mouse/keyboard.
 */
function bindActivation(target: HTMLElement, activate: () => void): void {
  let lastTouch = -1000;
  target.addEventListener("pointerup", (event) => {
    if (event.pointerType === "mouse") return;
    event.preventDefault(); lastTouch = performance.now(); activate();
  });
  target.addEventListener("click", (event) => {
    if (performance.now() - lastTouch < 1500) { event.preventDefault(); return; }
    activate();
  });
}

/** Delegated counterpart for stable groups such as menu tabs and the hotbar. */
function bindDelegatedActivation(target: HTMLElement, selector: string, activate: (button: HTMLButtonElement) => void): void {
  let lastTouch = -1000;
  const buttonAt = (event: Event) => {
    const origin = event.target;
    if (!(origin instanceof Element)) return undefined;
    const button = origin.closest<HTMLButtonElement>(selector);
    return button && target.contains(button) ? button : undefined;
  };
  target.addEventListener("pointerup", (event) => {
    if (event.pointerType === "mouse") return;
    const button = buttonAt(event); if (!button) return;
    event.preventDefault(); lastTouch = performance.now(); activate(button);
  });
  target.addEventListener("click", (event) => {
    const button = buttonAt(event); if (!button) return;
    if (performance.now() - lastTouch < 1500) { event.preventDefault(); return; }
    activate(button);
  });
}

/**
 * How a weapon handles, in the terms the player actually feels: its weight
 * class, whether it scopes, and what it spends. Weight never states damage,
 * because weight never sets damage.
 */
function weaponHandling(weapon: ReturnType<typeof resolvedWeapon>): string {
  if (!weapon || !weapon.ability) return "";
  const weight = weightOf(weapon);
  const parts = [`${weight.name} — moves at ${Math.round(weight.movement_multiplier * 100)}% speed`];
  if (weapon.recoil?.pattern.length) parts.push(`${weapon.recoil.pattern.length}-shot recoil pattern`);
  if (weapon.scope) parts.push("scopes with Shift or the right pointer button");
  const spent = specialAmmunition(weapon);
  if (spent) parts.push(`spends crafted ${materialName(spent).toLowerCase()}s instead of a magazine`);
  return `${parts.join(" · ")}.`;
}

/** The action-bar slot a digit key selects, or undefined for any other key. */
function slotKey(code: string): number | undefined {
  const match = /^(?:Digit|Numpad)([1-9])$/.exec(code);
  if (!match) return undefined;
  const slot = Number(match[1]) - 1;
  return slot < barSlots ? slot : undefined;
}

function isFormField(target: EventTarget | null): boolean { return target instanceof HTMLInputElement || target instanceof HTMLSelectElement || target instanceof HTMLTextAreaElement; }

function adminPositionValue(value: string): [number, number] {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (Array.isArray(parsed) && parsed.length === 2 && parsed.every((coordinate) => typeof coordinate === "number" && Number.isFinite(coordinate))) return [parsed[0], parsed[1]];
  } catch { /* The server will report malformed submitted values. */ }
  return [0, 0];
}

function roundAdminCoordinate(value: number): number { return Math.round(value * 100) / 100; }
function formatAdminNumber(value: number): string { return Number.isFinite(value) ? String(Math.round(value * 100) / 100) : "0"; }
/**
 * Lists content the character has not unlocked as disabled options, labelled
 * with the level that grants it. Hiding a locked row entirely is what makes a
 * kit look empty rather than unfinished.
 */
function appendLocked(select: HTMLSelectElement, rows: LockedContent[]): void {
  for (const row of rows) {
    const option = new Option(`${row.name} — unlocks at level ${row.level}`, row.id);
    option.disabled = true;
    select.append(option);
  }
}

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
