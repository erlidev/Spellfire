import { Application, Container, Graphics, Text } from "pixi.js";
// Install eval-free polyfills so WebGL works under a CSP without 'unsafe-eval'. Side-effect import; must run before the renderer is created.
import "pixi.js/unsafe-eval";
import { abilities, effects, projectileByKind, safeRadius, simulation, world } from "../tuning";
import type { Entity, ServerMessage } from "../types";
import { Allegiance, EntityType } from "../types";
import type { Predictor } from "./prediction";
import { telegraphStyle } from "./telegraph";

interface Sample { at: number; entity: Entity }
interface ActorView {
  root: Container; body: Graphics; weapon: Graphics; health: Graphics; stance: Graphics; label: Text; type: number;
  /** The last shot count seen for this body, and when it changed: one shot is one kick. */
  shots: number; firedAt: number;
  /** Deterministic puff layout, for a deployable field. Empty for everything else. */
  puffs: Puff[]; bornAt: number;
}
interface Puff { graphic: Graphics; distance: number; angle: number; radius: number; drift: number }

// How a shot reads: the weapon is shoved back along its own axis and a flash
// sits at the muzzle for the same window. The window is short enough to survive
// the fastest cadence in the tables.
const kickMS = 130;
const kickDistance = 9;
// The camera is only knocked by a weapon that fires an explosive, so ordinary
// gunfire never shakes the view it has to be aimed through.
const shakeMS = 220;
const shakeDistance = 6;
// Smoke is drawn as a handful of overlapping puffs that drift around the centre
// rather than one flat disc, so a cloud reads as volume the moment it lands.
const puffCount = 9;
const puffFadeMS = 320;

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
  private ground = new Graphics();
  private telegraphLayer = new Container();
  private entityLayer = new Container();
  // Fields draw over bodies: a cloud that players rendered on top of would show
  // exactly what the server just stopped sending.
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
  private initialized = false;
  // The scope's camera exception: the view is pushed toward where the weapon is
  // pointed, by a fraction of the reach the server widened the snapshot by.
  private scopeOffset = { x: 0, y: 0 };

  async init(host: HTMLElement): Promise<void> {
    await this.app.init({ resizeTo: window, antialias: true, backgroundColor: colors.ground, resolution: Math.min(2, devicePixelRatio), autoDensity: true });
    host.replaceChildren(this.app.canvas);
    this.world.addChild(this.ground, this.telegraphLayer, this.entityLayer, this.fogLayer);
    this.app.stage.addChild(this.world, this.blackout);
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
    for (const [id, samples] of this.samples) if (!present.has(id) && receivedAt - (samples.at(-1)?.at ?? 0) > 500) this.removeActor(id);
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
    this.app.destroy(true, { children: true }); this.initialized = false;
  }

  private renderFrame(): void {
    const predictor = this.predictor;
    if (!predictor) return;
    const now = performance.now();
    const width = this.app.screen.width, height = this.app.screen.height;
    const shake = this.cameraShake(now);
    const cameraX = predictor.x + this.scopeOffset.x + shake.x, cameraY = predictor.y + this.scopeOffset.y + shake.y;
    this.world.position.set(width / 2 - cameraX, height / 2 - cameraY);
    this.drawGround(cameraX, cameraY, width, height);
    const local = this.latestEntities.get(this.localID);
    if (local) this.drawEntity({ ...local, x: predictor.x, y: predictor.y, aimX: predictor.aimX, aimY: predictor.aimY }, true, now);
    const renderAt = now - simulation.interpolation_delay_ms;
    for (const [id, samples] of this.samples) {
      const entity = interpolate(samples, renderAt);
      if (entity) this.drawEntity(entity, false, now);
      else if (!this.latestEntities.has(id)) this.removeActor(id);
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
    this.ground.circle(0, 0, safeRadius).stroke({ color: colors.safe, width: 5, alpha: .7 });
    this.ground.circle(0, 0, world.radius).stroke({ color: colors.rim, width: 8, alpha: .8 });
  }

  private drawEntity(entity: Entity, self: boolean, now = performance.now()): void {
    let view = this.actors.get(entity.id);
    if (view && view.type !== entity.type) { this.removeActor(entity.id); view = undefined; }
    if (!view) {
      view = this.createActor(entity, self, now); this.actors.set(entity.id, view);
      this.layerFor(entity.type).addChild(view.root);
    }
    view.root.position.set(entity.x, entity.y);
    view.root.alpha = entity.deleting ? Math.max(0, 1 - entity.deleteProgress) : entity.alive ? entity.lingering ? .62 : 1 : .32;
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

  private layerFor(type: number): Container {
    if (type === EntityType.Telegraph) return this.telegraphLayer;
    return type === EntityType.Deployable ? this.fogLayer : this.entityLayer;
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
   * A deployed field, drawn as drifting puffs rather than a disc. The layout is
   * seeded from the field's own identity so it is stable frame to frame, and it
   * fades in on arrival and out with the shared removal fade.
   */
  private drawField(view: ActorView, now: number): void {
    const age = now - view.bornAt;
    const arriving = Math.min(1, age / puffFadeMS);
    for (const puff of view.puffs) {
      const angle = puff.angle + (age / 1000) * puff.drift;
      const breathe = 1 + Math.sin(age / 620 + puff.angle) * .08;
      puff.graphic.position.set(Math.cos(angle) * puff.distance, Math.sin(angle) * puff.distance);
      puff.graphic.scale.set(breathe);
      puff.graphic.alpha = .5 * arriving;
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
      for (let index = 0; index < puffCount; index++) {
        const spin = fraction(seed + index * 97);
        const puff = new Graphics();
        const size = radius * (.4 + fraction(seed + index * 31) * .25);
        puff.circle(0, 0, size).fill({ color: 0xdfe6ef, alpha: 1 });
        puffs.push({
          graphic: puff, distance: radius * .55 * fraction(seed + index * 53),
          angle: spin * Math.PI * 2, radius: size, drift: (spin - .5) * .5,
        });
        body.addChild(puff);
      }
    } else if (entity.type === EntityType.Boss) {
      body.moveTo(0, -42).lineTo(38, -18).lineTo(32, 34).lineTo(-32, 34).lineTo(-38, -18).closePath().fill(0x657186).stroke({ color: outline, width: 7 });
    } else if (entity.type === EntityType.WorldItem && entity.className === "tree") {
      const radius = entity.radius;
      body.rect(-6, 4, 12, radius).fill(colors.trunk).stroke({ color: colors.outline, width: 4 });
      body.circle(-radius * .3, 0, radius * .72).circle(radius * .35, 2, radius * .67).circle(0, -radius * .42, radius * .72).fill(colors.tree).stroke({ color: colors.outline, width: 4 });
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
    return { root, body, weapon, health, stance, label, type: entity.type, shots: entity.shots, firedAt: 0, puffs, bornAt: now };
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
