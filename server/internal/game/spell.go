package game

import (
	"math"
	"time"

	"spellfire/server/internal/tuning"
)

// The Mage's delivery shapes that are neither a round nor an area: moving the
// caster, stripping what a fight has stacked on a body, arcing a landed hit
// onward, and steering a round while it flies.
//
// Nothing here reads a class or a spell ID. Each one is a field on the ability
// contract, so a Sentry that declares a blink blinks and a gadget that declared
// a chain would chain; what makes these the Mage's is that only spells author
// them today.

// blink moves a body along the direction its cast locked in, stopping short of
// anything solid rather than passing through it. It is deliberately a walk
// rather than a jump: a blink that could cross a wall would make every piece of
// cover in the world optional.
func (w *World) blink(p *Player, direction Vec, distance float64, now time.Time) {
	direction = direction.Normalized()
	if !p.Alive || p.Lingering() || p.Mass < 0 || direction.LengthSq() == 0 || distance <= 0 {
		return
	}
	step := math.Max(8, p.circleRadius())
	target, clear := p.Position, true
	for travelled := step; travelled < distance && clear; travelled += step {
		candidate := p.Position.Add(direction.Mul(travelled))
		if !w.standable(candidate) {
			clear = false
			break
		}
		target = candidate
	}
	// The full distance is checked on its own, because the walk above lands on
	// whole steps and the last one is rarely exactly the declared reach. It is
	// only reachable when nothing interrupted the walk: past a blocked step, an
	// open patch of ground beyond the obstacle is on the other side of it.
	if candidate := p.Position.Add(direction.Mul(distance)); clear && w.standable(candidate) {
		target = candidate
	}
	if target == p.Position {
		return
	}
	// An in-flight dash is cancelled by the arrival, and the jump is recorded in
	// the position history so a shot rewound across it resolves against where the
	// body actually was.
	p.Position, p.Velocity, p.DashTicksLeft = target, Vec{}, 0
	w.recordHistory(p, now)
}

// cleanse is the dispel. It strips every status running on the caster and on
// hostile bodies inside its radius, and returns mana for each one removed.
//
// It is deliberately indiscriminate: buffs, shields, and debuffs all go, on
// both sides. That is what makes it a read rather than a free button — casting
// it while your own ward is up throws the ward away with the enemy's.
func (w *World) cleanse(owner *Player, at Vec, rule tuning.Cleanse, now time.Time) {
	if owner == nil {
		return
	}
	stripped := w.stripEffects(owner)
	for _, target := range w.playersWithin(at, rule.Radius) {
		if target.ID == owner.ID {
			continue
		}
		// Stripping a shield off a body is an offensive act, so it is bound by
		// the same PvP protection damage is.
		if !w.hostileReach(owner, target.Position) {
			continue
		}
		stripped += w.stripEffects(target)
	}
	owner.Mana = math.Min(w.tuning.MaxMana, owner.Mana+rule.ManaPerEffect*float64(stripped))
}

// chainFrom arcs a landed hit onward through nearby bodies. Each arc is
// measured from the body it last struck rather than from the caster, so the
// spell's reach is the shape of the fight rather than a second radius, and no
// body is struck twice by one cast.
func (w *World) chainFrom(projectile *Projectile, struck *Player, at time.Time) {
	if projectile.Chain == nil {
		return
	}
	owner := w.players[projectile.OwnerID]
	reached := map[string]bool{struck.ID: true, projectile.OwnerID: true}
	from := struck.Position
	for jump := 0; jump < projectile.Chain.Jumps; jump++ {
		next := w.nearestPlayer(from, projectile.Chain.Range, reached, owner)
		if next == nil {
			return
		}
		reached[next.ID] = true
		w.damage(next, projectile.hitDamage(), projectile.OwnerID, at)
		w.applyEffects(next, projectile.Effects, projectile.OwnerID, next.Position.Sub(from), at)
		from = next.Position
	}
}

// steer turns a homing round toward what it is following, by no more than its
// declared rate. The cap is the whole counterplay: a round that turned freely
// would be a lock-on, and travel time would stop being a dodge vector.
func (w *World) steer(projectile *Projectile, dt float64) {
	homing := projectile.Spec.Homing
	if homing == nil {
		return
	}
	speed := math.Sqrt(projectile.Velocity.LengthSq())
	if speed <= 0 {
		return
	}
	target := w.nearestPlayer(projectile.Position, homing.AcquireRange, map[string]bool{projectile.OwnerID: true}, w.players[projectile.OwnerID])
	if target == nil {
		return
	}
	heading := projectile.Velocity.Mul(1 / speed)
	desired := target.Position.Sub(projectile.Position).Normalized()
	if desired.LengthSq() == 0 {
		return
	}
	cross := heading.X*desired.Y - heading.Y*desired.X
	dot := heading.X*desired.X + heading.Y*desired.Y
	turn := math.Atan2(cross, dot) * 180 / math.Pi
	limit := homing.TurnDegreesPerSecond * dt
	turn = math.Max(-limit, math.Min(limit, turn))
	projectile.Velocity = rotate(heading, turn).Mul(speed)
}

// nearestPlayer is the closest visible living body inside a radius that an
// owner may actually act on: never itself, never one already reached, never one
// hidden by terrain or smoke, and never one PvP protection puts out of reach.
func (w *World) nearestPlayer(from Vec, radius float64, exclude map[string]bool, owner *Player) *Player {
	// Blind fire remains possible because ordinary aim is just a direction, but
	// an automatic homing or chain choice cannot use information its owner is
	// not receiving.
	if owner != nil && w.blinded(owner) {
		return nil
	}
	var found *Player
	nearest := radius * radius
	// Collect the sight-blockers once, not per candidate: acquisition can scan
	// every living body in range on a single cast.
	occ := w.collectOccluders(from, radius)
	for _, candidate := range w.playersWithin(from, radius) {
		if exclude[candidate.ID] {
			continue
		}
		if !w.hostileReach(owner, candidate.Position) {
			continue
		}
		if !occ.visible(from, candidate.Position, candidate.circleRadius()) {
			continue
		}
		if distance := candidate.Position.Sub(from).LengthSq(); distance <= nearest {
			found, nearest = candidate, distance
		}
	}
	return found
}
