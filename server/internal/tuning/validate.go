package tuning

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// validate rejects a table set the simulation cannot trust. It reports every
// problem at once rather than the first, because these files are hand-edited
// and one balance pass usually breaks several rows together.
func (t *Tables) validate() error {
	problems := &report{}
	t.validateManifest(problems)
	t.validateAdmins(problems)
	t.validateEntityAdmin(problems)
	t.validateSimulation(problems)
	t.validateSession(problems)
	t.validateEntities(problems)
	t.validateWorld(problems)
	t.validateOutposts(problems)
	t.validateField(problems)
	t.validateCombat(problems)
	t.validateLoadout(problems)
	t.validateProgression(problems)
	t.validateElements(problems)
	t.validateComponents(problems)
	t.validateMaterials(problems)
	t.validateBiomes(problems)
	t.validateEffects(problems)
	t.validateAbilities(problems)
	t.validateSpells(problems)
	t.validateGadgets(problems)
	t.validateWeapons(problems)
	t.validateAmmunition(problems)
	t.validateRideables(problems)
	t.validateUnlockIDs(problems)
	t.validateMobs(problems)
	t.validateRetired(problems)
	t.validateProjectileKinds(problems)
	t.validateRoleCoverage(problems)
	t.validateVerticalBudget(problems)
	return problems.err()
}

func (t *Tables) validateEntities(r *report) {
	for _, id := range sortedKeys(t.Entities) {
		definition := t.Entities[id]
		prefix := fmt.Sprintf("entities: %s", id)
		r.require(definition.Mass == -1 || definition.Mass >= 0, "%s mass must be -1 or non-negative", prefix)
		r.require(definition.MaxHealth == -1 || definition.MaxHealth > 0, "%s max_health must be -1 or positive", prefix)
		r.require(!definition.OccludesVision || len(definition.CollisionObjects) > 0, "%s occludes vision but has no collision geometry", prefix)
		for index, object := range definition.CollisionObjects {
			objectPrefix := fmt.Sprintf("%s collision object %d", prefix, index)
			switch object.Type {
			case "circle":
				r.require(object.Radius > 0, "%s circle needs a positive radius", objectPrefix)
				r.require(object.Width == 0 && object.Height == 0, "%s circle must not declare width or height", objectPrefix)
			case "box":
				r.require(object.Width > 0 && object.Height > 0, "%s box needs positive width and height", objectPrefix)
				r.require(object.Radius == 0, "%s box must not declare a radius", objectPrefix)
			default:
				r.addf("%s has unsupported type %q", objectPrefix, object.Type)
			}
		}
	}
	for _, id := range []string{"player", "projectile", "telegraph", "smoke", "tree", "wall"} {
		_, ok := t.Entities[id]
		r.require(ok, "entities: missing required definition %q", id)
	}
	if player, ok := t.Entities["player"]; ok {
		r.require(len(player.CollisionObjects) == 1 && player.CollisionObjects[0].Type == "circle", "entities: player must have one circle collision object")
	}
	if tree, ok := t.Entities["tree"]; ok {
		r.require(len(tree.CollisionObjects) == 1 && tree.CollisionObjects[0].Type == "circle", "entities: tree must have one circle collision object")
	}
	if projectile, ok := t.Entities["projectile"]; ok {
		r.require(len(projectile.CollisionObjects) == 1 && projectile.CollisionObjects[0].Type == "circle", "entities: projectile must have one circle collision object")
	}
	if telegraph, ok := t.Entities["telegraph"]; ok {
		r.require(len(telegraph.CollisionObjects) == 0, "entities: telegraph must not have collision objects")
	}
	if wall, ok := t.Entities["wall"]; ok {
		r.require(len(wall.CollisionObjects) == 1 && wall.CollisionObjects[0].Type == "box", "entities: wall must have one box collision object")
		if len(wall.CollisionObjects) == 1 {
			r.require(wall.CollisionObjects[0].Width == wall.CollisionObjects[0].Height, "entities: wall collision box must be square")
		}
	}
}

func (t *Tables) validateEntityAdmin(r *report) {
	spawnables := 0
	for _, id := range sortedKeys(t.Entities) {
		definition := t.Entities[id]
		if definition.Admin.Spawnable {
			spawnables++
			r.require(definition.Admin.Name != "", "entities: spawnable %q has no admin name", id)
		}
		validateAdminFields(r, "entities: "+id, definition.Admin.Fields)
	}
	r.require(spawnables > 0, "entities: at least one entity must be admin spawnable")
	validateAdminFields(r, "materials: admin_grant", []AdminField{t.Materials.AdminGrant})
	r.require(t.Materials.AdminGrant.Attribute == "inventory.material_count" && t.Materials.AdminGrant.Input == "number", "materials: admin_grant must configure numeric inventory.material_count")
	validateAdminFields(r, "progression: admin_grant", []AdminField{t.Progression.AdminGrant})
	r.require(t.Progression.AdminGrant.Attribute == "progression.level" && t.Progression.AdminGrant.Input == "number", "progression: admin_grant must configure numeric progression.level")
}

func validateAdminFields(r *report, prefix string, fields []AdminField) {
	seen := map[string]bool{}
	for _, field := range fields {
		r.require(field.Attribute != "", "%s has a field with no attribute", prefix)
		r.require(strings.Contains(field.Attribute, "."), "%s attribute %q must be component.attribute", prefix, field.Attribute)
		r.require(!seen[field.Attribute], "%s repeats field %q", prefix, field.Attribute)
		seen[field.Attribute] = true
		r.require(field.Label != "", "%s field %q has no label", prefix, field.Attribute)
		r.require(contains([]string{"spawn", "edit", "both"}, field.Scope), "%s field %q has invalid scope %q", prefix, field.Attribute, field.Scope)
		switch field.Input {
		case "number", "rotation":
			if r.require(field.Minimum != nil && field.Maximum != nil && field.Step != nil, "%s number field %q needs min, max, and step", prefix, field.Attribute) {
				value, err := strconv.ParseFloat(field.Default, 64)
				r.require(err == nil && *field.Minimum <= value && value <= *field.Maximum, "%s number field %q default is outside its bounds", prefix, field.Attribute)
				r.require(*field.Minimum < *field.Maximum && *field.Step > 0, "%s number field %q needs min < max and positive step", prefix, field.Attribute)
			}
		case "position":
			if r.require(field.Minimum != nil && field.Maximum != nil && field.Step != nil, "%s position field %q needs min, max, and step", prefix, field.Attribute) {
				position := []float64{}
				err := json.Unmarshal([]byte(field.Default), &position)
				r.require(err == nil && len(position) == 2 && *field.Minimum <= position[0] && position[0] <= *field.Maximum && *field.Minimum <= position[1] && position[1] <= *field.Maximum, "%s position field %q default is outside its bounds", prefix, field.Attribute)
				r.require(*field.Minimum < *field.Maximum && *field.Step > 0, "%s position field %q needs min < max and positive step", prefix, field.Attribute)
			}
		case "text":
			r.require(field.MaxLength > 0, "%s text field %q needs a positive max_length", prefix, field.Attribute)
			r.require(len(field.Default) <= field.MaxLength, "%s text field %q default exceeds max_length", prefix, field.Attribute)
		case "select":
			r.require(len(field.Options) > 0, "%s select field %q needs options", prefix, field.Attribute)
			found := false
			for _, option := range field.Options {
				found = found || option.Value == field.Default
				r.require(option.Value != "" && option.Label != "", "%s select field %q has an incomplete option", prefix, field.Attribute)
			}
			r.require(found, "%s select field %q default is not an option", prefix, field.Attribute)
		default:
			r.addf("%s field %q has unsupported input %q", prefix, field.Attribute, field.Input)
		}
	}
}

func (t *Tables) validateAdmins(r *report) {
	seen := map[string]bool{}
	for index, email := range t.Admins.Emails {
		r.require(email != "" && len(email) <= 254 && strings.Count(email, "@") == 1,
			"admins: email %d %q is not a valid account email", index, email)
		r.require(!seen[email], "admins: duplicate account email %q", email)
		seen[email] = true
	}
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
	r.require(s.ExitInvulnSeconds > 0, "session: exit_invuln_seconds must be positive; leaving an outpost must cover the transition out")
	r.require(s.MountLockoutSeconds > 0, "session: mount_lockout_seconds must be positive; a ride must not be enterable mid-fight")
}

// validateOutposts keeps every outpost a legal safe fixture: inside the world,
// out of the Deadlands, with a no-PvP bubble, a discovery reach at least as wide
// as it, and a known, non-empty service set.
func (t *Tables) validateOutposts(r *report) {
	services := map[string]bool{"loadout": true, "crafting": true, "respawn": true}
	for _, id := range sortedKeys(t.Outposts) {
		outpost := t.Outposts[id]
		r.require(outpost.Name != "", "outposts: %q has no name", id)
		distance := math.Hypot(outpost.Position[0], outpost.Position[1])
		r.require(distance+outpost.SafeRadius <= t.World.Radius, "outposts: %q sits %g from the origin, outside the %g world radius", id, distance, t.World.Radius)
		r.require(outpost.SafeRadius > 0, "outposts: %q needs a positive safe_radius", id)
		r.require(outpost.DiscoveryRadius >= outpost.SafeRadius, "outposts: %q discovery_radius must be at least its safe_radius", id)
		r.require(len(outpost.Services) > 0, "outposts: %q offers no services", id)
		for _, service := range outpost.Services {
			r.require(services[service], "outposts: %q offers unknown service %q", id, service)
		}
		band := t.World.BandAt(distance)
		r.require(band.PvP != "full", "outposts: %q sits in the Deadlands (%q), which has no safe outposts", id, band.ID)
	}
}

// validateRideables keeps every rideable recipe buildable: a known class, a
// concrete entity archetype, a speed that actually helps, and a real material
// cost.
func (t *Tables) validateRideables(r *report) {
	for _, id := range sortedKeys(t.Rideables) {
		rideable := t.Rideables[id]
		r.require(rideable.Name != "", "rideables: %q has no name", id)
		r.require(rideable.Class == "gunslinger" || rideable.Class == "mage", "rideables: %q has unknown class %q", id, rideable.Class)
		r.require(t.Entities[rideable.Entity].MaxHealth > 0, "rideables: %q references entity %q, which must have positive health", id, rideable.Entity)
		r.require(rideable.RideSpeed > 1, "rideables: %q ride_speed must exceed 1 to be worth summoning", id)
		r.require(len(rideable.Cost) > 0, "rideables: %q has no material cost", id)
		for material, amount := range rideable.Cost {
			r.require(t.Materials.Materials[material].Name != "", "rideables: %q costs unknown material %q", id, material)
			r.require(amount > 0, "rideables: %q material %q amount must be positive", id, material)
		}
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
	terrain := w.Terrain
	r.require(terrain.Cell > 0, "world: terrain.cell must be positive; it is the site lattice the chunk generator places on")
	r.require(terrain.Spacing >= 0, "world: terrain.spacing must not be negative")
	r.require(w.ChunkSize > 0, "world: chunk_size must be positive")
	if terrain.Cell > 0 && w.ChunkSize > 0 {
		r.require(math.Mod(w.ChunkSize, terrain.Cell) == 0,
			"world: chunk_size %g must be a whole multiple of terrain.cell %g, or a site lattice would not line up with chunk edges", w.ChunkSize, terrain.Cell)
	}
	r.require(w.ChunkSize <= w.Radius, "world: chunk_size %g must not exceed the world radius %g", w.ChunkSize, w.Radius)
	r.require(w.SafeRadius()+terrain.InnerMargin+terrain.OuterMargin < w.Radius, "world: terrain margins leave no room between the safe radius and the rim")

	// The scatter and barrier archetypes across every biome, gathered so the
	// jitter-room and fixture-prefix rules can range over all of them.
	prefixes := map[string]bool{"belt-": true}
	widestScatter := 0.0
	validateBiomeTerrain := func(label string, set BiomeTerrain) {
		barrier, known := t.Entities[set.Barrier]
		if r.require(known, "world: %s barrier %q is not an entity archetype", label, set.Barrier) {
			r.require(len(barrier.CollisionObjects) > 0, "world: %s barrier %q has no collision geometry; an impassable formation must collide", label, set.Barrier)
			r.require(terrain.Belts.Cell < 2*entityExtent(barrier),
				"world: belts.cell %g is not smaller than twice the %q radius; the belt would be a passable picket rather than a solid mass", terrain.Belts.Cell, set.Barrier)
		}
		sum := 0.0
		for _, scatter := range set.Scatter {
			definition, ok := t.Entities[scatter.Entity]
			if !r.require(ok, "world: %s scatters unknown entity %q", label, scatter.Entity) {
				continue
			}
			r.require(scatter.Fill >= 0 && scatter.Fill <= 1, "world: %s scatter %q fill %g must be a fraction between 0 and 1", label, scatter.Entity, scatter.Fill)
			r.require(scatter.RadiusSpread > 0, "world: %s scatter %q needs a positive radius_spread", label, scatter.Entity)
			sum += scatter.Fill
			widestScatter = math.Max(widestScatter, entityExtent(definition)+scatter.RadiusSpread)
			prefixes[scatter.Entity+"-"] = true
		}
		r.require(sum <= 1+1e-9, "world: %s scatter fills sum to %g, over 1; they are absolute probabilities", label, sum)
	}
	validateBiomeTerrain("terrain.default", terrain.Default)
	for _, id := range sortedKeys(terrain.Biomes) {
		r.require(t.Biomes[id].Name != "", "world: terrain.biomes references unknown biome %q", id)
		validateBiomeTerrain("terrain.biomes."+id, terrain.Biomes[id])
	}
	// The generator jitters each scatter site inside its own cell by whatever
	// Spacing and the widest item leave room for. Without that room two
	// neighbouring sites could overlap, which is the one thing chunked
	// generation cannot repair after the fact: they may belong to different chunks.
	r.require(widestScatter == 0 || terrain.Cell > terrain.Spacing+2*widestScatter,
		"world: terrain.cell %g leaves no room for a %g-unit scatter item at %g spacing; widen the cell or thin the scatter", terrain.Cell, widestScatter, terrain.Spacing)

	t.validateBelts(r, w)

	seenFixtures := map[string]bool{}
	for index, fixture := range w.Fixtures {
		r.require(fixture.ID != "", "world: fixture %d has no id", index)
		r.require(!seenFixtures[fixture.ID], "world: duplicate fixture id %q", fixture.ID)
		seenFixtures[fixture.ID] = true
		for prefix := range prefixes {
			r.require(!strings.HasPrefix(fixture.ID, prefix), "world: fixture id %q uses the %q prefix the terrain generator owns", fixture.ID, prefix)
		}
		definition, ok := t.Entities[fixture.Entity]
		r.require(ok, "world: fixture %q references unknown entity %q", fixture.ID, fixture.Entity)
		distance := math.Hypot(fixture.Position[0], fixture.Position[1])
		r.require(distance+entityExtent(definition) <= w.Radius, "world: fixture %q extends outside the world", fixture.ID)
	}
}

// validateBelts holds the macro-structure layer to the contract the traversal
// design depends on: belts sit inside the terrain band, ordered outward, thick
// enough to be un-dashable, and each broken by at least one pass so nothing is
// sealed. The connectivity and journey-length guarantees themselves are
// executable tests over the live generator rather than static checks here.
func (t *Tables) validateBelts(r *report, w World) {
	belts := w.Terrain.Belts
	r.require(belts.Cell > 0, "world: belts.cell must be positive; it is the lattice a belt is filled on")
	if belts.Cell > 0 && w.ChunkSize > 0 {
		r.require(math.Mod(w.ChunkSize, belts.Cell) == 0,
			"world: chunk_size %g must be a whole multiple of belts.cell %g, or the belt lattice would not line up with chunk edges", w.ChunkSize, belts.Cell)
	}
	r.require(belts.Thickness > 0, "world: belts.thickness must be positive")
	dash := t.Combat.Dash.Distance
	r.require(belts.Thickness > dash, "world: belts.thickness %g must exceed the %g dash distance, or a belt is a formality", belts.Thickness, dash)
	r.require(belts.WaveCount >= 0, "world: belts.wave_count must not be negative")
	r.require(belts.PassesPerBelt >= 1, "world: belts.passes_per_belt must be at least 1, or a belt seals the world")
	r.require(belts.PassHalfAngle > 0, "world: belts.pass_half_angle must be positive; a pass is a gap")
	r.require(len(belts.Radii) > 0, "world: belts.radii must place at least one belt")
	inner := w.SafeRadius() + w.Terrain.InnerMargin
	outer := w.Radius - w.Terrain.OuterMargin
	previous := 0.0
	for index, radius := range belts.Radii {
		reach := belts.Thickness/2 + belts.Waviness
		r.require(radius-reach > inner, "world: belt %d at %g reaches inside the %g terrain floor", index, radius, inner)
		r.require(radius+reach < outer, "world: belt %d at %g reaches past the %g terrain ceiling", index, radius, outer)
		r.require(radius > previous, "world: belts.radii must climb outward; belt %d at %g is not past %g", index, radius, previous)
		previous = radius
	}
}

func entityExtent(definition EntityDefinition) float64 {
	extent := 0.0
	for _, object := range definition.CollisionObjects {
		local := math.Hypot(object.OffsetX, object.OffsetY)
		switch object.Type {
		case "circle":
			local += object.Radius
		case "box":
			local += math.Hypot(object.Width/2, object.Height/2)
		}
		extent = math.Max(extent, local)
	}
	return extent
}

func (t *Tables) validateCombat(r *report) {
	c := t.Combat
	r.require(len(c.Roles) > 0, "combat: roles must not be empty")
	r.require(len(c.DodgeVectors) > 0, "combat: dodge_vectors must not be empty")
	r.require(c.Player.Speed > 0, "combat: player.speed must be positive")
	r.require(c.Player.MaxMana > 0, "combat: player.max_mana must be positive")
	r.require(c.Player.ManaRegen > 0, "combat: player.mana_regen must be positive")
	r.require(c.Dash.Distance > 0, "combat: dash.distance must be positive")
	r.require(c.Dash.DurationMS > 0, "combat: dash.duration_ms must be positive")
	r.require(c.Dash.CooldownMS > c.Dash.DurationMS, "combat: dash.cooldown_ms must exceed dash.duration_ms")
	// Weight classes are the Gunslinger's balance axis, so they may only move
	// handling. A class that scaled movement or handling upward past its
	// unmodified baseline would be a straight upgrade rather than a tradeoff.
	if r.require(len(c.WeightClasses) > 0, "combat: weight_classes must not be empty; weapon handling has nothing to scale against") {
		for _, id := range sortedKeys(c.WeightClasses) {
			weight := c.WeightClasses[id]
			r.require(weight.Name != "", "combat: weight class %q has no name", id)
			r.require(weight.MovementMultiplier > 0 && weight.MovementMultiplier <= 1,
				"combat: weight class %q has movement_multiplier %g, want a fraction in (0,1]; weight slows a carrier, never speeds one up", id, weight.MovementMultiplier)
			r.require(weight.RecoilMultiplier > 0, "combat: weight class %q must scale recoil by a positive multiplier", id)
			r.require(weight.MoveSpreadMultiplier > 0, "combat: weight class %q must scale move spread by a positive multiplier", id)
		}
	}
	if !r.require(len(c.DamageBands) > 0, "combat: damage_bands must not be empty") {
		return
	}
	for _, id := range sortedKeys(c.DamageBands) {
		band := c.DamageBands[id]
		r.require(band.Name != "", "combat: damage band %q has no name", id)
		r.require(band.DamagePerHit > 0, "combat: damage band %q must deal positive damage", id)
		r.require(band.IntervalMS > 0, "combat: damage band %q must declare a positive interval_ms", id)
		r.require(band.TargetTTKSeconds > 0, "combat: damage band %q must declare a target_ttk_seconds", id)
		r.require(band.TTKToleranceSeconds > 0, "combat: damage band %q must declare a ttk_tolerance_seconds", id)
		if band.DamagePerHit > 0 && band.IntervalMS > 0 {
			hits := math.Ceil(t.Entities["player"].MaxHealth / band.DamagePerHit)
			ttk := math.Max(0, hits-1) * float64(band.IntervalMS) / 1000
			r.require(math.Abs(ttk-band.TargetTTKSeconds) <= band.TTKToleranceSeconds,
				"combat: damage band %q resolves to %.2fs, outside %.2f±%.2fs", id, ttk, band.TargetTTKSeconds, band.TTKToleranceSeconds)
		}
	}
}

// validateLoadout keeps the slot model coherent. The one rule that is not just
// a positive count is the bar width: a Gunslinger's weapon plus gadgets and a
// Mage's spells must lay out to the same number of selectable slots, or one
// class would have bindings the other cannot reach.
func (t *Tables) validateLoadout(r *report) {
	l := t.Loadout
	r.require(l.WeaponSlots == 1, "loadout: weapon_slots is %d; a character carries exactly one weapon", l.WeaponSlots)
	r.require(l.GadgetSlots > 0, "loadout: gadget_slots must be positive")
	r.require(l.SpellSlots > 0, "loadout: spell_slots must be positive")
	r.require(l.WeaponSlots+l.GadgetSlots == l.SpellSlots,
		"loadout: weapon_slots %d plus gadget_slots %d must equal spell_slots %d, or the two classes fill action bars of different widths",
		l.WeaponSlots, l.GadgetSlots, l.SpellSlots)
	r.require(l.Affinity.SameElementPerTier >= 0, "loadout: affinity.same_element_per_tier must not be negative")
	// A tier-4 signature must remain equippable inside the bar it shares with
	// the rest of the loadout, or the grid's own 4 + 2 build is unbuildable.
	r.require(l.RequiredSameElement(4)+1 <= l.SpellSlots,
		"loadout: a tier-4 spell needs %d same-element spells beside it but only %d spell slots exist",
		l.RequiredSameElement(4), l.SpellSlots)
}

// validateProgression keeps the character axis usable: a curve that actually
// costs something, every XP source the simulation can award priced, and an
// opening draw wide enough to fill the action bar — the starter kit's whole
// purpose is that a zero-material character is combat-capable immediately.
func (t *Tables) validateProgression(r *report) {
	p := t.Progression
	r.require(p.MaxLevel > 1, "progression: max_level must exceed 1")
	r.require(p.BaseXP > 0, "progression: base_xp must be positive")
	r.require(p.Growth >= 1, "progression: growth %g must be at least 1, or a later level would cost less than an earlier one", p.Growth)
	for _, source := range XPSources {
		award, ok := p.Sources[source]
		r.require(ok && award > 0, "progression: source %q must declare a positive award", source)
	}
	for _, source := range sortedKeys(p.Sources) {
		r.require(contains(XPSources, source), "progression: source %q is not one the simulation awards; want one of %v", source, XPSources)
	}
	r.require(p.CraftedItemCapacity > 0,
		"progression: crafted_item_capacity must be positive; permanent crafted inventory must have a defined capacity")
	r.require(p.StarterKit.Unlocks >= t.Loadout.BarSlots(),
		"progression: starter_kit.unlocks %d cannot fill the %d-slot action bar", p.StarterKit.Unlocks, t.Loadout.BarSlots())
	// The developer-mode grant is bounded by the curve it drives, so a request
	// can never name a level the progression table cannot reach.
	if p.AdminGrant.Minimum != nil && p.AdminGrant.Maximum != nil {
		r.require(*p.AdminGrant.Minimum == 1 && int(*p.AdminGrant.Maximum) == p.MaxLevel,
			"progression: admin_grant must span level 1 to max_level %d", p.MaxLevel)
	}
}

// validateUnlockIDs keeps the permanent ledger flat. It stores bare IDs, so a
// weapon and a spell sharing one would make an entry ambiguous, and an entry
// that no level ever grants would be unreachable for anyone whose starter draw
// missed it.
func (t *Tables) validateUnlockIDs(r *report) {
	owners := map[string]string{}
	claim := func(kind, id string, level int) {
		r.require(owners[id] == "", "%s: %q is also a live %s row; unlock IDs are flat and must be unique across tables", kind, id, owners[id])
		owners[id] = kind
		r.require(level >= 1, "%s: %q must declare a positive unlock_level; content no level grants can never be earned", kind, id)
		r.require(level <= t.Progression.MaxLevel, "%s: %q unlocks at level %d, past the level cap of %d", kind, id, level, t.Progression.MaxLevel)
	}
	for _, id := range sortedKeys(t.Weapons) {
		claim("weapons", id, t.Weapons[id].UnlockLevel)
	}
	for _, id := range sortedKeys(t.Spells) {
		claim("spells", id, t.Spells[id].UnlockLevel)
	}
	for _, id := range sortedKeys(t.Gadgets) {
		claim("gadgets", id, t.Gadgets[id].UnlockLevel)
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
		r.require(blueprint.Summary != "", "components: blueprint %q has no summary", id)
		r.require(len(blueprint.Slots) > 0, "components: blueprint %q exposes no slots", id)
	}
	for _, id := range sortedKeys(t.Components.Components) {
		component := t.Components.Components[id]
		r.require(component.Name != "", "components: %q has no name", id)
		r.require(contains([]string{"gun_part", "mana_crystal", "stave"}, component.Kind),
			"components: %q has unknown kind %q", id, component.Kind)
		r.require(component.Tier > 0, "components: %q must declare a positive tier", id)
		_, gradeExists := t.GradeAt(component.Tier)
		r.require(gradeExists, "components: %q has tier %d with no matching material grade", id, component.Tier)
		// The crafting UI must state a behaviour change in plain language, so a
		// row without one would leave a player spending materials on a mystery.
		r.require(component.Effect != "", "components: %q must describe its behaviour in plain language for the crafting UI", id)
		blueprint, ok := t.Components.Blueprints[component.Blueprint]
		if !r.require(ok, "components: %q references unknown blueprint %q", id, component.Blueprint) {
			continue
		}
		r.require(contains(blueprint.Slots, component.Slot), "components: %q fills slot %q, which blueprint %q does not expose", id, component.Slot, component.Blueprint)
		if component.Blueprint == "gun" {
			r.require(component.Kind == "gun_part", "components: %q is a %s on the gun blueprint", id, component.Kind)
		} else if component.Blueprint == "staff" {
			want := map[string]string{"crystal": "mana_crystal", "stave": "stave"}[component.Slot]
			r.require(want != "" && component.Kind == want, "components: %q is a %s in the staff %s slot; want %s", id, component.Kind, component.Slot, want)
		}
		// Materials must be hauled to a safe zone and spent. A free component
		// would put a behaviour change outside the economy entirely.
		if r.require(len(component.Cost) > 0, "components: %q declares no material cost; crafting is what the hauled materials are for", id) {
			maxMaterialTier := 0
			for _, material := range sortedKeys(component.Cost) {
				r.require(component.Cost[material] > 0, "components: %q costs a non-positive count of %q", id, material)
				r.require(t.Live("material", material), "components: %q costs unknown material %q", id, material)
				row := t.Materials.Materials[material]
				maxMaterialTier = max(maxMaterialTier, t.Materials.Grades[row.Grade].Tier)
			}
			r.require(maxMaterialTier >= component.Tier,
				"components: %q is tier %d but its highest material tier is %d", id, component.Tier, maxMaterialTier)
		}
		t.validateModifiers(r, id, component)
	}
	for _, id := range sortedKeys(t.Components.Recipes) {
		recipe := t.Components.Recipes[id]
		weapon, ok := t.Weapons[id]
		if !r.require(ok, "components: recipe %q has no matching weapon row", id) {
			continue
		}
		blueprint, ok := t.Components.Blueprints[recipe.Blueprint]
		if !r.require(ok, "components: recipe %q references unknown blueprint %q", id, recipe.Blueprint) {
			continue
		}
		r.require(weapon.Blueprint == recipe.Blueprint,
			"components: recipe %q uses blueprint %q but its weapon uses %q", id, recipe.Blueprint, weapon.Blueprint)
		r.require(recipe.Summary != "", "components: recipe %q has no player-facing summary", id)
		for _, slot := range blueprint.Slots {
			accepted, exists := recipe.Slots[slot]
			if !r.require(exists && len(accepted) > 0, "components: recipe %q does not fill required %s slot", id, slot) {
				continue
			}
			for _, componentID := range accepted {
				component, live := t.Components.Components[componentID]
				r.require(live, "components: recipe %q accepts unknown component %q", id, componentID)
				if live {
					r.require(component.Blueprint == recipe.Blueprint && component.Slot == slot,
						"components: recipe %q puts %q in %s, but it belongs to %s/%s", id, componentID, slot, component.Blueprint, component.Slot)
				}
			}
		}
		for _, slot := range sortedKeys(recipe.Slots) {
			r.require(contains(blueprint.Slots, slot), "components: recipe %q fills slot %q, which blueprint %q does not expose", id, slot, recipe.Blueprint)
		}
	}
	for _, id := range sortedKeys(t.Weapons) {
		_, ok := t.Components.Recipes[id]
		r.require(ok, "components: weapon %q has no crafting recipe", id)
	}
	recipeIDs := sortedKeys(t.Components.Recipes)
	for left := 0; left < len(recipeIDs); left++ {
		for right := left + 1; right < len(recipeIDs); right++ {
			a, b := t.Components.Recipes[recipeIDs[left]], t.Components.Recipes[recipeIDs[right]]
			if a.Blueprint != b.Blueprint {
				continue
			}
			ambiguous := true
			for _, slot := range t.Components.Blueprints[a.Blueprint].Slots {
				overlap := false
				for _, component := range a.Slots[slot] {
					if contains(b.Slots[slot], component) {
						overlap = true
						break
					}
				}
				if !overlap {
					ambiguous = false
					break
				}
			}
			r.require(!ambiguous, "components: recipes %q and %q can be built from the same parts", recipeIDs[left], recipeIDs[right])
		}
	}
}

// validateModifiers keeps every modifier on an attribute the simulation
// consumes. Mana crystals have a narrow output exception; fire cadence remains
// rejected outright because scaling it is an unrestricted DPS multiplier.
func (t *Tables) validateModifiers(r *report, id string, component Component) {
	// A stave is structural support. Its tier gates the crystal it can safely
	// carry, and it intentionally contributes no combat modifier of its own.
	if component.Kind == "stave" {
		r.require(len(component.Modifiers) == 0, "components: stave %q must not have magical modifiers", id)
		return
	}
	if !r.require(len(component.Modifiers) > 0, "components: %q declares no modifiers; a component that changes nothing is not a choice", id) {
		return
	}
	magazines, handling := t.blueprintHoldsMagazine(component.Blueprint), t.blueprintHandles(component.Blueprint)
	for _, attribute := range sortedKeys(component.Modifiers) {
		modifier := component.Modifiers[attribute]
		if contains(ForbiddenAttributes, attribute) {
			r.addf("components: %q modifies %q, which crafting may never touch: fire cadence is an unrestricted DPS multiplier", id, attribute)
			continue
		}
		if !r.require(contains(ComponentAttributes, attribute),
			"components: %q modifies %q, which the simulation does not read; want one of %v", id, attribute, ComponentAttributes) {
			continue
		}
		r.require(modifier >= ModifierMin && modifier <= ModifierMax,
			"components: %q scales %q by %g, outside the [%g,%g] band crafting is allowed to move an attribute", id, attribute, modifier, ModifierMin, ModifierMax)
		r.require(modifier != 1, "components: %q scales %q by 1, which is no change at all", id, attribute)
		if contains(MagazineAttributes, attribute) {
			r.require(magazines, "components: %q modifies %q, but no %q weapon holds a magazine", id, attribute, component.Blueprint)
		}
		if contains(HandlingAttributes, attribute) {
			r.require(handling, "components: %q modifies %q, but no %q weapon has gunplay handling to change", id, attribute, component.Blueprint)
		}
		if contains(CrystalAttributes, attribute) {
			r.require(component.Kind == "mana_crystal", "components: %q modifies %q, but only a mana crystal may alter spell output", id, attribute)
		}
	}
	if component.Tier > 1 {
		horizontal := false
		for attribute := range component.Modifiers {
			horizontal = horizontal || !contains([]string{AttrSpellDamage, AttrSpellHealing, AttrEffectiveHealth}, attribute)
		}
		r.require(horizontal, "components: %q is above Common but spends no value horizontally", id)
	}
	// An element bias and the element it is biased toward are one choice: a
	// crystal that names a school without favouring it, or favours one without
	// naming it, is a modifier nothing can resolve.
	_, biased := component.Modifiers[AttrElementDamage]
	r.require(biased == (component.Element != ""),
		"components: %q must declare an element and an %q modifier together, or neither", id, AttrElementDamage)
	if component.Element != "" {
		r.require(component.Kind == "mana_crystal", "components: %q names an element but is a %s; only a mana crystal specialises", id, component.Kind)
		r.require(t.Elements[component.Element].Name != "", "components: %q names unknown element %q", id, component.Element)
	}
}

// blueprintHoldsMagazine reports whether any live weapon of the blueprint has a
// magazine, which is what makes the magazine attributes mean anything.
func (t *Tables) blueprintHoldsMagazine(blueprint string) bool {
	for _, weapon := range t.Weapons {
		if weapon.Blueprint == blueprint && weapon.MagazineSize > 0 {
			return true
		}
	}
	return false
}

// blueprintHandles reports whether any live weapon of the blueprint has gunplay
// handling — a recoil pattern, a spread, or a scope — which is what makes the
// handling attributes mean anything.
func (t *Tables) blueprintHandles(blueprint string) bool {
	for _, weapon := range t.Weapons {
		if weapon.Blueprint != blueprint {
			continue
		}
		if len(weapon.Recoil.Pattern) > 0 || weapon.Spread.MovingDegrees > 0 || weapon.Scoped() {
			return true
		}
	}
	return false
}

func (t *Tables) validateMaterials(r *report) {
	tiers := map[int]string{}
	multipliers := map[int]float64{}
	for _, id := range sortedKeys(t.Materials.Grades) {
		grade := t.Materials.Grades[id]
		r.require(grade.Name != "", "materials: grade %q has no name", id)
		r.require(grade.Tier > 0, "materials: grade %q must have a positive tier", id)
		r.require(grade.PowerMultiplier >= 1, "materials: grade %q power_multiplier %g must be at least baseline", id, grade.PowerMultiplier)
		r.require(grade.PowerMultiplier <= 1.45, "materials: grade %q power_multiplier %g exceeds the damage budget of 1.45", id, grade.PowerMultiplier)
		r.require(tiers[grade.Tier] == "", "materials: grades %q and %q share tier %d", tiers[grade.Tier], id, grade.Tier)
		tiers[grade.Tier] = id
		multipliers[grade.Tier] = grade.PowerMultiplier
	}
	for tier := 1; tier <= len(tiers); tier++ {
		r.require(tiers[tier] != "", "materials: rarity tiers are not contiguous at tier %d", tier)
		if tier == 1 {
			r.require(multipliers[tier] == 1, "materials: Common tier multiplier is %g, want baseline 1", multipliers[tier])
			continue
		}
		r.require(multipliers[tier] > multipliers[tier-1], "materials: tier %d power %g does not exceed tier %d power %g", tier, multipliers[tier], tier-1, multipliers[tier-1])
		r.require(multipliers[tier]/multipliers[tier-1] <= 1.26,
			"materials: tier %d step %.3f exceeds the per-tier 1.26 cap", tier, multipliers[tier]/multipliers[tier-1])
	}
	for _, id := range sortedKeys(t.Materials.Kinds) {
		kind := t.Materials.Kinds[id]
		r.require(kind.Name != "", "materials: kind %q has no name", id)
		r.require(contains([]string{"node", "mob", "craft"}, kind.Source), "materials: kind %q has unknown source %q", id, kind.Source)
	}
	for _, id := range sortedKeys(t.Materials.Materials) {
		material := t.Materials.Materials[id]
		r.require(material.Name != "", "materials: %q has no name", id)
		r.require(t.Materials.Grades[material.Grade].Name != "", "materials: %q references unknown grade %q", id, material.Grade)
		kind, ok := t.Materials.Kinds[material.Kind]
		if !r.require(ok, "materials: %q references unknown kind %q", id, material.Kind) {
			continue
		}
		// A crafted material has no natural source, so a recipe has to make it or
		// nothing in the world could ever produce one.
		r.require(kind.Source != "craft" || t.producedByCraft(id),
			"materials: %q is a crafted kind but no ammunition recipe produces it", id)
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
		// A player should be able to name the biome they are standing in without
		// reading the HUD, which world.md makes a requirement rather than a wish:
		// identity arrives through aligned materials, terrain, and palette at
		// once, so a biome with no palette is only two thirds of a biome.
		r.require(biome.Summary != "", "biomes: %q has no summary; the HUD names a region and then has nothing to say about it", id)
		for label, colour := range map[string]string{"ground": biome.Palette.Ground, "accent": biome.Palette.Accent, "haze": biome.Palette.Haze} {
			r.require(isHexColour(colour), "biomes: %q palette %s is %q, want a #rrggbb colour", id, label, colour)
		}
		// Every element needs somewhere to be farmed, and two biomes sharing one
		// would leave another element with nowhere at all.
		for _, other := range sortedKeys(t.Biomes) {
			r.require(other == id || t.Biomes[other].Element != biome.Element,
				"biomes: %q and %q are both %s; each element needs its own region or one element has no geography", id, other, biome.Element)
		}
	}
	for _, element := range sortedKeys(t.Elements) {
		covered := false
		for _, biome := range t.Biomes {
			covered = covered || biome.Element == element
		}
		r.require(covered, "biomes: no region yields %s material; that element's recipes would be unbuildable", element)
	}
}

func isHexColour(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for index := 1; index < len(value); index++ {
		digit := value[index]
		if !(digit >= '0' && digit <= '9') && !(digit >= 'a' && digit <= 'f') && !(digit >= 'A' && digit <= 'F') {
			return false
		}
	}
	return true
}

// honouredDodgeVectors are the counterplay vectors the simulation actually
// delivers. A damaging row may only claim one of these; the ability validation
// below also requires the windup or shared geometry that makes each claim real.
var honouredDodgeVectors = []string{"projectile_travel", "cast_time", "telegraph", "ground_indicator", "scoped_commit"}

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
		armor := effect.Kind == "armor"
		if armor {
			// Armor is mitigation with no pool behind it, so a multiplier of zero
			// would be flat invulnerability for its whole window.
			r.require(effect.DamageMultiplier > 0 && effect.DamageMultiplier < 1,
				"effects: armor %q has damage_multiplier %g, want a fraction between 0 and 1 exclusive; immunity is not a status", id, effect.DamageMultiplier)
		}
		r.require(armor || effect.DamageMultiplier == 0, "effects: %q is a %s but declares armor's damage_multiplier", id, effect.Kind)
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
		t.validateGuard(r, id, ability)
		t.validateBlast(r, id, ability)
		t.validateDeployable(r, id, ability)
		t.validatePlacement(r, id, ability)
		t.validateWall(r, id, ability)
		t.validateBlink(r, id, ability)
		t.validateChain(r, id, ability)
		t.validateCleanse(r, id, ability)
		for _, effect := range ability.SelfEffects {
			r.require(t.Effects[effect].Name != "", "abilities: %q applies unknown self effect %q", id, effect)
		}
		if ability.Telegraph != nil {
			t.validateTelegraph(r, "abilities", id, *ability.Telegraph)
		}
		r.require((ability.WindupMS > 0) == (ability.Telegraph != nil),
			"abilities: %q must declare a positive windup_ms and a telegraph together, or neither", id)
		// Every ability has to actually do something. A row that costs mana and
		// puts nothing into the world is a button that plays no part in a fight.
		r.require(ability.Projectile != nil || ability.Blast != nil || ability.Deployable != nil || ability.Guard != nil ||
			ability.Wall != nil || ability.Blink != nil || ability.Cleanse != nil || len(ability.SelfEffects) > 0,
			"abilities: %q delivers nothing at all", id)
		if !ability.DealsDamage() {
			r.require(ability.DodgeVector == "", "abilities: %q deals no damage, so it must not claim a dodge vector", id)
			r.require(!ability.RequiresScope, "abilities: %q requires a scope but delivers no damage; scoping is a commitment paid for accuracy", id)
			r.require(ability.Chain == nil, "abilities: %q chains a hit that deals no damage", id)
			r.require(!ability.BlinkOnHit, "abilities: %q blinks on a hit it can never land", id)
			if ability.Projectile != nil {
				// A round that deals nothing has to be delivering something else,
				// or it is a body the world carries and nobody ever feels.
				r.require(ability.Deployable != nil || ability.Blast != nil,
					"abilities: %q throws a projectile that deals no damage, leaves nothing behind, and goes off into nothing", id)
				t.validateProjectileShape(r, "abilities", id, ability.Projectile)
				r.require(ability.Projectile.HitscanRange == 0 && ability.Projectile.Pellets == 0 && ability.Projectile.FalloffStart == 0,
					"abilities: %q delivers no damage, so its projectile must not declare hitscan, pellets, or falloff", id)
			}
			continue
		}
		t.validateDamaging(r, "abilities", id, ability)
		if ability.Damaging() {
			band := t.Combat.DamageBands[ability.DamageBand]
			r.require(ability.IntervalMS == band.IntervalMS,
				"abilities: %q interval_ms %d disagrees with damage band %q cadence %d; cadence is band identity",
				id, ability.IntervalMS, ability.DamageBand, band.IntervalMS)
		}
		r.require(contains(honouredDodgeVectors, ability.DodgeVector),
			"abilities: %q claims dodge vector %q, which the simulation does not deliver; only %v are honoured", id, ability.DodgeVector, honouredDodgeVectors)
		if ability.DodgeVector == "cast_time" {
			r.require(ability.WindupMS > 0, "abilities: %q claims cast_time but declares no windup", id)
		}
		if ability.DodgeVector == "telegraph" || ability.DodgeVector == "ground_indicator" {
			r.require(ability.Telegraph != nil, "abilities: %q claims %s but declares no telegraph", id, ability.DodgeVector)
		}
		// A ground indicator is a danger area drawn away from its caster: the
		// warning and the ground it covers are the whole counterplay, so an
		// ability claiming one has to place its delivery somewhere to be avoided.
		if ability.DodgeVector == "ground_indicator" {
			r.require(ability.Placement != nil, "abilities: %q claims ground_indicator but lands on the caster rather than on placed ground", id)
		}
		if ability.DodgeVector == "projectile_travel" {
			r.require(ability.Projectile != nil, "abilities: %q claims projectile_travel but nothing travels", id)
		}
		// Hitscan is the one delivery with no travel to dodge, so it is legal only
		// as the sniper's exception: it must be gated on scoping, and it must say
		// so by claiming the dodge vector that names that commitment.
		hitscan := ability.Projectile != nil && ability.Projectile.HitscanRange > 0
		if ability.DodgeVector == "scoped_commit" {
			r.require(hitscan, "abilities: %q claims scoped_commit but lands nothing instantly; an ordinary travelling shot is dodged by its travel", id)
		}
		if hitscan {
			r.require(ability.RequiresScope, "abilities: %q lands instantly without requiring a scope, so it has no counterplay at all", id)
			r.require(ability.DodgeVector == "scoped_commit", "abilities: %q lands instantly but claims dodge vector %q; only scoped_commit describes that trade", id, ability.DodgeVector)
		}
	}
}

// validateGuard keeps a raised barrier a defensive tool. It blocks a frontal
// arc and slows its user; an ability that both guards and deals damage would
// make the shield free, which is the cost the design pairs it with.
func (t *Tables) validateGuard(r *report, id string, ability Ability) {
	if ability.Guard == nil {
		return
	}
	r.require(ability.Guard.ArcDegrees > 0 && ability.Guard.ArcDegrees < 360,
		"abilities: %q guards %g degrees, want an arc between 0 and 360 exclusive; a full circle blocks everything", id, ability.Guard.ArcDegrees)
	r.require(ability.Guard.MovementMultiplier > 0 && ability.Guard.MovementMultiplier < 1,
		"abilities: %q guards at movement_multiplier %g, want a fraction between 0 and 1 exclusive; raising a shield has to cost mobility", id, ability.Guard.MovementMultiplier)
	r.require(!ability.Damaging(), "abilities: %q both guards and deals damage; a shield locks fire while it is up", id)
	// A barrier with no durability is invulnerability with an arc drawn on it,
	// which is the one thing the design does not allow a defensive tool to be.
	r.require(ability.Guard.Durability > 0,
		"abilities: %q guards with no durability, so it blocks forever; a shield has to be spendable", id)
	r.require(ability.Guard.RegenPerSecond > 0,
		"abilities: %q guards with durability that never recovers, so one fight retires the shield permanently", id)
	r.require(ability.Guard.RegenDelayMS >= 0,
		"abilities: %q guards with regen_delay_ms %d, want zero or more", id, ability.Guard.RegenDelayMS)
}

// validateBlast keeps an area impact attached to something that travels, so the
// area still has the projectile's flight as its dodge vector.
func (t *Tables) validateBlast(r *report, id string, ability Ability) {
	if ability.Blast == nil {
		return
	}
	r.require(ability.Blast.Radius > 0, "abilities: %q declares a blast with no radius", id)
	// A blast either travels to where it lands or is placed on ground the caster
	// warned about. Anything else would go off on top of whoever is standing
	// there with no window to leave it.
	r.require(ability.Projectile != nil || ability.Placement != nil,
		"abilities: %q blasts but neither travels to the impact nor places it on warned ground", id)
	for _, effect := range ability.Blast.Effects {
		r.require(t.Effects[effect].Name != "", "abilities: %q blast applies unknown effect %q", id, effect)
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
	case CostMaterial:
		r.require(cost.Amount >= 1 && cost.Amount == math.Trunc(cost.Amount), "abilities: %q charges %g of a carried material; a stack spends whole units", id, cost.Amount)
		if r.require(cost.Material != "", "abilities: %q spends a material but names none", id) {
			r.require(t.Live("material", cost.Material), "abilities: %q spends unknown material %q", id, cost.Material)
			r.require(t.producedByCraft(cost.Material), "abilities: %q spends %q, which no ammunition recipe produces; special ammunition has to be craftable or the weapon is dead on arrival", id, cost.Material)
		}
	default:
		r.addf("abilities: %q has unknown cost kind %q, want one of %q, %q, %q, %q", id, cost.Kind, CostNone, CostAmmo, CostMana, CostMaterial)
	}
	r.require(cost.Kind == CostMaterial || cost.Material == "", "abilities: %q names a material but spends %q", id, cost.Kind)
}

// producedByCraft reports whether some ammunition recipe yields the material.
func (t *Tables) producedByCraft(material string) bool {
	for _, ammunition := range t.Ammunition {
		if ammunition.Material == material {
			return true
		}
	}
	return false
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
		t.validateRoles(r, "spells", id, spell.Roles)
		// A drawn starter kit has to be a legal loadout the moment it is rolled.
		// Tier 1 is the only tier that needs no same-element company beside it, so
		// it is the only tier the draw may reach into.
		r.require(!spell.Starter || spell.Tier == 1,
			"spells: %q is tier %d and in the starter draw; a drawn kit has to satisfy affinity without the player arranging it", id, spell.Tier)
	}
	// Grid completeness — every element authored to tier 4, so affinity's 4 + 2
	// build is satisfiable — is a claim about the shipped content rather than a
	// structural rule, and `TestShippedSpellGridIsComplete` holds it. Requiring
	// it here would make the loader refuse any deployment or fixture that ships
	// a narrower spell table.
}

// validateGadgets mirrors validateSpells: a gadget is identity — name, class,
// unlock — over one ability, and every combat value it has lives on that
// ability row.
func (t *Tables) validateGadgets(r *report) {
	for _, id := range sortedKeys(t.Gadgets) {
		gadget := t.Gadgets[id]
		r.require(gadget.Name != "", "gadgets: %q has no name", id)
		r.require(gadget.Class == "gunslinger", "gadgets: %q has class %q; gadgets are the Gunslinger's slot kind and the Mage's are spells", id, gadget.Class)
		r.require(t.Abilities[gadget.Ability].Name != "", "gadgets: %q references unknown ability %q", id, gadget.Ability)
		t.validateRoles(r, "gadgets", id, gadget.Roles)
	}
}

func (t *Tables) validateRoles(r *report, table, id string, roles []string) {
	r.require(len(roles) > 0, "%s: %q declares no combat roles", table, id)
	seen := map[string]bool{}
	for _, role := range roles {
		r.require(contains(t.Combat.Roles, role), "%s: %q has unknown combat role %q", table, id, role)
		r.require(!seen[role], "%s: %q repeats combat role %q", table, id, role)
		seen[role] = true
	}
}

func (t *Tables) validateWeapons(r *report) {
	starters := map[string]int{}
	for _, id := range sortedKeys(t.Weapons) {
		weapon := t.Weapons[id]
		r.require(weapon.Name != "", "weapons: %q has no name", id)
		r.require(weapon.Class == "gunslinger" || weapon.Class == "mage", "weapons: %q has unknown class %q", id, weapon.Class)
		r.require(weapon.Category != "", "weapons: %q has no category", id)
		r.require(t.Components.Blueprints[weapon.Blueprint].Name != "", "weapons: %q references unknown blueprint %q", id, weapon.Blueprint)
		t.validateRoles(r, "weapons", id, weapon.Roles)
		// The basic set is a pool, not one row: a new character draws one of it.
		// A drawn weapon has to be usable as it is drawn, so it may never be one
		// the economy withholds until it has been built.
		if weapon.Starter {
			starters[weapon.Class]++
			r.require(!weapon.RequiresCraft, "weapons: %q is in the basic set but may only be carried as a crafted instance, so a new character would draw an unusable weapon", id)
		}
		for _, material := range sortedKeys(weapon.Cost) {
			r.require(weapon.Cost[material] > 0, "weapons: %q costs a non-positive count of %q", id, material)
			r.require(t.Live("material", material), "weapons: %q costs unknown material %q", id, material)
		}
		// Withholding the stock configuration is only a gate if building it costs
		// something; without a cost it would just be an unlock that reads oddly.
		r.require(!weapon.RequiresCraft || len(weapon.Cost) > 0,
			"weapons: %q may only be carried as a crafted instance but costs no materials, so nothing gates it", id)
		t.validateGunplay(r, id, weapon)
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
		// An ability that requires a scope needs a weapon that has one, or the
		// commitment its counterplay rests on could never be paid.
		r.require(!ability.RequiresScope || weapon.Scoped(),
			"weapons: %q fires %q, which requires a scope, but declares none", id, weapon.Ability)
		// Special ammunition is the exception to the magazine: a launcher spends a
		// carried, crafted round and so has neither a magazine nor a reload.
		if ability.Cost.Kind == CostMaterial {
			r.require(weapon.MagazineSize == 0 && weapon.ReloadMS == 0,
				"weapons: %q fires crafted ammunition, so it must not also declare a magazine or a reload", id)
			continue
		}
		r.require(weapon.MagazineSize > 0, "weapons: %q must declare a positive magazine_size", id)
		r.require(weapon.ReloadMS > 0, "weapons: %q must declare a positive reload_ms", id)
		// The magazine is the weapon's, the round spent is the ability's: they
		// must agree, or firing would drain a resource the weapon does not hold.
		r.require(ability.Cost.Kind == CostAmmo, "weapons: %q holds a magazine but its ability %q spends %q", id, weapon.Ability, ability.Cost.Kind)
		r.require(ability.Cost.Amount <= float64(weapon.MagazineSize), "weapons: %q holds %d rounds but its ability %q spends %g per use", id, weapon.MagazineSize, weapon.Ability, ability.Cost.Amount)
	}
	r.require(starters["gunslinger"] > 0, "weapons: no starter weapon for gunslinger; a new character would be unarmed")
	r.require(starters["mage"] > 0, "weapons: no starter weapon for mage; a new character would be unarmed")
}

func (t *Tables) validateRoleCoverage(r *report) {
	coverage := map[string]map[string]bool{"gunslinger": {}, "mage": {}}
	for _, weapon := range t.Weapons {
		classCoverage, ok := coverage[weapon.Class]
		if !ok {
			continue
		}
		for _, role := range weapon.Roles {
			classCoverage[role] = true
		}
	}
	for _, gadget := range t.Gadgets {
		classCoverage, ok := coverage[gadget.Class]
		if !ok {
			continue
		}
		for _, role := range gadget.Roles {
			classCoverage[role] = true
		}
	}
	for _, spell := range t.Spells {
		for _, role := range spell.Roles {
			coverage["mage"][role] = true
		}
	}
	for _, class := range []string{"gunslinger", "mage"} {
		for _, role := range t.Combat.Roles {
			r.require(coverage[class][role], "combat: %s content does not cover the %q role", class, role)
		}
	}
}

const (
	maxDamageMultiplier          = 1.45
	maxEffectiveHealthMultiplier = 1.38
	maxSingleItemPower           = 4.0 / 3.0
	minimumRawTTKSeconds         = 2.0
)

// validateVerticalBudget enumerates every legal recipe arrangement. Bounds are
// enforced after modifiers combine, because five individually modest parts can
// otherwise stack into an immodest weapon.
func (t *Tables) validateVerticalBudget(r *report) {
	for _, weaponID := range sortedKeys(t.Components.Recipes) {
		weapon := t.Weapons[weaponID]
		t.eachRecipeBuild(weaponID, func(parts map[string]string) {
			if weapon.Blueprint == "staff" {
				crystal := t.Components.Components[parts["crystal"]]
				stave := t.Components.Components[parts["stave"]]
				if stave.Tier < crystal.Tier {
					return
				}
			}
			damage, effectiveHealth := t.assembledPower(parts, "")
			if weapon.Blueprint == "staff" {
				for element := range t.Elements {
					candidate, defense := t.assembledPower(parts, element)
					damage = math.Max(damage, candidate)
					effectiveHealth = math.Max(effectiveHealth, defense)
				}
			}
			label := fmt.Sprintf("%s with %v", weaponID, parts)
			r.require(damage <= maxDamageMultiplier+1e-9, "vertical budget: %s damage multiplier %.3f exceeds %.2f", label, damage, maxDamageMultiplier)
			r.require(effectiveHealth <= maxEffectiveHealthMultiplier+1e-9, "vertical budget: %s effective-health multiplier %.3f exceeds %.2f", label, effectiveHealth, maxEffectiveHealthMultiplier)
			r.require(damage*effectiveHealth <= maxSingleItemPower+1e-9,
				"vertical budget: %s combined power %.3f exceeds one item's %.3f share", label, damage*effectiveHealth, maxSingleItemPower)
			if weapon.Blueprint == "staff" {
				for _, spell := range t.Spells {
					ability := t.Abilities[spell.Ability]
					if !ability.Damaging() {
						continue
					}
					ability.DamageMultiplier = damage
					r.require(t.ResolveDamage(ability, t.Entities["player"].MaxHealth).RawTTK.Seconds() >= minimumRawTTKSeconds,
						"vertical budget: %s pulls %s raw TTK below %.1fs", label, spell.ID, minimumRawTTKSeconds)
				}
			} else if ability, ok := t.Abilities[weapon.Ability]; ok && ability.Damaging() {
				ability.DamageMultiplier = damage
				r.require(t.ResolveDamage(ability, t.Entities["player"].MaxHealth).RawTTK.Seconds() >= minimumRawTTKSeconds,
					"vertical budget: %s pulls raw TTK below %.1fs", label, minimumRawTTKSeconds)
			}
		})
	}
}

func (t *Tables) assembledPower(parts map[string]string, element string) (damage, effectiveHealth float64) {
	damage, effectiveHealth = t.RarityMultiplier(parts), 1
	for _, componentID := range parts {
		component := t.Components.Components[componentID]
		if modifier := component.Modifiers[AttrSpellDamage]; modifier != 0 {
			damage *= modifier
		}
		if component.Element == element {
			if modifier := component.Modifiers[AttrElementDamage]; modifier != 0 {
				damage *= modifier
			}
		}
		if modifier := component.Modifiers[AttrEffectiveHealth]; modifier != 0 {
			effectiveHealth *= modifier
		}
		if modifier := component.Modifiers[AttrSpellHealing]; modifier != 0 {
			effectiveHealth *= modifier
		}
	}
	return damage, effectiveHealth
}

func (t *Tables) eachRecipeBuild(weaponID string, visit func(map[string]string)) {
	recipe := t.Components.Recipes[weaponID]
	slots := t.Components.Blueprints[recipe.Blueprint].Slots
	parts := make(map[string]string, len(slots))
	var fill func(int)
	fill = func(index int) {
		if index == len(slots) {
			copy := make(map[string]string, len(parts))
			for slot, component := range parts {
				copy[slot] = component
			}
			visit(copy)
			return
		}
		slot := slots[index]
		for _, component := range recipe.Slots[slot] {
			parts[slot] = component
			fill(index + 1)
		}
	}
	fill(0)
}

// validateGunplay keeps a gun's handling coherent. Every gun declares the weight
// class its recoil and spread are scaled by, a recoil pattern that actually
// walks the muzzle, and a spread that costs more moving than standing — the
// accuracy-against-mobility trade the whole class is built on. A staff declares
// none of it and must not claim any.
func (t *Tables) validateGunplay(r *report, id string, weapon Weapon) {
	gun := weapon.Ability != ""
	if !gun {
		r.require(weapon.Weight == "" && len(weapon.Recoil.Pattern) == 0 && weapon.Spread == (Spread{}) && !weapon.Scoped(),
			"weapons: %q casts a spell, so it has no recoil, spread, weight, or scope", id)
		return
	}
	if r.require(weapon.Weight != "", "weapons: %q declares no weight class; weight is what balances a gun's handling", id) {
		r.require(t.Combat.WeightClasses[weapon.Weight].Name != "", "weapons: %q references unknown weight class %q", id, weapon.Weight)
	}
	if r.require(len(weapon.Recoil.Pattern) > 0, "weapons: %q declares no recoil pattern; every gun kicks", id) {
		r.require(weapon.Recoil.RecoveryMS > 0, "weapons: %q has a recoil pattern that never recovers, so it can only ever walk further off aim", id)
		walks := false
		for index, degrees := range weapon.Recoil.Pattern {
			r.require(math.Abs(degrees) < 90, "weapons: %q recoil step %d throws the shot %g degrees off aim, which is not a gun", id, index, degrees)
			walks = walks || degrees != 0
		}
		r.require(walks, "weapons: %q has a recoil pattern of nothing but zeroes, which is no recoil at all", id)
	}
	r.require(weapon.Spread.StandingDegrees >= 0 && weapon.Spread.MovingDegrees >= 0, "weapons: %q declares a negative spread", id)
	r.require(weapon.Spread.MovingDegrees > weapon.Spread.StandingDegrees,
		"weapons: %q spreads %g degrees standing and %g moving; firing on the move has to cost accuracy", id, weapon.Spread.StandingDegrees, weapon.Spread.MovingDegrees)
	if weapon.Scoped() {
		scope := weapon.Scope
		r.require(scope.MovementMultiplier > 0 && scope.MovementMultiplier < 1,
			"weapons: %q scopes at movement_multiplier %g, want a fraction between 0 and 1 exclusive; the scope is a committed vulnerability", id, scope.MovementMultiplier)
		r.require(scope.SpreadMultiplier > 0 && scope.SpreadMultiplier < 1,
			"weapons: %q scopes at spread_multiplier %g, want a fraction between 0 and 1 exclusive; scoping that does not steady the shot buys nothing", id, scope.SpreadMultiplier)
		r.require(scope.ViewBonus > 0, "weapons: %q scopes without seeing any further, so the peripheral blackout costs the user everything and returns nothing", id)
	}
}

// validateAmmunition keeps every crafted round buildable and spendable: it must
// yield a real material, cost real materials, and belong to a class.
func (t *Tables) validateAmmunition(r *report) {
	for _, id := range sortedKeys(t.Ammunition) {
		ammunition := t.Ammunition[id]
		r.require(ammunition.Name != "", "ammunition: %q has no name", id)
		r.require(ammunition.Class == "gunslinger" || ammunition.Class == "mage", "ammunition: %q has unknown class %q", id, ammunition.Class)
		r.require(ammunition.Count > 0, "ammunition: %q yields a non-positive count", id)
		if r.require(ammunition.Material != "", "ammunition: %q produces no material", id) {
			r.require(t.Live("material", ammunition.Material), "ammunition: %q produces unknown material %q", id, ammunition.Material)
			r.require(t.Materials.Kinds[t.Materials.Materials[ammunition.Material].Kind].Source == "craft",
				"ammunition: %q produces %q, whose kind is not crafted; a round a node also yields is not finite", id, ammunition.Material)
		}
		// A free round would put an infinite resource behind a weapon whose whole
		// balance is that its ammunition runs out.
		if r.require(len(ammunition.Cost) > 0, "ammunition: %q costs nothing, so the round it makes is not finite", id) {
			for _, material := range sortedKeys(ammunition.Cost) {
				r.require(ammunition.Cost[material] > 0, "ammunition: %q costs a non-positive count of %q", id, material)
				r.require(t.Live("material", material), "ammunition: %q costs unknown material %q", id, material)
				r.require(material != ammunition.Material, "ammunition: %q costs the very material it produces", id)
			}
		}
	}
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
func (t *Tables) validateDamaging(r *report, table, id string, ability Ability) {
	band, dodge, projectile := ability.DamageBand, ability.DodgeVector, ability.Projectile
	if ability.Damaging() {
		r.require(t.Combat.DamageBands[band].Name != "", "%s: %q references unknown damage band %q", table, id, band)
	}
	r.require(contains(t.Combat.DodgeVectors, dodge), "%s: %q declares dodge vector %q, which is not a recognised counterplay vector", table, id, dodge)
	if projectile == nil {
		// Damage that never travels has to be placed and warned about instead:
		// the telegraph is the whole dodge window, and without one this would be
		// point-and-click damage — the one thing the invariant refuses.
		r.require(ability.Telegraph != nil && ability.WindupMS > 0,
			"%s: %q deals damage without anything travelling to carry it, and shows no telegraph; that is instant point-and-click damage", table, id)
		return
	}
	t.validateProjectileShape(r, table, id, projectile)
	r.require(projectile.Pellets >= 0, "%s: %q cannot fire a negative number of pellets", table, id)
	r.require((projectile.PelletCount() > 1) == (projectile.PelletSpreadDegrees > 0),
		"%s: %q must declare pellets and pellet_spread_degrees together, or neither: one pellet has no cone and a cone needs pellets to fill it", table, id)
	r.require(projectile.PelletSpreadDegrees < 360, "%s: %q spreads pellets over %g degrees, which is not a cone", table, id, projectile.PelletSpreadDegrees)
	r.require(projectile.MaxRange >= 0 && projectile.FalloffStart >= 0 && projectile.HitscanRange >= 0,
		"%s: %q declares a negative range", table, id)
	if projectile.FalloffStart > 0 {
		r.require(projectile.MaxRange > projectile.FalloffStart,
			"%s: %q starts falloff at %g but has no max_range beyond it to decay over", table, id, projectile.FalloffStart)
		r.require(projectile.FalloffMin > 0 && projectile.FalloffMin < 1,
			"%s: %q has falloff_min %g, want a fraction between 0 and 1 exclusive; a shot that decays to nothing is a range limit, not falloff", table, id, projectile.FalloffMin)
	}
	r.require(projectile.FalloffStart > 0 || projectile.FalloffMin == 0,
		"%s: %q declares a falloff_min with no falloff_start, so nothing ever decays", table, id)
	if projectile.HitscanRange > 0 {
		r.require(projectile.MaxRange > projectile.HitscanRange,
			"%s: %q lands instantly to %g and has no travelling range past it; a hitscan cap with nothing beyond it is unlimited instant damage", table, id, projectile.HitscanRange)
	}
}

// validateProjectileKinds keeps kinds unique across every table, because a
// snapshot carries only the kind and the renderer resolves the silhouette
// from it.
// validateProjectileShape checks what every travelling body owes regardless of
// what it delivers: an identity the renderer can draw it from, travel the target
// can react to, and a body large enough to resolve against.
func (t *Tables) validateProjectileShape(r *report, table, id string, projectile *Projectile) {
	r.require(projectile.Kind != "", "%s: %q has a projectile with no kind", table, id)
	r.require(projectile.Speed > 0, "%s: %q has a projectile with no travel speed; instant damage has no dodge vector", table, id)
	r.require(projectile.LifeSeconds > 0, "%s: %q has a projectile with no lifetime", table, id)
	r.require(projectile.Radius > 0, "%s: %q has a projectile with no radius", table, id)
	r.require(projectile.Silhouette != "", "%s: %q has a projectile with no silhouette for the renderer", table, id)
	if projectile.Homing != nil {
		// A round that turns instantly is a lock-on with no dodge left in it: the
		// turn rate is the whole counterplay, so it has to be bounded.
		r.require(projectile.Homing.TurnDegreesPerSecond > 0 && projectile.Homing.TurnDegreesPerSecond <= 360,
			"%s: %q homes at %g degrees per second, want a rate above zero and no more than a full turn; a round that turns freely cannot be dodged",
			table, id, projectile.Homing.TurnDegreesPerSecond)
		r.require(projectile.Homing.AcquireRange > 0, "%s: %q homes but looks for nothing", table, id)
		r.require(projectile.HitscanRange == 0, "%s: %q both lands instantly and steers", table, id)
	}
}

// validateDeployable keeps a persistent field coherent: it materialises as an
// entity archetype the world knows how to build, it covers ground, it expires,
// and it leaves a gap close enough in that a body standing in its own cloud can
// still see what it is touching.
func (t *Tables) validateDeployable(r *report, id string, ability Ability) {
	if ability.Deployable == nil {
		return
	}
	field := ability.Deployable
	if definition, ok := t.Entities[field.Kind]; r.require(ok, "abilities: %q deploys %q, which is not an entity archetype", id, field.Kind) {
		// A cloud changes what can be seen, never where a body may walk: it is a
		// field, and a field with geometry would be a wall nobody authored.
		r.require(len(definition.CollisionObjects) == 0,
			"abilities: %q deploys %q, which has collision geometry; a deployable field blocks vision, not movement", id, field.Kind)
	}
	r.require(field.Radius > 0, "abilities: %q deploys a field with no radius", id)
	r.require(field.DurationMS > 0, "abilities: %q deploys a field that never expires", id)
	r.require(field.RevealRadius >= 0 && field.RevealRadius < field.Radius,
		"abilities: %q reveals at %g inside a %g field; the gap has to be smaller than the cloud", id, field.RevealRadius, field.Radius)
	// A reveal gap only means something to a field that hides anything.
	r.require(field.Conceals || field.RevealRadius == 0,
		"abilities: %q deploys a field that hides nothing but declares a reveal radius", id)
	// A field is either thrown to where it lands or placed on ground the caster
	// pointed at. Both give the target the same thing: somewhere the field is not.
	r.require(ability.Projectile != nil || ability.Placement != nil,
		"abilities: %q deploys a field but neither travels to where it lands nor places it", id)
	r.require(!ability.Damaging(), "abilities: %q both deploys a field and deals damage on impact; a deployable is placed, not landed on someone", id)
	r.require(ability.CooldownMS > 0, "abilities: %q deploys a field with no cooldown, so one body could cover the world in them", id)
	// A pulse is priced against the shared band like every other damage source,
	// and it needs a cadence to run at.
	if field.DamageBand != "" || field.DamageFraction > 0 {
		r.require(t.Combat.DamageBands[field.DamageBand].Name != "", "abilities: %q deploys a field referencing unknown damage band %q", id, field.DamageBand)
		r.require(field.DamageFraction > 0, "abilities: %q deploys a damaging field with no damage_fraction of its band", id)
	}
	r.require((field.TickMS > 0) == (field.DamageBand != "" || len(field.Effects) > 0),
		"abilities: %q must declare a field tick_ms together with what the pulse does, or neither", id)
	for _, effect := range append(append([]string(nil), field.Effects...), field.FinalEffects...) {
		r.require(t.Effects[effect].Name != "", "abilities: %q deploys a field applying unknown effect %q", id, effect)
	}
	// A trap that never does anything is furniture, and a closing pulse on a
	// field that does not pulse has nothing to close.
	r.require(!field.Trigger || field.Pulses(), "abilities: %q deploys a trap that does nothing when it is triggered", id)
	r.require(len(field.FinalEffects) == 0 || field.Pulses(), "abilities: %q deploys a field with a closing pulse but no pulse", id)
	r.require(!field.Trigger || len(field.FinalEffects) == 0, "abilities: %q deploys a trap with a closing pulse; a trap resolves once, on the body that springs it", id)
}

// validatePlacement keeps a placed cast reachable and warned about. Range zero
// is legal and means the caster's own feet, which is where a self-centred aura
// or shockwave lands.
func (t *Tables) validatePlacement(r *report, id string, ability Ability) {
	if ability.Placement == nil {
		return
	}
	r.require(ability.Placement.Range >= 0, "abilities: %q places its cast at a negative range", id)
	r.require(ability.Blast != nil || ability.Deployable != nil || ability.Wall != nil,
		"abilities: %q places a point in the world but puts nothing there", id)
}

// validateWall keeps player-authored terrain a spell rather than a fixture: it
// materialises segments of a real archetype, they are destructible, and they
// stand for a bounded time.
func (t *Tables) validateWall(r *report, id string, ability Ability) {
	if ability.Wall == nil {
		return
	}
	wall := ability.Wall
	if definition, ok := t.Entities[wall.Kind]; r.require(ok, "abilities: %q raises %q, which is not an entity archetype", id, wall.Kind) {
		r.require(len(definition.CollisionObjects) > 0, "abilities: %q raises %q, which has no collision geometry; a wall that blocks nothing is not a wall", id, wall.Kind)
		// An indestructible wall is a permanent safe corner. It has to be
		// answerable by shooting it, exactly like a tree.
		r.require(definition.MaxHealth > 0, "abilities: %q raises %q, which cannot be destroyed; a placed wall has to be breakable", id, wall.Kind)
	}
	r.require(wall.Segments >= 1, "abilities: %q raises a wall of no segments", id)
	r.require(wall.Spacing > 0, "abilities: %q raises wall segments with no spacing", id)
	r.require(wall.DurationMS > 0, "abilities: %q raises a wall that never expires", id)
	r.require(ability.Placement != nil, "abilities: %q raises a wall but places it nowhere", id)
	r.require(ability.CooldownMS > 0, "abilities: %q raises a wall with no cooldown", id)
	r.require(!ability.DealsDamage(), "abilities: %q both raises a wall and deals damage; terrain is cover, not a weapon", id)
}

func (t *Tables) validateBlink(r *report, id string, ability Ability) {
	if ability.Blink == nil {
		return
	}
	r.require(ability.Blink.Distance > 0, "abilities: %q blinks nowhere", id)
	// Mobility this direct is a defining tool, so it is cooldown-gated rather
	// than mana-gated: spamming it would erase the spacing game entirely.
	r.require(ability.CooldownMS > 0, "abilities: %q blinks with no cooldown", id)
}

func (t *Tables) validateChain(r *report, id string, ability Ability) {
	if ability.Chain == nil {
		return
	}
	r.require(ability.Chain.Jumps >= 1, "abilities: %q chains to nobody", id)
	r.require(ability.Chain.Range > 0, "abilities: %q chains over no distance", id)
}

func (t *Tables) validateCleanse(r *report, id string, ability Ability) {
	if ability.Cleanse == nil {
		return
	}
	r.require(ability.Cleanse.Radius > 0, "abilities: %q strips effects over no area", id)
	r.require(ability.Cleanse.ManaPerEffect >= 0, "abilities: %q returns negative mana per effect stripped", id)
	r.require(ability.CooldownMS > 0, "abilities: %q strips effects with no cooldown", id)
	r.require(!ability.DealsDamage(), "abilities: %q both strips effects and deals damage", id)
}

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
