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
// The occlusion rule here is deliberately narrow. General line of sight — cover,
// walls, and the Mage/Gunslinger matchup — is Phase 2.6's substrate and is not
// approximated here; what this file owns is the one case a smoke canister is
// bought for: a cloud between two bodies hides each from the other, and a body
// inside one is blind past arm's reach.
type Deployable struct {
	Entity
	OwnerID string
	// Field is the authored row this was deployed from, carried rather than
	// looked up so a balance edit retunes a cloud already standing in the world.
	Field     tuning.Deployable
	ExpiresAt time.Time
}

// deployFrom materialises the field a spent projectile was carrying, at the
// point it stopped. A round with no field does nothing, which is every ordinary
// shot.
func (w *World) deployFrom(projectile *Projectile, at Vec, now time.Time) {
	if projectile.Deploy == nil || projectile.Deployed {
		return
	}
	projectile.Deployed = true
	w.deploy(projectile.OwnerID, *projectile.Deploy, at, now)
}

// deploy places one field. It is owner-agnostic: a player's gadget places one
// today, and a mob or a boss places one through the same entry point.
func (w *World) deploy(ownerID string, field tuning.Deployable, at Vec, now time.Time) *Deployable {
	definition, ok := w.tuning.Tables.Entities[field.Kind]
	if !ok {
		return nil
	}
	deployable := &Deployable{
		Entity:  newEntity(fmt.Sprintf("d-%d", w.nextDeployable), field.Kind, at, definition, EntityOverrides{}),
		OwnerID: ownerID, Field: field, ExpiresAt: now.Add(field.Duration()),
	}
	w.nextDeployable++
	w.deployables[deployable.ID] = deployable
	return deployable
}

// stepDeployables retires the fields whose window has closed. Expiry starts the
// shared graceful removal rather than deleting outright, so a cloud fades from
// every client instead of blinking out of the world.
func (w *World) stepDeployables(now time.Time) {
	for _, id := range sortedDeployableIDs(w.deployables) {
		deployable := w.deployables[id]
		if !deployable.Deleting && !now.Before(deployable.ExpiresAt) {
			deployable.Delete(now)
		}
	}
}

// occluded reports whether a cloud stands between two points, hiding whatever is
// at one from a viewer at the other. Two bodies closer together than the field's
// reveal radius always see each other, so standing in your own smoke does not
// blind you to the body you are touching.
//
// A fading cloud stops occluding the moment it expires: the fade is a render
// courtesy, and vision has to match what the simulation says is there.
func (w *World) occluded(from, to Vec) bool {
	// Iterated unordered on purpose: this runs once per candidate entity per
	// viewer per send, and the answer is a boolean that no ordering can change.
	for _, cloud := range w.deployables {
		if cloud.Deleting || cloud.Field.Radius <= 0 {
			continue
		}
		if to.Sub(from).LengthSq() <= cloud.Field.RevealRadius*cloud.Field.RevealRadius {
			continue
		}
		if segmentCircle(from, to, cloud.Position, cloud.Field.Radius) {
			return true
		}
	}
	return false
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
