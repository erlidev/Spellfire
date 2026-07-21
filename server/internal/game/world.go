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
	ButtonUp       uint32 = 1
	ButtonDown     uint32 = 2
	ButtonLeft     uint32 = 4
	ButtonRight    uint32 = 8
	ButtonFire     uint32 = 16
	ButtonDash     uint32 = 32
	ButtonReload   uint32 = 64
	ButtonInteract uint32 = 128
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
	ID, AccountID, Name string
	Class               model.Class
	WeaponID            string
	// SquadID is empty until Phase 5 forms squads. It lives on the actor now so
	// snapshots and future attribution code do not need a protocol migration.
	SquadID                         string
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
	// Cooldowns is each ability's own lockout, keyed by ability ID, alongside
	// the global cadence gate NextFire holds.
	Cooldowns map[string]time.Time
	// Effects are the statuses running on the body, in application order.
	Effects []ActiveEffect
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
	ID, OwnerID, Kind, Element string
	Position, Velocity         Vec
	Radius, Damage, Remaining  float64
	// Effects are the statuses a hit applies, carried from the ability that
	// launched it so the resolver needs no lookup back to the shooter's kit.
	Effects []string
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
	tuning      Tuning
	tick        uint64
	players     map[string]*Player
	projectiles map[string]*Projectile
	telegraphs  map[string]*Telegraph
	colliders   []Collider
	history     map[string][]historySample
	// occupants maps an account to the one character it has a body for, so the
	// one-body-per-account rule is a lookup rather than a scan of the world.
	occupants      map[string]string
	nextProjectile uint64
	nextTelegraph  uint64
	combat         *combatLog
}

func NewWorld(t Tuning) *World {
	if t.Tables == nil || t.TickRate <= 0 {
		t = DefaultTuning()
	}
	return &World{
		tuning: t, players: make(map[string]*Player), projectiles: make(map[string]*Projectile), telegraphs: make(map[string]*Telegraph),
		colliders: generateTrees(t.Tables.World), history: make(map[string][]historySample),
		occupants: make(map[string]string), combat: newCombatLog(combatEventCapacity),
	}
}

// Occupant reports which character of an account has a body in the world,
// connected or lingering, and is empty when none does. Characters with no
// account — only the simulation tests build those — are never indexed, so they
// never occupy each other's slot.
func (w *World) Occupant(accountID string) string {
	if accountID == "" {
		return ""
	}
	return w.occupants[accountID]
}

func (w *World) AddPlayer(character model.Character, now time.Time) *Player {
	if existing := w.players[character.ID]; existing != nil && existing.Alive {
		// Reconnecting inside the logout window resumes the body that stayed
		// behind, wherever the fight has since moved it. The input sequence
		// belongs to the connection, not the body: a new client counts from
		// zero again, so the old high-water mark must go or every input it
		// sends is rejected as stale.
		existing.LingerUntil = time.Time{}
		existing.Input, existing.Acknowledged, existing.PreviousButtons = protocol.Input{}, 0, 0
		return existing
	} else if existing != nil {
		// A body killed inside its logout window is a corpse, not a session to
		// resume: dropping the connection must not park the death at the spot
		// it happened. The corpse goes and the character re-enters at the hub,
		// the same place the saved-as-unplaced record would have sent it had
		// the window closed first.
		w.RemovePlayer(character.ID)
		character.State.Placed = false
	}
	p := &Player{
		ID: character.ID, AccountID: character.AccountID, Name: character.Name, Class: character.Class,
		Position: w.entryPosition(character, now), Aim: Vec{1, 0},
		Health: w.tuning.MaxHealth, Mana: w.tuning.MaxMana, Alive: true,
		Materials: w.carriedMaterials(character.State.Materials),
		Outposts:  append([]string(nil), character.State.Outposts...),
		Cooldowns: make(map[string]time.Time),
	}
	// Until the Phase 2 loadout lands, the equipped weapon is the class starter
	// row. It is a table reference, never a copy of its stats.
	if weapon, ok := w.tuning.Tables.StarterWeapon(string(character.Class)); ok {
		p.WeaponID, p.Ammo = weapon.ID, weapon.MagazineSize
	}
	w.players[p.ID] = p
	if p.AccountID != "" {
		w.occupants[p.AccountID] = p.ID
	}
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

func (w *World) RemovePlayer(id string) {
	if p := w.players[id]; p != nil && w.occupants[p.AccountID] == id {
		delete(w.occupants, p.AccountID)
	}
	delete(w.players, id)
	delete(w.history, id)
	for telegraphID, telegraph := range w.telegraphs {
		if telegraph.OwnerID == id {
			delete(w.telegraphs, telegraphID)
		}
	}
	w.combat.resetTarget(id)
}

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
	// A fresh body carries neither the statuses that killed it nor the
	// cooldowns it died holding.
	p.Effects, p.Cooldowns = nil, make(map[string]time.Time)
	p.NextFire, p.ReloadEnds, p.DashReady = now, now, now
	w.combat.resetTarget(id)
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
	w.stepTelegraphs(now)
	w.stepProjectiles(now, dt)
	for _, id := range ids {
		if p := w.players[id]; p != nil {
			w.recordHistory(p, now)
		}
	}
}

func (w *World) stepPlayer(p *Player, now time.Time, dt float64) {
	// Statuses run before the body acts, and on a lingering body too: a burn
	// left on someone who disconnects keeps burning.
	w.stepEffects(p, now)
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
	// A stun suppresses everything the body does; a root only takes its
	// movement, leaving it able to aim, reload, and act.
	stunned, rooted := w.stunned(p), w.rooted(p)
	if p.Input.Buttons&ButtonDash != 0 && p.PreviousButtons&ButtonDash == 0 && !now.Before(p.DashReady) && !stunned && !rooted {
		p.DashDirection = move
		if p.DashDirection.LengthSq() == 0 {
			p.DashDirection = p.Aim
		}
		p.DashTicksLeft = w.tuning.dashTicks()
		p.DashReady = now.Add(w.tuning.DashCooldown)
	}
	switch knocked, knockedBack := w.knockback(p); {
	case knockedBack:
		// A knockback overrides input and cancels an in-flight dash: control
		// beats mobility for as long as it runs.
		p.Velocity, p.DashTicksLeft = knocked, 0
	case stunned || rooted:
		p.Velocity, p.DashTicksLeft = Vec{}, 0
	case p.DashTicksLeft > 0:
		p.Velocity = p.DashDirection.Mul(w.tuning.dashSpeed())
		p.DashTicksLeft--
	default:
		p.Velocity = move.Mul(w.tuning.PlayerSpeed * w.movementScale(p))
	}
	p.Position = w.moveCircle(p.Position, p.Velocity.Mul(dt), w.tuning.PlayerRadius)
	if p.Class == model.Mage {
		p.Mana = math.Min(w.tuning.MaxMana, p.Mana+w.tuning.ManaRegen*dt)
	}
	// Magazine size and reload time are weapon properties; a weapon without a
	// magazine (a staff) never enters the reload path.
	if weapon, ok := w.weapon(p); ok && weapon.MagazineSize > 0 && !stunned {
		if !p.ReloadEnds.IsZero() && !now.Before(p.ReloadEnds) {
			p.Ammo, p.ReloadEnds = weapon.MagazineSize, time.Time{}
		}
		if p.Input.Buttons&ButtonReload != 0 && p.ReloadEnds.IsZero() && p.Ammo < weapon.MagazineSize {
			p.ReloadEnds = now.Add(weapon.ReloadDuration())
		}
	}
	if p.Input.Buttons&ButtonFire != 0 && !stunned {
		w.useAbility(p, now)
	}
	p.PreviousButtons = p.Input.Buttons
	p.Acknowledged = p.Input.Sequence
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
		// PvP protection covers the hit whole: no damage, and no status either.
		// A slow or a knockback landed from inside safety would be exactly the
		// offensive use of a safe zone the invariant forbids.
		if owner != nil && owner.Position.LengthSq() > w.tuning.PvPRadius*w.tuning.PvPRadius && position.LengthSq() > w.tuning.PvPRadius*w.tuning.PvPRadius {
			w.damage(target, projectile.Damage, projectile.OwnerID, at)
			w.applyEffects(target, projectile.Effects, projectile.OwnerID, to.Sub(from), at)
		}
		return true
	}
	projectile.Position = to
	return to.LengthSq() > w.tuning.WorldRadius*w.tuning.WorldRadius
}

// damage is the one path health is lost through: shields absorb first, the
// contribution ledger records only effective health damage, and a body that
// reaches zero dies. The lethal event freezes the full per-life ledger for
// later drop ownership and ranking consumers.
func (w *World) damage(target *Player, amount float64, sourceID string, at time.Time) {
	if !target.Alive || amount <= 0 {
		return
	}
	amount = w.absorb(target, amount)
	if amount <= 0 {
		return
	}
	applied := math.Min(target.Health, amount)
	target.Health = math.Max(0, target.Health-applied)
	w.combat.recordDamage(at, sourceID, target.ID, applied, target.Health == 0)
	if target.Health == 0 {
		target.Alive, target.Velocity, target.Effects, target.DashTicksLeft = false, Vec{}, nil, 0
		w.cancelTelegraphs(target.ID, at)
	}
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
