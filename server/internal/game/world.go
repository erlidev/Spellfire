package game

import (
	"fmt"
	"math"
	"sort"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

const (
	ButtonUp     uint32 = 1
	ButtonDown   uint32 = 2
	ButtonLeft   uint32 = 4
	ButtonRight  uint32 = 8
	ButtonFire   uint32 = 16
	ButtonDash   uint32 = 32
	ButtonReload uint32 = 64
)

// Tuning is the simulation's runtime view of the versioned tables. No balance
// value is authored here: FromTables derives every field from a table row, so
// editing a row changes the simulation without any code or character change.
// Only the process-level rates are meant to be overridden after construction,
// and only from deployment configuration.
type Tuning struct {
	Tables *tuning.Tables

	TickRate, SendRate int
	AOIRadius          float64
	MaxRewind          time.Duration

	WorldRadius, SafeRadius, PvPRadius float64
	PlayerRadius, PlayerSpeed          float64
	DashDistance                       float64
	DashDuration, DashCooldown         time.Duration
	MaxHealth, MaxMana, ManaRegen      float64
	LogoutLinger, PositionExpiry       time.Duration
}

func FromTables(tables *tuning.Tables) Tuning {
	body, dash := tables.Combat.Player, tables.Combat.Dash
	return Tuning{
		Tables:   tables,
		TickRate: tables.Simulation.TickRate, SendRate: tables.Simulation.SendRate,
		AOIRadius: tables.Simulation.AOIRadius, MaxRewind: tables.Simulation.MaxRewind(),
		WorldRadius: tables.World.Radius, SafeRadius: tables.World.SafeRadius(), PvPRadius: tables.World.PvPRadius(),
		PlayerRadius: body.Radius, PlayerSpeed: body.Speed,
		DashDistance: dash.Distance, DashDuration: dash.Duration(), DashCooldown: dash.Cooldown(),
		MaxHealth: body.MaxHealth, MaxMana: body.MaxMana, ManaRegen: body.ManaRegen,
		LogoutLinger: tables.Session.LogoutLinger(), PositionExpiry: tables.Session.PositionExpiry(),
	}
}

func DefaultTuning() Tuning { return FromTables(tuning.MustLoad()) }

type Vec struct{ X, Y float64 }

func (v Vec) Add(o Vec) Vec     { return Vec{v.X + o.X, v.Y + o.Y} }
func (v Vec) Sub(o Vec) Vec     { return Vec{v.X - o.X, v.Y - o.Y} }
func (v Vec) Mul(s float64) Vec { return Vec{v.X * s, v.Y * s} }
func (v Vec) LengthSq() float64 { return v.X*v.X + v.Y*v.Y }
func (v Vec) Normalized() Vec {
	l := math.Sqrt(v.LengthSq())
	if l < 0.0001 {
		return Vec{}
	}
	return v.Mul(1 / l)
}

type Player struct {
	ID, Name                        string
	Class                           model.Class
	WeaponID                        string
	Position, Velocity, Aim         Vec
	Health, Mana                    float64
	Alive                           bool
	Input                           protocol.Input
	Acknowledged                    uint32
	PreviousButtons                 uint32
	NextFire, DashReady, ReloadEnds time.Time
	Ammo                            int
	DashDirection                   Vec
	DashTicksLeft                   int
	// Carried materials and unlocked outposts are persisted references, held
	// here so they survive a disconnect. Harvesting (Phase 4.1) and outpost
	// discovery (Phase 3) are what will mutate them.
	Materials map[string]int
	Outposts  []string
	// LingerUntil is set when the connection drops: the body stays in the world,
	// killable and unable to act, until it passes. Zero means connected.
	LingerUntil time.Time
}

// Lingering reports whether the body is present only because its owner
// disconnected. A lingering player takes damage but cannot act.
func (p *Player) Lingering() bool { return !p.LingerUntil.IsZero() }

// A dash covers DashDistance over DashDuration, quantized to whole ticks so the
// client's fixed-rate prediction reproduces it exactly.
func (t Tuning) dashTicks() int {
	ticks := int(math.Round(t.DashDuration.Seconds() * float64(t.TickRate)))
	if ticks < 1 {
		ticks = 1
	}
	return ticks
}

func (t Tuning) dashSpeed() float64 {
	return t.DashDistance / (float64(t.dashTicks()) / float64(t.TickRate))
}

type Projectile struct {
	ID, OwnerID, Kind         string
	Position, Velocity        Vec
	Radius, Damage, Remaining float64
}

type Collider struct {
	ID, Kind string
	Position Vec
	Radius   float64
}

type historySample struct {
	at       time.Time
	position Vec
}

type World struct {
	tuning         Tuning
	tick           uint64
	players        map[string]*Player
	projectiles    map[string]*Projectile
	colliders      []Collider
	history        map[string][]historySample
	nextProjectile uint64
}

func NewWorld(t Tuning) *World {
	if t.Tables == nil || t.TickRate <= 0 {
		t = DefaultTuning()
	}
	return &World{
		tuning: t, players: make(map[string]*Player), projectiles: make(map[string]*Projectile),
		colliders: generateTrees(t.Tables.World), history: make(map[string][]historySample),
	}
}

func (w *World) AddPlayer(character model.Character, now time.Time) *Player {
	if existing := w.players[character.ID]; existing != nil {
		// Reconnecting inside the logout window resumes the body that stayed
		// behind, wherever the fight has since moved it.
		existing.LingerUntil = time.Time{}
		return existing
	}
	p := &Player{
		ID: character.ID, Name: character.Name, Class: character.Class,
		Position: w.entryPosition(character, now), Aim: Vec{1, 0},
		Health: w.tuning.MaxHealth, Mana: w.tuning.MaxMana, Alive: true,
		Materials: w.carriedMaterials(character.State.Materials),
		Outposts:  append([]string(nil), character.State.Outposts...),
	}
	// Until the Phase 2 loadout lands, the equipped weapon is the class starter
	// row. It is a table reference, never a copy of its stats.
	if weapon, ok := w.tuning.Tables.StarterWeapon(string(character.Class)); ok {
		p.WeaponID, p.Ammo = weapon.ID, weapon.MagazineSize
	}
	w.players[p.ID] = p
	w.recordHistory(p, now)
	return p
}

// entryPosition decides where a character re-enters the world. A recent save is
// honoured exactly, so a disconnect costs the session rather than the walk back
// out. Once the save has gone stale — or the world has moved under it — the
// character is recalled to safety near where it left off instead.
func (w *World) entryPosition(character model.Character, now time.Time) Vec {
	if !character.State.Placed {
		return w.hubSpawn(character.ID)
	}
	position := Vec{character.State.Position.X, character.State.Position.Y}
	if math.IsNaN(position.X) || math.IsNaN(position.Y) {
		return w.hubSpawn(character.ID)
	}
	if !w.positionExpired(character.State.LastSeen, now) && w.standable(position) {
		return position
	}
	return w.recallDestination(character, position)
}

// positionExpired reports whether a saved position is too old to honour. An
// unstamped save is expired: it may be arbitrarily old, and honouring it would
// drop a character into whatever the world has become since.
func (w *World) positionExpired(lastSeen, now time.Time) bool {
	return lastSeen.IsZero() || now.Sub(lastSeen) >= w.tuning.PositionExpiry
}

// standable rejects a saved position the current world can no longer accept:
// outside the rim, or inside cover that did not exist when it was written.
func (w *World) standable(position Vec) bool {
	limit := w.tuning.WorldRadius - w.tuning.PlayerRadius
	return position.LengthSq() <= limit*limit && !w.collides(position, w.tuning.PlayerRadius)
}

// recallDestination picks the safe fixture nearest to where the character left
// off: any outpost it has unlocked, or the central hub. Outposts have no
// geography until Phase 3 fills the table, so today this always resolves to the
// hub — the fallback the design already guarantees exists.
func (w *World) recallDestination(character model.Character, from Vec) Vec {
	best := w.hubSpawn(character.ID)
	nearest := best.Sub(from).LengthSq()
	for _, id := range character.State.Outposts {
		outpost, ok := w.tuning.Tables.Outposts[id]
		if !ok {
			continue
		}
		position := Vec{outpost.Position[0], outpost.Position[1]}
		if distance := position.Sub(from).LengthSq(); distance < nearest && w.standable(position) {
			best, nearest = position, distance
		}
	}
	return best
}

// hubSpawn is the character's deterministic point on the central spawn ring.
func (w *World) hubSpawn(id string) Vec {
	angle := float64(hash(id)%628) / 100
	spawn := w.tuning.Tables.World.SpawnRadius
	return Vec{math.Cos(angle) * spawn, math.Sin(angle) * spawn}
}

// carriedMaterials maps a saved inventory through the retirement table. A
// material the content has since retired resolves to its replacement or to its
// refund rather than vanishing; only an ID this build has never heard of is
// dropped, because there is nothing left to honour it with.
func (w *World) carriedMaterials(saved map[string]int) map[string]int {
	carried := make(map[string]int, len(saved))
	ids := make([]string, 0, len(saved))
	for id := range saved {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		count := saved[id]
		if count <= 0 {
			continue
		}
		resolved, ok := w.tuning.Tables.Resolve("material", id)
		if !ok {
			continue
		}
		if resolved.ID != "" {
			carried[resolved.ID] += count
			continue
		}
		for material, per := range resolved.Refund {
			carried[material] += per * count
		}
	}
	return carried
}

// BeginLinger leaves a disconnected player's body in the world until the logout
// window closes. It stays a valid target the whole time, so dropping the
// connection is not an escape from a fight.
func (w *World) BeginLinger(id string, now time.Time) bool {
	p := w.players[id]
	if p == nil {
		return false
	}
	p.LingerUntil = now.Add(w.tuning.LogoutLinger)
	p.Input.Buttons, p.PreviousButtons = 0, 0
	p.Velocity, p.DashTicksLeft = Vec{}, 0
	return true
}

// ExpiredLingering lists the bodies whose logout window has closed, in
// deterministic order. The caller saves each before removing it.
func (w *World) ExpiredLingering(now time.Time) []string {
	expired := make([]string, 0)
	for _, id := range sortedPlayerIDs(w.players) {
		if p := w.players[id]; p.Lingering() && !now.Before(p.LingerUntil) {
			expired = append(expired, id)
		}
	}
	return expired
}

// StateOf captures what must survive a disconnect. A dead player is saved
// unplaced, so the next join enters at the hub spawn — the same destination the
// current instant respawn uses; Phase 4.2 replaces both with a chosen outpost.
func (w *World) StateOf(id string, now time.Time) (model.CharacterState, bool) {
	p := w.players[id]
	if p == nil {
		return model.CharacterState{}, false
	}
	materials := make(map[string]int, len(p.Materials))
	for material, count := range p.Materials {
		materials[material] = count
	}
	return model.CharacterState{
		Position:  model.Point{X: p.Position.X, Y: p.Position.Y},
		Placed:    p.Alive,
		LastSeen:  now,
		Materials: materials,
		Outposts:  append([]string(nil), p.Outposts...),
	}, true
}

// States captures every present player's persistable state for one save pass,
// lingering bodies included — their position is the one that must be kept.
func (w *World) States(now time.Time) map[string]model.CharacterState {
	states := make(map[string]model.CharacterState, len(w.players))
	for _, id := range sortedPlayerIDs(w.players) {
		if state, ok := w.StateOf(id, now); ok {
			states[id] = state
		}
	}
	return states
}

// weapon resolves the player's equipped row from the tables each time it is
// needed, so nothing caches a stat snapshot.
func (w *World) weapon(p *Player) (tuning.Weapon, bool) {
	weapon, ok := w.tuning.Tables.Weapons[p.WeaponID]
	return weapon, ok
}

func (w *World) RemovePlayer(id string) { delete(w.players, id); delete(w.history, id) }

func (w *World) ApplyInput(id string, input protocol.Input) bool {
	p := w.players[id]
	if p == nil || input.Sequence <= p.Input.Sequence {
		return false
	}
	if math.IsNaN(float64(input.AimX)) || math.IsNaN(float64(input.AimY)) {
		return false
	}
	p.Input = input
	return true
}

func (w *World) Respawn(id string, now time.Time) bool {
	p := w.players[id]
	if p == nil || p.Alive {
		return false
	}
	p.Position, p.Velocity, p.DashDirection, p.DashTicksLeft = Vec{}, Vec{}, Vec{}, 0
	p.Health, p.Mana, p.Alive = w.tuning.MaxHealth, w.tuning.MaxMana, true
	if weapon, ok := w.weapon(p); ok {
		p.Ammo = weapon.MagazineSize
	}
	p.NextFire, p.ReloadEnds, p.DashReady = now, now, now
	w.recordHistory(p, now)
	return true
}

func (w *World) Step(now time.Time) {
	dt := 1 / float64(w.tuning.TickRate)
	w.tick++
	ids := sortedPlayerIDs(w.players)
	for _, id := range ids {
		w.stepPlayer(w.players[id], now, dt)
	}
	w.stepProjectiles(now, dt)
	for _, id := range ids {
		if p := w.players[id]; p != nil {
			w.recordHistory(p, now)
		}
	}
}

func (w *World) stepPlayer(p *Player, now time.Time, dt float64) {
	// A lingering body is a target, not an actor: it holds its ground, takes
	// damage, and neither moves nor fires until the logout window closes.
	if !p.Alive || p.Lingering() {
		p.Velocity, p.DashTicksLeft = Vec{}, 0
		p.Acknowledged = p.Input.Sequence
		return
	}
	aim := Vec{float64(p.Input.AimX), float64(p.Input.AimY)}.Normalized()
	if aim.LengthSq() > 0 {
		p.Aim = aim
	}
	move := Vec{}
	if p.Input.Buttons&ButtonUp != 0 {
		move.Y--
	}
	if p.Input.Buttons&ButtonDown != 0 {
		move.Y++
	}
	if p.Input.Buttons&ButtonLeft != 0 {
		move.X--
	}
	if p.Input.Buttons&ButtonRight != 0 {
		move.X++
	}
	move = move.Normalized()
	if p.Input.Buttons&ButtonDash != 0 && p.PreviousButtons&ButtonDash == 0 && !now.Before(p.DashReady) {
		p.DashDirection = move
		if p.DashDirection.LengthSq() == 0 {
			p.DashDirection = p.Aim
		}
		p.DashTicksLeft = w.tuning.dashTicks()
		p.DashReady = now.Add(w.tuning.DashCooldown)
	}
	if p.DashTicksLeft > 0 {
		p.Velocity = p.DashDirection.Mul(w.tuning.dashSpeed())
		p.DashTicksLeft--
	} else {
		p.Velocity = move.Mul(w.tuning.PlayerSpeed)
	}
	p.Position = w.moveCircle(p.Position, p.Velocity.Mul(dt), w.tuning.PlayerRadius)
	if p.Class == model.Mage {
		p.Mana = math.Min(w.tuning.MaxMana, p.Mana+w.tuning.ManaRegen*dt)
	}
	// Magazine size and reload time are weapon properties; a weapon without a
	// magazine (a staff) never enters the reload path.
	if weapon, ok := w.weapon(p); ok && weapon.MagazineSize > 0 {
		if !p.ReloadEnds.IsZero() && !now.Before(p.ReloadEnds) {
			p.Ammo, p.ReloadEnds = weapon.MagazineSize, time.Time{}
		}
		if p.Input.Buttons&ButtonReload != 0 && p.ReloadEnds.IsZero() && p.Ammo < weapon.MagazineSize {
			p.ReloadEnds = now.Add(weapon.ReloadDuration())
		}
	}
	if p.Input.Buttons&ButtonFire != 0 {
		w.tryFire(p, now)
	}
	p.PreviousButtons = p.Input.Buttons
	p.Acknowledged = p.Input.Sequence
}

// tryFire spends the cost the equipped row declares. A weapon with a magazine
// spends ammunition and reloads; one that casts a spell spends the spell's
// mana. The branch is data, not class.
func (w *World) tryFire(p *Player, now time.Time) {
	if now.Before(p.NextFire) {
		return
	}
	weapon, ok := w.weapon(p)
	if !ok {
		return
	}
	shot, ok := w.tuning.Tables.Shot(weapon)
	if !ok {
		return
	}
	if weapon.MagazineSize > 0 {
		if !p.ReloadEnds.IsZero() {
			return
		}
		if p.Ammo <= 0 {
			p.ReloadEnds = now.Add(weapon.ReloadDuration())
			return
		}
		p.Ammo--
	} else if shot.ManaCost > 0 {
		if p.Mana < shot.ManaCost {
			return
		}
		p.Mana -= shot.ManaCost
	}
	p.NextFire = now.Add(shot.Interval)
	w.spawnRewoundProjectile(p, shot, now)
}

func (w *World) spawnRewoundProjectile(p *Player, shot tuning.Shot, now time.Time) {
	shotAt := time.UnixMilli(int64(p.Input.ClientTimeMS))
	oldest := now.Add(-w.tuning.MaxRewind)
	if shotAt.Before(oldest) {
		shotAt = oldest
	}
	if shotAt.After(now) {
		shotAt = now
	}
	origin := w.positionAt(p.ID, shotAt)
	projectile := &Projectile{
		ID: fmt.Sprintf("p-%d", w.nextProjectile), OwnerID: p.ID, Kind: shot.Projectile.Kind,
		Radius: shot.Projectile.Radius, Damage: shot.Damage, Remaining: shot.Projectile.LifeSeconds,
		Velocity: p.Aim.Mul(shot.Projectile.Speed),
	}
	w.nextProjectile++
	projectile.Position = origin.Add(p.Aim.Mul(w.tuning.PlayerRadius + shot.Projectile.Radius + 2))
	step := time.Second / time.Duration(w.tuning.TickRate)
	for at := shotAt; at.Before(now); at = at.Add(step) {
		duration := step
		if at.Add(duration).After(now) {
			duration = now.Sub(at)
		}
		if w.advanceProjectile(projectile, duration.Seconds(), at.Add(duration), true) {
			return
		}
	}
	w.projectiles[projectile.ID] = projectile
}

func (w *World) stepProjectiles(now time.Time, dt float64) {
	ids := make([]string, 0, len(w.projectiles))
	for id := range w.projectiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := w.projectiles[id]
		p.Remaining -= dt
		if p.Remaining <= 0 || w.advanceProjectile(p, dt, now, false) {
			delete(w.projectiles, id)
		}
	}
}

func (w *World) advanceProjectile(projectile *Projectile, dt float64, at time.Time, historical bool) bool {
	from, to := projectile.Position, projectile.Position.Add(projectile.Velocity.Mul(dt))
	for _, tree := range w.colliders {
		if segmentCircle(from, to, tree.Position, projectile.Radius+tree.Radius) {
			return true
		}
	}
	owner := w.players[projectile.OwnerID]
	for id, target := range w.players {
		if id == projectile.OwnerID || !target.Alive {
			continue
		}
		position := target.Position
		if historical {
			position = w.positionAt(id, at)
		}
		if !segmentCircle(from, to, position, projectile.Radius+w.tuning.PlayerRadius) {
			continue
		}
		if owner != nil && owner.Position.LengthSq() > w.tuning.PvPRadius*w.tuning.PvPRadius && position.LengthSq() > w.tuning.PvPRadius*w.tuning.PvPRadius {
			target.Health = math.Max(0, target.Health-projectile.Damage)
			if target.Health == 0 {
				target.Alive = false
				target.Velocity = Vec{}
			}
		}
		return true
	}
	projectile.Position = to
	return to.LengthSq() > w.tuning.WorldRadius*w.tuning.WorldRadius
}

func (w *World) moveCircle(from, delta Vec, radius float64) Vec {
	result := Vec{from.X + delta.X, from.Y}
	if w.collides(result, radius) {
		result.X = from.X
	}
	result.Y += delta.Y
	if w.collides(result, radius) {
		result.Y = from.Y
	}
	limit := w.tuning.WorldRadius - radius
	if length := math.Sqrt(result.LengthSq()); length > limit {
		result = result.Mul(limit / length)
	}
	return result
}

func (w *World) collides(position Vec, radius float64) bool {
	for _, collider := range w.colliders {
		if position.Sub(collider.Position).LengthSq() < math.Pow(radius+collider.Radius, 2) {
			return true
		}
	}
	return false
}

func (w *World) recordHistory(p *Player, now time.Time) {
	samples := append(w.history[p.ID], historySample{at: now, position: p.Position})
	cutoff := now.Add(-w.tuning.MaxRewind - 50*time.Millisecond)
	first := 0
	for first < len(samples)-1 && samples[first].at.Before(cutoff) {
		first++
	}
	w.history[p.ID] = samples[first:]
}

func (w *World) positionAt(id string, at time.Time) Vec {
	samples := w.history[id]
	if len(samples) == 0 {
		if p := w.players[id]; p != nil {
			return p.Position
		}
		return Vec{}
	}
	if at.Before(samples[0].at) {
		return samples[0].position
	}
	for i := 1; i < len(samples); i++ {
		if at.After(samples[i].at) {
			continue
		}
		a, b := samples[i-1], samples[i]
		span := b.at.Sub(a.at)
		if span <= 0 {
			return b.position
		}
		t := float64(at.Sub(a.at)) / float64(span)
		return a.position.Add(b.position.Sub(a.position).Mul(t))
	}
	return samples[len(samples)-1].position
}

// generateTrees lays out deterministic static cover from the world table. The
// same seed and margins produce the same forest on every process start.
func generateTrees(world tuning.World) []Collider {
	trees, safeRadius := world.Trees, world.SafeRadius()
	// Trees start InnerMargin outside the safe radius and stop OuterMargin
	// short of the rim, so neither the hub nor the world edge is walled in.
	reach := world.Radius - safeRadius - trees.OuterMargin
	if trees.Count <= 0 || reach <= trees.InnerMargin || trees.RadiusSpread < 1 {
		return nil
	}
	result := make([]Collider, 0, trees.Count)
	state := trees.Seed
	for len(result) < trees.Count {
		state = state*6364136223846793005 + 1442695040888963407
		a := float64(state%62832) / 10000
		state = state*6364136223846793005 + 1442695040888963407
		r := safeRadius + trees.InnerMargin + float64(state%uint64(reach))
		position := Vec{math.Cos(a) * r, math.Sin(a) * r}
		radius := trees.MinRadius + float64(state%uint64(trees.RadiusSpread))
		clear := true
		for _, other := range result {
			if position.Sub(other.Position).LengthSq() < math.Pow(radius+other.Radius+trees.Spacing, 2) {
				clear = false
				break
			}
		}
		if clear {
			result = append(result, Collider{ID: fmt.Sprintf("tree-%02d", len(result)), Kind: "tree", Position: position, Radius: radius})
		}
	}
	return result
}

func segmentCircle(a, b, center Vec, radius float64) bool {
	ab := b.Sub(a)
	denominator := ab.LengthSq()
	if denominator == 0 {
		return a.Sub(center).LengthSq() <= radius*radius
	}
	t := center.Sub(a).X*ab.X + center.Sub(a).Y*ab.Y
	t = math.Max(0, math.Min(1, t/denominator))
	closest := a.Add(ab.Mul(t))
	return closest.Sub(center).LengthSq() <= radius*radius
}

func hash(value string) uint64 {
	var h uint64 = 1469598103934665603
	for i := range value {
		h ^= uint64(value[i])
		h *= 1099511628211
	}
	return h
}

func sortedPlayerIDs(players map[string]*Player) []string {
	ids := make([]string, 0, len(players))
	for id := range players {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
