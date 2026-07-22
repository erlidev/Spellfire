package game

import (
	"math"
	"time"

	"spellfire/server/internal/tuning"
)

// ActiveEffect is one status running on a body. It stores the effect's ID
// rather than a copy of its values, so a balance edit retunes a status already
// running on a player, exactly as it retunes an owned item.
type ActiveEffect struct {
	EffectID string
	// SourceID is who applied it. The contribution ledger reads this to credit
	// damage a burn deals after its caster has stopped shooting.
	SourceID  string
	ExpiresAt time.Time
	// NextTickAt paces a burn's damage. It is zero for every other kind.
	NextTickAt time.Time
	// Direction is the unit vector a knockback carries the body along, fixed at
	// application so it cannot be steered.
	Direction Vec
	// Absorb is a shield's remaining pool, drained by incoming damage.
	Absorb float64
}

// applyEffects starts the statuses an ability's hit leaves behind. Direction is
// the travel direction of the hit, which is what a knockback pushes along.
func (w *World) applyEffects(target *Player, ids []string, sourceID string, direction Vec, now time.Time) {
	if !target.Alive {
		return
	}
	for _, id := range ids {
		effect, ok := w.tuning.Tables.Effects[id]
		if !ok {
			continue
		}
		active := ActiveEffect{
			EffectID: id, SourceID: sourceID, ExpiresAt: now.Add(effect.Duration()),
			Direction: direction.Normalized(),
		}
		if effect.Kind == "burn" {
			active.NextTickAt = now.Add(effect.Tick())
		}
		if effect.Kind == "shield" {
			active.Absorb = effect.AbsorbHits * w.tuning.Tables.BandDamage(effect.DamageBand)
		}
		if effect.Stacking == tuning.StackRefresh {
			if index := indexOfEffect(target.Effects, id); index >= 0 {
				target.Effects[index] = active
				continue
			}
		}
		target.Effects = append(target.Effects, active)
	}
}

// stepEffects advances every status on a body: burns deal their tick, and
// anything whose window has closed is dropped. It runs before the body acts, so
// a status applied on one tick governs the next.
func (w *World) stepEffects(p *Player, now time.Time) {
	if len(p.Effects) == 0 {
		return
	}
	live := p.Effects[:0]
	for _, active := range p.Effects {
		effect, ok := w.tuning.Tables.Effects[active.EffectID]
		if !ok || !now.Before(active.ExpiresAt) {
			continue
		}
		if effect.Kind == "burn" {
			// Catch up whole ticks rather than one per frame, so a burn deals
			// the same total however the tick rate divides its cadence.
			for effect.TickMS > 0 && !now.Before(active.NextTickAt) && p.Alive {
				w.damage(p, effect.DamageFraction*w.tuning.Tables.BandDamage(effect.DamageBand), active.SourceID, active.NextTickAt)
				active.NextTickAt = active.NextTickAt.Add(effect.Tick())
			}
		}
		if effect.Kind == "shield" && active.Absorb <= 0 {
			continue
		}
		live = append(live, active)
	}
	p.Effects = live
	// A body killed by its own burn carries nothing forward.
	if !p.Alive {
		p.Effects = nil
	}
}

// absorb drains shields in the order they were applied and reports the damage
// that reaches health.
func (w *World) absorb(p *Player, amount float64) float64 {
	for index := range p.Effects {
		if amount <= 0 {
			break
		}
		active := &p.Effects[index]
		if active.Absorb <= 0 || w.tuning.Tables.Effects[active.EffectID].Kind != "shield" {
			continue
		}
		taken := math.Min(active.Absorb, amount)
		active.Absorb, amount = active.Absorb-taken, amount-taken
	}
	return amount
}

// armorScale is the fraction of incoming damage that still reaches the body.
// Armor is mitigation rather than a pool: like slows it takes the strongest
// rather than compounding, so two wards can never stack into immunity.
func (w *World) armorScale(p *Player) float64 {
	scale := 1.0
	for _, active := range p.Effects {
		if effect := w.tuning.Tables.Effects[active.EffectID]; effect.Kind == "armor" {
			scale = math.Min(scale, effect.DamageMultiplier)
		}
	}
	return scale
}

// stripEffects clears every status running on a body and reports how many were
// removed. It is what a dispel does: buffs, shields, and debuffs alike, because
// "strips effects and shields" is one rule and not two.
func (w *World) stripEffects(p *Player) int {
	removed := len(p.Effects)
	p.Effects = nil
	return removed
}

// movementScale is the slowest multiplier acting on the body. Slows take the
// strongest rather than multiplying, so stacking control cannot compound into a
// root that no dodge answers.
func (w *World) movementScale(p *Player) float64 {
	scale := 1.0
	for _, active := range p.Effects {
		if effect := w.tuning.Tables.Effects[active.EffectID]; effect.Kind == "slow" {
			scale = math.Min(scale, effect.SpeedMultiplier)
		}
	}
	return scale
}

// rooted reports whether the body may not move under its own power. It may
// still aim and act.
func (w *World) rooted(p *Player) bool { return w.hasEffectKind(p, "root") }

// stunned reports whether the body may neither move nor act.
func (w *World) stunned(p *Player) bool { return w.hasEffectKind(p, "stun") }

// knockback is the velocity a control effect is carrying the body at, which
// overrides both input and an in-flight dash for as long as it runs.
func (w *World) knockback(p *Player) (Vec, bool) {
	for _, active := range p.Effects {
		if effect := w.tuning.Tables.Effects[active.EffectID]; effect.Kind == "knockback" {
			return active.Direction.Mul(effect.Speed), true
		}
	}
	return Vec{}, false
}

func (w *World) hasEffectKind(p *Player, kind string) bool {
	for _, active := range p.Effects {
		if w.tuning.Tables.Effects[active.EffectID].Kind == kind {
			return true
		}
	}
	return false
}

func indexOfEffect(effects []ActiveEffect, id string) int {
	for index, active := range effects {
		if active.EffectID == id {
			return index
		}
	}
	return -1
}
