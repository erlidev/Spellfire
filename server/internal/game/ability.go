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

// deliver launches what the ability puts into the world. Every damaging ability
// validated at load carries a travelling projectile, so this is the one shape
// today; Phase 2.5's areas and walls extend it rather than bypass it.
func (w *World) deliver(p *Player, ability tuning.Ability, now time.Time) {
	if ability.Telegraph != nil {
		w.startTelegraph(p.ID, w.playerElement(p), p.Position, p.Aim, ability, now)
		return
	}
	if ability.Projectile == nil {
		return
	}
	// One use may put several bodies into the world — a shotgun's cone — and
	// each leaves on its own direction, walked off aim by recoil and spread.
	for _, direction := range w.firingDirections(p, ability, now) {
		w.spawnRewoundProjectile(p, ability, direction, now)
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
	projectile := &Projectile{
		OwnerID:   p.ID,
		Element:   w.playerElement(p),
		Damage:    w.pelletDamage(ability),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Spec: *ability.Projectile, Blast: ability.Blast, Deploy: ability.Deployable,
	}
	if ability.Blast != nil {
		projectile.BlastEffects = ability.Blast.Effects
	}
	projectile.Entity = w.newProjectileEntity(fmt.Sprintf("p-%d", w.nextProjectile), Vec{}, direction.Mul(ability.Projectile.Speed), ability.Projectile.Radius)
	projectile.Kind = ability.Projectile.Kind
	w.nextProjectile++
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
	if ability.Projectile == nil {
		return
	}
	projectile := &Projectile{
		OwnerID: ownerID, Element: element,
		Damage:    w.pelletDamage(ability),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Spec: *ability.Projectile, Blast: ability.Blast, Deploy: ability.Deployable,
	}
	if ability.Blast != nil {
		projectile.BlastEffects = ability.Blast.Effects
	}
	projectile.Entity = w.newProjectileEntity(fmt.Sprintf("p-%d", w.nextProjectile), Vec{}, direction.Mul(ability.Projectile.Speed), ability.Projectile.Radius)
	projectile.Kind = ability.Projectile.Kind
	w.nextProjectile++
	ownerRadius := w.tuning.PlayerRadius
	if owner := w.players[ownerID]; owner != nil {
		ownerRadius = owner.circleRadius()
	}
	projectile.Position = origin.Add(direction.Mul(ownerRadius + ability.Projectile.Radius + 2))
	w.projectiles[projectile.ID] = projectile
}
