package game

import (
	"fmt"
	"math"
	"sort"
	"time"

	"spellfire/server/internal/tuning"
)

// Deployables are the things an ability leaves standing in the world instead of
// resolving and vanishing. Smoke is the first: a circular field with a lifetime
// that changes what can be seen without changing where a body may walk.
//
// Containment supplies smoke's close-range exception; visibility.go separately
// treats an authored smoke field as a circular LOS occluder without making it
// physical collision.
type Deployable struct {
	Entity
	OwnerID string
	// Element is what cast it, and is empty for a thrown gadget. It travels to
	// the client, which tints the field by it — a burning patch and a blizzard
	// are the same shape and must never read as the same thing.
	Element string
	// Field is the authored row this was deployed from, carried rather than
	// looked up so a balance edit retunes a cloud already standing in the world.
	Field     tuning.Deployable
	ExpiresAt time.Time
	// NextTickAt paces the field's pulse, and Spent marks a trap that has
	// already caught someone.
	NextTickAt time.Time
	Spent      bool
}

// deployFrom materialises the field a spent projectile was carrying, at the
// point it stopped. A round with no field does nothing, which is every ordinary
// shot.
func (w *World) deployFrom(projectile *Projectile, at Vec, now time.Time) {
	if projectile.Deploy == nil || projectile.Deployed {
		return
	}
	projectile.Deployed = true
	w.deploy(projectile.OwnerID, *projectile.Deploy, at, projectile.Element, now)
}

// deploy places one field. It is owner-agnostic: a player's gadget places one
// today, and a mob or a boss places one through the same entry point.
func (w *World) deploy(ownerID string, field tuning.Deployable, at Vec, element string, now time.Time) *Deployable {
	definition, ok := w.tuning.Tables.Entities[field.Kind]
	if !ok {
		return nil
	}
	deployable := &Deployable{
		Entity:  newEntity(fmt.Sprintf("d-%d", w.nextDeployable), field.Kind, at, definition, EntityOverrides{}),
		OwnerID: ownerID, Element: element, Field: field, ExpiresAt: now.Add(field.Duration()),
	}
	if field.Pulses() {
		// A field is not felt the instant it lands: the first pulse is a cadence
		// away, which is the moment a body caught by the placement has to leave.
		deployable.NextTickAt = now.Add(field.Tick())
	}
	w.nextDeployable++
	w.deployables[deployable.ID] = deployable
	return deployable
}

// stepDeployables pulses standing fields and retires the ones whose window has
// closed. Expiry starts the shared graceful removal rather than deleting
// outright, so a field fades from every client instead of blinking out.
func (w *World) stepDeployables(now time.Time) {
	for _, id := range sortedDeployableIDs(w.deployables) {
		deployable := w.deployables[id]
		if deployable.Deleting {
			continue
		}
		if !now.Before(deployable.ExpiresAt) {
			// The closing pulse is what lets a stacking slow end in a stun. A trap
			// that timed out without catching anyone never resolves one.
			if !deployable.Spent && len(deployable.Field.FinalEffects) > 0 {
				w.pulse(deployable, deployable.Field.FinalEffects, now)
			}
			deployable.Delete(now)
			continue
		}
		w.stepField(deployable, now)
	}
}

// stepField runs one standing field's pulse. A trap waits for a body and is
// spent by the first one that reaches it; every other field pulses on its own
// cadence for as long as it stands.
func (w *World) stepField(deployable *Deployable, now time.Time) {
	field := deployable.Field
	if !field.Pulses() || deployable.Spent || now.Before(deployable.NextTickAt) {
		return
	}
	// Whole cadences are caught up rather than one per frame, so a field deals
	// the same total however the tick rate divides its pulse.
	for !now.Before(deployable.NextTickAt) {
		deployable.NextTickAt = deployable.NextTickAt.Add(field.Tick())
	}
	if w.pulse(deployable, field.Effects, now) && field.Trigger {
		deployable.Spent = true
		deployable.ExpiresAt = now
	}
}

// pulse applies one of a field's beats to everyone standing in it, and reports
// whether it reached anybody. Damage is priced against the shared band like
// every other source, and PvP protection covers a field exactly as it covers a
// bullet: a patch burning inside safety costs nothing.
func (w *World) pulse(deployable *Deployable, effects []string, now time.Time) bool {
	field := deployable.Field
	owner := w.players[deployable.OwnerID]
	damage := 0.0
	if field.DamageBand != "" {
		damage = w.tuning.Tables.BandDamage(field.DamageBand) * field.DamageFraction * field.DamageScale()
	}
	reached := false
	for _, id := range sortedPlayerIDs(w.players) {
		target := w.players[id]
		if id == deployable.OwnerID || !target.Alive {
			continue
		}
		if target.Position.Sub(deployable.Position).LengthSq() > field.Radius*field.Radius {
			continue
		}
		if !w.hazardReach(owner, deployable.OwnerID, target.Position) {
			continue
		}
		reached = true
		w.damage(target, damage, deployable.OwnerID, now)
		w.applyEffects(target, effects, deployable.OwnerID, target.Position.Sub(deployable.Position), now)
	}
	return reached
}

// blinded reports whether a body can currently see anything at all. A flashbang
// takes vision whole rather than dimming it: the counterplay is the travel time
// of the canister and the ground it covers, not a partial view.
func (w *World) blinded(p *Player) bool { return w.hasEffectKind(p, "blind") }

// Deployables exposes the standing fields for tests and tooling.
func (w *World) Deployables() []Deployable {
	fields := make([]Deployable, 0, len(w.deployables))
	for _, id := range sortedDeployableIDs(w.deployables) {
		fields = append(fields, *w.deployables[id])
	}
	return fields
}

// remaining is the fraction of the field's window still to run. The snapshot
// carries it so a client can fade a cloud in and out without keeping a second
// clock of its own.
func (d *Deployable) remaining(now time.Time) float64 {
	total := d.Field.Duration()
	if total <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, float64(d.ExpiresAt.Sub(now))/float64(total)))
}

func sortedDeployableIDs(deployables map[string]*Deployable) []string {
	ids := make([]string, 0, len(deployables))
	for id := range deployables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
