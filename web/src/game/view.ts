import { Application, Container, Graphics, Text } from "pixi.js";
// Install eval-free polyfills so WebGL works under a CSP without 'unsafe-eval'. Side-effect import; must run before the renderer is created.
import "pixi.js/unsafe-eval";
import { projectileByKind, safeRadius, simulation, world } from "../tuning";
import type { Collider, Entity, ServerMessage } from "../types";
import { Allegiance, EntityType } from "../types";
import type { Predictor } from "./prediction";
import { telegraphStyle } from "./telegraph";

interface Sample { at: number; entity: Entity }
interface ActorView { root: Container; body: Graphics; weapon: Graphics; health: Graphics; label: Text; type: number }

const colors = {
  ground: 0x16233a, grid: 0x2c405e, safe: 0x7ee1bb, rim: 0x8d5260, self: 0xffffff, hostile: 0xff6f69,
  gunner: 0x9cabbf, mage: 0xff754d, bullet: 0xffd166, tree: 0x46795a, trunk: 0x795b43, outline: 0x0b1220,
  squad: 0x7ee1bb, neutral: 0xaeb9c8,
};

const elementColors: Record<string, number> = { fire: 0xff754d, frost: 0x75dbf0, storm: 0xffd166, arcane: 0xd879e8, earth: 0xb99568 };

export class GameView {
  readonly app = new Application();
  private world = new Container();
  private ground = new Graphics();
  private colliderLayer = new Container();
  private telegraphLayer = new Container();
  private entityLayer = new Container();
  private actors = new Map<string, ActorView>();
  private samples = new Map<string, Sample[]>();
  private colliders = new Map<string, Collider>();
  private localID = "";
  private predictor?: Predictor;
  private latestEntities = new Map<string, Entity>();
  private initialized = false;

  async init(host: HTMLElement): Promise<void> {
    await this.app.init({ resizeTo: window, antialias: true, backgroundColor: colors.ground, resolution: Math.min(2, devicePixelRatio), autoDensity: true });
    host.replaceChildren(this.app.canvas);
    this.world.addChild(this.ground, this.colliderLayer, this.telegraphLayer, this.entityLayer);
    this.app.stage.addChild(this.world);
    this.app.ticker.add(() => this.renderFrame());
    this.initialized = true;
  }

  bindPredictor(predictor: Predictor): void { this.predictor = predictor; }

  apply(message: ServerMessage): void {
    this.localID = message.playerID || this.localID;
    for (const collider of message.colliders) {
      if (this.colliders.has(collider.id)) continue;
      this.colliders.set(collider.id, collider); this.colliderLayer.addChild(this.createTree(collider));
    }
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
    return { x: clientX - this.app.screen.width / 2, y: clientY - this.app.screen.height / 2 };
  }

  // Placement tools need an absolute world coordinate, unlike aiming, which
  // deliberately uses the player-relative vector returned by pointerWorld.
  worldAtPointer(clientX: number, clientY: number): { x: number; y: number } {
    const point = this.pointerWorld(clientX, clientY), predictor = this.predictor;
    return predictor ? { x: predictor.x + point.x, y: predictor.y + point.y } : point;
  }

  destroy(): void {
    if (!this.initialized) return;
    this.app.destroy(true, { children: true }); this.initialized = false;
  }

  private renderFrame(): void {
    const predictor = this.predictor;
    if (!predictor) return;
    const width = this.app.screen.width, height = this.app.screen.height;
    this.world.position.set(width / 2 - predictor.x, height / 2 - predictor.y);
    this.drawGround(predictor.x, predictor.y, width, height);
    const local = this.latestEntities.get(this.localID);
    if (local) this.drawEntity({ ...local, x: predictor.x, y: predictor.y, aimX: predictor.aimX, aimY: predictor.aimY }, true);
    const renderAt = performance.now() - simulation.interpolation_delay_ms;
    for (const [id, samples] of this.samples) {
      const entity = interpolate(samples, renderAt);
      if (entity) this.drawEntity(entity, false);
      else if (!this.latestEntities.has(id)) this.removeActor(id);
    }
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

  private createTree(collider: Collider): Container {
    const root = new Container(); root.position.set(collider.x, collider.y);
    const shape = new Graphics().rect(-6, 4, 12, collider.radius).fill(colors.trunk).stroke({ color: colors.outline, width: 4 });
    shape.circle(-collider.radius * .3, 0, collider.radius * .72).circle(collider.radius * .35, 2, collider.radius * .67).circle(0, -collider.radius * .42, collider.radius * .72).fill(colors.tree).stroke({ color: colors.outline, width: 4 });
    root.addChild(shape); return root;
  }

  private drawEntity(entity: Entity, self: boolean): void {
    let view = this.actors.get(entity.id);
    if (view && view.type !== entity.type) { this.removeActor(entity.id); view = undefined; }
    if (!view) {
      view = this.createActor(entity, self); this.actors.set(entity.id, view);
      (entity.type === EntityType.Telegraph ? this.telegraphLayer : this.entityLayer).addChild(view.root);
    }
    view.root.position.set(entity.x, entity.y);
    view.root.alpha = entity.alive ? entity.lingering ? .62 : 1 : .32;
    if (entity.type === EntityType.Player) {
      view.weapon.rotation = Math.atan2(entity.aimY, entity.aimX);
      view.health.clear().roundRect(-27, -39, 54, 7, 3).fill(colors.outline).roundRect(-25, -37, 50 * Math.max(0, entity.health / Math.max(1, entity.maxHealth)), 3, 2).fill(entity.health > 30 ? 0x65d89d : 0xff7f73);
      view.label.text = entity.lingering ? `${entity.name} · offline` : entity.name;
      if (entity.invulnerable) view.health.circle(0, 0, 27).stroke({ color: colors.safe, width: 3, alpha: .9 });
    } else if (entity.type === EntityType.Mob) {
      view.weapon.rotation = Math.atan2(entity.aimY, entity.aimX);
    } else if (entity.type === EntityType.Telegraph) {
      this.drawTelegraph(view.body, entity);
    }
  }

  private createActor(entity: Entity, self: boolean): ActorView {
    const root = new Container(), body = new Graphics(), weapon = new Graphics(), health = new Graphics();
    const outline = allegianceColor(entity.allegiance, self);
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
      body.rect(-15, -15, 30, 30).fill(colors.gunner).stroke({ color: outline, width: 4 });
    } else if (entity.type === EntityType.Boss) {
      body.moveTo(0, -42).lineTo(38, -18).lineTo(32, 34).lineTo(-32, 34).lineTo(-38, -18).closePath().fill(0x657186).stroke({ color: outline, width: 7 });
    } else if (entity.className === "mage") {
      body.circle(0, 0, 20).fill(elementColors[entity.element] ?? colors.mage).stroke({ color: outline, width: 4 });
      body.moveTo(-15, 14).lineTo(0, -23).lineTo(15, 14).closePath().fill(0xd54e64).stroke({ color: colors.outline, width: 3 });
      weapon.moveTo(8, 7).lineTo(37, 0).stroke({ color: 0xb68862, width: 6 }).circle(39, 0, 7).fill(0xff754d).stroke({ color: colors.outline, width: 3 });
    } else {
      body.moveTo(-17, -17).lineTo(18, -13).lineTo(17, 16).lineTo(-18, 14).closePath().fill(colors.gunner).stroke({ color: outline, width: 4 });
      weapon.roundRect(8, -5, 36, 10, 2).fill(0x5c6674).stroke({ color: colors.outline, width: 3 }).rect(18, 5, 10, 8).fill(0x353e4c);
    }
    const label = new Text({ text: entity.name, style: { fontFamily: "system-ui", fontSize: 12, fill: 0xffffff, stroke: { color: colors.outline, width: 3 } } }); label.anchor.set(.5); label.position.set(0, -50);
    root.addChild(body, weapon, health, label);
    return { root, body, weapon, health, label, type: entity.type };
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
    return { ...next.entity, x: previous.entity.x + (next.entity.x - previous.entity.x) * t, y: previous.entity.y + (next.entity.y - previous.entity.y) * t, aimX: previous.entity.aimX + (next.entity.aimX - previous.entity.aimX) * t, aimY: previous.entity.aimY + (next.entity.aimY - previous.entity.aimY) * t, telegraphState: sameTelegraphPhase ? next.entity.telegraphState : previous.entity.telegraphState, telegraphProgress: sameTelegraphPhase ? previous.entity.telegraphProgress + (next.entity.telegraphProgress - previous.entity.telegraphProgress) * t : previous.entity.telegraphProgress };
  }
  return samples.at(-1)?.entity;
}

function allegianceColor(allegiance: number, self: boolean): number {
  if (self || allegiance === Allegiance.Self) return colors.self;
  if (allegiance === Allegiance.Squad) return colors.squad;
  if (allegiance === Allegiance.Neutral) return colors.neutral;
  return colors.hostile;
}
