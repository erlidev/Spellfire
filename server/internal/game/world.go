package game

import (
	"fmt"
	"math"
	"sort"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
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

type Tuning struct {
	TickRate, SendRate                  int
	AOIRadius, WorldRadius              float64
	SafeRadius, PvPRadius, PlayerRadius float64
	PlayerSpeed, DashDistance           float64
	DashDuration, DashCooldown          time.Duration
	FireInterval                        time.Duration
	ReloadDuration, MaxRewind           time.Duration
	ProjectileSpeed, ProjectileLife     float64
	ProjectileDamage, MaxHealth         float64
	MaxMana, ManaRegen                  float64
}

func DefaultTuning() Tuning {
	return Tuning{
		TickRate: 60, SendRate: 20, AOIRadius: 1200, WorldRadius: 3000, SafeRadius: 430, PvPRadius: 1000,
		PlayerRadius: 20, PlayerSpeed: 260, DashDistance: 105,
		DashDuration: 133 * time.Millisecond, DashCooldown: 2200 * time.Millisecond,
		FireInterval: 300 * time.Millisecond, ReloadDuration: 1400 * time.Millisecond,
		MaxRewind: 200 * time.Millisecond, ProjectileSpeed: 760, ProjectileLife: 1.5,
		ProjectileDamage: 10, MaxHealth: 100, MaxMana: 100, ManaRegen: 13,
	}
}

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
}

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
	if t.TickRate <= 0 {
		t = DefaultTuning()
	}
	return &World{
		tuning: t, players: make(map[string]*Player), projectiles: make(map[string]*Projectile),
		colliders: generateTrees(t.WorldRadius, t.SafeRadius), history: make(map[string][]historySample),
	}
}

func (w *World) AddPlayer(character model.Character, now time.Time) *Player {
	if existing := w.players[character.ID]; existing != nil {
		return existing
	}
	angle := float64(hash(character.ID)%628) / 100
	p := &Player{ID: character.ID, Name: character.Name, Class: character.Class, Position: Vec{math.Cos(angle) * 170, math.Sin(angle) * 170}, Aim: Vec{1, 0}, Health: w.tuning.MaxHealth, Mana: w.tuning.MaxMana, Alive: true, Ammo: 10}
	w.players[p.ID] = p
	w.recordHistory(p, now)
	return p
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
	p.Health, p.Mana, p.Alive, p.Ammo = w.tuning.MaxHealth, w.tuning.MaxMana, true, 10
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
	if !p.Alive {
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
	if p.Class == model.Gunslinger && !p.ReloadEnds.IsZero() && !now.Before(p.ReloadEnds) {
		p.Ammo = 10
		p.ReloadEnds = time.Time{}
	}
	if p.Class == model.Gunslinger && p.Input.Buttons&ButtonReload != 0 && p.ReloadEnds.IsZero() && p.Ammo < 10 {
		p.ReloadEnds = now.Add(w.tuning.ReloadDuration)
	}
	if p.Input.Buttons&ButtonFire != 0 {
		w.tryFire(p, now)
	}
	p.PreviousButtons = p.Input.Buttons
	p.Acknowledged = p.Input.Sequence
}

func (w *World) tryFire(p *Player, now time.Time) {
	if now.Before(p.NextFire) {
		return
	}
	if p.Class == model.Gunslinger {
		if !p.ReloadEnds.IsZero() {
			return
		}
		if p.Ammo <= 0 {
			p.ReloadEnds = now.Add(w.tuning.ReloadDuration)
			return
		}
		p.Ammo--
	} else {
		if p.Mana < 12 {
			return
		}
		p.Mana -= 12
	}
	p.NextFire = now.Add(w.tuning.FireInterval)
	w.spawnRewoundProjectile(p, now)
}

func (w *World) spawnRewoundProjectile(p *Player, now time.Time) {
	shotAt := time.UnixMilli(int64(p.Input.ClientTimeMS))
	oldest := now.Add(-w.tuning.MaxRewind)
	if shotAt.Before(oldest) {
		shotAt = oldest
	}
	if shotAt.After(now) {
		shotAt = now
	}
	origin := w.positionAt(p.ID, shotAt)
	radius, speed, kind := 5.0, w.tuning.ProjectileSpeed, "bullet"
	if p.Class == model.Mage {
		radius, speed, kind = 9, speed*0.78, "fireball"
	}
	projectile := &Projectile{ID: fmt.Sprintf("p-%d", w.nextProjectile), OwnerID: p.ID, Kind: kind, Radius: radius, Damage: w.tuning.ProjectileDamage, Remaining: w.tuning.ProjectileLife, Velocity: p.Aim.Mul(speed)}
	w.nextProjectile++
	projectile.Position = origin.Add(p.Aim.Mul(w.tuning.PlayerRadius + radius + 2))
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

func generateTrees(worldRadius, safeRadius float64) []Collider {
	result := make([]Collider, 0, 72)
	state := uint64(0x5eed5eed)
	for len(result) < 72 {
		state = state*6364136223846793005 + 1442695040888963407
		a := float64(state%62832) / 10000
		state = state*6364136223846793005 + 1442695040888963407
		r := safeRadius + 130 + float64(state%uint64(worldRadius-safeRadius-260))
		position := Vec{math.Cos(a) * r, math.Sin(a) * r}
		radius := 27 + float64(state%17)
		clear := true
		for _, other := range result {
			if position.Sub(other.Position).LengthSq() < math.Pow(radius+other.Radius+55, 2) {
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
