package game

import (
	"math"
	"time"

	"spellfire/server/internal/tuning"
)

func (w *World) keystone(p *Player) (tuning.Keystone, bool) {
	if p == nil {
		return tuning.Keystone{}, false
	}
	for _, id := range p.Loadout.Keystones {
		row, ok := w.tuning.Tables.Keystones[id]
		if ok && row.Class == string(p.Class) {
			return row, true
		}
	}
	return tuning.Keystone{}, false
}

func (w *World) applyKeystone(p *Player, ability tuning.Ability) tuning.Ability {
	keystone, ok := w.keystone(p)
	if !ok || keystone.Behavior != tuning.KeystoneOvercharge || ability.Cost.Kind != tuning.CostMana {
		return ability
	}
	ability.DamageMultiplier = ability.DamageScale() * keystone.DamageMultiplier
	if ability.Deployable != nil {
		field := *ability.Deployable
		field.DamageMultiplier = field.DamageScale() * keystone.DamageMultiplier
		ability.Deployable = &field
	}
	ability.Cost.Amount *= keystone.CostMultiplier
	return ability
}

func (w *World) heatKeystone(p *Player) (tuning.Keystone, bool) {
	keystone, ok := w.keystone(p)
	return keystone, ok && keystone.Behavior == tuning.KeystoneOverheat
}

func (w *World) stepHeat(p *Player, now time.Time, dt float64) {
	keystone, ok := w.heatKeystone(p)
	if !ok {
		p.Heat, p.Overheated = 0, false
		return
	}
	// A short quiet window keeps sustained fire from cooling between shots.
	if now.Sub(p.LastShot) >= 500*time.Millisecond {
		p.Heat = math.Max(0, p.Heat-keystone.HeatCoolPerSecond*dt)
	}
	if p.Overheated && p.Heat <= keystone.HeatCapacity*keystone.HeatResumeFraction {
		p.Overheated = false
	}
}
