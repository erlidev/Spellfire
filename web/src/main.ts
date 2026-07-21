import { API } from "./api";
import { componentOf, cost as craftCost, craftable, describe, fitting, itemLabel, materialName, resolvedWeapon, shortfall, slotsOf } from "./game/crafting";
import { bar, barSlots, contentName, defaultLoadout, equippable, ledgerOf, loadoutProblem, type Ledger, type SlotKind } from "./game/loadout";
import { Predictor } from "./game/prediction";
import { GameView } from "./game/view";
import { GameSocket } from "./net/socket";
import { adminTools, damageBandFor, dangerBandAt, materials as materialsTable, progression as progressionTable, resourceMax, safeRadius, session, weapons, world, xpToNext, type AdminAttribute, type AdminSpawnable, type AdminToolField } from "./tuning";
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
  private pressed = new Set<number>();
  private aim = { x: 1, y: 0 };
  private noticeTimer = 0;
  private lastBand = "";
  private activeCharacter?: Character;
  private developerMode = false;
  private adminSpawnID = Object.keys(adminTools.spawnables).sort()[0] ?? "";
  private adminSpawnConfig: Record<string, string> = this.defaultAdminConfig(this.adminSpawnID);
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
  private inSafety = true;
  private respecOwed = false;
  private loadoutStatus = "";
  private menuTab = "character";
  // What the character owns and carries. Both arrive on the welcome and change
  // only on a confirmed craft, so nothing here is ever inferred from a snapshot.
  private items: CraftedItem[] = [];
  private materials: Record<string, number> = {};
  // The unconfirmed build in the Crafting section: the weapon category and the
  // component chosen per slot. Nothing is shown as spent until a Craft reply
  // confirms it.
  private craftWeapon = "";
  private craftChoices: Record<string, string> = {};
  private craftStatus = "";
  private adminMaterialID = Object.keys(materialsTable.materials).sort()[0] ?? "";

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
    element("exit-button").addEventListener("click", () => { if (confirm(`Exit to Home? Your body stays in the world for ${session.logout_linger_seconds} seconds after you leave and can still be attacked.`)) this.exitGame(); });
    element("connection-cancel").addEventListener("click", () => this.exitGame());
    element("respawn-button").addEventListener("click", () => this.socket?.respawn());
    element("developer-mode-exit").addEventListener("click", () => this.setDeveloperMode(false));
  }

  private bindControls(): void {
    const keyMap: Record<string, number> = { KeyW: Buttons.Up, ArrowUp: Buttons.Up, KeyS: Buttons.Down, ArrowDown: Buttons.Down, KeyA: Buttons.Left, ArrowLeft: Buttons.Left, KeyD: Buttons.Right, ArrowRight: Buttons.Right, Space: Buttons.Dash, KeyR: Buttons.Reload, KeyE: Buttons.Interact };
    window.addEventListener("keydown", (event) => {
      // 1–6 select the equipped slot the use button acts through: a Mage's six
      // spells, a Gunslinger's weapon and five gadgets.
      const slot = slotKey(event.code);
      if (slot !== undefined && !isFormField(event.target)) { event.preventDefault(); this.selectSlot(slot); return; }
      const button = keyMap[event.code]; if (button && !isFormField(event.target)) { event.preventDefault(); this.pressed.add(button); }
    });
    window.addEventListener("keyup", (event) => { const button = keyMap[event.code]; if (button) this.pressed.delete(button); });
    window.addEventListener("pointermove", (event) => { if (!this.view) return; this.aim = this.view.pointerWorld(event.clientX, event.clientY); });
    element("canvas-host").addEventListener("pointerdown", (event) => {
      if ((event as PointerEvent).button !== 0) return;
      if (this.developerMode) { void this.placeAdminEntity(event as PointerEvent); return; }
      this.pressed.add(Buttons.Fire);
    });
    window.addEventListener("pointerup", (event) => { if ((event as PointerEvent).button === 0) this.pressed.delete(Buttons.Fire); });
    element("canvas-host").addEventListener("contextmenu", (event) => event.preventDefault());
    // The wheel steps through the same slots, wrapping in both directions.
    element("canvas-host").addEventListener("wheel", (event) => { event.preventDefault(); this.selectSlot((this.selectedSlot + (event.deltaY > 0 ? 1 : barSlots - 1)) % barSlots); }, { passive: false });
    element("touch-slots").addEventListener("click", (event) => { const button = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-slot]"); if (button) this.selectSlot(Number(button.dataset.slot)); });
    const touchMap: Record<string, number> = { up: Buttons.Up, down: Buttons.Down, left: Buttons.Left, right: Buttons.Right, fire: Buttons.Fire, dash: Buttons.Dash, interact: Buttons.Interact };
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
    if (message.kind === ServerKind.Welcome) this.predictor.initialize(local); else this.predictor.reconcile(local);
    this.updateHUD(local); element("connection-overlay").hidden = true; element("death-overlay").hidden = local.alive;
  }

  private simulateInput(): void {
    if (!this.predictor || element("game").hidden) return;
    const blocked = element<HTMLDialogElement>("menu-dialog").open;
    let buttons = 0; if (!blocked) for (const value of this.pressed) buttons |= value;
    const input = this.predictor.step(buttons, this.aim.x, this.aim.y, this.selectedSlot, performance.now()); this.socket?.sendInput(input);
  }

  private selectSlot(slot: number): void {
    if (element<HTMLDialogElement>("menu-dialog").open || slot < 0 || slot >= barSlots) return;
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
    if (this.menuTab === "loadout" && element<HTMLDialogElement>("menu-dialog").open) this.renderMenu("loadout");
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
    if (message.kind === ServerKind.Welcome) return;
    if (levelled) {
      const gained = this.ledger.size - before;
      this.notice(gained > 0
        ? `Level ${this.level}. ${gained} new option${gained === 1 ? "" : "s"} unlocked — respec is free in any safe zone.`
        : `Level ${this.level}.`);
    }
    if (element<HTMLDialogElement>("menu-dialog").open) this.renderMenu(this.menuTab);
  }

  private renderAbilityBar(): void {
    const character = this.selectedCharacter(); if (!character) return;
    const slots = bar(character.class, this.loadout, this.items);
    const label = (index: number) => `${index + 1}`;
    element("ability-bar").replaceChildren(...slots.map((slot) => {
      const cell = document.createElement("div");
      cell.className = slot.index === this.selectedSlot ? "slot selected" : "slot";
      cell.innerHTML = `<kbd>${label(slot.index)}</kbd><span>${escapeHTML(slot.name || "Empty")}</span>`;
      return cell;
    }));
    element("touch-slots").replaceChildren(...slots.map((slot) => {
      const button = document.createElement("button");
      button.dataset.slot = String(slot.index); button.className = slot.index === this.selectedSlot ? "selected" : "";
      button.textContent = label(slot.index);
      button.setAttribute("aria-label", `Slot ${slot.index + 1}: ${slot.name || "empty"}`);
      button.setAttribute("aria-pressed", String(slot.index === this.selectedSlot));
      return button;
    }));
  }

  private connectionStatus(state: "connecting" | "connected" | "reconnecting" | "failed", detail?: string): void {
    const overlay = element("connection-overlay");
    if (state === "connected") { element("connection-message").textContent = "Synchronizing world state…"; return; }
    overlay.hidden = false; element("connection-title").textContent = state === "failed" ? "Connection failed" : state === "reconnecting" ? "Reconnecting" : "Connecting"; element("connection-message").textContent = detail ?? (state === "reconnecting" ? "Gameplay input is paused." : "Contacting the world server…");
  }

  private updateHUD(entity: Entity): void {
    const health = Math.max(0, entity.health / Math.max(1, entity.maxHealth)); element("health-bar").style.width = `${health * 100}%`; element("health-label").textContent = `${Math.ceil(entity.health)} / ${Math.ceil(entity.maxHealth)}`;
    const { label, max } = resourceMax(resolvedWeapon(this.loadout.weapon, this.items)), resource = entity.mana; element("resource-label").innerHTML = `${label} <span>${Math.floor(resource)} / ${max}</span>`; element("resource-bar").style.width = `${Math.max(0, resource / max) * 100}%`;
    const distance = Math.hypot(entity.x, entity.y), band = dangerBandAt(distance);
    element("danger-text").textContent = `${band.name} · ${band.summary}`; element("danger-shape").textContent = band.shape;
    if (band.name !== this.lastBand && this.lastBand) this.notice(`${band.name}: ${band.summary}`); this.lastBand = band.name;
    // Crossing out of safety locks the equipped set. Warn at the crossing, not
    // only when the player later opens the menu and finds the controls dead.
    const safe = distance <= safeRadius;
    if (safe !== this.inSafety) {
      this.notice(safe ? "Safe zone: loadout unlocked." : "You left the safe zone. Your loadout is locked until you return.");
      if (this.menuTab === "loadout" && element<HTMLDialogElement>("menu-dialog").open) this.renderMenu("loadout");
    }
    this.inSafety = safe;
  }

  private notice(message: string): void { const notice = element("world-notice"); notice.textContent = message; notice.classList.add("visible"); window.clearTimeout(this.noticeTimer); this.noticeTimer = window.setTimeout(() => notice.classList.remove("visible"), 2600); }

  private renderMenu(tab: string): void {
    const admin = Boolean(this.api.account?.is_admin);
    const adminTab = element<HTMLButtonElement>("admin-menu-tab"); adminTab.hidden = !admin;
    if (tab === "admin" && !admin) tab = "character";
    this.menuTab = tab;
    for (const button of document.querySelectorAll<HTMLButtonElement>("#menu-tabs button")) button.classList.toggle("active", button.dataset.tab === tab);
    const character = this.selectedCharacter(); const content = element("menu-content");
    if (tab === "admin") { this.renderAdminMenu(content); return; }
    if (tab === "loadout") { this.renderLoadoutSection(content, character); return; }
    if (tab === "crafting") { this.renderCraftingSection(content, character); return; }
    if (tab === "inventory") { this.renderInventorySection(content); return; }
    const equipped = resolvedWeapon(this.loadout.weapon, this.items);
    const pages: Record<string, string> = {
      character: `<h3>${escapeHTML(character?.name ?? "Character")}</h3><p>${titleCase(character?.class ?? "gunslinger")} · Level ${this.level}</p><p>${this.xpNext ? `${this.xp} / ${this.xpNext} XP to level ${this.level + 1}` : "Level cap reached"} · ${this.ledger.size} unlock${this.ledger.size === 1 ? "" : "s"} owned</p><p>Progression unlocks options, never raw combat power.</p>`,
      world: `<h3>Known world</h3><p>${world.danger_bands.map((band) => escapeHTML(band.name)).join(" → ")}. The circular world is contiguous; trees are authoritative static cover.</p>`,
      reference: `<h3>Field reference</h3><p>WASD/Arrows move · pointer aims · primary pointer fires · 1–6 or the wheel select an equipped slot · Space dashes · R reloads · E interacts. The hub is safe. Combat is server-authoritative and raw time-to-kill is about ${equipped ? damageBandFor(equipped).target_ttk_seconds : 3} seconds.</p>`,
      settings: "<h3>Settings</h3><p>Accessibility and interface-scale controls remain available on Home. Opening this menu does not pause the shared world.</p>",
    };
    content.innerHTML = pages[tab] ?? pages.character!;
  }

  private renderAdminMenu(content: HTMLElement, query = ""): void {
    const selected = this.selectedAdminSpawn();
    const search = query.toLowerCase();
    const entries = Object.entries(adminTools.spawnables).filter(([, spawnable]) => spawnable.name.toLowerCase().includes(search) || spawnable.kind.includes(search)).sort(([, left], [, right]) => left.name.localeCompare(right.name));
    content.innerHTML = `<h3>Developer mode</h3><p>Developer mode replaces primary fire with repeatable placement. Configure an entity, close this menu, then click the world.</p><button id="developer-mode-toggle" class="${this.developerMode ? "danger-button" : "primary"}">${this.developerMode ? "Disable developer mode" : "Enable developer mode"}</button><label>Search spawnables<input id="admin-spawn-search" value="${escapeHTML(query)}" placeholder="Player, projectile, telegraph…" /></label><div id="admin-spawn-list" class="admin-spawn-list">${entries.map(([id, spawnable]) => `<button data-admin-spawn="${escapeHTML(id)}" aria-pressed="${id === this.adminSpawnID}"><strong>${escapeHTML(spawnable.name)}</strong><small>${escapeHTML(spawnable.kind)}</small></button>`).join("") || "<p>No spawnables match.</p>"}</div>${selected ? this.adminConfigMarkup(selected) : "<p class=\"error\">No spawnable is configured.</p>"}<form id="admin-materials-form"><h4>Grant materials</h4><p>Harvesting is not implemented yet, so this is the only way to put materials in a character's hands and exercise a real crafting spend.</p><label>Material<select id="admin-material-select">${Object.keys(materialsTable.materials).sort().map((id) => `<option value="${escapeHTML(id)}"${id === this.adminMaterialID ? " selected" : ""}>${escapeHTML(materialsTable.materials[id]!.name)}</option>`).join("")}</select></label>${this.adminFieldMarkup({ ...adminTools.material_grant, id: "count" }, "admin-material-count", "count")}<button class="secondary" type="submit">Grant to your character</button></form><form id="admin-attributes-form"><h4>Your player</h4><p>These temporary overrides affect only your current body and reset when it leaves the world.</p>${Object.entries(adminTools.attributes).map(([id, field]) => this.adminFieldMarkup(field, `admin-attribute-${id}`, id)).join("")}<button class="secondary" type="submit">Apply player overrides</button></form><p id="admin-notice" class="error" role="status"></p>`;
    element<HTMLButtonElement>("developer-mode-toggle").addEventListener("click", () => this.setDeveloperMode(!this.developerMode));
    element<HTMLInputElement>("admin-spawn-search").addEventListener("input", (event) => this.renderAdminMenu(content, (event.currentTarget as HTMLInputElement).value));
    for (const button of document.querySelectorAll<HTMLButtonElement>("[data-admin-spawn]")) button.addEventListener("click", () => { this.adminSpawnID = button.dataset.adminSpawn ?? ""; this.adminSpawnConfig = this.defaultAdminConfig(this.adminSpawnID); this.renderAdminMenu(content, query); });
    for (const input of document.querySelectorAll<HTMLInputElement>("[data-admin-config]")) input.addEventListener("input", () => { this.adminSpawnConfig[input.dataset.adminConfig ?? ""] = input.value; });
    element<HTMLFormElement>("admin-attributes-form").addEventListener("submit", (event) => void this.applyAdminAttributes(event));
    element<HTMLSelectElement>("admin-material-select").addEventListener("change", (event) => { this.adminMaterialID = (event.currentTarget as HTMLSelectElement).value; });
    element<HTMLFormElement>("admin-materials-form").addEventListener("submit", (event) => void this.grantAdminMaterials(event));
  }

  private adminConfigMarkup(selected: AdminSpawnable): string {
    return `<section class="admin-config"><h4>Place ${escapeHTML(selected.name)}</h4>${selected.fields.map((field) => this.adminFieldMarkup(field, `admin-config-${field.id}`, field.id, "data-admin-config")).join("")}</section>`;
  }

  private adminFieldMarkup(field: AdminToolField | AdminAttribute, inputID: string, key: string, dataAttribute = ""): string {
    if (field.kind === "text") {
      const value = dataAttribute ? this.adminSpawnConfig[key] ?? field.default_text ?? "" : field.default_text ?? "";
      return `<label>${escapeHTML(field.label)}<input id="${escapeHTML(inputID)}" ${dataAttribute}="${escapeHTML(key)}" type="text" maxlength="${field.max_length ?? 1}" value="${escapeHTML(value)}" /></label>`;
    }
    const value = dataAttribute ? this.adminSpawnConfig[key] ?? String(field.default_number ?? 0) : String(field.default_number ?? 0);
    const attribute = dataAttribute || "data-admin-attribute";
    return `<label>${escapeHTML(field.label)}<input id="${escapeHTML(inputID)}" ${attribute}="${escapeHTML(key)}" type="number" min="${field.minimum ?? 0}" max="${field.maximum ?? 0}" step="${field.step ?? 1}" value="${escapeHTML(value)}" /></label>`;
  }

  private selectedAdminSpawn(): AdminSpawnable | undefined { return adminTools.spawnables[this.adminSpawnID]; }

  private defaultAdminConfig(id: string): Record<string, string> {
    const values: Record<string, string> = {};
    for (const field of adminTools.spawnables[id]?.fields ?? []) values[field.id] = field.kind === "number" ? String(field.default_number ?? 0) : field.default_text ?? "";
    return values;
  }

  private setDeveloperMode(enabled: boolean): void {
    if (enabled && !this.api.account?.is_admin) return;
    this.developerMode = enabled;
    document.body.classList.toggle("developer-mode", enabled);
    const hud = element("developer-mode-hud"); hud.hidden = !enabled;
    const label = this.selectedAdminSpawn()?.name ?? "entity";
    element("developer-mode-selection").textContent = enabled ? `Placing: ${label}` : "";
    const menu = element<HTMLDialogElement>("menu-dialog"); if (enabled && menu.open) this.renderAdminMenu(element("menu-content"));
  }

  private async placeAdminEntity(event: PointerEvent): Promise<void> {
    const character = this.activeCharacter, spawnable = this.selectedAdminSpawn();
    if (!character || !spawnable || !this.view) return;
    event.preventDefault();
    const point = this.view.worldAtPointer(event.clientX, event.clientY);
    try {
      await this.api.adminSpawn({ character_id: character.id, spawn_id: this.adminSpawnID, x: point.x, y: point.y, config: this.adminSpawnConfig });
      this.notice(`Placed ${spawnable.name}.`);
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

  private async applyAdminAttributes(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!this.activeCharacter) return;
    const attributes: Record<string, number> = {};
    for (const input of document.querySelectorAll<HTMLInputElement>("[data-admin-attribute]")) attributes[input.dataset.adminAttribute ?? ""] = Number(input.value);
    const notice = element("admin-notice"); notice.textContent = "";
    try { await this.api.adminAttributes(this.activeCharacter.id, attributes); notice.textContent = "Player overrides applied."; notice.classList.remove("error"); }
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
    if (element<HTMLDialogElement>("menu-dialog").open && (this.menuTab === "crafting" || this.menuTab === "inventory" || this.menuTab === "loadout")) this.renderMenu(this.menuTab);
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
      empty.textContent = "You have not unlocked a weapon to build from yet.";
      content.append(empty);
      return;
    }

    // Blueprint choice. The category is the blueprint; its slots follow from it.
    const blueprint = document.createElement("label"); blueprint.className = "loadout-slot";
    const select = document.createElement("select");
    select.disabled = !this.inSafety;
    for (const id of buildable) select.append(new Option(weapons[id]?.name ?? id, id));
    select.value = this.craftWeapon;
    select.addEventListener("change", () => { this.craftWeapon = select.value; this.craftChoices = {}; this.craftStatus = ""; this.renderMenu("crafting"); });
    blueprint.append(document.createTextNode("Blueprint"), select);
    content.append(blueprint);

    const slots = document.createElement("div"); slots.className = "loadout-slots";
    for (const slot of slotsOf(this.craftWeapon)) {
      const row = document.createElement("label"); row.className = "loadout-slot";
      const choice = document.createElement("select");
      choice.disabled = !this.inSafety;
      choice.append(new Option("Stock", ""));
      const options = fitting(this.craftWeapon, slot);
      for (const id of options) choice.append(new Option(componentOf(id)?.name ?? id, id));
      if (!options.length) choice.append(new Option("No components fit this slot yet", "", true, true));
      choice.value = this.craftChoices[slot] ?? "";
      choice.addEventListener("change", () => {
        if (choice.value) this.craftChoices[slot] = choice.value; else delete this.craftChoices[slot];
        this.craftStatus = ""; this.renderMenu("crafting");
      });
      row.append(document.createTextNode(titleCase(slot)), choice);
      slots.append(row);
    }
    content.append(slots);

    // What this build does, stated as behaviour rather than as multipliers: a
    // rare part must never read as a higher power tier.
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

    const required = craftCost(this.craftChoices);
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
    build.disabled = !this.inSafety || full || Object.keys(missing).length > 0;
    build.addEventListener("click", () => this.commitCraft(required));
    content.append(build);
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
    window.clearInterval(this.inputTimer); this.socket?.close(); this.socket = undefined; this.view?.destroy(); this.view = undefined; this.predictor = undefined; this.pressed.clear(); this.lastBand = "";
    this.draft = undefined; this.selectedSlot = 0; this.inSafety = true; this.respecOwed = false; this.loadoutStatus = "";
    this.items = []; this.materials = {}; this.craftWeapon = ""; this.craftChoices = {}; this.craftStatus = "";
    this.ledger = ledgerOf([]); this.level = 1; this.xp = 0; this.xpNext = 0;
    element("ability-bar").replaceChildren(); element("touch-slots").replaceChildren();
    const menu = element<HTMLDialogElement>("menu-dialog"); if (menu.open) menu.close(); this.activeCharacter = undefined; this.setDeveloperMode(false); element("game").hidden = true; element("home").hidden = false; element("death-overlay").hidden = true; element("connection-overlay").hidden = true;
  }
}

function heading(text: string): HTMLElement { const node = document.createElement("h3"); node.textContent = text; return node; }

/** The action-bar slot a digit key selects, or undefined for any other key. */
function slotKey(code: string): number | undefined {
  const match = /^(?:Digit|Numpad)([1-9])$/.exec(code);
  if (!match) return undefined;
  const slot = Number(match[1]) - 1;
  return slot < barSlots ? slot : undefined;
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
