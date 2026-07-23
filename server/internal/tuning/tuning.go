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
	"spellfire/server/internal/worldfield"
)

// SchemaVersion is the table shape this build understands. Bump it only when a
// table changes shape, and add the matching forward migration; a plain balance
// edit bumps Manifest.Version instead and needs no code change.
const SchemaVersion = 22

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
	OccludesVision   bool              `json:"occludes_vision"`
	VisibleInShadow  bool              `json:"visible_in_shadow"`
	CollisionObjects []CollisionObject `json:"collision_objects"`
	Admin            EntityAdmin       `json:"admin"`
}

// Terrain drives the chunked deterministic cover generator. There is no total
// count: at radius 45,000 the world is never fully resident, so density is
// declared per unit area and a chunk materialises the sites that fall inside it.
//
// Cell is the site lattice — one candidate every Cell units on each axis — and
// Fill is the fraction of those sites that actually carry an item. Placement is
// jittered inside each cell by exactly as much as Spacing and the archetype's
// widest radius leave room for, so two items can never be closer than Spacing
// however their chunks were loaded.
type Terrain struct {
	Entity       string  `json:"entity"`
	Seed         uint64  `json:"seed"`
	Cell         float64 `json:"cell"`
	Fill         float64 `json:"fill"`
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

// GradeThreshold is the richness at which a material grade takes over. It is a
// threshold on the reward curve rather than a radius, so re-shaping the curve
// moves every boundary with it.
type GradeThreshold struct {
	Grade string  `json:"grade"`
	At    float64 `json:"at"`
}

// GradeCurve is the convex reward curve, as vertices of a piecewise-linear
// function of the fraction of the world radius. It is piecewise linear rather
// than a power because the client computes the identical value and only
// +, -, *, / and sqrt are guaranteed to agree between Go and JavaScript.
type GradeCurve struct {
	Points     [][2]float64     `json:"points"`
	Thresholds []GradeThreshold `json:"thresholds"`
}

// CoverageRule is the load-time check on the generated biome field. It is what
// turns "geography never hard-locks a build" from a hope about the seed into a
// refusal at load.
type CoverageRule struct {
	Resolution     int     `json:"resolution"`
	MinimumShare   float64 `json:"minimum_share"`
	MinimumSamples int     `json:"minimum_samples"`
}

// WorldField parameterises the deterministic world field: which biome covers a
// position and what the ground there is worth. The simulation, this loader's
// coverage check, and the browser all read these same numbers.
type WorldField struct {
	Seed            uint32       `json:"seed"`
	RegionCell      float64      `json:"region_cell"`
	RegionJitter    float64      `json:"region_jitter"`
	RadialReference float64      `json:"radial_reference"`
	WarpCell        float64      `json:"warp_cell"`
	WarpAmplitude   float64      `json:"warp_amplitude"`
	BlendWidth      float64      `json:"blend_width"`
	GradeCurve      GradeCurve   `json:"grade_curve"`
	Coverage        CoverageRule `json:"coverage"`
}

type World struct {
	Radius      float64 `json:"radius"`
	SpawnRadius float64 `json:"spawn_radius"`
	// ChunkSize is the edge of one generation chunk and of one spatial-index
	// cell — the two are the same coordinate space deliberately, so residency
	// and broad-phase bucketing never disagree. It is sized at the AOI
	// half-extent, which keeps a player's interest square inside a 3 × 3
	// neighbourhood of chunks.
	ChunkSize   float64      `json:"chunk_size"`
	DangerBands []DangerBand `json:"danger_bands"`
	Field       WorldField   `json:"field"`
	Terrain     Terrain      `json:"terrain"`
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
	IntervalMS          int     `json:"interval_ms"`
	TargetTTKSeconds    float64 `json:"target_ttk_seconds"`
	TTKToleranceSeconds float64 `json:"ttk_tolerance_seconds"`
}

// WeightClass is the Gunslinger's balance axis. It sets how a weapon handles —
// how far it walks off aim, how much firing on the move costs, and how much it
// slows its carrier — and never what it deals: damage stays on the shared band,
// so a heavy category is a set of conditions rather than a power level.
type WeightClass struct {
	ID                   string  `json:"-"`
	Name                 string  `json:"name"`
	MovementMultiplier   float64 `json:"movement_multiplier"`
	RecoilMultiplier     float64 `json:"recoil_multiplier"`
	MoveSpreadMultiplier float64 `json:"move_spread_multiplier"`
}

type Combat struct {
	Roles         []string               `json:"roles"`
	DodgeVectors  []string               `json:"dodge_vectors"`
	Player        PlayerBody             `json:"player"`
	Dash          Dash                   `json:"dash"`
	WeightClasses map[string]WeightClass `json:"weight_classes"`
	DamageBands   map[string]DamageBand  `json:"damage_bands"`
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
	// Pellets is how many bodies one use puts into the world, spread over
	// PelletSpreadDegrees. The band's damage is divided between them, so a full
	// cone connecting is worth exactly one band hit and a grazing one is worth
	// less — the shotgun's identity is its condition, not extra damage.
	Pellets             int     `json:"pellets"`
	PelletSpreadDegrees float64 `json:"pellet_spread_degrees"`
	// HitscanRange is how far a round lands instantly. It is the sniper's
	// exception and is honoured only while the weapon is scoped, which is the
	// committed vulnerability the "scoped_commit" dodge vector names.
	HitscanRange float64 `json:"hitscan_range"`
	// MaxRange is the hard stop, in world units travelled, and zero means the
	// lifetime alone bounds the shot. FalloffStart is the distance past which
	// damage decays linearly toward FalloffMin, expressed as a fraction of the
	// band's damage rather than as a number of its own.
	MaxRange     float64 `json:"max_range"`
	FalloffStart float64 `json:"falloff_start"`
	FalloffMin   float64 `json:"falloff_min"`
	// Homing steers the round toward a target while it flies. It is the Arcane
	// missile's forgiving aim, and it is bounded rather than absolute: a limited
	// turn rate is what keeps travel time a dodge vector instead of a delay.
	Homing *Homing `json:"homing"`
}

// Homing is a bounded steering rule. TurnDegreesPerSecond caps how fast the
// round may change heading, so sidestepping late still beats it, and
// AcquireRange is how far it looks for something to follow.
type Homing struct {
	TurnDegreesPerSecond float64 `json:"turn_degrees_per_second"`
	AcquireRange         float64 `json:"acquire_range"`
}

// PelletCount is how many bodies one use spawns; an unstated count is one.
func (p Projectile) PelletCount() int {
	if p.Pellets < 1 {
		return 1
	}
	return p.Pellets
}

// DamageScale is the fraction of the band a hit is worth after distance
// falloff. Everything before FalloffStart is a full hit and everything past
// MaxRange has already expired, so this only interpolates the band between.
func (p Projectile) DamageScale(travelled float64) float64 {
	if p.FalloffStart <= 0 || travelled <= p.FalloffStart {
		return 1
	}
	end := p.MaxRange
	if end <= p.FalloffStart {
		return p.FalloffMin
	}
	ratio := (travelled - p.FalloffStart) / (end - p.FalloffStart)
	if ratio > 1 {
		ratio = 1
	}
	return 1 - ratio*(1-p.FalloffMin)
}

// Blast is the area an impact resolves into. It is what makes a launcher a
// control tool: the damage is still the band's, but it lands on everyone inside
// the radius and carries the effects the ability declares.
type Blast struct {
	Radius  float64  `json:"radius"`
	Effects []string `json:"effects"`
}

// Deployable is what an impact leaves standing in the world instead of
// resolving and vanishing: a persistent circular field with a lifetime. Smoke is
// the first, and the field is deliberately not a collider — a cloud changes what
// can be seen, never where a body may walk.
type Deployable struct {
	// Kind names the entities.json archetype the field materialises as, which is
	// also what the renderer draws it from.
	Kind       string  `json:"kind"`
	Radius     float64 `json:"radius"`
	DurationMS int     `json:"duration_ms"`
	// RevealRadius is how close two bodies have to be for the cloud to stop
	// hiding them from each other. Without it a body standing inside its own
	// smoke would be unable to see an opponent it is touching.
	RevealRadius float64 `json:"reveal_radius"`
	// Conceals is what makes a field smoke rather than ground: only a concealing
	// field is checked by the vision rule. A burning patch is plainly visible, and
	// hiding what stands in it would be a rule no player could read.
	Conceals bool `json:"conceals"`
	// A field may pulse. DamageBand and DamageFraction price one pulse against
	// the shared band, TickMS is its cadence, and Effects are the statuses each
	// pulse applies to whoever is standing inside. A field with no band deals
	// nothing and is pure control.
	DamageBand     string  `json:"damage_band"`
	DamageFraction float64 `json:"damage_fraction"`
	// DamageMultiplier is derived from an equipped crafted item at cast time.
	// It is runtime-only: persisted items continue to store component IDs.
	DamageMultiplier float64  `json:"-"`
	TickMS           int      `json:"tick_ms"`
	Effects          []string `json:"effects"`
	// FinalEffects land once, on the pulse that closes the field. It is what lets
	// a stacking slow end in a stun rather than simply stopping.
	FinalEffects []string `json:"final_effects"`
	// Trigger makes the field a trap: it lies inert until the first hostile body
	// enters it, applies its pulse to that body, and is spent. Its lifetime is
	// therefore how long it waits, not how long it runs.
	Trigger bool `json:"trigger"`
}

func (d Deployable) Tick() time.Duration { return time.Duration(d.TickMS) * time.Millisecond }

func (d Deployable) DamageScale() float64 {
	if d.DamageMultiplier == 0 {
		return 1
	}
	return d.DamageMultiplier
}

// Pulses reports whether the field does anything to a body standing in it.
func (d Deployable) Pulses() bool {
	return d.TickMS > 0 && (d.DamageBand != "" || len(d.Effects) > 0)
}

// Placement is where a cast lands instead of at the caster: a point Range units
// along the aim the cast committed to. The aim vector is all the client sends —
// there is no cursor distance on the wire — so a placed spell reaches a fixed,
// authoritative distance that its telegraph draws exactly.
type Placement struct {
	Range float64 `json:"range"`
}

// Blink is the Storm and Arcane repositioning tool: the caster is moved
// Distance units along the direction the cast locked in, stopping short of
// anything solid rather than passing through it.
type Blink struct {
	Distance float64 `json:"distance"`
}

// Chain arcs a landed hit onward. Jumps is how many further bodies it may reach
// and Range how far each arc may travel, measured from the body it last struck.
type Chain struct {
	Jumps int     `json:"jumps"`
	Range float64 `json:"range"`
}

// Cleanse is the dispel: it strips running statuses from the caster and from
// hostile bodies inside Radius, and returns ManaPerEffect for each one removed,
// which is what makes stripping a full kit worth the cast.
type Cleanse struct {
	Radius        float64 `json:"radius"`
	ManaPerEffect float64 `json:"mana_per_effect"`
}

// Wall is the one spell that authors terrain. Segments boxes of the named
// archetype are laid perpendicular to the cast's direction, Spacing apart, and
// stand for DurationMS unless something destroys them first.
type Wall struct {
	Kind       string  `json:"kind"`
	Segments   int     `json:"segments"`
	Spacing    float64 `json:"spacing"`
	DurationMS int     `json:"duration_ms"`
}

func (w Wall) Duration() time.Duration { return time.Duration(w.DurationMS) * time.Millisecond }

func (d Deployable) Duration() time.Duration { return time.Duration(d.DurationMS) * time.Millisecond }

// Guard is a raised frontal barrier. It blocks bullets and projectiles inside
// its arc, slows its user, and locks fire while it is up — never ground effects
// placed behind or beneath it, which is what keeps it an answer to a burst
// opener rather than an answer to everything.
type Guard struct {
	ArcDegrees         float64 `json:"arc_degrees"`
	MovementMultiplier float64 `json:"movement_multiplier"`
	// Durability is the shield's own health: what it stops is spent from this
	// pool rather than absorbed for free, and damage past what is left carries
	// through to the body behind it. At zero the shield is broken and cannot be
	// raised again until it has recovered in full, so a shield is a resource a
	// player spends rather than a stance they hold.
	Durability float64 `json:"durability"`
	// RegenPerSecond is how fast a lowered shield recovers, and RegenDelayMS the
	// quiet after the last impact before that starts.
	RegenPerSecond float64 `json:"regen_per_second"`
	RegenDelayMS   int     `json:"regen_delay_ms"`
}

func (g Guard) RegenDelay() time.Duration { return time.Duration(g.RegenDelayMS) * time.Millisecond }

// Blocks reports whether an impact arriving along direction is inside the arc
// the guard is facing. Both vectors are expected normalized.
func (g Guard) Blocks(facingX, facingY, towardX, towardY float64) bool {
	dot := facingX*towardX + facingY*towardY
	if dot < -1 {
		dot = -1
	} else if dot > 1 {
		dot = 1
	}
	return math.Acos(dot) <= g.ArcDegrees*math.Pi/360
}

// Scope is the committed aiming mode. It blacks out peripheral vision on the
// client, and here it trades movement for accuracy and extends how far the
// scoped body can see, which is also how far a hitscan round may reach.
type Scope struct {
	MovementMultiplier float64 `json:"movement_multiplier"`
	SpreadMultiplier   float64 `json:"spread_multiplier"`
	ViewBonus          float64 `json:"view_bonus"`
}

// Recoil walks the muzzle off aim in a fixed left/right pattern unique to each
// gun, so a burst is a shape a player learns rather than a random cone. Entries
// are *steps* in degrees, applied one per shot to a muzzle offset that persists
// between shots: the pattern is where the weapon travels, not where it points.
// The first entry is zero on every gun, so a settled first shot is always true.
// RecoveryMS of quiet settles the offset back to zero and returns the pattern to
// its first entry, which is what makes burst discipline the way a gun is
// controlled.
type Recoil struct {
	Pattern    []float64 `json:"pattern"`
	RecoveryMS int       `json:"recovery_ms"`
}

func (r Recoil) Recovery() time.Duration { return time.Duration(r.RecoveryMS) * time.Millisecond }

// DegreesAt is the pattern step for a shot index, wrapping so a magazine longer
// than the pattern repeats it instead of running off the end.
func (r Recoil) DegreesAt(shot int) float64 {
	if len(r.Pattern) == 0 {
		return 0
	}
	return r.Pattern[((shot%len(r.Pattern))+len(r.Pattern))%len(r.Pattern)]
}

// MaxDegrees bounds how far the accumulated offset may wander from aim. Every
// authored pattern alternates and so stays well inside it; the bound exists
// because a magazine longer than the pattern repeats it, and a pattern whose
// steps do not sum to zero would otherwise drift without limit over a long
// burst. It is derived rather than authored: half of one full walk of the
// pattern is the furthest a shape a player can learn ever needs to reach.
func (r Recoil) MaxDegrees() float64 {
	total := 0.0
	for _, step := range r.Pattern {
		total += math.Abs(step)
	}
	return total / 2
}

// Spread is how wide the weapon throws a shot. Standing is the floor a settled
// aim earns; Moving is what firing at full speed costs, and the simulation
// interpolates between them by how fast the body is actually travelling.
type Spread struct {
	StandingDegrees float64 `json:"standing_degrees"`
	MovingDegrees   float64 `json:"moving_degrees"`
}

// Cost kinds an ability may spend. A magazine weapon's ability spends ammo and
// so drives the reload path; a spell spends mana. "none" is the free ability.
const (
	CostNone = "none"
	CostAmmo = "ammo"
	CostMana = "mana"
	// CostMaterial spends a carried material rather than a magazine: crafted
	// special ammunition such as rockets, which is finite and has to be built
	// and hauled rather than reloaded.
	CostMaterial = "material"
)

// Cost is what one use of an ability charges the actor. Material names the
// carried stack a CostMaterial use draws from, and is empty for every other kind.
type Cost struct {
	Kind     string  `json:"kind"`
	Material string  `json:"material"`
	Amount   float64 `json:"amount"`
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
	// Output multipliers are derived from an equipped mana crystal. They are not
	// authored ability fields or persisted snapshots; zero means the untouched
	// baseline of one. Healing is carried now so every future healing delivery
	// uses the same all-spell crystal contract as damage.
	DamageMultiplier          float64 `json:"-"`
	HealingMultiplier         float64 `json:"-"`
	EffectiveHealthMultiplier float64 `json:"-"`
	// RequiresScope gates the use on the weapon being scoped. It is what makes a
	// sniper's hitscan a commitment rather than a free instant hit.
	RequiresScope bool `json:"requires_scope"`
	// Blast is the area an impact resolves into, and Guard is the barrier a
	// raised deployable holds. Both are alternative shapes of the same contract:
	// an ability declares what it puts into the world, and the simulation has
	// exactly one path for each.
	Blast *Blast `json:"blast"`
	Guard *Guard `json:"guard"`
	// Deployable is the persistent field an impact leaves behind. Like Blast and
	// Guard it is an alternative shape of the same contract: the projectile is
	// how it is delivered, and the field is what the world keeps.
	Deployable *Deployable `json:"deployable"`
	// Effects are the status effects each hit applies, resolved against the
	// effects table.
	Effects []string `json:"effects"`
	// The Mage's remaining delivery shapes. Each is an alternative to a
	// travelling round rather than a modifier on one, and every one of them is
	// resolved by the same deliver/deliverAt pair a projectile is, so a spell
	// never gets a path of its own.
	//
	// Placement moves where the cast lands; SelfEffects are what it leaves on the
	// caster; Blink moves the caster; BlinkOnHit moves the caster to the impact,
	// but only when the impact actually reached someone; Chain arcs a landed hit
	// onward; Cleanse strips statuses; Wall authors terrain.
	Placement   *Placement `json:"placement"`
	SelfEffects []string   `json:"self_effects"`
	Blink       *Blink     `json:"blink"`
	BlinkOnHit  bool       `json:"blink_on_hit"`
	Chain       *Chain     `json:"chain"`
	Cleanse     *Cleanse   `json:"cleanse"`
	Wall        *Wall      `json:"wall"`
}

func (a Ability) DamageScale() float64 {
	if a.DamageMultiplier == 0 {
		return 1
	}
	return a.DamageMultiplier
}

func (a Ability) HealingScale() float64 {
	if a.HealingMultiplier == 0 {
		return 1
	}
	return a.HealingMultiplier
}

func (a Ability) EffectiveHealthScale() float64 {
	if a.EffectiveHealthMultiplier == 0 {
		return 1
	}
	return a.EffectiveHealthMultiplier
}

// Interval is the cadence gate between uses of any ability — the global
// cooldown. Cooldown is this ability's own lockout on top of it.
func (a Ability) Interval() time.Duration { return time.Duration(a.IntervalMS) * time.Millisecond }
func (a Ability) Cooldown() time.Duration { return time.Duration(a.CooldownMS) * time.Millisecond }
func (a Ability) Windup() time.Duration   { return time.Duration(a.WindupMS) * time.Millisecond }

// Damaging reports whether the ability deals damage, and therefore owes a
// damage band and a dodge vector. Damage only ever comes from the shared band,
// so the band alone decides: a projectile that carries a deployable or a
// utility blast travels without dealing anything.
func (a Ability) Damaging() bool { return a.DamageBand != "" }

// DealsDamage reports whether anything the ability puts into the world costs a
// body health — the ability's own band, or a field that pulses one. It is what
// the dodge-vector requirement is measured against, because a burning patch
// nobody was warned about is exactly the thing the invariant forbids.
func (a Ability) DealsDamage() bool {
	return a.Damaging() || (a.Deployable != nil && a.Deployable.DamageBand != "")
}

// EffectKinds are the status effects the simulation knows how to run. A row of
// any other kind would be data the world silently ignores, so the loader
// rejects it.
var EffectKinds = []string{"burn", "slow", "root", "stun", "knockback", "shield", "blind", "armor"}

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
	// DamageMultiplier belongs to "armor": the fraction of incoming damage that
	// still reaches the body. It is mitigation rather than absorption — it has no
	// pool to spend — which is what separates Frost's light mitigation and
	// Earth's heavy one from an Arcane ward.
	DamageMultiplier float64 `json:"damage_multiplier"`
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
	// Weight names the class in Combat.WeightClasses that scales this weapon's
	// handling. Recoil and Spread are the weapon's own gunplay: the pattern the
	// muzzle walks and how wide it throws standing and moving. Scope is the
	// committed aiming mode, present only on the categories that have one.
	Weight string         `json:"weight"`
	Recoil Recoil         `json:"recoil"`
	Spread Spread         `json:"spread"`
	Scope  *Scope         `json:"scope"`
	Cost   map[string]int `json:"cost"`
	// RequiresCraft withholds the stock configuration: the row may only be
	// carried as a crafted instance, so its material Cost has to be paid before
	// it reaches a loadout. It is how rare materials gate the heavy categories
	// economically rather than statistically.
	RequiresCraft bool     `json:"requires_craft"`
	Roles         []string `json:"roles"`
}

// Scoped reports whether the weapon has a committed aiming mode at all.
func (w Weapon) Scoped() bool { return w.Scope != nil }

func (w Weapon) ReloadDuration() time.Duration {
	return time.Duration(w.ReloadMS) * time.Millisecond
}

type Spell struct {
	ID          string   `json:"-"`
	Name        string   `json:"name"`
	Element     string   `json:"element"`
	Tier        int      `json:"tier"`
	Starter     bool     `json:"starter"`
	UnlockLevel int      `json:"unlock_level"`
	Ability     string   `json:"ability"`
	Roles       []string `json:"roles"`
}

// Gadget is the Gunslinger's equivalent of a spell: identity over one ability,
// occupying a loadout slot. The table ships empty — Phase 2.4 authors smoke,
// flashbangs, and the rest — so today a Gunslinger's bar holds its weapon and
// five empty slots.
type Gadget struct {
	ID          string   `json:"-"`
	Name        string   `json:"name"`
	Class       string   `json:"class"`
	Starter     bool     `json:"starter"`
	UnlockLevel int      `json:"unlock_level"`
	Ability     string   `json:"ability"`
	Roles       []string `json:"roles"`
}

// Ammunition is a craftable special round: a recipe that spends materials and
// yields a carried material of its own, which the ability that fires it spends.
// It is deliberately not an unlock — the recipe is the launcher's, and what
// gates the launcher is the launcher's own unlock and material cost.
type Ammunition struct {
	ID       string         `json:"-"`
	Name     string         `json:"name"`
	Class    string         `json:"class"`
	Material string         `json:"material"`
	Count    int            `json:"count"`
	Cost     map[string]int `json:"cost"`
}

// AmmunitionFor lists the special-ammunition recipes a class may build, in
// stable order.
func (t *Tables) AmmunitionFor(class string) []string {
	ids := make([]string, 0, len(t.Ammunition))
	for _, id := range sortedKeys(t.Ammunition) {
		if t.Ammunition[id].Class == class {
			ids = append(ids, id)
		}
	}
	return ids
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
	// CraftedItemCapacity bounds how many permanent crafted weapons a character
	// may own and is also the capacity outcome the crafting UI owes.
	CraftedItemCapacity int        `json:"crafted_item_capacity"`
	StarterKit          StarterKit `json:"starter_kit"`
	// AdminGrant bounds the developer-mode level grant, the same way
	// materials.admin_grant bounds the material one. Progression's only trigger
	// is a player kill until Phase 4.3, so without it nothing above the opening
	// kit can be reached — and therefore exercised — on a fresh server.
	AdminGrant AdminField `json:"admin_grant"`
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
	ID      string   `json:"-"`
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Slots   []string `json:"slots"`
}

// CraftRecipe is the authoritative shape of one finished weapon. Slots map
// each blank on the generic blueprint to the component recipes accepted there.
// The weapon row is the map key, so a complete set of parts resolves to one
// category without trusting a category selected by the client.
type CraftRecipe struct {
	ID        string              `json:"-"`
	Blueprint string              `json:"blueprint"`
	Summary   string              `json:"summary"`
	Slots     map[string][]string `json:"slots"`
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
	// Gunplay handling, delivered by Phase 2.4: how far the muzzle walks, how
	// wide the weapon throws standing and moving, and how freely a scoped body
	// may move. All four are conditions on landing a shot, never its damage.
	AttrRecoilDegrees     = "recoil_degrees"
	AttrSpreadDegrees     = "spread_degrees"
	AttrMoveSpreadDegrees = "move_spread_degrees"
	AttrScopeMovement     = "scope_movement_multiplier"
	// Staff-only output scalars. Unlike a gun part, a mana crystal is allowed to
	// move spell output within the narrow bounds below; it applies to every spell
	// cast through that staff rather than naming an element or spell row.
	AttrSpellDamage  = "spell_damage"
	AttrSpellHealing = "spell_healing"
	// AttrAreaRadius is the area half of "projectile or area shape": it widens
	// the blast, the field, and the telegraph that warns about them together, so
	// a crystal can never quietly grow an area past the figure it shows.
	AttrAreaRadius = "area_radius"
	// AttrElementDamage is a mana crystal's element bias. It applies only to
	// spells of the element the crystal names, which is the horizontal half a
	// rarer crystal has to earn: specialising into one school rather than adding
	// output to all five.
	AttrElementDamage = "element_damage"
	// AttrEffectiveHealth scales shield pools and armor effectiveness together.
	// It is a staff-only output axis and is validated on the assembled item.
	AttrEffectiveHealth = "effective_health"
)

// ComponentAttributes is the set a modifier may name. Mana crystals scale the
// result of a spell through derived output multipliers; they still cannot edit
// the shared damage-band row or a particular spell's authored values.
var ComponentAttributes = []string{
	AttrMagazineSize, AttrReloadMS, AttrCooldownMS, AttrWindupMS, AttrCostAmount,
	AttrProjectileSpeed, AttrProjectileLife, AttrProjectileRadius,
	AttrRecoilDegrees, AttrSpreadDegrees, AttrMoveSpreadDegrees, AttrScopeMovement,
	AttrSpellDamage, AttrSpellHealing, AttrAreaRadius, AttrElementDamage,
	AttrEffectiveHealth,
}

// CrystalAttributes are the ones only a mana crystal may claim: the all-spell
// output scalars and the element bias. A gun part naming any of them would be
// moving spell output from the wrong blueprint entirely.
var CrystalAttributes = []string{AttrSpellDamage, AttrSpellHealing, AttrElementDamage, AttrEffectiveHealth}

// MagazineAttributes only mean anything on a weapon that holds a magazine, so a
// blueprint whose weapons cast spells may not modify them.
var MagazineAttributes = []string{AttrMagazineSize, AttrReloadMS}

// HandlingAttributes are gunplay: they mean nothing on a blueprint whose weapons
// have no recoil pattern to walk or scope to look through.
var HandlingAttributes = []string{AttrRecoilDegrees, AttrSpreadDegrees, AttrMoveSpreadDegrees, AttrScopeMovement}

// ForbiddenAttributes are the ones crafting may never touch. Fire cadence is an
// unrestricted DPS axis; bounded mana-crystal output is the explicit exception.
var ForbiddenAttributes = []string{AttrInterval}

// Modifier bounds keep handling and mana-crystal output inside the compressed
// progression envelope. A modifier of exactly 1 claims an effect it does not
// have.
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
	// Kind distinguishes ordinary gun parts from the two staff subassemblies.
	// Tier is meaningful for mana crystals and staves: the stave must be at
	// least as strong as the crystal it carries.
	Kind string `json:"kind"`
	Tier int    `json:"tier"`
	// Element is the school a mana crystal is biased toward, and is empty on
	// every other component. It is what an element_damage modifier applies to.
	Element string `json:"element"`
	Effect  string `json:"effect"`
	// Cost is the material ID → count one of this component consumes. Materials
	// must be hauled to a safe zone before they can be spent.
	Cost map[string]int `json:"cost"`
	// Modifiers scale an attribute by a multiplier: 1.2 is twenty percent more.
	Modifiers map[string]float64 `json:"modifiers"`
}

type Components struct {
	Blueprints map[string]Blueprint   `json:"blueprints"`
	Components map[string]Component   `json:"components"`
	Recipes    map[string]CraftRecipe `json:"recipes"`
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
	ID              string  `json:"-"`
	Name            string  `json:"name"`
	Tier            int     `json:"tier"`
	PowerMultiplier float64 `json:"power_multiplier"`
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

// BiomePalette is the ambient colouring the renderer cross-fades between as a
// player crosses a border. It is presentation only — nothing in the simulation
// reads it — but it lives here because a biome's identity reaches a player
// through its palette as much as through its materials, and the two must be
// edited in one place.
type BiomePalette struct {
	Ground string `json:"ground"`
	Accent string `json:"accent"`
	Haze   string `json:"haze"`
}

type Biome struct {
	ID      string       `json:"-"`
	Name    string       `json:"name"`
	Element string       `json:"element"`
	Summary string       `json:"summary"`
	Palette BiomePalette `json:"palette"`
}

// RetiredKinds are the tables a retirement may name. A retired ID resolves
// within its own kind; nothing is ever retired across tables.
var RetiredKinds = []string{"weapon", "spell", "gadget", "ability", "effect", "component", "blueprint", "material", "element", "biome", "mob", "ammunition"}

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
	Ammunition  map[string]Ammunition
	Components  Components
	Materials   Materials
	Mobs        map[string]Mob
	Biomes      map[string]Biome
	Retired     map[string]Retirement

	// field is built once from the rows above. It is derived rather than
	// parsed, so it is unexported and reached through Field().
	field *worldfield.Field
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
	case "ammunition":
		return t.Ammunition[id].Name != ""
	}
	return false
}

// WeightOf is the handling class a weapon is balanced on. A weapon with no
// declared weight — a staff — resolves to a neutral class that scales nothing.
func (t *Tables) WeightOf(weapon Weapon) WeightClass {
	if weight, ok := t.Combat.WeightClasses[weapon.Weight]; ok {
		return weight
	}
	return WeightClass{MovementMultiplier: 1, RecoilMultiplier: 1, MoveSpreadMultiplier: 1}
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

// GradeAt resolves a component tier to the rarity row that prices it.
func (t *Tables) GradeAt(tier int) (Grade, bool) {
	for _, grade := range t.Materials.Grades {
		if grade.Tier == tier {
			return grade, true
		}
	}
	return Grade{}, false
}

// RarityMultiplier applies an assembled item's weakest-link rarity. Every
// required component must reach a tier before the item receives that tier's
// band multiplier, so one Signature part cannot upgrade four Common parts.
func (t *Tables) RarityMultiplier(components map[string]string) float64 {
	if len(components) == 0 {
		return 1
	}
	tier := int(^uint(0) >> 1)
	for _, id := range components {
		component, ok := t.Components.Components[id]
		if !ok {
			return 1
		}
		if component.Tier < tier {
			tier = component.Tier
		}
	}
	if grade, ok := t.GradeAt(tier); ok {
		return grade.PowerMultiplier
	}
	return 1
}

type DamageProfile struct {
	Band     string
	Damage   float64
	Interval time.Duration
	DPS      float64
	RawTTK   time.Duration
}

// ResolveDamage computes output from the band and the derived item multiplier;
// neither damage nor DPS is copied onto a persisted item.
func (t *Tables) ResolveDamage(ability Ability, targetHealth float64) DamageProfile {
	band, ok := t.Combat.DamageBands[ability.DamageBand]
	if !ok || band.DamagePerHit <= 0 || band.IntervalMS <= 0 {
		return DamageProfile{}
	}
	damage := band.DamagePerHit * ability.DamageScale()
	interval := time.Duration(band.IntervalMS) * time.Millisecond
	hits := math.Ceil(targetHealth / damage)
	return DamageProfile{
		Band: ability.DamageBand, Damage: damage, Interval: interval,
		DPS: damage / interval.Seconds(), RawTTK: time.Duration(math.Max(0, hits-1) * float64(interval)),
	}
}

// UnlockKinds are the tables a permanent unlock ID may name. The ledger is
// flat, so an ID is unique across all of them — validation enforces it — and a
// saved entry can be resolved without also storing which table it came from.
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
		{"ammunition.json", &tables.Ammunition},
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
	for id, ammunition := range t.Ammunition {
		ammunition.ID = id
		t.Ammunition[id] = ammunition
	}
	for id, weight := range t.Combat.WeightClasses {
		weight.ID = id
		t.Combat.WeightClasses[id] = weight
	}
	for id, blueprint := range t.Components.Blueprints {
		blueprint.ID = id
		t.Components.Blueprints[id] = blueprint
	}
	for id, component := range t.Components.Components {
		component.ID = id
		t.Components.Components[id] = component
	}
	for id, recipe := range t.Components.Recipes {
		recipe.ID = id
		t.Components.Recipes[id] = recipe
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
