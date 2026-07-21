package game

import (
	"fmt"
	"time"

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
	return ability, ok
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
	w.spawnRewoundProjectile(p, ability, now)
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

func (w *World) spawnRewoundProjectile(p *Player, ability tuning.Ability, now time.Time) {
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
		ID: fmt.Sprintf("p-%d", w.nextProjectile), OwnerID: p.ID, Kind: ability.Projectile.Kind,
		Element: w.playerElement(p),
		Radius:  ability.Projectile.Radius, Damage: w.tuning.Tables.BandDamage(ability.DamageBand),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Velocity: p.Aim.Mul(ability.Projectile.Speed),
	}
	w.nextProjectile++
	projectile.Position = origin.Add(p.Aim.Mul(w.tuning.PlayerRadius + ability.Projectile.Radius + 2))
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

// deliverAt resolves a committed windup from the exact geometry it showed.
// Unlike an immediate shot, it is not rewound: the telegraph began at server
// time and has already paid the player-facing latency compensation by being
// visible for the full declared windup.
func (w *World) deliverAt(ownerID string, origin, direction Vec, ability tuning.Ability, now time.Time, element string) {
	if ability.Projectile == nil {
		return
	}
	projectile := &Projectile{
		ID: fmt.Sprintf("p-%d", w.nextProjectile), OwnerID: ownerID, Kind: ability.Projectile.Kind, Element: element,
		Radius: ability.Projectile.Radius, Damage: w.tuning.Tables.BandDamage(ability.DamageBand),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Velocity: direction.Mul(ability.Projectile.Speed),
	}
	w.nextProjectile++
	projectile.Position = origin.Add(direction.Mul(w.tuning.PlayerRadius + ability.Projectile.Radius + 2))
	w.projectiles[projectile.ID] = projectile
}
