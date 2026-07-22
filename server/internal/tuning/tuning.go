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
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"spellfire/data"
)

// SchemaVersion is the table shape this build understands. Bump it only when a
// table changes shape, and add the matching forward migration; a plain balance
// edit bumps Manifest.Version instead and needs no code change.
const SchemaVersion = 12

type Manifest struct {
	// Version is the content revision. Bump it on any balance edit; a change
	// is what entitles characters to the global respec/refund.
	Version       int `json:"version"`
	SchemaVersion int `json:"schema_version"`
}

// Admins identifies accounts with access to explicitly admin-gated features.
// Email addresses are normalized at load using the same trim/lowercase rule as
// account registration, so authorization never depends on presentation case.
type Admins struct {
	Emails []string `json:"emails"`
}

// AdminField describes one editable value in the developer-mode UI. The
// server validates every submitted value against this catalog; the browser
// renders the same schema and never decides what an item is allowed to do.
type AdminField struct {
	Attribute string        `json:"attribute"`
	Label     string        `json:"label"`
	Input     string        `json:"input"`
	Scope     string        `json:"scope"`
	Default   string        `json:"default"`
	Minimum   *float64      `json:"min"`
	Maximum   *float64      `json:"max"`
	Step      *float64      `json:"step"`
	MaxLength int           `json:"max_length"`
	Options   []AdminOption `json:"options"`
}

type AdminOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type EntityAdmin struct {
	Name      string       `json:"name"`
	Spawnable bool         `json:"spawnable"`
	Fields    []AdminField `json:"fields"`
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

// Session governs a character's presence around a disconnect: how long the body
// stays behind, and how long the position it was left at remains honoured.
type Session struct {
	// LogoutLingerSeconds keeps the body in the world after the connection
	// drops, so disconnecting is not an escape from a fight.
	LogoutLingerSeconds int `json:"logout_linger_seconds"`
	// PositionExpirySeconds is how long an offline character keeps the spot it
	// logged out at. Past it, the next login recalls it to safety.
	PositionExpirySeconds int `json:"position_expiry_seconds"`
}

func (s Session) LogoutLinger() time.Duration {
	return time.Duration(s.LogoutLingerSeconds) * time.Second
}

func (s Session) PositionExpiry() time.Duration {
	return time.Duration(s.PositionExpirySeconds) * time.Second
}

// Outpost is a fixed safe fixture a character can be recalled to. Positions are
// Phase 3 geography; the table ships empty and every lookup falls back to the
// central hub until it is filled.
type Outpost struct {
	ID       string     `json:"-"`
	Name     string     `json:"name"`
	Position [2]float64 `json:"position"`
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

// CollisionObject is one entity-local collision primitive. Keeping geometry
// as typed data instead of behavior makes the definition directly reusable by
// a later ECS collision component. Offset is relative to the entity position.
type CollisionObject struct {
	Type    string  `json:"type"`
	OffsetX float64 `json:"offset_x"`
	OffsetY float64 `json:"offset_y"`
	Radius  float64 `json:"radius"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
}

// EntityDefinition contains immutable defaults copied into each runtime
// entity. Runtime state is deliberately not a pointer back to this row: health,
// mass, and collision geometry can be overridden or changed per instance.
type EntityDefinition struct {
	Mass             float64           `json:"mass"`
	MaxHealth        float64           `json:"max_health"`
	CollisionObjects []CollisionObject `json:"collision_objects"`
	Admin            EntityAdmin       `json:"admin"`
}

// Trees drives the deterministic procedural cover generator.
type Trees struct {
	Count        int     `json:"count"`
	Seed         uint64  `json:"seed"`
	RadiusSpread float64 `json:"radius_spread"`
	InnerMargin  float64 `json:"inner_margin"`
	OuterMargin  float64 `json:"outer_margin"`
	Spacing      float64 `json:"spacing"`
}

// Fixture places one entity archetype at a fixed world coordinate. Procedural
// families such as trees remain generator-driven; singular geography such as
// an authored wall belongs here.
type Fixture struct {
	ID       string     `json:"id"`
	Entity   string     `json:"entity"`
	Position [2]float64 `json:"position"`
}

type World struct {
	Radius      float64      `json:"radius"`
	SpawnRadius float64      `json:"spawn_radius"`
	DangerBands []DangerBand `json:"danger_bands"`
	Trees       Trees        `json:"trees"`
	Fixtures    []Fixture    `json:"fixtures"`
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
	Speed     float64 `json:"speed"`
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

// Cost kinds an ability may spend. A magazine weapon's ability spends ammo and
// so drives the reload path; a spell spends mana. "none" is the free ability.
const (
	CostNone = "none"
	CostAmmo = "ammo"
	CostMana = "mana"
)

// Cost is what one use of an ability charges the actor.
type Cost struct {
	Kind   string  `json:"kind"`
	Amount float64 `json:"amount"`
}

// TelegraphShapes are the standardized ground figures a windup may show. The
// grammar is shared, so nothing hand-rolls a telegraph.
var TelegraphShapes = []string{"circle", "cone", "line", "ring"}

// Telegraph is the shared geometry and phase timing an ability shows during
// its windup. Players, mobs, bosses, and deployables all emit the same world
// entity; the renderer never needs an owner-specific telegraph branch.
type Telegraph struct {
	Shape        string  `json:"shape"`
	Radius       float64 `json:"radius"`
	Length       float64 `json:"length"`
	Width        float64 `json:"width"`
	AngleDegrees float64 `json:"angle_degrees"`
	ActiveMS     int     `json:"active_ms"`
	ResolvedMS   int     `json:"resolved_ms"`
}

func (t Telegraph) ActiveDuration() time.Duration {
	return time.Duration(t.ActiveMS) * time.Millisecond
}

func (t Telegraph) ResolvedDuration() time.Duration {
	return time.Duration(t.ResolvedMS) * time.Millisecond
}

// Ability is the one contract every deliberate action draws from: what it
// costs, how often it may be used, what it commits the user to, how it is
// dodged, what it delivers, and what it leaves behind. Weapons and spells hold
// identity and reference an ability; mobs and deployables join them in the
// phases that build them. Damage is never authored here — it comes from the
// shared band row, like every other damaging thing.
type Ability struct {
	ID          string      `json:"-"`
	Name        string      `json:"name"`
	Cost        Cost        `json:"cost"`
	IntervalMS  int         `json:"interval_ms"`
	CooldownMS  int         `json:"cooldown_ms"`
	WindupMS    int         `json:"windup_ms"`
	Telegraph   *Telegraph  `json:"telegraph"`
	DodgeVector string      `json:"dodge_vector"`
	DamageBand  string      `json:"damage_band"`
	Projectile  *Projectile `json:"projectile"`
	// Effects are the status effects each hit applies, resolved against the
	// effects table.
	Effects []string `json:"effects"`
}

// Interval is the cadence gate between uses of any ability — the global
// cooldown. Cooldown is this ability's own lockout on top of it.
func (a Ability) Interval() time.Duration { return time.Duration(a.IntervalMS) * time.Millisecond }
func (a Ability) Cooldown() time.Duration { return time.Duration(a.CooldownMS) * time.Millisecond }
func (a Ability) Windup() time.Duration   { return time.Duration(a.WindupMS) * time.Millisecond }

// Damaging reports whether the ability deals damage, and therefore owes a
// damage band and a dodge vector.
func (a Ability) Damaging() bool { return a.DamageBand != "" || a.Projectile != nil }

// EffectKinds are the status effects the simulation knows how to run. A row of
// any other kind would be data the world silently ignores, so the loader
// rejects it.
var EffectKinds = []string{"burn", "slow", "root", "stun", "knockback", "shield"}

// Effect stacking rules. "refresh" keeps one instance and restarts its
// duration; "stack" runs independent instances side by side.
const (
	StackRefresh = "refresh"
	StackStack   = "stack"
)

// Effect is one status the simulation can carry on a body. Every magnitude that
// touches health is expressed against a damage band rather than authored as a
// raw number, so the compressed power band still owns the damage axis.
type Effect struct {
	ID       string `json:"-"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Stacking string `json:"stacking"`
	// DurationMS is how long the effect lasts. Zero is rejected: an effect with
	// no duration is either permanent or a no-op, and neither is a status.
	DurationMS int `json:"duration_ms"`
	// TickMS and DamageFraction belong to "burn": one tick deals the band's
	// damage_per_hit scaled by the fraction.
	TickMS         int     `json:"tick_ms"`
	DamageBand     string  `json:"damage_band"`
	DamageFraction float64 `json:"damage_fraction"`
	// SpeedMultiplier belongs to "slow": the fraction of normal speed left.
	SpeedMultiplier float64 `json:"speed_multiplier"`
	// Speed belongs to "knockback": units per second along the hit direction,
	// carried for the effect's duration and colliding like ordinary movement.
	Speed float64 `json:"speed"`
	// AbsorbHits belongs to "shield": the pool, in multiples of the band's
	// damage_per_hit.
	AbsorbHits float64 `json:"absorb_hits"`
}

func (e Effect) Duration() time.Duration { return time.Duration(e.DurationMS) * time.Millisecond }
func (e Effect) Tick() time.Duration     { return time.Duration(e.TickMS) * time.Millisecond }

// Weapon is a craftable blueprint instance. A gun points at the ability it
// fires and owns its magazine; a staff owns neither and delegates to the spell
// it casts, which points at an ability of its own.
type Weapon struct {
	ID        string `json:"-"`
	Name      string `json:"name"`
	Class     string `json:"class"`
	Blueprint string `json:"blueprint"`
	Category  string `json:"category"`
	// Starter marks membership of the basic set a new character draws its one
	// opening weapon from, and UnlockLevel is the level at which every character
	// receives the row outright. Basic rows sit above level 1 so the opening
	// draw stays a draw and the rest of the set arrives shortly after.
	Starter      bool   `json:"starter"`
	UnlockLevel  int    `json:"unlock_level"`
	MagazineSize int    `json:"magazine_size"`
	ReloadMS     int    `json:"reload_ms"`
	Ability      string `json:"ability"`
	Spell        string `json:"spell"`
}

func (w Weapon) ReloadDuration() time.Duration {
	return time.Duration(w.ReloadMS) * time.Millisecond
}

type Spell struct {
	ID          string `json:"-"`
	Name        string `json:"name"`
	Element     string `json:"element"`
	Tier        int    `json:"tier"`
	Starter     bool   `json:"starter"`
	UnlockLevel int    `json:"unlock_level"`
	Ability     string `json:"ability"`
}

// Gadget is the Gunslinger's equivalent of a spell: identity over one ability,
// occupying a loadout slot. The table ships empty — Phase 2.4 authors smoke,
// flashbangs, and the rest — so today a Gunslinger's bar holds its weapon and
// five empty slots.
type Gadget struct {
	ID          string `json:"-"`
	Name        string `json:"name"`
	Class       string `json:"class"`
	Starter     bool   `json:"starter"`
	UnlockLevel int    `json:"unlock_level"`
	Ability     string `json:"ability"`
}

// XP sources the simulation knows how to award. A row of any other name would
// be a value nothing ever reads, so the loader rejects it, and a missing row
// would leave an award silently worth nothing, so the loader requires them all.
// Only PlayerKill has a trigger today: mobs are Phase 4.3, harvesting Phase 4.1,
// and outpost discovery Phase 3.
const (
	SourcePlayerKill = "player_kill"
	SourceMobKill    = "mob_kill"
	SourceHarvest    = "harvest"
	SourceDiscovery  = "discovery"
)

var XPSources = []string{SourcePlayerKill, SourceMobKill, SourceHarvest, SourceDiscovery}

// StarterKit is how much a character is given at creation beyond its one drawn
// weapon: Unlocks is how many rows are drawn from the basic set of its slot
// kind, sized to fill the action bar so a zero-material character is never a
// spectator.
type StarterKit struct {
	Unlocks int `json:"unlocks"`
}

// Progression is the character axis: what XP is worth, how much of it a level
// costs, and how large the opening draw is. Which content a level grants is not
// here — each weapon, spell, and gadget row declares its own unlock_level, so
// adding content never means editing a second table.
type Progression struct {
	MaxLevel int            `json:"max_level"`
	BaseXP   int            `json:"base_xp"`
	Growth   float64        `json:"growth"`
	Sources  map[string]int `json:"sources"`
	// CraftedItemCapacity bounds how many crafted weapons a character may own. A
	// stock build costs no materials, so without a ceiling a client could mint
	// rows forever; it is also the capacity outcome the crafting UI owes.
	CraftedItemCapacity int        `json:"crafted_item_capacity"`
	StarterKit          StarterKit `json:"starter_kit"`
}

// XPToNext is what the level costs to leave. It is zero at the cap, which is
// how Award knows to stop accumulating.
func (p Progression) XPToNext(level int) int {
	if level < 1 || level >= p.MaxLevel {
		return 0
	}
	return int(math.Round(float64(p.BaseXP) * math.Pow(p.Growth, float64(level-1))))
}

// Award is what one occurrence of a source is worth, and zero for a source this
// build does not recognise.
func (p Progression) Award(source string) int { return p.Sources[source] }

// Affinity is the Mage's specialisation rule. Its shape is locked by
// mage.md#element-affinity — a tier-N spell needs N−1 other spells of its
// element — and only the multiplier is tunable.
type Affinity struct {
	SameElementPerTier int `json:"same_element_per_tier"`
}

// Loadout declares how many of each slot kind a character equips. The action
// bar is one size for both classes: a Gunslinger fills it with its weapon plus
// gadgets, a Mage with spells, and validation keeps the two arrangements the
// same length so one binding set serves both.
type Loadout struct {
	WeaponSlots int      `json:"weapon_slots"`
	GadgetSlots int      `json:"gadget_slots"`
	SpellSlots  int      `json:"spell_slots"`
	Affinity    Affinity `json:"affinity"`
}

// BarSlots is the number of selectable action-bar slots, which the keyboard
// binds to 1–6 and the touch layout renders as buttons.
func (l Loadout) BarSlots() int { return l.SpellSlots }

// RequiredSameElement is how many *other* spells of the same element a loadout
// must already hold for a tier-N spell to be equippable.
func (l Loadout) RequiredSameElement(tier int) int {
	if tier <= 1 {
		return 0
	}
	return (tier - 1) * l.Affinity.SameElementPerTier
}

type Blueprint struct {
	ID    string   `json:"-"`
	Name  string   `json:"name"`
	Slots []string `json:"slots"`
}

// Component attribute names a modifier may scale. The map is open — a component
// names whatever it changes — but every name must be an attribute the
// simulation actually reads at use time, so a modifier can never be data the
// world silently ignores.
const (
	AttrMagazineSize     = "magazine_size"
	AttrReloadMS         = "reload_ms"
	AttrCooldownMS       = "cooldown_ms"
	AttrWindupMS         = "windup_ms"
	AttrCostAmount       = "cost_amount"
	AttrProjectileSpeed  = "projectile_speed"
	AttrProjectileLife   = "projectile_life"
	AttrProjectileRadius = "projectile_radius"
	AttrInterval         = "interval_ms"
)

// ComponentAttributes is the set a modifier may name. Damage is absent by
// construction rather than by exclusion: it lives on the shared band row, not on
// any numeric item field, so no component can reach it.
var ComponentAttributes = []string{
	AttrMagazineSize, AttrReloadMS, AttrCooldownMS, AttrWindupMS, AttrCostAmount,
	AttrProjectileSpeed, AttrProjectileLife, AttrProjectileRadius,
}

// MagazineAttributes only mean anything on a weapon that holds a magazine, so a
// blueprint whose weapons cast spells may not modify them.
var MagazineAttributes = []string{AttrMagazineSize, AttrReloadMS}

// ForbiddenAttributes are the ones crafting may never touch. Fire cadence is the
// DPS axis: scaling it moves an item out of its damage band, which is precisely
// what progression-and-crafting.md forbids crafting from doing.
var ForbiddenAttributes = []string{AttrInterval}

// Modifier bounds. Crafting changes handling and ceiling, so a component may
// halve or double an attribute and no more; a modifier of exactly 1 is a row
// that claims an effect it does not have.
const (
	ModifierMin = 0.5
	ModifierMax = 2.0
)

// Component fills one blueprint slot. Modifiers are behavioural scalars over the
// attributes the simulation reads, and Effect states the same change in the
// plain language the crafting UI shows. A component may never carry a damage
// band, which is what keeps crafting out of the power axis.
type Component struct {
	ID        string `json:"-"`
	Name      string `json:"name"`
	Blueprint string `json:"blueprint"`
	Slot      string `json:"slot"`
	Effect    string `json:"effect"`
	// Cost is the material ID → count one of this component consumes. Materials
	// must be hauled to a safe zone before they can be spent.
	Cost map[string]int `json:"cost"`
	// Modifiers scale an attribute by a multiplier: 1.2 is twenty percent more.
	Modifiers map[string]float64 `json:"modifiers"`
}

type Components struct {
	Blueprints map[string]Blueprint `json:"blueprints"`
	Components map[string]Component `json:"components"`
}

// ComponentsFor lists the components that fit one slot of a blueprint, in
// stable order. It is what the crafting UI offers for the active slot and what
// the server checks a requested fill against.
func (t *Tables) ComponentsFor(blueprint, slot string) []string {
	fitting := make([]string, 0, len(t.Components.Components))
	for _, id := range sortedKeys(t.Components.Components) {
		if component := t.Components.Components[id]; component.Blueprint == blueprint && component.Slot == slot {
			fitting = append(fitting, id)
		}
	}
	return fitting
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
	Grades     map[string]Grade        `json:"grades"`
	Kinds      map[string]MaterialKind `json:"kinds"`
	Materials  map[string]Material     `json:"materials"`
	AdminGrant AdminField              `json:"admin_grant"`
}

type Mob struct {
	ID             string `json:"-"`
	Name           string `json:"name"`
	Family         string `json:"family"`
	Silhouette     string `json:"silhouette"`
	DamageBand     string `json:"damage_band"`
	DodgeVector    string `json:"dodge_vector"`
	TelegraphShape string `json:"telegraph_shape"`
	Turrets        int    `json:"turrets"`
	Behavior       string `json:"behavior"`
}

type Biome struct {
	ID      string `json:"-"`
	Name    string `json:"name"`
	Element string `json:"element"`
}

// RetiredKinds are the tables a retirement may name. A retired ID resolves
// within its own kind; nothing is ever retired across tables.
var RetiredKinds = []string{"weapon", "spell", "gadget", "ability", "effect", "component", "blueprint", "material", "element", "biome", "mob"}

// maxRetirementHops bounds a replacement chain. Validation rejects cycles, so
// this only guards a table that somehow reached the resolver unvalidated.
const maxRetirementHops = 8

// Retirement records what a withdrawn content ID resolves to. Content changes
// are additive: an ID is never deleted from the tables, it is retired here and
// points at either a live replacement or a material refund, so a save that
// still names it stays resolvable forever.
type Retirement struct {
	ID          string         `json:"-"`
	Kind        string         `json:"kind"`
	Replacement string         `json:"replacement"`
	Refund      map[string]int `json:"refund"`
	Note        string         `json:"note"`
}

type Tables struct {
	Manifest    Manifest
	Admins      Admins
	Simulation  Simulation
	Session     Session
	Entities    map[string]EntityDefinition
	World       World
	Outposts    map[string]Outpost
	Combat      Combat
	Loadout     Loadout
	Progression Progression
	Elements    map[string]Element
	Abilities   map[string]Ability
	Effects     map[string]Effect
	Weapons     map[string]Weapon
	Spells      map[string]Spell
	Gadgets     map[string]Gadget
	Components  Components
	Materials   Materials
	Mobs        map[string]Mob
	Biomes      map[string]Biome
	Retired     map[string]Retirement
}

// Resolution is what a persisted reference resolves to against today's tables:
// either a live row, or the materials refunded for a retired one.
type Resolution struct {
	// ID is the live row the reference resolves to, empty when it was refunded.
	ID string
	// Refund is the material ID → count owed per retired unit held.
	Refund map[string]int
}

// Resolve maps a persisted reference of a kind onto live content, following
// retirement chains. It reports false only for an ID this build has never heard
// of — neither live nor retired — which is the one case a caller may drop.
func (t *Tables) Resolve(kind, id string) (Resolution, bool) {
	for hop := 0; hop <= maxRetirementHops; hop++ {
		if t.Live(kind, id) {
			return Resolution{ID: id}, true
		}
		retirement, ok := t.Retired[id]
		if !ok || retirement.Kind != kind {
			return Resolution{}, false
		}
		if retirement.Replacement == "" {
			return Resolution{Refund: retirement.Refund}, true
		}
		id = retirement.Replacement
	}
	return Resolution{}, false
}

// Live reports whether an ID names a current row of the kind.
func (t *Tables) Live(kind, id string) bool {
	switch kind {
	case "weapon":
		return t.Weapons[id].Name != ""
	case "spell":
		return t.Spells[id].Name != ""
	case "gadget":
		return t.Gadgets[id].Name != ""
	case "ability":
		return t.Abilities[id].Name != ""
	case "effect":
		return t.Effects[id].Name != ""
	case "component":
		return t.Components.Components[id].Name != ""
	case "blueprint":
		return t.Components.Blueprints[id].Name != ""
	case "material":
		return t.Materials.Materials[id].Name != ""
	case "element":
		return t.Elements[id].Name != ""
	case "biome":
		return t.Biomes[id].Name != ""
	case "mob":
		return t.Mobs[id].Name != ""
	}
	return false
}

// WeaponAbility resolves what a weapon does when used: its own ability, or the
// ability of the spell it casts. Both classes therefore reach the simulation as
// one shape, and it branches on the ability's declared cost rather than on
// class. It reports false only for references that no longer resolve, which
// validation rejects at load.
func (t *Tables) WeaponAbility(weapon Weapon) (Ability, bool) {
	id := weapon.Ability
	if weapon.Spell != "" {
		spell, ok := t.Spells[weapon.Spell]
		if !ok {
			return Ability{}, false
		}
		id = spell.Ability
	}
	ability, ok := t.Abilities[id]
	return ability, ok
}

// BandDamage is the per-hit damage of a band row, and zero for an ability that
// deals none.
func (t *Tables) BandDamage(band string) float64 {
	return t.Combat.DamageBands[band].DamagePerHit
}

// UnlockKinds are the tables a permanent unlock ID may name. The ledger is
// flat, so an ID is unique across all of them — validation enforces it — and a
// saved entry can be resolved without also storing which table it came from.
// Keystones join the list when Phase 2.7 settles them.
var UnlockKinds = []string{"weapon", "spell", "gadget"}

// StarterWeapons is the basic set of the class: the pool a new character draws
// its one opening weapon from. Validation guarantees at least one per class.
func (t *Tables) StarterWeapons(class string) []string {
	starters := make([]string, 0, len(t.Weapons))
	for _, id := range sortedKeys(t.Weapons) {
		if weapon := t.Weapons[id]; weapon.Starter && weapon.Class == class {
			starters = append(starters, id)
		}
	}
	return starters
}

// StarterWeapon is the deterministic first row of that pool. It is the
// guaranteed-available replacement for a weapon a content change withdrew, not
// the weapon a new character is given — that one is drawn.
func (t *Tables) StarterWeapon(class string) (Weapon, bool) {
	starters := t.StarterWeapons(class)
	if len(starters) == 0 {
		return Weapon{}, false
	}
	return t.Weapons[starters[0]], true
}

// StarterSpells lists the basic set a new Mage draws its opening spells from,
// in a stable order.
func (t *Tables) StarterSpells() []string {
	starters := make([]string, 0, len(t.Spells))
	for _, id := range sortedKeys(t.Spells) {
		if t.Spells[id].Starter {
			starters = append(starters, id)
		}
	}
	return starters
}

// StarterGadgets is the Gunslinger's equivalent of StarterSpells.
func (t *Tables) StarterGadgets(class string) []string {
	starters := make([]string, 0, len(t.Gadgets))
	for _, id := range sortedKeys(t.Gadgets) {
		if gadget := t.Gadgets[id]; gadget.Starter && gadget.Class == class {
			starters = append(starters, id)
		}
	}
	return starters
}

// UnlocksThrough lists every content ID a character of the level has been
// granted outright, in stable order. It is a scan of the content tables rather
// than a mapping table of its own: a new row declares the level it arrives at,
// and nothing else has to be edited to match.
func (t *Tables) UnlocksThrough(level int) []string {
	granted := make([]string, 0)
	if level < 1 {
		return granted
	}
	for _, id := range sortedKeys(t.Weapons) {
		if t.Weapons[id].UnlockLevel <= level {
			granted = append(granted, id)
		}
	}
	for _, id := range sortedKeys(t.Spells) {
		if t.Spells[id].UnlockLevel <= level {
			granted = append(granted, id)
		}
	}
	for _, id := range sortedKeys(t.Gadgets) {
		if t.Gadgets[id].UnlockLevel <= level {
			granted = append(granted, id)
		}
	}
	sort.Strings(granted)
	return granted
}

// UnlockKind names the table a live unlock ID belongs to.
func (t *Tables) UnlockKind(id string) (string, bool) {
	for _, kind := range UnlockKinds {
		if t.Live(kind, id) {
			return kind, true
		}
	}
	return "", false
}

// ResolveUnlock maps a persisted ledger entry onto live content without the
// caller having to know which table it came from. A retired entry follows its
// retirement chain like any other reference.
func (t *Tables) ResolveUnlock(id string) (Resolution, bool) {
	if kind, ok := t.UnlockKind(id); ok {
		return t.Resolve(kind, id)
	}
	if retirement, ok := t.Retired[id]; ok && contains(UnlockKinds, retirement.Kind) {
		return t.Resolve(retirement.Kind, id)
	}
	return Resolution{}, false
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
		{"admins.json", &tables.Admins},
		{"simulation.json", &tables.Simulation},
		{"session.json", &tables.Session},
		{"entities.json", &tables.Entities},
		{"world.json", &tables.World},
		{"outposts.json", &tables.Outposts},
		{"combat.json", &tables.Combat},
		{"loadout.json", &tables.Loadout},
		{"progression.json", &tables.Progression},
		{"elements.json", &tables.Elements},
		{"abilities.json", &tables.Abilities},
		{"effects.json", &tables.Effects},
		{"weapons.json", &tables.Weapons},
		{"spells.json", &tables.Spells},
		{"gadgets.json", &tables.Gadgets},
		{"components.json", &tables.Components},
		{"materials.json", &tables.Materials},
		{"mobs.json", &tables.Mobs},
		{"biomes.json", &tables.Biomes},
		{"retired.json", &tables.Retired},
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
	tables.normalizeAdmins()
	tables.stampIDs()
	if err := tables.validate(); err != nil {
		return nil, err
	}
	return tables, nil
}

func (t *Tables) normalizeAdmins() {
	for index, email := range t.Admins.Emails {
		t.Admins.Emails[index] = strings.ToLower(strings.TrimSpace(email))
	}
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
	for id, ability := range t.Abilities {
		ability.ID = id
		t.Abilities[id] = ability
	}
	for id, effect := range t.Effects {
		effect.ID = id
		t.Effects[id] = effect
	}
	for id, weapon := range t.Weapons {
		weapon.ID = id
		t.Weapons[id] = weapon
	}
	for id, spell := range t.Spells {
		spell.ID = id
		t.Spells[id] = spell
	}
	for id, gadget := range t.Gadgets {
		gadget.ID = id
		t.Gadgets[id] = gadget
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
	for id, outpost := range t.Outposts {
		outpost.ID = id
		t.Outposts[id] = outpost
	}
	for id, retirement := range t.Retired {
		retirement.ID = id
		t.Retired[id] = retirement
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
