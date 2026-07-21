// Package tuning parses and validates the versioned balance tables embedded
// from data/tuning. Nothing in the simulation authors balance values: every
// number below is read from a table row, so editing one row changes every item
// that references it without touching a persisted character.
package tuning

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"sync"
	"time"

	"spellfire/data"
)

// SchemaVersion is the table shape this build understands. Bump it only when a
// table changes shape, and add the matching forward migration; a plain balance
// edit bumps Manifest.Version instead and needs no code change.
const SchemaVersion = 1

type Manifest struct {
	// Version is the content revision. Bump it on any balance edit; a change
	// is what entitles characters to the global respec/refund.
	Version       int `json:"version"`
	SchemaVersion int `json:"schema_version"`
}

type Simulation struct {
	TickRate             int     `json:"tick_rate"`
	SendRate             int     `json:"send_rate"`
	AOIRadius            float64 `json:"aoi_radius"`
	MaxRewindMS          int     `json:"max_rewind_ms"`
	InterpolationDelayMS int     `json:"interpolation_delay_ms"`
}

func (s Simulation) MaxRewind() time.Duration {
	return time.Duration(s.MaxRewindMS) * time.Millisecond
}

type DangerBand struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Tier          int     `json:"tier"`
	OuterRadius   float64 `json:"outer_radius"`
	MaterialGrade string  `json:"material_grade"`
	PvP           string  `json:"pvp"`
	Shape         string  `json:"shape"`
	Summary       string  `json:"summary"`
}

// Trees drives the deterministic procedural cover generator.
type Trees struct {
	Count        int     `json:"count"`
	Seed         uint64  `json:"seed"`
	MinRadius    float64 `json:"min_radius"`
	RadiusSpread float64 `json:"radius_spread"`
	InnerMargin  float64 `json:"inner_margin"`
	OuterMargin  float64 `json:"outer_margin"`
	Spacing      float64 `json:"spacing"`
}

type World struct {
	Radius      float64      `json:"radius"`
	SpawnRadius float64      `json:"spawn_radius"`
	DangerBands []DangerBand `json:"danger_bands"`
	Trees       Trees        `json:"trees"`
}

// BandAt resolves the danger band containing a distance from the world origin.
// Distances past the rim resolve to the outermost band.
func (w World) BandAt(distance float64) DangerBand {
	for _, band := range w.DangerBands {
		if distance <= band.OuterRadius {
			return band
		}
	}
	return w.DangerBands[len(w.DangerBands)-1]
}

// SafeRadius is the outer edge of the fully safe centre: crafting, respawn, and
// loadout mutation. PvPRadius is the outer edge of PvP protection, which
// extends through the restricted fringe.
func (w World) SafeRadius() float64 { return w.outerRadiusWhile("off") }
func (w World) PvPRadius() float64  { return w.outerRadiusWhile("off", "restricted") }

func (w World) outerRadiusWhile(states ...string) float64 {
	radius := 0.0
	for _, band := range w.DangerBands {
		matched := false
		for _, state := range states {
			matched = matched || band.PvP == state
		}
		if !matched {
			break
		}
		radius = band.OuterRadius
	}
	return radius
}

type PlayerBody struct {
	Radius    float64 `json:"radius"`
	Speed     float64 `json:"speed"`
	MaxHealth float64 `json:"max_health"`
	MaxMana   float64 `json:"max_mana"`
	ManaRegen float64 `json:"mana_regen"`
}

type Dash struct {
	Distance   float64 `json:"distance"`
	DurationMS int     `json:"duration_ms"`
	CooldownMS int     `json:"cooldown_ms"`
}

func (d Dash) Duration() time.Duration { return time.Duration(d.DurationMS) * time.Millisecond }
func (d Dash) Cooldown() time.Duration { return time.Duration(d.CooldownMS) * time.Millisecond }

// DamageBand is the shared row every damaging item points at. Per-hit damage
// lives here rather than on the item so the compressed power band cannot drift
// item by item.
type DamageBand struct {
	ID                  string  `json:"-"`
	Name                string  `json:"name"`
	DamagePerHit        float64 `json:"damage_per_hit"`
	TargetTTKSeconds    float64 `json:"target_ttk_seconds"`
	TTKToleranceSeconds float64 `json:"ttk_tolerance_seconds"`
}

type Combat struct {
	Roles        []string              `json:"roles"`
	DodgeVectors []string              `json:"dodge_vectors"`
	Player       PlayerBody            `json:"player"`
	Dash         Dash                  `json:"dash"`
	DamageBands  map[string]DamageBand `json:"damage_bands"`
}

type Element struct {
	ID          string `json:"-"`
	Name        string `json:"name"`
	PrimaryRole string `json:"primary_role"`
	Secondary   string `json:"secondary"`
	Character   string `json:"character"`
}

// Projectile is the delivery shape of an attack. Kind is unique across every
// table so the renderer can look a silhouette up from a snapshot alone.
type Projectile struct {
	Kind        string  `json:"kind"`
	Speed       float64 `json:"speed"`
	LifeSeconds float64 `json:"life_seconds"`
	Radius      float64 `json:"radius"`
	Silhouette  string  `json:"silhouette"`
}

// Weapon is a craftable blueprint instance. A magazine weapon carries its own
// projectile; a staff carries none and delegates to the spell it casts.
type Weapon struct {
	ID             string      `json:"-"`
	Name           string      `json:"name"`
	Class          string      `json:"class"`
	Blueprint      string      `json:"blueprint"`
	Category       string      `json:"category"`
	Starter        bool        `json:"starter"`
	DamageBand     string      `json:"damage_band"`
	FireIntervalMS int         `json:"fire_interval_ms"`
	MagazineSize   int         `json:"magazine_size"`
	ReloadMS       int         `json:"reload_ms"`
	Spell          string      `json:"spell"`
	Projectile     *Projectile `json:"projectile"`
}

func (w Weapon) ReloadDuration() time.Duration {
	return time.Duration(w.ReloadMS) * time.Millisecond
}

type Spell struct {
	ID             string      `json:"-"`
	Name           string      `json:"name"`
	Element        string      `json:"element"`
	Tier           int         `json:"tier"`
	Starter        bool        `json:"starter"`
	DamageBand     string      `json:"damage_band"`
	ManaCost       float64     `json:"mana_cost"`
	CastIntervalMS int         `json:"cast_interval_ms"`
	CooldownMS     int         `json:"cooldown_ms"`
	DodgeVector    string      `json:"dodge_vector"`
	Projectile     *Projectile `json:"projectile"`
}

type Blueprint struct {
	ID    string   `json:"-"`
	Name  string   `json:"name"`
	Slots []string `json:"slots"`
}

// Component fills one blueprint slot. Effects are behavioural; a component may
// never carry a damage band, which is what keeps crafting out of the power axis.
type Component struct {
	ID        string `json:"-"`
	Name      string `json:"name"`
	Blueprint string `json:"blueprint"`
	Slot      string `json:"slot"`
	Effect    string `json:"effect"`
}

type Components struct {
	Blueprints map[string]Blueprint `json:"blueprints"`
	Components map[string]Component `json:"components"`
}

type Grade struct {
	ID   string `json:"-"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
}

type MaterialKind struct {
	ID        string `json:"-"`
	Name      string `json:"name"`
	Universal bool   `json:"universal"`
	Source    string `json:"source"`
	Summary   string `json:"summary"`
}

type Material struct {
	ID    string `json:"-"`
	Name  string `json:"name"`
	Grade string `json:"grade"`
	Kind  string `json:"kind"`
	Biome string `json:"biome"`
}

type Materials struct {
	Grades    map[string]Grade        `json:"grades"`
	Kinds     map[string]MaterialKind `json:"kinds"`
	Materials map[string]Material     `json:"materials"`
}

type Mob struct {
	ID          string `json:"-"`
	Name        string `json:"name"`
	Family      string `json:"family"`
	Silhouette  string `json:"silhouette"`
	DamageBand  string `json:"damage_band"`
	DodgeVector string `json:"dodge_vector"`
	Turrets     int    `json:"turrets"`
	Behavior    string `json:"behavior"`
}

type Biome struct {
	ID      string `json:"-"`
	Name    string `json:"name"`
	Element string `json:"element"`
}

type Tables struct {
	Manifest   Manifest
	Simulation Simulation
	World      World
	Combat     Combat
	Elements   map[string]Element
	Weapons    map[string]Weapon
	Spells     map[string]Spell
	Components Components
	Materials  Materials
	Mobs       map[string]Mob
	Biomes     map[string]Biome
}

// Shot is a weapon's resolved firing profile. Damage always comes from the
// shared band row, and a staff resolves through the spell it casts, so the
// simulation reads one shape for both classes instead of branching on class.
type Shot struct {
	Interval   time.Duration
	Damage     float64
	ManaCost   float64
	Projectile Projectile
}

// Shot resolves a weapon into the profile the simulation fires with. It reports
// false only for a weapon whose references no longer resolve, which validation
// rejects at load.
func (t *Tables) Shot(weapon Weapon) (Shot, bool) {
	band, projectile, interval, mana := weapon.DamageBand, weapon.Projectile, weapon.FireIntervalMS, 0.0
	if weapon.Spell != "" {
		spell, ok := t.Spells[weapon.Spell]
		if !ok {
			return Shot{}, false
		}
		band, projectile, interval, mana = spell.DamageBand, spell.Projectile, spell.CastIntervalMS, spell.ManaCost
	}
	damage, ok := t.Combat.DamageBands[band]
	if !ok || projectile == nil {
		return Shot{}, false
	}
	return Shot{
		Interval:   time.Duration(interval) * time.Millisecond,
		Damage:     damage.DamagePerHit,
		ManaCost:   mana,
		Projectile: *projectile,
	}, true
}

// StarterWeapon returns the weapon a freshly created character of the class
// carries. Validation guarantees exactly one per class.
func (t *Tables) StarterWeapon(class string) (Weapon, bool) {
	for _, id := range sortedKeys(t.Weapons) {
		if weapon := t.Weapons[id]; weapon.Starter && weapon.Class == class {
			return weapon, true
		}
	}
	return Weapon{}, false
}

var load = sync.OnceValues(func() (*Tables, error) { return Parse(data.Tuning) })

// Load parses and validates the embedded tables once per process.
func Load() (*Tables, error) { return load() }

// MustLoad panics on invalid embedded tables. The data ships inside the binary,
// so a failure here is a build defect, not a runtime condition.
func MustLoad() *Tables {
	tables, err := Load()
	if err != nil {
		panic(fmt.Sprintf("tuning: %v", err))
	}
	return tables
}

// Parse reads the tables from any filesystem laid out like data/tuning. Tests
// use it to load an edited copy of the shipped tables.
func Parse(fsys fs.FS) (*Tables, error) {
	tables := &Tables{}
	files := []struct {
		name   string
		target any
	}{
		{"manifest.json", &tables.Manifest},
		{"simulation.json", &tables.Simulation},
		{"world.json", &tables.World},
		{"combat.json", &tables.Combat},
		{"elements.json", &tables.Elements},
		{"weapons.json", &tables.Weapons},
		{"spells.json", &tables.Spells},
		{"components.json", &tables.Components},
		{"materials.json", &tables.Materials},
		{"mobs.json", &tables.Mobs},
		{"biomes.json", &tables.Biomes},
	}
	for _, file := range files {
		raw, err := fs.ReadFile(fsys, "tuning/"+file.name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.name, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(file.target); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file.name, err)
		}
	}
	tables.stampIDs()
	if err := tables.validate(); err != nil {
		return nil, err
	}
	return tables, nil
}

// stampIDs copies each row's map key onto the row so callers can pass a row
// around without also carrying its identifier. Danger bands are an ordered
// array and carry their own id field instead.
func (t *Tables) stampIDs() {
	for id, band := range t.Combat.DamageBands {
		band.ID = id
		t.Combat.DamageBands[id] = band
	}
	for id, element := range t.Elements {
		element.ID = id
		t.Elements[id] = element
	}
	for id, weapon := range t.Weapons {
		weapon.ID = id
		t.Weapons[id] = weapon
	}
	for id, spell := range t.Spells {
		spell.ID = id
		t.Spells[id] = spell
	}
	for id, blueprint := range t.Components.Blueprints {
		blueprint.ID = id
		t.Components.Blueprints[id] = blueprint
	}
	for id, component := range t.Components.Components {
		component.ID = id
		t.Components.Components[id] = component
	}
	for id, grade := range t.Materials.Grades {
		grade.ID = id
		t.Materials.Grades[id] = grade
	}
	for id, kind := range t.Materials.Kinds {
		kind.ID = id
		t.Materials.Kinds[id] = kind
	}
	for id, material := range t.Materials.Materials {
		material.ID = id
		t.Materials.Materials[id] = material
	}
	for id, mob := range t.Mobs {
		mob.ID = id
		t.Mobs[id] = mob
	}
	for id, biome := range t.Biomes {
		biome.ID = id
		t.Biomes[id] = biome
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
