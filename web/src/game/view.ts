import { Application, Container, Graphics, Sprite, Text, Texture } from "pixi.js";
// Install eval-free polyfills so WebGL works under a CSP without 'unsafe-eval'. Side-effect import; must run before the renderer is created.
import "pixi.js/unsafe-eval";
import { abilities, deployableByKind, effects, entityDefinitions, projectileByKind, safeRadius, simulation, world } from "../tuning";
import type { Collider, Entity, ServerMessage } from "../types";
import { Allegiance, EntityType, ServerKind } from "../types";
import type { Predictor } from "./prediction";
import { SightShadowFilter } from "./shadow";
import { telegraphStyle } from "./telegraph";

interface Sample { at: number; entity: Entity }
interface ActorView {
  root: Container; body: Graphics; weapon: Graphics; health: Graphics; stance: Graphics; label: Text; type: number;
  /** The last shot count seen for this body, and when it changed: one shot is one kick. */
  shots: number; firedAt: number;
  /** Deterministic puff layout, for a deployable field. Empty for everything else. */
  puffs: Puff[]; bornAt: number;
  /** Line-of-sight fade, 0..1: ramps up as an entity enters sight and down as it leaves. */
  los: number;
}
// A puff is a tinted sprite off the one shared disc texture rather than its own
// filled circle: every cloud on screen then batches into a single draw, and a
// drifting puff re-uploads four vertices instead of a tessellated ring. `scale`
// is the puff's authored size against that texture, which the breathe multiplies.
interface Puff { graphic: Sprite; distance: number; angle: number; scale: number; drift: number }

// How a shot reads: the weapon is shoved back along its own axis and a flash
// sits at the muzzle for the same window. The window is short enough to survive
// the fastest cadence in the tables.
const kickMS = 130;
const kickDistance = 9;
// The camera is only knocked by a weapon that fires an explosive, so ordinary
// gunfire never shakes the view it has to be aimed through.
const shakeMS = 220;
const shakeDistance = 6;
// Smoke is drawn as overlapping puffs and nothing else — no disc underneath it,
// which reads as a hard bubble the moment the puffs move off it. The puffs are
// all of a size and their bands overlap heavily: some scattered through the
// middle, the rest wandering a wide band that reaches both the centre and past
// the rim. Separating them by size and distance is what made a cloud read as
// small circles ringing one big one. The spill is deliberate: the server's rule is
// the exact radius, and a body the fog only laps at is one the server still
// shows, which is what keeps a half-covered opponent from vanishing.
const puffCount = 18;
const puffCoreCount = 6;
const puffFadeMS = 320;
const puffAlpha = .32;
// The radius the shared disc texture is drawn at. Large enough that the widest
// authored field scales it down rather than up, so no cloud is drawn from a
// magnified texture.
const puffTextureRadius = 128;
// A body entering or leaving line of sight fades rather than snapping, which is
// less jarring than a body blinking in and out behind cover. Short enough that a
// covered opponent does not linger as usable information.
const losFadeMS = 140;
// How long a sample buffer may go without a snapshot before its actor is
// collected regardless of what its fade is doing. It is comfortably longer than
// the fade plus snapshot jitter, so it never cuts a legitimate fade short.
const staleSampleMS = 1000;

const colors = {
  ground: 0x16233a, grid: 0x2c405e, safe: 0x7ee1bb, rim: 0x8d5260, self: 0xffffff, hostile: 0xff6f69,
  gunner: 0x9cabbf, mage: 0xff754d, bullet: 0xffd166, tree: 0x46795a, trunk: 0x795b43, outline: 0x0b1220,
  squad: 0x7ee1bb, neutral: 0xaeb9c8, shield: 0x9ecbff, scope: 0xffe9a8,
};

// The widest arc any live guard covers, so the drawn shield never claims to
// block more than the simulation does.
const guardArcDegrees = Math.max(0, ...Object.values(abilities).map((ability) => ability.guard?.arc_degrees ?? 0));

const elementColors: Record<string, number> = { fire: 0xff754d, frost: 0x75dbf0, storm: 0xffd166, arcane: 0xd879e8, earth: 0xb99568 };

export class GameView {
  readonly app = new Application();
  private world = new Container();
  private overlayWorld = new Container();
  private ground = new Graphics();
  private telegraphLayer = new Container();
  private entityLayer = new Container();
  // A low-opacity veil covers terrain the local sightline cannot reach. Static
  // landmarks explicitly marked visible_in_shadow render in the layer above it.
  private shadow = new Graphics();
  private shadowFilter = new SightShadowFilter();
  private shadowVisibleLayer = new Container();
  // Area effects — a firestorm, a blizzard, a cinder patch — draw above the veil
  // because they are unaffected by line of sight: ground the player is entitled
  // to see and play around even through cover.
  private fieldLayer = new Container();
  // Concealing smoke draws over bodies: a cloud that players rendered on top of
  // would show exactly what the server just stopped sending behind it.
  private fogLayer = new Container();
  // The blackout a flashbang leaves. It lives on the stage rather than in the
  // world container, because it is the viewer's eyes rather than a place.
  private blackout = new Graphics();
  private blindedSince = 0;
  private blindedUntil = 0;
  private shakeUntil = 0;
  // Whether the selected slot fires something heavy enough to knock the camera.
  private shakeOnFire = false;
  private actors = new Map<string, ActorView>();
  private samples = new Map<string, Sample[]>();
  private localID = "";
  private predictor?: Predictor;
  private latestEntities = new Map<string, Entity>();
  private colliders: Collider[] = [];
  // Ids present in the most recent authoritative snapshot: the target of each
  // actor's line-of-sight fade. An omitted entity fades out rather than vanishing.
  private visible = new Set<string>();
  private frameDt = 0;
  private lastFrame = 0;
  private initialized = false;
  // The scope's camera exception: the view is pushed toward where the weapon is
  // pointed, by a fraction of the reach the server widened the snapshot by.
  private scopeOffset = { x: 0, y: 0 };
  // One disc every puff of every field is drawn from. Built once the renderer
  // exists; a field spamming the world then costs sprites rather than geometry.
  private puffTexture?: Texture;

  async init(host: HTMLElement): Promise<void> {
    await this.app.init({ resizeTo: window, antialias: true, backgroundColor: colors.ground, resolution: Math.min(2, devicePixelRatio), autoDensity: true });
    host.replaceChildren(this.app.canvas);
    const disc = new Graphics().circle(puffTextureRadius, puffTextureRadius, puffTextureRadius).fill(0xffffff);
    this.puffTexture = this.app.renderer.generateTexture({ target: disc, antialias: true });
    disc.destroy();
    this.world.addChild(this.ground, this.telegraphLayer, this.entityLayer);
    this.overlayWorld.addChild(this.shadowVisibleLayer, this.fieldLayer, this.fogLayer);
    this.shadow.filters = [this.shadowFilter];
    this.app.stage.addChild(this.world, this.shadow, this.overlayWorld, this.blackout);
    this.app.ticker.add(() => this.renderFrame());
    this.initialized = true;
  }

  bindPredictor(predictor: Predictor): void { this.predictor = predictor; }

  /**
   * Offsets the camera toward the aim while scoped. A bonus of zero puts it
   * back on the body, which is what unscoping does.
   */
  setScope(bonus: number, aimX: number, aimY: number): void {
    if (bonus <= 0) { this.scopeOffset = { x: 0, y: 0 }; return; }
    const length = Math.hypot(aimX, aimY);
    if (length < 0.001) return;
    // Half the widened reach: the player still sees its own body, which is what
    // keeps the blackout a readable trade rather than a disorienting one.
    const reach = bonus / 2;
    this.scopeOffset = { x: (aimX / length) * reach, y: (aimY / length) * reach };
  }

  /**
   * Declares whether the currently selected slot fires an explosive. Only those
   * shots knock the camera; a rifle or an SMG leaves it still.
   */
  setHeavyRecoil(heavy: boolean): void { this.shakeOnFire = heavy; }

  apply(message: ServerMessage): void {
    this.localID = message.playerID || this.localID;
    // Loadout/progression replies carry no world frame. Retain the last
    // authoritative collider set until a snapshot (including an empty one)
    // replaces it, avoiding a one-frame flash of fully revealed ground.
    if (message.kind === ServerKind.Snapshot || message.kind === ServerKind.Welcome) this.colliders = message.colliders;
    const receivedAt = performance.now();
    const present = new Set<string>();
    for (const entity of message.entities) {
      present.add(entity.id); this.latestEntities.set(entity.id, entity);
      if (entity.id === this.localID) continue;
      const buffer = this.samples.get(entity.id) ?? [];
      buffer.push({ at: receivedAt, entity });
      while (buffer.length > 8) buffer.shift();
      this.samples.set(entity.id, buffer);
    }
    if (message.kind === ServerKind.Snapshot || message.kind === ServerKind.Welcome) {
      // Snapshot omission is the authoritative LOS result. Rather than dropping a
      // covered entity outright — which blinks it out — the present set drives a
      // short fade; the actor is collected only once it has faded away, and no new
      // samples arrive for it in the meantime so it freezes where it was last seen.
      this.visible = present;
    }
  }

  pointerWorld(clientX: number, clientY: number): { x: number; y: number } {
    const predictor = this.predictor;
    if (!predictor) return { x: 1, y: 0 };
    // Aim stays relative to the body, so pushing the camera while scoped does
    // not silently rotate where the weapon is pointed.
    return { x: clientX - this.app.screen.width / 2 + this.scopeOffset.x, y: clientY - this.app.screen.height / 2 + this.scopeOffset.y };
  }

  // Placement tools need an absolute world coordinate, unlike aiming, which
  // deliberately uses the player-relative vector returned by pointerWorld.
  worldAtPointer(clientX: number, clientY: number): { x: number; y: number } {
    const point = this.pointerWorld(clientX, clientY), predictor = this.predictor;
    return predictor ? { x: predictor.x + point.x, y: predictor.y + point.y } : point;
  }

  entityAtPointer(clientX: number, clientY: number): Entity | undefined {
    const point = this.worldAtPointer(clientX, clientY);
    let selected: Entity | undefined, selectedDistance = Number.POSITIVE_INFINITY;
    for (const entity of this.latestEntities.values()) {
      if (entity.deleting) continue;
      const radius = Math.max(12, entity.radius, entity.length, entity.width, entity.type === EntityType.Player ? 24 : 0);
      const x = entity.id === this.localID && this.predictor ? this.predictor.x : entity.x;
      const y = entity.id === this.localID && this.predictor ? this.predictor.y : entity.y;
      const distance = (x - point.x) ** 2 + (y - point.y) ** 2;
      if (distance <= (radius + 8) ** 2 && distance < selectedDistance) { selected = entity; selectedDistance = distance; }
    }
    return selected;
  }

  destroy(): void {
    if (!this.initialized) return;
    this.app.destroy(true, { children: true });
    this.puffTexture?.destroy(true); this.puffTexture = undefined;
    this.initialized = false;
  }

  private renderFrame(): void {
    const predictor = this.predictor;
    if (!predictor) return;
    const now = performance.now();
    this.frameDt = this.lastFrame ? now - this.lastFrame : 0;
    this.lastFrame = now;
    const width = this.app.screen.width, height = this.app.screen.height;
    const shake = this.cameraShake(now);
    const cameraX = predictor.x + this.scopeOffset.x + shake.x, cameraY = predictor.y + this.scopeOffset.y + shake.y;
    this.world.position.set(width / 2 - cameraX, height / 2 - cameraY);
    this.overlayWorld.position.copyFrom(this.world.position);
    this.drawGround(cameraX, cameraY, width, height);
    this.drawSightShadow(predictor.x, predictor.y, cameraX, cameraY, width, height);
    const local = this.latestEntities.get(this.localID);
    if (local) this.drawEntity({ ...local, x: predictor.x, y: predictor.y, aimX: predictor.aimX, aimY: predictor.aimY }, true, now);
    const renderAt = now - simulation.interpolation_delay_ms;
    for (const [id, samples] of this.samples) {
      // A buffer that has gone quiet for far longer than the fade window is not a
      // body held behind cover — the server has stopped sending it entirely — so
      // it is collected outright. The fade sweep below is the ordinary path; this
      // is the backstop that keeps a buffer from outliving its actor, because
      // interpolation past the last sample never reports absence on its own.
      const last = samples.at(-1);
      if (!last || now - last.at > staleSampleMS) { this.removeActor(id); continue; }
      const entity = interpolate(samples, renderAt);
      if (entity) this.drawEntity(entity, false, now);
      else this.removeActor(id);
    }
    // Collect any actor that has fully faded out of sight. Until then it keeps
    // being drawn, frozen at its last-seen position.
    for (const [id, view] of this.actors) {
      if (id !== this.localID && view.los <= 0.001 && !this.visible.has(id)) this.removeActor(id);
    }
    this.drawBlackout(local, now, width, height);
  }

  /**
   * The knock an explosive shot gives the local camera. It is an offset rather
   * than a rotation so aiming stays exactly where the pointer is.
   */
  private cameraShake(now: number): { x: number; y: number } {
    const left = this.shakeUntil - now;
    if (left <= 0) return { x: 0, y: 0 };
    const strength = (left / shakeMS) * shakeDistance;
    return { x: Math.sin(now / 9) * strength, y: Math.cos(now / 7) * strength };
  }

  /**
   * A flashbang takes vision whole, so the client shows exactly that: the world
   * is behind a white sheet for as long as the status runs, then it lifts. The
   * server has already stopped sending anything to draw behind it — this is the
   * player-facing half of a rule that is enforced on the wire.
   */
  private drawBlackout(local: Entity | undefined, now: number, width: number, height: number): void {
    const blinded = Boolean(local?.effectIDs.some((id) => effects[id]?.kind === "blind"));
    if (blinded) {
      if (now > this.blindedUntil) this.blindedSince = now;
      this.blindedUntil = now;
    }
    // The sheet lifts over the same window a real flash fades: instantly total,
    // then clearing, so the player knows when they have their eyes back.
    const since = now - this.blindedSince, lifting = now - this.blindedUntil;
    let alpha = 0;
    if (blinded) alpha = since < 120 ? 1 : 0.94;
    else if (lifting < 420) alpha = 0.94 * (1 - lifting / 420);
    this.blackout.clear();
    if (alpha <= 0.01) return;
    this.blackout.rect(0, 0, width, height).fill({ color: 0xffffff, alpha });
  }

  private drawGround(cameraX: number, cameraY: number, width: number, height: number): void {
    const cell = 80, left = cameraX - width / 2 - cell, right = cameraX + width / 2 + cell, top = cameraY - height / 2 - cell, bottom = cameraY + height / 2 + cell;
    this.ground.clear();
    for (let x = Math.floor(left / cell) * cell; x <= right; x += cell) this.ground.moveTo(x, top).lineTo(x, bottom);
    for (let y = Math.floor(top / cell) * cell; y <= bottom; y += cell) this.ground.moveTo(left, y).lineTo(right, y);
    this.ground.stroke({ color: colors.grid, width: 1, alpha: .62 });
    const reach = Math.hypot(width, height) / 2 + cell;
    this.drawRing(safeRadius, colors.safe, 5, .7, cameraX, cameraY, reach);
    this.drawRing(world.radius, colors.rim, 8, .8, cameraX, cameraY, reach);
  }

  /**
   * A world ring, drawn only where it can actually be seen. The world radius is
   * 45,000 units: a full circle at that size is thousands of tessellated
   * vertices rebuilt every frame for an arc a few pixels long, and off-screen it
   * is that cost for nothing at all. Only the span facing the camera is emitted,
   * and a ring the camera cannot reach is skipped outright.
   */
  private drawRing(radius: number, color: number, width: number, alpha: number, cameraX: number, cameraY: number, reach: number): void {
    const distance = Math.hypot(cameraX, cameraY);
    if (distance + reach < radius || distance - reach > radius) return;
    const facing = Math.atan2(cameraY, cameraX);
    // The half-angle of the arc that falls inside the camera's reach, from the
    // triangle (origin, camera, ring point). A camera at the origin, or one
    // close enough to see the whole ring, clamps to a full circle.
    const cosine = distance === 0 ? -1 : (distance * distance + radius * radius - reach * reach) / (2 * distance * radius);
    const span = Math.acos(Math.max(-1, Math.min(1, cosine))) + .05;
    const start = facing - span;
    // The moveTo is not decoration: arc() continues the path it is called on, so
    // without a fresh subpath it draws a chord from wherever the last grid line
    // or the previous ring ended to where this arc starts.
    this.ground.moveTo(Math.cos(start) * radius, Math.sin(start) * radius);
    this.ground.arc(0, 0, radius, start, facing + span).stroke({ color, width, alpha });
  }

  private drawSightShadow(viewerX: number, viewerY: number, cameraX: number, cameraY: number, width: number, height: number): void {
    const toScreen = (x: number, y: number): { x: number; y: number } => ({ x: x - cameraX + width / 2, y: y - cameraY + height / 2 });
    const occluders: Collider[] = this.colliders
      .filter((collider) => entityDefinitions[collider.kind]?.occludes_vision)
      .map((collider) => ({ ...collider, ...toScreen(collider.x, collider.y) }));
    // Concealing smoke now casts a shadow like terrain. A cloud the viewer stands
    // inside is not added as an occluder — it would shadow every ray from within
    // it; instead its reveal radius carves a small visible circle around the body,
    // which is what lets a body at the rim peek just outside the smoke.
    let reveal = 0;
    for (const entity of this.latestEntities.values()) {
      if (entity.type !== EntityType.Deployable || entity.deleting) continue;
      const field = deployableByKind(entity.className);
      const radius = entity.radius;
      if (!field?.conceals || radius <= 0) continue;
      if ((viewerX - entity.x) ** 2 + (viewerY - entity.y) ** 2 <= radius * radius) {
        reveal = Math.max(reveal, field.reveal_radius ?? 0);
        continue;
      }
      const screen = toScreen(entity.x, entity.y);
      occluders.push({ id: entity.id, entityID: entity.id, kind: entity.className, shape: "circle", x: screen.x, y: screen.y, radius, width: 0, height: 0 });
    }
    // If a browser cannot compile either shader backend, the filter is skipped;
    // this restrained fill is a safe dark fallback rather than a white screen.
    this.shadow.clear().rect(0, 0, width, height).fill({ color: 0x07101a, alpha: .27 });
    this.shadowFilter.update(toScreen(viewerX, viewerY), width, height, occluders, reveal);
  }

  private drawEntity(entity: Entity, self: boolean, now = performance.now()): void {
    let view = this.actors.get(entity.id);
    if (view && view.type !== entity.type) { this.removeActor(entity.id); view = undefined; }
    if (!view) {
      view = this.createActor(entity, self, now); this.actors.set(entity.id, view);
      this.layerFor(entity).addChild(view.root);
    }
    view.root.position.set(entity.x, entity.y);
    // Step the line-of-sight fade toward present (1) or omitted (0). The local
    // body is always in sight; everything else follows the authoritative
    // snapshot's present set. Fields are exempt from line of sight on the wire —
    // the server sends them through cover — so a standing field is always in that
    // set anyway, and pinning it here instead would mean an expired cloud never
    // faded and therefore was never collected.
    const inSight = self || this.visible.has(entity.id);
    const step = this.frameDt / losFadeMS;
    view.los = inSight ? Math.min(1, view.los + step) : Math.max(0, view.los - step);
    const base = entity.deleting ? Math.max(0, 1 - entity.deleteProgress) : entity.alive ? entity.lingering ? .62 : 1 : .32;
    view.root.alpha = base * view.los;
    if (entity.type === EntityType.Deployable) { this.drawField(view, now); return; }
    if (entity.type === EntityType.Player) {
      this.drawRecoil(view, entity, self, now);
      view.health.clear().roundRect(-27, -39, 54, 7, 3).fill(colors.outline).roundRect(-25, -37, 50 * Math.max(0, entity.health / Math.max(1, entity.maxHealth)), 3, 2).fill(entity.health > 30 ? 0x65d89d : 0xff7f73);
      view.label.text = entity.lingering ? `${entity.name} · offline` : entity.name;
      if (entity.invulnerable) view.health.circle(0, 0, 27).stroke({ color: colors.safe, width: 3, alpha: .9 });
      this.drawStance(view.stance, entity);
    } else if (entity.type === EntityType.WorldItem && entity.maxHealth > 0) {
      const ratio = Math.max(0, entity.health / entity.maxHealth);
      view.health.clear();
      if (ratio < 1) view.health.roundRect(-27, -49, 54, 7, 3).fill(colors.outline).roundRect(-25, -47, 50 * ratio, 3, 2).fill(ratio > .3 ? 0x65d89d : 0xff7f73);
    } else if (entity.type === EntityType.Mob) {
      view.weapon.rotation = Math.atan2(entity.aimY, entity.aimX);
    } else if (entity.type === EntityType.Telegraph) {
      this.drawTelegraph(view.body, entity);
    }
  }

  /**
   * Which layer an entity is drawn in. Only a concealing field draws over
   * bodies — that is exactly what the server has stopped sending behind it.
   * A burning patch or a blizzard is ground the server still shows everything
   * inside, so painting over it would hide what the player is entitled to see.
   */
  private layerFor(entity: Entity): Container {
    if (entity.type === EntityType.Telegraph) return this.telegraphLayer;
    if (entityDefinitions[entity.className]?.visible_in_shadow) return this.shadowVisibleLayer;
    if (entity.type !== EntityType.Deployable) return this.entityLayer;
    // Concealing smoke draws on top of everything; an area field draws above the
    // veil but below the smoke, since it is seen through cover but not over it.
    return deployableByKind(entity.className)?.conceals ? this.fogLayer : this.fieldLayer;
  }

  /**
   * What a shot looks like from outside the shooter: the muzzle sits wherever
   * the weapon's pattern has walked it, and each new shot shoves the weapon back
   * along its own axis with a flash at the barrel. Both come from the snapshot —
   * the offset the server is simulating and the body's own shot count — so every
   * player sees the same walk, not a local guess at one.
   */
  private drawRecoil(view: ActorView, entity: Entity, self: boolean, now: number): void {
    if (entity.shots > view.shots) {
      view.shots = entity.shots; view.firedAt = now;
      if (self && this.shakeOnFire) this.shakeUntil = now + shakeMS;
    }
    const aim = Math.atan2(entity.aimY, entity.aimX);
    view.weapon.rotation = aim + entity.recoilDegrees * Math.PI / 180;
    const kick = Math.max(0, 1 - (now - view.firedAt) / kickMS);
    view.weapon.position.set(-Math.cos(view.weapon.rotation) * kickDistance * kick, -Math.sin(view.weapon.rotation) * kickDistance * kick);
    this.drawMuzzleFlash(view, kick);
  }

  private drawMuzzleFlash(view: ActorView, kick: number): void {
    const flash = view.weapon.children[0] as Graphics | undefined;
    if (!flash) return;
    flash.clear();
    if (kick <= 0) return;
    const reach = 10 + 16 * kick;
    flash.moveTo(40, 0).lineTo(40 + reach, -7 * kick).lineTo(40 + reach * 1.35, 0).lineTo(40 + reach, 7 * kick).closePath()
      .fill({ color: colors.bullet, alpha: .85 * kick });
  }

  /**
   * A deployed field, drawn as drifting puffs and no disc. The layout is seeded
   * from the field's own identity so it is stable frame to frame, and it fades
   * in on arrival and out with the shared removal fade.
   *
   * Each puff carries a modest alpha and the overlaps do the rest: the middle,
   * where the most puffs stack, ends up the densest part of the cloud and the
   * rim thins out on its own, which is what gives it depth without a flat edge.
   */
  private drawField(view: ActorView, now: number): void {
    const age = now - view.bornAt;
    const arriving = Math.min(1, age / puffFadeMS);
    view.body.alpha = arriving;
    for (const puff of view.puffs) {
      // Each puff rotates around the centre at its own rate and breathes on its
      // own phase, so the cloud is never caught pulsing as one body.
      const angle = puff.angle + (age / 1000) * puff.drift;
      const breathe = 1 + Math.sin(age / 620 + puff.angle) * .07;
      puff.graphic.position.set(Math.cos(angle) * puff.distance, Math.sin(angle) * puff.distance);
      puff.graphic.scale.set(puff.scale * breathe);
    }
  }

  private createActor(entity: Entity, self: boolean, now = performance.now()): ActorView {
    const root = new Container(), body = new Graphics(), weapon = new Graphics(), health = new Graphics(), stance = new Graphics();
    const outline = allegianceColor(entity.allegiance, self);
    const puffs: Puff[] = [];
    if (entity.type === EntityType.Telegraph) {
      this.drawTelegraph(body, entity);
    } else if (entity.type === EntityType.Projectile) {
      // A snapshot carries only the projectile kind; size and silhouette come
      // from the table row that launched it.
      const projectile = projectileByKind(entity.className);
      const radius = projectile?.radius ?? 6;
      const fill = elementColors[entity.element] ?? colors.bullet;
      if (projectile?.silhouette === "bolt") body.circle(0, 0, radius).fill(fill).stroke({ color: outline, width: 3 });
      else body.rect(-radius * 1.4, -radius * .6, radius * 2.8, radius * 1.2).fill(fill).stroke({ color: outline, width: 2 });
    } else if (entity.type === EntityType.Mob) {
      body.circle(0, 0, 22).fill(0x687a8e).stroke({ color: outline, width: 4 });
      weapon.roundRect(7, -5, 35, 10, 3).fill(0x4d5b6a).stroke({ color: colors.outline, width: 2 });
    } else if (entity.type === EntityType.Drop) {
      body.moveTo(0, -12).lineTo(12, 0).lineTo(0, 12).lineTo(-12, 0).closePath().fill(elementColors[entity.element] ?? colors.bullet).stroke({ color: outline, width: 3 });
    } else if (entity.type === EntityType.Node) {
      body.circle(-8, 2, 9).circle(7, 5, 11).circle(1, -8, 8).fill(elementColors[entity.element] ?? colors.tree).stroke({ color: outline, width: 3 });
    } else if (entity.type === EntityType.Deployable) {
      // The puffs are laid out from a hash of the field's ID, so a cloud looks
      // the same to everyone watching it and never reshuffles between frames.
      const seed = hash(entity.id), radius = entity.radius || 120;
      const ring = puffCount - puffCoreCount;
      // The rim turns as one slow body with only a little play between puffs.
      // Letting each one pick its own rate tears the ring open after a few
      // seconds, and a cloud with a hole in it is worse than a still one.
      const turn = (fraction(seed + 11) - .5) * .12;
      for (let index = 0; index < puffCount; index++) {
        const spin = fraction(seed + index * 97), spread = fraction(seed + index * 53);
        const core = index < puffCoreCount;
        const puff = new Sprite(this.puffTexture ?? Texture.EMPTY);
        puff.anchor.set(.5);
        puff.alpha = puffAlpha;
        // The two groups are sized alike and their bands deliberately run into
        // each other — the core scattered out to .38r and the outer ones sitting
        // anywhere from .44r to .70r — so an outer puff reaches the middle and a
        // core puff reaches the rim. Keeping them apart is what made a cloud read
        // as small circles ringing one big one.
        const size = radius * (core ? .34 + fraction(seed + index * 31) * .18 : .34 + fraction(seed + index * 31) * .16);
        // Outer puffs walk the circle so no arc is left bare, but each may wander
        // most of a slot either way, which is enough for neighbours to crowd and
        // gap unevenly rather than parade around at a fixed spacing.
        const angle = core
          ? spin * Math.PI * 2
          : ((index - puffCoreCount + (spin - .5) * 1.1) / ring) * Math.PI * 2;
        // A field is tinted by what cast it, because a smoke cloud, a burning
        // patch, and a blizzard are the same shape and must never read alike.
        puff.tint = elementColors[entity.element] ?? 0xdfe6ef;
        puffs.push({
          graphic: puff, distance: radius * (core ? .38 * spread : .44 + spread * .26),
          angle, scale: size / puffTextureRadius, drift: turn + (spin - .5) * (core ? .3 : .05),
        });
        body.addChild(puff);
      }
    } else if (entity.type === EntityType.Boss) {
      body.moveTo(0, -42).lineTo(38, -18).lineTo(32, 34).lineTo(-32, 34).lineTo(-38, -18).closePath().fill(0x657186).stroke({ color: outline, width: 7 });
    } else if (entity.type === EntityType.WorldItem && entity.className === "tree") {
      const radius = entity.radius;
      body.rect(-6, 4, 12, radius).fill(colors.trunk).stroke({ color: colors.outline, width: 4 });
      body.circle(-radius * .3, 0, radius * .72).circle(radius * .35, 2, radius * .67).circle(0, -radius * .42, radius * .72).fill(colors.tree).stroke({ color: colors.outline, width: 4 });
    } else if (entity.type === EntityType.WorldItem && entity.className === "stone-wall") {
      // A raised segment reads as rock rather than masonry: it is destructible
      // terrain a caster put there, and its health bar is drawn like a tree's.
      const radius = entity.radius || 28;
      body.moveTo(-radius, radius * .5).lineTo(-radius * .7, -radius * .8).lineTo(0, -radius).lineTo(radius * .8, -radius * .6).lineTo(radius, radius * .6).closePath()
        .fill(elementColors.earth).stroke({ color: colors.outline, width: 4 });
      body.moveTo(-radius * .4, radius * .4).lineTo(-radius * .1, -radius * .5).stroke({ color: 0x8d7a5e, width: 3, alpha: .8 });
    } else if (entity.type === EntityType.WorldItem && entity.className === "wall") {
      body.rect(-entity.length / 2, -entity.width / 2, entity.length, entity.width).fill(0x68717d).stroke({ color: colors.outline, width: 5 });
      body.moveTo(-entity.length / 2, 0).lineTo(entity.length / 2, 0).moveTo(0, -entity.width / 2).lineTo(0, entity.width / 2).stroke({ color: 0x8d98a6, width: 2 });
    } else if (entity.className === "mage") {
      body.circle(0, 0, 20).fill(elementColors[entity.element] ?? colors.mage).stroke({ color: outline, width: 4 });
      body.moveTo(-15, 14).lineTo(0, -23).lineTo(15, 14).closePath().fill(0xd54e64).stroke({ color: colors.outline, width: 3 });
      weapon.moveTo(8, 7).lineTo(37, 0).stroke({ color: 0xb68862, width: 6 }).circle(39, 0, 7).fill(0xff754d).stroke({ color: colors.outline, width: 3 });
    } else {
      body.moveTo(-17, -17).lineTo(18, -13).lineTo(17, 16).lineTo(-18, 14).closePath().fill(colors.gunner).stroke({ color: outline, width: 4 });
      weapon.roundRect(8, -5, 36, 10, 2).fill(0x5c6674).stroke({ color: colors.outline, width: 3 }).rect(18, 5, 10, 8).fill(0x353e4c);
    }
    // Every actor that holds something carries a muzzle flash child, drawn only
    // while a shot is fresh.
    if (entity.type === EntityType.Player) weapon.addChild(new Graphics());
    const label = new Text({ text: entity.name, style: { fontFamily: "system-ui", fontSize: 12, fill: 0xffffff, stroke: { color: colors.outline, width: 3 } } }); label.anchor.set(.5); label.position.set(0, -50);
    root.addChild(body, stance, weapon, health, label);
    if (entity.type === EntityType.Deployable) label.visible = false;
    // A new actor starts out of sight and fades in, so an entity entering line of
    // sight arrives as smoothly as one leaving it departs.
    return { root, body, weapon, health, stance, label, type: entity.type, shots: entity.shots, firedAt: 0, puffs, bornAt: now, los: 0 };
  }

  /**
   * The two committed stances, drawn so an opponent can play around them: the
   * arc a raised shield actually covers, and the fact that a scoped body is
   * looking one way and moving slowly. Both use shape rather than colour alone.
   */
  private drawStance(stance: Graphics, entity: Entity): void {
    stance.clear();
    stance.rotation = Math.atan2(entity.aimY, entity.aimX);
    if (entity.guarding) {
      const half = guardArcDegrees * Math.PI / 360;
      // A shield is a destructible object, so the arc shows what is left of it:
      // the fill thins as durability drains, and the intact portion of the rim
      // is drawn over the spent one. An opponent has to be able to see a shield
      // running out to know when pressing it is worth the ammunition.
      const left = entity.maxShield > 0 ? Math.max(0, Math.min(1, entity.shield / entity.maxShield)) : 1;
      stance.moveTo(0, 0).arc(0, 0, 34, -half, half).closePath()
        .fill({ color: colors.shield, alpha: .08 + .2 * left })
        .stroke({ color: colors.shield, width: 4, alpha: .3 });
      if (left > 0) stance.arc(0, 0, 34, -half * left, half * left).stroke({ color: colors.shield, width: 4, alpha: .95 });
    }
    if (entity.scoped) {
      stance.moveTo(22, 0).lineTo(74, 0).stroke({ color: colors.scope, width: 2, alpha: .8 });
      stance.circle(74, 0, 6).stroke({ color: colors.scope, width: 2, alpha: .8 });
    }
  }

  private drawTelegraph(body: Graphics, entity: Entity): void {
    const style = telegraphStyle(entity.telegraphState, entity.telegraphProgress);
    const color = elementColors[entity.element] ?? colors.hostile;
    body.clear(); body.rotation = Math.atan2(entity.aimY, entity.aimX);
    switch (entity.telegraphShape) {
      case "circle": body.circle(0, 0, entity.radius); break;
      case "cone": {
        const half = entity.angleDegrees * Math.PI / 360;
        body.moveTo(0, 0).arc(0, 0, entity.length, -half, half).closePath();
        break;
      }
      case "line": body.roundRect(0, -entity.width / 2, entity.length, entity.width, entity.width / 2); break;
      case "ring": body.circle(0, 0, entity.radius).stroke({ color, width: entity.width, alpha: style.fillAlpha }); break;
    }
    if (entity.telegraphShape !== "ring") body.fill({ color, alpha: style.fillAlpha }).stroke({ color, width: style.strokeWidth, alpha: style.strokeAlpha });
    else body.circle(0, 0, entity.radius).stroke({ color, width: style.strokeWidth, alpha: style.strokeAlpha });
  }

  private removeActor(id: string): void { const view = this.actors.get(id); if (view) { view.root.destroy({ children: true }); this.actors.delete(id); } this.samples.delete(id); this.latestEntities.delete(id); }
}

function interpolate(samples: Sample[], at: number): Entity | undefined {
  if (!samples.length) return undefined;
  if (at <= samples[0]!.at) return samples[0]!.entity;
  for (let index = 1; index < samples.length; index++) {
    const next = samples[index]!, previous = samples[index - 1]!;
    if (at > next.at) continue;
    const t = Math.max(0, Math.min(1, (at - previous.at) / Math.max(1, next.at - previous.at)));
    const sameTelegraphPhase = previous.entity.telegraphState === next.entity.telegraphState;
    const sameDeletionPhase = previous.entity.deleting === next.entity.deleting;
    return { ...next.entity, x: previous.entity.x + (next.entity.x - previous.entity.x) * t, y: previous.entity.y + (next.entity.y - previous.entity.y) * t, aimX: previous.entity.aimX + (next.entity.aimX - previous.entity.aimX) * t, aimY: previous.entity.aimY + (next.entity.aimY - previous.entity.aimY) * t, telegraphState: sameTelegraphPhase ? next.entity.telegraphState : previous.entity.telegraphState, telegraphProgress: sameTelegraphPhase ? previous.entity.telegraphProgress + (next.entity.telegraphProgress - previous.entity.telegraphProgress) * t : previous.entity.telegraphProgress, deleteProgress: sameDeletionPhase ? previous.entity.deleteProgress + (next.entity.deleteProgress - previous.entity.deleteProgress) * t : next.entity.deleteProgress };
  }
  return samples.at(-1)?.entity;
}

// hash and fraction give a deployable a stable, well-spread layout from its own
// identity, so nothing has to travel on the wire to describe how a cloud looks.
function hash(value: string): number {
  let h = 2166136261;
  for (let index = 0; index < value.length; index++) { h ^= value.charCodeAt(index); h = Math.imul(h, 16777619); }
  return h >>> 0;
}

function fraction(state: number): number {
  let x = Math.imul(state ^ (state >>> 15), 2246822507);
  x = Math.imul(x ^ (x >>> 13), 3266489909);
  return ((x ^ (x >>> 16)) >>> 0) / 4294967296;
}

function allegianceColor(allegiance: number, self: boolean): number {
  if (self || allegiance === Allegiance.Self) return colors.self;
  if (allegiance === Allegiance.Squad) return colors.squad;
  if (allegiance === Allegiance.Neutral) return colors.neutral;
  return colors.hostile;
}
