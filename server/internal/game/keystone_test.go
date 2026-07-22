package game

import (
	"math"
	"testing"
	"time"

	"spellfire/server/internal/model"
)

func TestVolatileFocusEmpowersManaAndRaisesItsCost(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "mage-keystone", model.Mage, Vec{1200, 0}, now)
	p.Loadout.Keystones = []string{"volatile-focus"}
	base := w.tuning.Tables.Abilities["fire-bolt-cast"]
	applied := w.applyKeystone(p, base)
	if math.Abs(applied.DamageScale()-1.06) > 1e-9 || applied.Cost.Amount != base.Cost.Amount*1.5 {
		t.Fatalf("volatile focus produced damage=%g cost=%g from cost=%g", applied.DamageScale(), applied.Cost.Amount, base.Cost.Amount)
	}
	field := w.applyKeystone(p, w.tuning.Tables.Abilities["cinder-patch-cast"])
	if field.Deployable == nil || math.Abs(field.Deployable.DamageScale()-1.06) > 1e-9 {
		t.Fatalf("volatile focus did not reach field damage: %#v", field.Deployable)
	}
}

func TestThermalCycleLocksAtCapacityAndResumesAfterCooling(t *testing.T) {
	w, now := testWorld()
	p := addTestPlayer(w, "gun-keystone", model.Gunslinger, Vec{1200, 0}, now)
	p.Loadout.Keystones = []string{"thermal-cycle"}
	ability := w.tuning.Tables.Abilities["pistol-shot"]
	keystone := w.tuning.Tables.Keystones["thermal-cycle"]
	for shots := 0; shots < int(keystone.HeatCapacity); shots++ {
		if !w.spend(p, ability, now) {
			t.Fatalf("shot %d refused before capacity", shots+1)
		}
	}
	if !p.Overheated || w.spend(p, ability, now) {
		t.Fatalf("capacity did not lock fire: heat=%g overheated=%v", p.Heat, p.Overheated)
	}
	p.LastShot = now
	w.stepHeat(p, now.Add(400*time.Millisecond), 0.4)
	if p.Heat != keystone.HeatCapacity {
		t.Fatalf("heat cooled during quiet delay: %g", p.Heat)
	}
	w.stepHeat(p, now.Add(time.Second), 2)
	if p.Overheated || p.Heat > keystone.HeatCapacity*keystone.HeatResumeFraction {
		t.Fatalf("thermal cycle did not resume after cooling: heat=%g overheated=%v", p.Heat, p.Overheated)
	}
}
