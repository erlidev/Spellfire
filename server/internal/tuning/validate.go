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

func (t *Tables) validateSpells(r *report) {
	for _, id := range sortedKeys(t.Spells) {
		spell := t.Spells[id]
		r.require(spell.Name != "", "spells: %q has no name", id)
		r.require(t.Elements[spell.Element].Name != "", "spells: %q references unknown element %q", id, spell.Element)
		r.require(spell.Tier >= 1 && spell.Tier <= 4, "spells: %q has tier %d, want 1-4", id, spell.Tier)
		r.require(spell.ManaCost >= 0, "spells: %q has a negative mana cost", id)
		r.require(spell.CooldownMS >= 0, "spells: %q has a negative cooldown", id)
		r.require(spell.CastIntervalMS > 0, "spells: %q must declare a positive cast_interval_ms", id)
		t.validateDamaging(r, "spells", id, spell.DamageBand, spell.DodgeVector, spell.Projectile)
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
		if weapon.Spell != "" {
			// A staff delegates cadence, cost, damage band, and dodge vector to
			// the spell it casts, so it must not also declare its own.
			r.require(t.Spells[weapon.Spell].Name != "", "weapons: %q casts unknown spell %q", id, weapon.Spell)
			r.require(weapon.Projectile == nil && weapon.DamageBand == "" && weapon.MagazineSize == 0 && weapon.FireIntervalMS == 0 && weapon.ReloadMS == 0,
				"weapons: %q casts a spell, so it must not declare its own projectile, damage band, magazine, cadence, or reload", id)
			continue
		}
		r.require(weapon.FireIntervalMS > 0, "weapons: %q must declare a positive fire_interval_ms", id)
		r.require(weapon.MagazineSize > 0, "weapons: %q must declare a positive magazine_size", id)
		r.require(weapon.ReloadMS > 0, "weapons: %q must declare a positive reload_ms", id)
		// A gun's dodge vector is its projectile travel time; hitscan weapons
		// arrive with Phase 2.4 and will declare a scope commitment instead.
		t.validateDamaging(r, "weapons", id, weapon.DamageBand, "projectile_travel", weapon.Projectile)
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
	claim := func(owner string, projectile *Projectile) {
		if projectile == nil || projectile.Kind == "" {
			return
		}
		r.require(owners[projectile.Kind] == "", "projectile kind %q is claimed by both %s and %s; kinds must be unique across tables", projectile.Kind, owners[projectile.Kind], owner)
		owners[projectile.Kind] = owner
	}
	for _, id := range sortedKeys(t.Weapons) {
		claim("weapon "+id, t.Weapons[id].Projectile)
	}
	for _, id := range sortedKeys(t.Spells) {
		claim("spell "+id, t.Spells[id].Projectile)
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
