import { Application, Container, Graphics, Text } from "pixi.js";
// Install eval-free polyfills so WebGL works under a CSP without 'unsafe-eval'. Side-effect import; must run before the renderer is created.
import "pixi.js/unsafe-eval";
import type { Collider, Entity, ServerMessage } from "../types";
import type { Predictor } from "./prediction";

interface Sample { at: number; entity: Entity }
interface ActorView { root: Container; weapon: Graphics; health: Graphics; label: Text; type: number }

const colors = {
  ground: 0x16233a, grid: 0x2c405e, safe: 0x7ee1bb, self: 0xffffff, hostile: 0xff6f69,
  gunner: 0x9cabbf, mage: 0xff754d, tree: 0x46795a, trunk: 0x795b43, outline: 0x0b1220,
};

export class GameView {
  readonly app = new Application();
  private world = new Container();
  private ground = new Graphics();
  private colliderLayer = new Container();
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
    this.world.addChild(this.ground, this.colliderLayer, this.entityLayer);
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
    const renderAt = performance.now() - 100;
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
    this.ground.circle(0, 0, 430).stroke({ color: colors.safe, width: 5, alpha: .7 });
    this.ground.circle(0, 0, 3000).stroke({ color: 0x8d5260, width: 8, alpha: .8 });
  }

  private createTree(collider: Collider): Container {
    const root = new Container(); root.position.set(collider.x, collider.y);
    const shape = new Graphics().rect(-6, 4, 12, collider.radius).fill(colors.trunk).stroke({ color: colors.outline, width: 4 });
    shape.circle(-collider.radius * .3, 0, collider.radius * .72).circle(collider.radius * .35, 2, collider.radius * .67).circle(0, -collider.radius * .42, collider.radius * .72).fill(colors.tree).stroke({ color: colors.outline, width: 4 });
    root.addChild(shape); return root;
  }

  private drawEntity(entity: Entity, self: boolean): void {
    let view = this.actors.get(entity.id);
    if (!view) { view = this.createActor(entity, self); this.actors.set(entity.id, view); this.entityLayer.addChild(view.root); }
    view.root.position.set(entity.x, entity.y); view.root.alpha = entity.alive ? 1 : .32;
    if (entity.type === 1) {
      view.weapon.rotation = Math.atan2(entity.aimY, entity.aimX);
      view.health.clear().roundRect(-27, -39, 54, 7, 3).fill(colors.outline).roundRect(-25, -37, 50 * Math.max(0, entity.health / Math.max(1, entity.maxHealth)), 3, 2).fill(entity.health > 30 ? 0x65d89d : 0xff7f73);
      view.label.text = entity.name;
    }
  }

  private createActor(entity: Entity, self: boolean): ActorView {
    const root = new Container(), body = new Graphics(), weapon = new Graphics(), health = new Graphics();
    if (entity.type === 2) {
      if (entity.className === "fireball") body.circle(0, 0, 9).fill(0xff754d).stroke({ color: colors.outline, width: 3 });
      else body.rect(-7, -3, 14, 6).fill(0xffd166).stroke({ color: colors.outline, width: 2 });
    } else if (entity.className === "mage") {
      body.circle(0, 0, 20).fill(colors.mage).stroke({ color: self ? colors.self : colors.hostile, width: 4 });
      body.moveTo(-15, 14).lineTo(0, -23).lineTo(15, 14).closePath().fill(0xd54e64).stroke({ color: colors.outline, width: 3 });
      weapon.moveTo(8, 7).lineTo(37, 0).stroke({ color: 0xb68862, width: 6 }).circle(39, 0, 7).fill(0xff754d).stroke({ color: colors.outline, width: 3 });
    } else {
      body.moveTo(-17, -17).lineTo(18, -13).lineTo(17, 16).lineTo(-18, 14).closePath().fill(colors.gunner).stroke({ color: self ? colors.self : colors.hostile, width: 4 });
      weapon.roundRect(8, -5, 36, 10, 2).fill(0x5c6674).stroke({ color: colors.outline, width: 3 }).rect(18, 5, 10, 8).fill(0x353e4c);
    }
    const label = new Text({ text: entity.name, style: { fontFamily: "system-ui", fontSize: 12, fill: 0xffffff, stroke: { color: colors.outline, width: 3 } } }); label.anchor.set(.5); label.position.set(0, -50);
    root.addChild(body, weapon, health, label);
    return { root, weapon, health, label, type: entity.type };
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
    return { ...next.entity, x: previous.entity.x + (next.entity.x - previous.entity.x) * t, y: previous.entity.y + (next.entity.y - previous.entity.y) * t, aimX: previous.entity.aimX + (next.entity.aimX - previous.entity.aimX) * t, aimY: previous.entity.aimY + (next.entity.aimY - previous.entity.aimY) * t };
  }
  return samples.at(-1)?.entity;
}
