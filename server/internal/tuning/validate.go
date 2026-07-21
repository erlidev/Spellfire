package tuning

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// validate rejects a table set the simulation cannot trust. It reports every
// problem at once rather than the first, because these files are hand-edited
// and one balance pass usually breaks several rows together.
func (t *Tables) validate() error {
	problems := &report{}
	t.validateManifest(problems)
	t.validateSimulation(problems)
	t.validateSession(problems)
	t.validateWorld(problems)
	t.validateOutposts(problems)
	t.validateCombat(problems)
	t.validateElements(problems)
	t.validateComponents(problems)
	t.validateMaterials(problems)
	t.validateBiomes(problems)
	t.validateEffects(problems)
	t.validateAbilities(problems)
	t.validateSpells(problems)
	t.validateWeapons(problems)
	t.validateMobs(problems)
	t.validateRetired(problems)
	t.validateProjectileKinds(problems)
	return problems.err()
}

type report struct{ messages []string }

func (r *report) addf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

// require reports a problem unless the condition holds, and returns the
// condition so a caller can skip checks that depend on it.
func (r *report) require(ok bool, format string, args ...any) bool {
	if !ok {
		r.addf(format, args...)
	}
	return ok
}

func (r *report) err() error {
	if len(r.messages) == 0 {
		return nil
	}
	sort.Strings(r.messages)
	return errors.New("invalid tuning tables:\n  " + strings.Join(r.messages, "\n  "))
}

func (t *Tables) validateManifest(r *report) {
	r.require(t.Manifest.Version > 0, "manifest: version must be positive")
	r.require(t.Manifest.SchemaVersion == SchemaVersion,
		"manifest: schema_version %d is not the supported version %d; run the forward migration",
		t.Manifest.SchemaVersion, SchemaVersion)
}

func (t *Tables) validateSimulation(r *report) {
	s := t.Simulation
	r.require(s.TickRate > 0, "simulation: tick_rate must be positive")
	r.require(s.SendRate > 0, "simulation: send_rate must be positive")
	if s.TickRate > 0 && s.SendRate > 0 {
		r.require(s.TickRate%s.SendRate == 0,
			"simulation: tick_rate %d must be a whole multiple of send_rate %d for even snapshot pacing", s.TickRate, s.SendRate)
	}
	r.require(s.AOIRadius > 0, "simulation: aoi_radius must be positive")
	r.require(s.MaxRewindMS > 0, "simulation: max_rewind_ms must be positive")
	r.require(s.InterpolationDelayMS > 0, "simulation: interpolation_delay_ms must be positive")
}

func (t *Tables) validateSession(r *report) {
	s := t.Session
	// A zero linger would make disconnecting an escape from a fight, which the
	// safety invariant forbids as much as any offensive use of a safe zone.
	r.require(s.LogoutLingerSeconds > 0, "session: logout_linger_seconds must be positive; a body that vanishes on disconnect makes combat logging free")
	r.require(s.PositionExpirySeconds > 0, "session: position_expiry_seconds must be positive")
	r.require(s.PositionExpirySeconds > s.LogoutLingerSeconds,
		"session: position_expiry_seconds %d must exceed logout_linger_seconds %d, or a position expires before the body it belongs to is gone",
		s.PositionExpirySeconds, s.LogoutLingerSeconds)
}

// validateOutposts keeps every recall destination inside the world. The table
// ships empty; Phase 3 owns where outposts actually sit.
func (t *Tables) validateOutposts(r *report) {
	for _, id := range sortedKeys(t.Outposts) {
		outpost := t.Outposts[id]
		r.require(outpost.Name != "", "outposts: %q has no name", id)
		distance := math.Hypot(outpost.Position[0], outpost.Position[1])
		r.require(distance <= t.World.Radius, "outposts: %q sits %g from the origin, outside the %g world radius", id, distance, t.World.Radius)
	}
}

func (t *Tables) validateWorld(r *report) {
	w := t.World
	r.require(w.Radius > 0, "world: radius must be positive")
	r.require(w.SpawnRadius > 0 && w.SpawnRadius < w.Radius, "world: spawn_radius must lie inside the world")
	if !r.require(len(w.DangerBands) > 0, "world: danger_bands must not be empty") {
		return
	}
	seen := map[string]bool{}
	previous, protectedRun := 0.0, true
	for index, band := range w.DangerBands {
		r.require(band.ID != "", "world: danger band %d has no id", index)
		r.require(!seen[band.ID], "world: duplicate danger band id %q", band.ID)
		seen[band.ID] = true
		r.require(band.Tier == index, "world: danger band %q has tier %d, want %d (bands are ordered outward from the hub)", band.ID, band.Tier, index)
		r.require(band.OuterRadius > previous, "world: danger band %q outer_radius %g must exceed the previous band's %g", band.ID, band.OuterRadius, previous)
		previous = band.OuterRadius
		switch band.PvP {
		case "off", "restricted":
			r.require(protectedRun, "world: danger band %q re-enables PvP protection outside a hostile band; protection must be contiguous from the hub", band.ID)
		case "on", "full":
			protectedRun = false
		default:
			r.addf("world: danger band %q has unknown pvp state %q", band.ID, band.PvP)
		}
		if band.MaterialGrade != "" {
			r.require(t.Materials.Grades[band.MaterialGrade].Name != "", "world: danger band %q references unknown material grade %q", band.ID, band.MaterialGrade)
		}
		r.require(band.Name != "" && band.Shape != "" && band.Summary != "", "world: danger band %q needs a name, shape, and summary for the HUD readout", band.ID)
	}
	r.require(previous == w.Radius, "world: outermost danger band ends at %g but the world radius is %g", previous, w.Radius)
	r.require(w.SafeRadius() > 0, "world: no band has pvp \"off\"; there is nowhere to craft or respawn")
	r.require(w.PvPRadius() >= w.SafeRadius(), "world: PvP-protected radius must contain the safe radius")
	trees := w.Trees
	r.require(trees.Count >= 0, "world: trees.count must not be negative")
	r.require(trees.MinRadius > 0 && trees.RadiusSpread > 0, "world: trees need a positive min_radius and radius_spread")
	r.require(trees.Spacing >= 0, "world: trees.spacing must not be negative")
	r.require(w.SafeRadius()+trees.InnerMargin+trees.OuterMargin < w.Radius, "world: tree margins leave no room between the safe radius and the rim")
}

func (t *Tables) validateCombat(r *report) {
	c := t.Combat
	r.require(len(c.Roles) > 0, "combat: roles must not be empty")
	r.require(len(c.DodgeVectors) > 0, "combat: dodge_vectors must not be empty")
	r.require(c.Player.Radius > 0, "combat: player.radius must be positive")
	r.require(c.Player.Speed > 0, "combat: player.speed must be positive")
	r.require(c.Player.MaxHealth > 0, "combat: player.max_health must be positive")
	r.require(c.Player.MaxMana > 0, "combat: player.max_mana must be positive")
	r.require(c.Player.ManaRegen > 0, "combat: player.mana_regen must be positive")
	r.require(c.Dash.Distance > 0, "combat: dash.distance must be positive")
	r.require(c.Dash.DurationMS > 0, "combat: dash.duration_ms must be positive")
	r.require(c.Dash.CooldownMS > c.Dash.DurationMS, "combat: dash.cooldown_ms must exceed dash.duration_ms")
	if !r.require(len(c.DamageBands) > 0, "combat: damage_bands must not be empty") {
		return
	}
	for _, id := range sortedKeys(c.DamageBands) {
		band := c.DamageBands[id]
		r.require(band.Name != "", "combat: damage band %q has no name", id)
		r.require(band.DamagePerHit > 0, "combat: damage band %q must deal positive damage", id)
		r.require(band.TargetTTKSeconds > 0, "combat: damage band %q must declare a target_ttk_seconds", id)
		r.require(band.TTKToleranceSeconds > 0, "combat: damage band %q must declare a ttk_tolerance_seconds", id)
	}
}

func (t *Tables) validateElements(r *report) {
	for _, id := range sortedKeys(t.Elements) {
		element := t.Elements[id]
		r.require(element.Name != "", "elements: %q has no name", id)
		r.require(contains(t.Combat.Roles, element.PrimaryRole), "elements: %q has primary_role %q, which is not a combat role", id, element.PrimaryRole)
		r.require(element.Secondary != "" && element.Character != "", "elements: %q must declare a secondary tool and a character", id)
	}
}

func (t *Tables) validateComponents(r *report) {
	for _, id := range sortedKeys(t.Components.Blueprints) {
		blueprint := t.Components.Blueprints[id]
		r.require(blueprint.Name != "", "components: blueprint %q has no name", id)
		r.require(len(blueprint.Slots) > 0, "components: blueprint %q exposes no slots", id)
	}
	for _, id := range sortedKeys(t.Components.Components) {
		component := t.Components.Components[id]
		r.require(component.Name != "", "components: %q has no name", id)
		blueprint, ok := t.Components.Blueprints[component.Blueprint]
		if !r.require(ok, "components: %q references unknown blueprint %q", id, component.Blueprint) {
			continue
		}
		r.require(contains(blueprint.Slots, component.Slot), "components: %q fills slot %q, which blueprint %q does not expose", id, component.Slot, component.Blueprint)
	}
}

func (t *Tables) validateMaterials(r *report) {
	tiers := map[int]string{}
	for _, id := range sortedKeys(t.Materials.Grades) {
		grade := t.Materials.Grades[id]
		r.require(grade.Name != "", "materials: grade %q has no name", id)
		r.require(grade.Tier > 0, "materials: grade %q must have a positive tier", id)
		r.require(tiers[grade.Tier] == "", "materials: grades %q and %q share tier %d", tiers[grade.Tier], id, grade.Tier)
		tiers[grade.Tier] = id
	}
	for _, id := range sortedKeys(t.Materials.Kinds) {
		kind := t.Materials.Kinds[id]
		r.require(kind.Name != "", "materials: kind %q has no name", id)
		r.require(kind.Source == "node" || kind.Source == "mob", "materials: kind %q has unknown source %q", id, kind.Source)
	}
	for _, id := range sortedKeys(t.Materials.Materials) {
		material := t.Materials.Materials[id]
		r.require(material.Name != "", "materials: %q has no name", id)
		r.require(t.Materials.Grades[material.Grade].Name != "", "materials: %q references unknown grade %q", id, material.Grade)
		kind, ok := t.Materials.Kinds[material.Kind]
		if !r.require(ok, "materials: %q references unknown kind %q", id, material.Kind) {
			continue
		}
		if kind.Universal {
			r.require(material.Biome == "", "materials: %q is a universal kind but is gated to biome %q", id, material.Biome)
			continue
		}
		r.require(material.Biome != "" && t.Biomes[material.Biome].Name != "", "materials: %q is biome-gated but references unknown biome %q", id, material.Biome)
	}
}

func (t *Tables) validateBiomes(r *report) {
	for _, id := range sortedKeys(t.Biomes) {
		biome := t.Biomes[id]
		r.require(biome.Name != "", "biomes: %q has no name", id)
		r.require(t.Elements[biome.Element].Name != "", "biomes: %q references unknown element %q", id, biome.Element)
	}
}

// honouredDodgeVectors are the counterplay vectors the simulation actually
// delivers. A damaging row may only claim one of these; the ability validation
// below also requires the windup or shared geometry that makes each claim real.
var honouredDodgeVectors = []string{"projectile_travel", "cast_time", "telegraph", "ground_indicator"}

func (t *Tables) validateEffects(r *report) {
	for _, id := range sortedKeys(t.Effects) {
		effect := t.Effects[id]
		r.require(effect.Name != "", "effects: %q has no name", id)
		r.require(effect.DurationMS > 0, "effects: %q must declare a positive duration_ms", id)
		r.require(effect.Stacking == StackRefresh || effect.Stacking == StackStack,
			"effects: %q has unknown stacking %q, want %q or %q", id, effect.Stacking, StackRefresh, StackStack)
		if !r.require(contains(EffectKinds, effect.Kind), "effects: %q has kind %q, which the simulation cannot run; want one of %v", id, effect.Kind, EffectKinds) {
			continue
		}
		// Each kind owns exactly the fields it uses. Anything else set on the
		// row is a value the world would silently ignore.
		burn, slow, knockback, shield := effect.Kind == "burn", effect.Kind == "slow", effect.Kind == "knockback", effect.Kind == "shield"
		if burn {
			r.require(effect.TickMS > 0, "effects: burn %q must declare a positive tick_ms", id)
			r.require(effect.DamageFraction > 0, "effects: burn %q must declare a positive damage_fraction of its band", id)
			r.require(t.Combat.DamageBands[effect.DamageBand].Name != "", "effects: burn %q references unknown damage band %q", id, effect.DamageBand)
		}
		if shield {
			r.require(effect.AbsorbHits > 0, "effects: shield %q must declare a positive absorb_hits", id)
			r.require(t.Combat.DamageBands[effect.DamageBand].Name != "", "effects: shield %q references unknown damage band %q", id, effect.DamageBand)
		}
		if slow {
			r.require(effect.SpeedMultiplier > 0 && effect.SpeedMultiplier < 1,
				"effects: slow %q has speed_multiplier %g, want a fraction between 0 and 1 exclusive; a full stop is a root", id, effect.SpeedMultiplier)
		}
		if knockback {
			r.require(effect.Speed > 0, "effects: knockback %q must declare a positive speed", id)
		}
		r.require(burn || shield || effect.DamageBand == "", "effects: %q is a %s but references a damage band, which only burn and shield use", id, effect.Kind)
		r.require(burn || (effect.TickMS == 0 && effect.DamageFraction == 0), "effects: %q is a %s but declares burn's tick_ms/damage_fraction", id, effect.Kind)
		r.require(slow || effect.SpeedMultiplier == 0, "effects: %q is a %s but declares slow's speed_multiplier", id, effect.Kind)
		r.require(knockback || effect.Speed == 0, "effects: %q is a %s but declares knockback's speed", id, effect.Kind)
		r.require(shield || effect.AbsorbHits == 0, "effects: %q is a %s but declares shield's absorb_hits", id, effect.Kind)
	}
}

// validateAbilities holds the ability contract: every use costs something the
// simulation knows how to charge, every damaging ability draws from a shared
// band and offers a dodge vector the server actually delivers, and a declared
// windup or telegraph must match the dodge vector it is there to justify.
func (t *Tables) validateAbilities(r *report) {
	for _, id := range sortedKeys(t.Abilities) {
		ability := t.Abilities[id]
		r.require(ability.Name != "", "abilities: %q has no name", id)
		r.require(ability.IntervalMS > 0, "abilities: %q must declare a positive interval_ms", id)
		r.require(ability.CooldownMS >= 0, "abilities: %q has a negative cooldown_ms", id)
		r.require(ability.WindupMS >= 0, "abilities: %q has a negative windup_ms", id)
		t.validateCost(r, id, ability.Cost)
		for _, effect := range ability.Effects {
			r.require(t.Effects[effect].Name != "", "abilities: %q applies unknown effect %q", id, effect)
		}
		if ability.Telegraph != nil {
			t.validateTelegraph(r, "abilities", id, *ability.Telegraph)
		}
		r.require((ability.WindupMS > 0) == (ability.Telegraph != nil),
			"abilities: %q must declare a positive windup_ms and a telegraph together, or neither", id)
		if !ability.Damaging() {
			r.require(ability.DodgeVector == "", "abilities: %q deals no damage, so it must not claim a dodge vector", id)
			continue
		}
		t.validateDamaging(r, "abilities", id, ability.DamageBand, ability.DodgeVector, ability.Projectile)
		r.require(contains(honouredDodgeVectors, ability.DodgeVector),
			"abilities: %q claims dodge vector %q, which the simulation does not deliver; only %v are honoured", id, ability.DodgeVector, honouredDodgeVectors)
		if ability.DodgeVector == "cast_time" {
			r.require(ability.WindupMS > 0, "abilities: %q claims cast_time but declares no windup", id)
		}
		if ability.DodgeVector == "telegraph" || ability.DodgeVector == "ground_indicator" {
			r.require(ability.Telegraph != nil, "abilities: %q claims %s but declares no telegraph", id, ability.DodgeVector)
		}
	}
}

func (t *Tables) validateTelegraph(r *report, table, id string, telegraph Telegraph) {
	prefix := fmt.Sprintf("%s: %q telegraph", table, id)
	if !r.require(contains(TelegraphShapes, telegraph.Shape),
		"%s has shape %q, want one of the standardized figures %v", prefix, telegraph.Shape, TelegraphShapes) {
		return
	}
	r.require(telegraph.ActiveMS > 0, "%s must declare a positive active_ms", prefix)
	r.require(telegraph.ResolvedMS > 0, "%s must declare a positive resolved_ms", prefix)
	switch telegraph.Shape {
	case "circle":
		r.require(telegraph.Radius > 0, "%s circle must declare a positive radius", prefix)
		r.require(telegraph.Length == 0 && telegraph.Width == 0 && telegraph.AngleDegrees == 0,
			"%s circle declares geometry it does not use", prefix)
	case "cone":
		r.require(telegraph.Length > 0, "%s cone must declare a positive length", prefix)
		r.require(telegraph.AngleDegrees > 0 && telegraph.AngleDegrees < 360,
			"%s cone angle_degrees must lie between 0 and 360 exclusive", prefix)
		r.require(telegraph.Radius == 0 && telegraph.Width == 0, "%s cone declares geometry it does not use", prefix)
	case "line":
		r.require(telegraph.Length > 0 && telegraph.Width > 0, "%s line must declare a positive length and width", prefix)
		r.require(telegraph.Radius == 0 && telegraph.AngleDegrees == 0, "%s line declares geometry it does not use", prefix)
	case "ring":
		r.require(telegraph.Radius > 0 && telegraph.Width > 0 && telegraph.Width < telegraph.Radius,
			"%s ring must declare a positive radius and a width smaller than it", prefix)
		r.require(telegraph.Length == 0 && telegraph.AngleDegrees == 0, "%s ring declares geometry it does not use", prefix)
	}
}

func (t *Tables) validateCost(r *report, id string, cost Cost) {
	switch cost.Kind {
	case CostNone:
		r.require(cost.Amount == 0, "abilities: %q costs nothing but declares an amount of %g", id, cost.Amount)
	case CostAmmo:
		r.require(cost.Amount >= 1 && cost.Amount == math.Trunc(cost.Amount), "abilities: %q charges %g ammunition; a magazine spends whole rounds", id, cost.Amount)
	case CostMana:
		r.require(cost.Amount > 0, "abilities: %q spends mana but charges %g", id, cost.Amount)
	default:
		r.addf("abilities: %q has unknown cost kind %q, want one of %q, %q, %q", id, cost.Kind, CostNone, CostAmmo, CostMana)
	}
}

func (t *Tables) validateSpells(r *report) {
	for _, id := range sortedKeys(t.Spells) {
		spell := t.Spells[id]
		r.require(spell.Name != "", "spells: %q has no name", id)
		r.require(t.Elements[spell.Element].Name != "", "spells: %q references unknown element %q", id, spell.Element)
		r.require(spell.Tier >= 1 && spell.Tier <= 4, "spells: %q has tier %d, want 1-4", id, spell.Tier)
		// A spell is identity — element, tier, unlock — over one ability. Cost,
		// cadence, cooldown, counterplay, and delivery all live on the ability.
		r.require(t.Abilities[spell.Ability].Name != "", "spells: %q references unknown ability %q", id, spell.Ability)
	}
}

func (t *Tables) validateWeapons(r *report) {
	starters := map[string]string{}
	for _, id := range sortedKeys(t.Weapons) {
		weapon := t.Weapons[id]
		r.require(weapon.Name != "", "weapons: %q has no name", id)
		r.require(weapon.Class == "gunslinger" || weapon.Class == "mage", "weapons: %q has unknown class %q", id, weapon.Class)
		r.require(weapon.Category != "", "weapons: %q has no category", id)
		r.require(t.Components.Blueprints[weapon.Blueprint].Name != "", "weapons: %q references unknown blueprint %q", id, weapon.Blueprint)
		if weapon.Starter {
			r.require(starters[weapon.Class] == "", "weapons: %q and %q are both the starter weapon for %s", starters[weapon.Class], id, weapon.Class)
			starters[weapon.Class] = id
		}
		if !r.require((weapon.Ability == "") != (weapon.Spell == ""),
			"weapons: %q must declare exactly one of ability or spell", id) {
			continue
		}
		if weapon.Spell != "" {
			// A staff delegates everything it does to the spell it casts, and a
			// spell is never reloaded.
			r.require(t.Spells[weapon.Spell].Name != "", "weapons: %q casts unknown spell %q", id, weapon.Spell)
			r.require(weapon.MagazineSize == 0 && weapon.ReloadMS == 0, "weapons: %q casts a spell, so it must not declare a magazine or a reload", id)
			continue
		}
		ability, ok := t.Abilities[weapon.Ability]
		if !r.require(ok, "weapons: %q references unknown ability %q", id, weapon.Ability) {
			continue
		}
		r.require(weapon.MagazineSize > 0, "weapons: %q must declare a positive magazine_size", id)
		r.require(weapon.ReloadMS > 0, "weapons: %q must declare a positive reload_ms", id)
		// The magazine is the weapon's, the round spent is the ability's: they
		// must agree, or firing would drain a resource the weapon does not hold.
		r.require(ability.Cost.Kind == CostAmmo, "weapons: %q holds a magazine but its ability %q spends %q", id, weapon.Ability, ability.Cost.Kind)
		r.require(ability.Cost.Amount <= float64(weapon.MagazineSize), "weapons: %q holds %d rounds but its ability %q spends %g per use", id, weapon.MagazineSize, weapon.Ability, ability.Cost.Amount)
	}
	r.require(starters["gunslinger"] != "", "weapons: no starter weapon for gunslinger; a new character would be unarmed")
	r.require(starters["mage"] != "", "weapons: no starter weapon for mage; a new character would be unarmed")
}

func (t *Tables) validateMobs(r *report) {
	for _, id := range sortedKeys(t.Mobs) {
		mob := t.Mobs[id]
		r.require(mob.Name != "", "mobs: %q has no name", id)
		r.require(mob.Family != "", "mobs: %q has no family", id)
		r.require(mob.Silhouette != "", "mobs: %q has no silhouette", id)
		r.require(mob.Turrets >= 1, "mobs: %q must have at least one turret", id)
		r.require(t.Combat.DamageBands[mob.DamageBand].Name != "", "mobs: %q references unknown damage band %q", id, mob.DamageBand)
		r.require(contains(t.Combat.DodgeVectors, mob.DodgeVector), "mobs: %q declares no valid dodge vector; every damaging tool needs one", id)
		if mob.TelegraphShape != "" {
			r.require(contains(TelegraphShapes, mob.TelegraphShape),
				"mobs: %q telegraph_shape %q is not one of the shared figures %v", id, mob.TelegraphShape, TelegraphShapes)
		}
		if mob.DodgeVector == "telegraph" || mob.DodgeVector == "ground_indicator" {
			r.require(mob.TelegraphShape != "", "mobs: %q claims %s but selects no shared telegraph_shape", id, mob.DodgeVector)
		}
	}
}

// validateRetired keeps every withdrawn ID resolvable. A save may name an ID
// this build no longer ships, so the retirement must terminate on live content
// or on a refund; a dangling or circular chain would silently confiscate
// earned progress.
func (t *Tables) validateRetired(r *report) {
	for _, id := range sortedKeys(t.Retired) {
		retirement := t.Retired[id]
		if !r.require(contains(RetiredKinds, retirement.Kind), "retired: %q has unknown kind %q", id, retirement.Kind) {
			continue
		}
		r.require(!t.Live(retirement.Kind, id), "retired: %q is still a live %s row; an ID is either current or retired, never both", id, retirement.Kind)
		r.require(retirement.Note != "", "retired: %q must record why it was retired", id)
		replaced, refunded := retirement.Replacement != "", len(retirement.Refund) > 0
		if !r.require(replaced != refunded, "retired: %q must declare exactly one of replacement or refund", id) {
			continue
		}
		if refunded {
			for _, material := range sortedKeys(retirement.Refund) {
				r.require(retirement.Refund[material] > 0, "retired: %q refunds a non-positive count of %q", id, material)
				r.require(t.Live("material", material), "retired: %q refunds unknown material %q", id, material)
			}
			continue
		}
		if _, ok := t.Resolve(retirement.Kind, retirement.Replacement); !ok {
			r.addf("retired: %q replaces %q, which reaches neither a live %s nor a refund", id, retirement.Replacement, retirement.Kind)
		}
	}
}

// validateDamaging enforces the cross-cutting rule that anything which deals
// damage draws from a shared band and offers a declared dodge vector.
func (t *Tables) validateDamaging(r *report, table, id, band, dodge string, projectile *Projectile) {
	r.require(t.Combat.DamageBands[band].Name != "", "%s: %q references unknown damage band %q", table, id, band)
	r.require(contains(t.Combat.DodgeVectors, dodge), "%s: %q declares dodge vector %q, which is not a recognised counterplay vector", table, id, dodge)
	if !r.require(projectile != nil, "%s: %q deals damage but declares no projectile", table, id) {
		return
	}
	r.require(projectile.Kind != "", "%s: %q has a projectile with no kind", table, id)
	r.require(projectile.Speed > 0, "%s: %q has a projectile with no travel speed; instant damage has no dodge vector", table, id)
	r.require(projectile.LifeSeconds > 0, "%s: %q has a projectile with no lifetime", table, id)
	r.require(projectile.Radius > 0, "%s: %q has a projectile with no radius", table, id)
	r.require(projectile.Silhouette != "", "%s: %q has a projectile with no silhouette for the renderer", table, id)
}

// validateProjectileKinds keeps kinds unique across every table, because a
// snapshot carries only the kind and the renderer resolves the silhouette
// from it.
func (t *Tables) validateProjectileKinds(r *report) {
	owners := map[string]string{}
	for _, id := range sortedKeys(t.Abilities) {
		projectile := t.Abilities[id].Projectile
		if projectile == nil || projectile.Kind == "" {
			continue
		}
		r.require(owners[projectile.Kind] == "", "projectile kind %q is claimed by both ability %s and ability %s; kinds must be unique across tables", projectile.Kind, owners[projectile.Kind], id)
		owners[projectile.Kind] = id
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
