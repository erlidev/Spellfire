package game

import (
	"fmt"
	"time"

	"spellfire/server/internal/crafting"
	"spellfire/server/internal/loadout"
	"spellfire/server/internal/tuning"
)

// One authoritative path runs every deliberate action. Nothing here branches on
// class or weapon shape: an ability declares what it costs, how often it may be
// used, and what it delivers, and this file charges, gates, and delivers it.
// Mob attacks, deployables, and the Phase 2 loadout's spell slots enter through
// the same three functions.

// ability resolves what the player's use button performs: the ability bound to
// the selected action-bar slot. A Gunslinger's slot zero is its weapon and the
// rest are gadgets; a Mage's slots are spells, cast through the staff it holds.
// An empty slot has nothing to perform, which is not an error — it is a slot
// the player has not filled.
func (w *World) ability(p *Player) (tuning.Ability, bool) {
	slot, ok := w.selectedSlot(p)
	if !ok || slot.AbilityID == "" {
		return tuning.Ability{}, false
	}
	ability, ok := w.tuning.Tables.Abilities[slot.AbilityID]
	if !ok {
		return tuning.Ability{}, false
	}
	// A crafted weapon's components shape what it delivers. They reach the
	// weapon slot a gun fires from and the spell slots a staff casts through —
	// the staff is the delivery device, so its parts change the cast — but never
	// a gadget, which the weapon has no part in throwing.
	if slot.Item.ID == "" || slot.Kind == loadout.KindGadget {
		return ability, true
	}
	weapon, _, resolved := w.inventory(p).Equipped(w.tuning.Tables, p.Loadout.Weapon)
	if !resolved {
		return ability, true
	}
	_, ability = crafting.Apply(w.tuning.Tables, weapon, ability, slot.Item.Components)
	// A crystal's element bias is the one modifier that depends on what is being
	// cast rather than on what is holding it, so it is applied against the slot's
	// element after everything the staff does to every spell alike.
	ability = crafting.Bias(w.tuning.Tables, ability, slot.Element, slot.Item.Components)
	return ability, true
}

// useAbility charges and delivers one use. It is the only way an action reaches
// the world, so every gate the design owes a player — cadence, cooldown, cost,
// and the reload a spent magazine forces — is enforced in exactly one place.
func (w *World) useAbility(p *Player, now time.Time) bool {
	ability, ok := w.ability(p)
	if !ok {
		return false
	}
	// Interval is the global cadence gate shared by every ability; Cooldowns
	// holds each ability's own lockout on top of it, the second resource axis
	// alongside mana.
	if now.Before(p.NextFire) || now.Before(p.Cooldowns[ability.ID]) {
		return false
	}
	// A shield is held rather than fired, and an ability that requires a scope
	// may not be used from the hip at all: the committed vulnerability is the
	// counterplay it is sold against.
	if ability.Guard != nil || (ability.RequiresScope && !p.Scoped) {
		return false
	}
	// A cast that authors terrain is refused before it is charged, so a wall the
	// placement rules forbid costs nothing and reads as a rule rather than as a
	// wasted cooldown.
	aim := p.Aim.Normalized()
	if aim.LengthSq() == 0 {
		aim = Vec{1, 0}
	}
	if !w.placeable(ability, w.anchor(p.Position, aim, ability), aim) {
		return false
	}
	if !w.spend(p, ability, now) {
		return false
	}
	p.NextFire = now.Add(ability.Interval())
	if ability.CooldownMS > 0 {
		p.Cooldowns[ability.ID] = now.Add(ability.Cooldown())
	}
	w.deliver(p, ability, now)
	return true
}

// spend charges the ability's declared cost and reports whether it could be
// paid. An ammunition cost that cannot be met commits the weapon to a reload,
// which is the downtime the invariant pairs with the magazine.
func (w *World) spend(p *Player, ability tuning.Ability, now time.Time) bool {
	switch ability.Cost.Kind {
	case tuning.CostAmmo:
		if !p.ReloadEnds.IsZero() {
			return false
		}
		if float64(p.Ammo) < ability.Cost.Amount {
			if weapon, ok := w.weapon(p); ok && weapon.MagazineSize > 0 {
				p.ReloadEnds = now.Add(weapon.ReloadDuration())
			}
			return false
		}
		p.Ammo -= int(ability.Cost.Amount)
	case tuning.CostMana:
		if p.Mana < ability.Cost.Amount {
			return false
		}
		p.Mana -= ability.Cost.Amount
	case tuning.CostMaterial:
		// Special ammunition is finite and carried: there is no reload to fall
		// back on, so running out is running out until more is built.
		amount := int(ability.Cost.Amount)
		if p.Materials[ability.Cost.Material] < amount {
			return false
		}
		if p.Materials[ability.Cost.Material] -= amount; p.Materials[ability.Cost.Material] <= 0 {
			delete(p.Materials, ability.Cost.Material)
		}
	}
	return true
}

// deliver launches what the ability puts into the world. A windup shows its
// warning and delivers later through the same resolver; everything else lands
// now. Nothing here knows what kind of ability it is holding: a round, an area,
// a field, a wall, a blink, and a dispel are branches of one path.
func (w *World) deliver(p *Player, ability tuning.Ability, now time.Time) {
	direction := p.Aim.Normalized()
	if direction.LengthSq() == 0 {
		direction = Vec{1, 0}
	}
	anchor := w.anchor(p.Position, direction, ability)
	if ability.Telegraph != nil {
		w.startTelegraph(p.ID, w.playerElement(p), anchor, direction, ability, now)
		return
	}
	w.resolve(p.ID, anchor, direction, ability, now, w.playerElement(p))
	if ability.Projectile == nil {
		return
	}
	// One use may put several bodies into the world — a shotgun's cone — and
	// each leaves on its own direction, walked off aim by recoil and spread.
	for _, direction := range w.firingDirections(p, ability, now) {
		w.spawnRewoundProjectile(p, ability, direction, now)
	}
}

// anchor is where a cast lands. A placed spell reaches a fixed distance along
// the aim it committed to — the wire carries an aim direction and no cursor
// distance, so the range is the ability's rather than the player's — and
// everything else lands on the caster.
func (w *World) anchor(origin, direction Vec, ability tuning.Ability) Vec {
	if ability.Placement == nil {
		return origin
	}
	at := origin.Add(direction.Mul(ability.Placement.Range))
	// A cast placed past the rim lands on it rather than outside the world.
	if limit := w.tuning.WorldRadius; at.LengthSq() > limit*limit {
		at = at.Normalized().Mul(limit)
	}
	return at
}

// resolve puts everything an ability delivers besides its rounds into the world
// at one point. It is owner-agnostic and time-agnostic, so an immediate cast and
// a completed windup reach it by the same call and a mob will too.
func (w *World) resolve(ownerID string, at, direction Vec, ability tuning.Ability, now time.Time, element string) {
	owner := w.players[ownerID]
	if owner != nil && len(ability.SelfEffects) > 0 {
		w.applyEffectsScaled(owner, ability.SelfEffects, ownerID, direction, now, ability.EffectiveHealthScale())
	}
	if ability.Cleanse != nil {
		w.cleanse(owner, at, *ability.Cleanse, now)
	}
	if ability.Blink != nil && owner != nil {
		w.blink(owner, direction, ability.Blink.Distance, now)
	}
	if ability.Wall != nil {
		w.raiseWall(ownerID, at, direction, *ability.Wall, now)
	}
	// A blast or a field with nothing to carry it was placed rather than thrown,
	// so it resolves on the ground the telegraph drew. When a projectile carries
	// it, the impact is what resolves it instead.
	if ability.Projectile != nil {
		return
	}
	if ability.Blast != nil {
		w.explode(ownerID, at, ability.Blast.Radius, w.pelletDamage(ability), ability.Blast.Effects, now, ability.BlinkOnHit)
	}
	if ability.Deployable != nil {
		w.deploy(ownerID, *ability.Deployable, at, element, now)
	}
}

// playerElement is the element the selected slot delivers, which is what the
// renderer tints a body and its projectiles by. A gun or a gadget has none.
func (w *World) playerElement(p *Player) string {
	slot, ok := w.selectedSlot(p)
	if !ok {
		return ""
	}
	return slot.Element
}

func (w *World) spawnRewoundProjectile(p *Player, ability tuning.Ability, direction Vec, now time.Time) {
	shotAt := time.UnixMilli(int64(p.Input.ClientTimeMS))
	oldest := now.Add(-w.tuning.MaxRewind)
	if shotAt.Before(oldest) {
		shotAt = oldest
	}
	if shotAt.After(now) {
		shotAt = now
	}
	origin := w.positionAt(p.ID, shotAt)
	muzzle := origin.Add(direction.Mul(p.circleRadius() + ability.Projectile.Radius + 2))
	// A hitscan round never becomes an entity inside its instant reach: it is
	// resolved along the line, and only what it fails to reach travels on.
	if ability.Projectile.HitscanRange > 0 && p.Scoped {
		if w.hitscan(p, ability, muzzle, direction, shotAt) {
			return
		}
	}
	projectile := w.newProjectile(p.ID, w.playerElement(p), ability, direction)
	projectile.Position = muzzle
	// A round that starts past its instant reach has already covered it, so its
	// falloff continues from there rather than restarting at the muzzle.
	if ability.Projectile.HitscanRange > 0 && p.Scoped {
		projectile.Position = origin.Add(direction.Mul(ability.Projectile.HitscanRange))
		projectile.Travelled = ability.Projectile.HitscanRange
	}
	step := time.Second / time.Duration(w.tuning.TickRate)
	for at := shotAt; at.Before(now); at = at.Add(step) {
		duration := step
		if at.Add(duration).After(now) {
			duration = now.Sub(at)
		}
		if w.advanceProjectile(projectile, duration.Seconds(), at.Add(duration), true) {
			// A round resolved inside the rewind window never becomes an entity,
			// so anything it was carrying has to be placed here instead.
			w.resolveBlast(projectile, projectile.Position, now)
			w.deployFrom(projectile, projectile.Position, now)
			return
		}
	}
	w.projectiles[projectile.ID] = projectile
}

// deliverAt resolves a committed windup from the exact geometry it showed.
// Unlike an immediate shot, it is not rewound: the telegraph began at server
// time and has already paid the player-facing latency compensation by being
// visible for the full declared windup.
func (w *World) deliverAt(ownerID string, origin, direction Vec, ability tuning.Ability, now time.Time, element string) {
	w.resolve(ownerID, origin, direction, ability, now, element)
	if ability.Projectile == nil {
		return
	}
	ownerRadius := w.tuning.PlayerRadius
	if owner := w.players[ownerID]; owner != nil {
		ownerRadius = owner.circleRadius()
	}
	// A committed cast fans exactly as an immediate one does; what it does not
	// carry is gunplay, because a staff has no pattern to walk.
	for _, heading := range coneDirections(direction, ability.Projectile) {
		projectile := w.newProjectile(ownerID, element, ability, heading)
		projectile.Position = origin.Add(heading.Mul(ownerRadius + ability.Projectile.Radius + 2))
		w.projectiles[projectile.ID] = projectile
	}
}

// newProjectile builds one round from an ability, carrying everything the
// resolver needs so nothing has to look back at the shooter's kit.
func (w *World) newProjectile(ownerID, element string, ability tuning.Ability, direction Vec) *Projectile {
	projectile := &Projectile{
		OwnerID: ownerID, Element: element,
		Damage:    w.pelletDamage(ability),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Spec: *ability.Projectile, Blast: ability.Blast, Deploy: ability.Deployable,
		Chain: ability.Chain, BlinkOnHit: ability.BlinkOnHit,
	}
	if ability.Blast != nil {
		projectile.BlastEffects = ability.Blast.Effects
	}
	projectile.Entity = w.newProjectileEntity(fmt.Sprintf("p-%d", w.nextProjectile), Vec{}, direction.Mul(ability.Projectile.Speed), ability.Projectile.Radius)
	projectile.Kind = ability.Projectile.Kind
	w.nextProjectile++
	return projectile
}

// coneDirections lays a multi-body use out over its declared cone, from the
// centre outward, and answers with the single heading for everything else.
func coneDirections(aim Vec, spec *tuning.Projectile) []Vec {
	pellets := spec.PelletCount()
	if pellets <= 1 {
		return []Vec{aim}
	}
	directions := make([]Vec, 0, pellets)
	step := spec.PelletSpreadDegrees / float64(pellets-1)
	for index := 0; index < pellets; index++ {
		directions = append(directions, rotate(aim, -spec.PelletSpreadDegrees/2+float64(index)*step))
	}
	return directions
}
