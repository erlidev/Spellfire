package game

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"spellfire/server/internal/crafting"
	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/tuning"
)

const (
	ButtonUp       uint32 = 1
	ButtonDown     uint32 = 2
	ButtonLeft     uint32 = 4
	ButtonRight    uint32 = 8
	ButtonFire     uint32 = 16
	ButtonDash     uint32 = 32
	ButtonReload   uint32 = 64
	ButtonInteract uint32 = 128
	// ButtonScope holds the committed aiming mode of a weapon that has one. It
	// is held rather than toggled, so the vulnerability it buys accuracy with is
	// always a deliberate, ongoing choice.
	ButtonScope uint32 = 256
)

// Tuning is the simulation's runtime view of the versioned tables. No balance
// value is authored here: FromTables derives every field from a table row, so
// editing a row changes the simulation without any code or character change.
// Only the process-level rates are meant to be overridden after construction,
// and only from deployment configuration.
type Tuning struct {
	Tables *tuning.Tables

	TickRate, SendRate int
	AOIRadius          float64
	MaxRewind          time.Duration

	WorldRadius, SafeRadius, PvPRadius float64
	PlayerRadius, PlayerSpeed          float64
	DashDistance                       float64
	DashDuration, DashCooldown         time.Duration
	MaxHealth, MaxMana, ManaRegen      float64
	LogoutLinger, PositionExpiry       time.Duration
}

func FromTables(tables *tuning.Tables) Tuning {
	body, dash := tables.Combat.Player, tables.Combat.Dash
	player := newEntity("", "player", Vec{}, tables.Entities["player"], EntityOverrides{})
	return Tuning{
		Tables:   tables,
		TickRate: tables.Simulation.TickRate, SendRate: tables.Simulation.SendRate,
		AOIRadius: tables.Simulation.AOIRadius, MaxRewind: tables.Simulation.MaxRewind(),
		WorldRadius: tables.World.Radius, SafeRadius: tables.World.SafeRadius(), PvPRadius: tables.World.PvPRadius(),
		PlayerRadius: player.circleRadius(), PlayerSpeed: body.Speed,
		DashDistance: dash.Distance, DashDuration: dash.Duration(), DashCooldown: dash.Cooldown(),
		MaxHealth: player.MaxHealth, MaxMana: body.MaxMana, ManaRegen: body.ManaRegen,
		LogoutLinger: tables.Session.LogoutLinger(), PositionExpiry: tables.Session.PositionExpiry(),
	}
}

func DefaultTuning() Tuning { return FromTables(tuning.MustLoad()) }

type Vec struct{ X, Y float64 }

func (v Vec) Add(o Vec) Vec     { return Vec{v.X + o.X, v.Y + o.Y} }
func (v Vec) Sub(o Vec) Vec     { return Vec{v.X - o.X, v.Y - o.Y} }
func (v Vec) Mul(s float64) Vec { return Vec{v.X * s, v.Y * s} }
func (v Vec) LengthSq() float64 { return v.X*v.X + v.Y*v.Y }
func (v Vec) Normalized() Vec {
	l := math.Sqrt(v.LengthSq())
	if l < 0.0001 {
		return Vec{}
	}
	return v.Mul(1 / l)
}

type Player struct {
	Entity
	AccountID, Name string
	Class           model.Class
	// Loadout is the equipped set — content IDs by slot, resolved against the
	// tables on every use, never a copy of what those rows hold. Selected is
	// the action-bar slot the use button acts through, bound to 1–6.
	Loadout  model.Loadout
	Selected int
	// RespecOwed is set when a balance patch or a content withdrawal
	// re-validated the loadout at join. It stays set until the player next
	// commits a loadout, which is the free respec the patch entitles them to.
	RespecOwed bool
	// Level, XP, and Unlocks are the permanent character axis, carried on the
	// body so what it earns is credited where it is earned. ProgressDirty marks
	// a change the engine has not persisted and pushed to the client yet.
	Level, XP     int
	Unlocks       progression.Ledger
	ProgressDirty bool
	// SquadID is empty until Phase 5 forms squads. It lives on the actor now so
	// snapshots and future attribution code do not need a protocol migration.
	SquadID                         string
	Aim                             Vec
	Mana                            float64
	Input                           protocol.Input
	Acknowledged                    uint32
	PreviousButtons                 uint32
	NextFire, DashReady, ReloadEnds time.Time
	Ammo                            int
	// Shot is the position in the equipped weapon's recoil pattern, RecoilPeak
	// where the last shot left the muzzle in degrees off aim, and LastShot when
	// that was: the offset decays back to aim over the weapon's recovery window
	// and the pattern returns to its first entry, so burst discipline is what
	// controls a gun. Fired is the body's total shot count; it seeds the
	// move-spread draw, which keeps spread reproducible for a test while staying
	// unpredictable to a player, and it reaches the wire so every client can
	// show a shot the instant it happens.
	Shot       int
	Fired      uint64
	RecoilPeak float64
	LastShot   time.Time
	// Scoped and Guarding are the two committed stances. Both are derived from
	// the held input every tick rather than toggled, and both reach the wire so
	// an opponent can see the commitment they are being charged for.
	Scoped, Guarding bool
	DashDirection    Vec
	DashTicksLeft    int
	// Cooldowns is each ability's own lockout, keyed by ability ID, alongside
	// the global cadence gate NextFire holds.
	Cooldowns map[string]time.Time
	// Effects are the statuses running on the body, in application order.
	Effects []ActiveEffect
	// Carried materials and unlocked outposts are persisted references, held
	// here so they survive a disconnect. Harvesting (Phase 4.1) and outpost
	// discovery (Phase 3) are what will mutate them. Crafting is the first thing
	// that spends them.
	Materials map[string]int
	Outposts  []string
	// Items are the crafted weapons the character owns, as weapon and component
	// references. They are permanent like unlocks rather than carried like
	// materials, so death never drops one.
	Items []model.CraftedItem
	// LingerUntil is set when the connection drops: the body stays in the world,
	// killable and unable to act, until it passes. Zero means connected.
	LingerUntil                   time.Time
	SpeedMultiplier, ViewDistance float64
}

// Lingering reports whether the body is present only because its owner
// disconnected. A lingering player takes damage but cannot act.
func (p *Player) Lingering() bool { return !p.LingerUntil.IsZero() }

// A dash covers DashDistance over DashDuration, quantized to whole ticks so the
// client's fixed-rate prediction reproduces it exactly.
func (t Tuning) dashTicks() int {
	ticks := int(math.Round(t.DashDuration.Seconds() * float64(t.TickRate)))
	if ticks < 1 {
		ticks = 1
	}
	return ticks
}

func (t Tuning) dashSpeed() float64 {
	return t.DashDistance / (float64(t.dashTicks()) / float64(t.TickRate))
}

type Projectile struct {
	Entity
	OwnerID, Element  string
	Damage, Remaining float64
	// Effects are the statuses a hit applies, carried from the ability that
	// launched it so the resolver needs no lookup back to the shooter's kit.
	Effects []string
	// Spec is the delivery shape the ability launched this with, already scaled
	// by whatever components the weapon carries. It is carried rather than looked
	// up so a crafted weapon's falloff and range are the ones its own round flew
	// with, and Travelled is how far it has flown against them.
	Spec      tuning.Projectile
	Travelled float64
	// Blast is the area an impact resolves into, and BlastEffects what that area
	// applies. Both are nil on an ordinary round.
	Blast        *tuning.Blast
	BlastEffects []string
	// Deploy is the persistent field this round leaves where it stops, and is
	// nil on an ordinary round. Deployed marks it placed, so a round that is
	// resolved and then reaped cannot place two.
	Deploy   *tuning.Deployable
	Deployed bool
}

// hitDamage is what this round is worth where it currently is: the band's
// damage after distance falloff.
func (p *Projectile) hitDamage() float64 {
	return p.Damage * p.Spec.DamageScale(p.Travelled)
}

type historySample struct {
	at       time.Time
	position Vec
}

type World struct {
	tuning      Tuning
	tick        uint64
	players     map[string]*Player
	projectiles map[string]*Projectile
	telegraphs  map[string]*Telegraph
	// deployables are the persistent fields abilities leave standing — smoke
	// today — keyed like every other short-lived family so expiry is a sweep
	// rather than a scan of the world.
	deployables map[string]*Deployable
	// worldItems is a dense component-friendly slice for authored fixtures and
	// procedural terrain. Dead entries remain as inactive slots, avoiding map
	// iteration and compaction in hot collision paths.
	worldItems []*Entity
	history    map[string][]historySample
	// occupants maps an account to the one character it has a body for, so the
	// one-body-per-account rule is a lookup rather than a scan of the world.
	occupants       map[string]string
	nextProjectile  uint64
	nextTelegraph   uint64
	nextDeployable  uint64
	nextAdminPlayer uint64
	nextAdminEntity uint64
	combat          *combatLog
}

func NewWorld(t Tuning) *World {
	if t.Tables == nil || t.TickRate <= 0 {
		t = DefaultTuning()
	}
	return &World{
		tuning: t, players: make(map[string]*Player), projectiles: make(map[string]*Projectile), telegraphs: make(map[string]*Telegraph),
		deployables: make(map[string]*Deployable),
		worldItems:  generateWorldItems(t.Tables), history: make(map[string][]historySample),
		occupants: make(map[string]string), combat: newCombatLog(combatEventCapacity),
	}
}

// Occupant reports which character of an account has a body in the world,
// connected or lingering, and is empty when none does. Characters with no
// account — only the simulation tests build those — are never indexed, so they
// never occupy each other's slot.
func (w *World) Occupant(accountID string) string {
	if accountID == "" {
		return ""
	}
	return w.occupants[accountID]
}

func (w *World) AddPlayer(character model.Character, now time.Time) *Player {
	if existing := w.players[character.ID]; existing != nil && existing.Alive {
		// Reconnecting inside the logout window resumes the body that stayed
		// behind, wherever the fight has since moved it. The input sequence
		// belongs to the connection, not the body: a new client counts from
		// zero again, so the old high-water mark must go or every input it
		// sends is rejected as stale.
		existing.LingerUntil = time.Time{}
		existing.Input, existing.Acknowledged, existing.PreviousButtons = protocol.Input{}, 0, 0
		return existing
	} else if existing != nil {
		// A body killed inside its logout window is a corpse, not a session to
		// resume: dropping the connection must not park the death at the spot
		// it happened. The corpse goes and the character re-enters at the hub,
		// the same place the saved-as-unplaced record would have sent it had
		// the window closed first.
		w.RemovePlayer(character.ID)
		character.State.Placed = false
	}
	// The ledger is carried onto today's content first, because what a character
	// owns decides what it may equip: retired unlocks follow their retirement,
	// an empty ledger is rolled into a starter kit, and anything the level has
	// since come to grant is added.
	ledger, granted := progression.Sync(w.tuning.Tables, character.Class, character.ID, character.Level, character.Unlocks)
	// The saved set is then carried onto today's content before the body exists:
	// retired IDs follow their retirement, an arrangement a balance patch
	// invalidated is re-validated, content the ledger does not own is unequipped,
	// and an empty record resolves to the class default. A character therefore
	// never enters the world holding a set the rules would refuse.
	// Crafted items are loaded with the record and resolved the same way: an
	// instance whose weapon row a content change withdrew stops being equippable
	// rather than arming the character with a row that no longer exists.
	items := w.craftedItems(character.Items)
	equipped, respec := loadout.Resolve(w.tuning.Tables, character.Class, crafting.Inventory{Ledger: ledger, Items: items}, character.State.Loadout)
	p := &Player{
		Entity:    newEntity(character.ID, "player", w.entryPosition(character, now), w.tuning.Tables.Entities["player"], EntityOverrides{}),
		AccountID: character.AccountID, Name: character.Name, Class: character.Class,
		Aim: Vec{1, 0}, Mana: w.tuning.MaxMana,
		Materials:       w.carriedMaterials(character.State.Materials),
		Outposts:        append([]string(nil), character.State.Outposts...),
		Items:           items,
		Cooldowns:       make(map[string]time.Time),
		SpeedMultiplier: 1,
		ViewDistance:    w.tuning.AOIRadius,
		Loadout:         equipped,
		RespecOwed:      respec,
		Level:           max(1, character.Level),
		XP:              character.XP,
		Unlocks:         ledger,
		// A ledger that changed at join — a fresh starter kit, a retirement, or
		// content added at a level this character is already past — is a grant
		// the record does not yet hold, so it is written back on the next drain.
		ProgressDirty: granted,
	}
	if weapon, ok := w.weapon(p); ok {
		p.Ammo = weapon.MagazineSize
	}
	w.players[p.ID] = p
	if p.AccountID != "" {
		w.occupants[p.AccountID] = p.ID
	}
	w.recordHistory(p, now)
	return p
}

// entryPosition decides where a character re-enters the world. A recent save is
// honoured exactly, so a disconnect costs the session rather than the walk back
// out. Once the save has gone stale — or the world has moved under it — the
// character is recalled to safety near where it left off instead.
func (w *World) entryPosition(character model.Character, now time.Time) Vec {
	if !character.State.Placed {
		return w.hubSpawn(character.ID)
	}
	position := Vec{character.State.Position.X, character.State.Position.Y}
	if math.IsNaN(position.X) || math.IsNaN(position.Y) {
		return w.hubSpawn(character.ID)
	}
	if !w.positionExpired(character.State.LastSeen, now) && w.standable(position) {
		return position
	}
	return w.recallDestination(character, position)
}

// positionExpired reports whether a saved position is too old to honour. An
// unstamped save is expired: it may be arbitrarily old, and honouring it would
// drop a character into whatever the world has become since.
func (w *World) positionExpired(lastSeen, now time.Time) bool {
	return lastSeen.IsZero() || now.Sub(lastSeen) >= w.tuning.PositionExpiry
}

// standable rejects a saved position the current world can no longer accept:
// outside the rim, or inside cover that did not exist when it was written.
func (w *World) standable(position Vec) bool {
	limit := w.tuning.WorldRadius - w.tuning.PlayerRadius
	return position.LengthSq() <= limit*limit && !w.collides(position, w.tuning.PlayerRadius)
}

// recallDestination picks the safe fixture nearest to where the character left
// off: any outpost it has unlocked, or the central hub. Outposts have no
// geography until Phase 3 fills the table, so today this always resolves to the
// hub — the fallback the design already guarantees exists.
func (w *World) recallDestination(character model.Character, from Vec) Vec {
	best := w.hubSpawn(character.ID)
	nearest := best.Sub(from).LengthSq()
	for _, id := range character.State.Outposts {
		outpost, ok := w.tuning.Tables.Outposts[id]
		if !ok {
			continue
		}
		position := Vec{outpost.Position[0], outpost.Position[1]}
		if distance := position.Sub(from).LengthSq(); distance < nearest && w.standable(position) {
			best, nearest = position, distance
		}
	}
	return best
}

// hubSpawn is the character's deterministic point on the central spawn ring.
func (w *World) hubSpawn(id string) Vec {
	angle := float64(hash(id)%628) / 100
	spawn := w.tuning.Tables.World.SpawnRadius
	return Vec{math.Cos(angle) * spawn, math.Sin(angle) * spawn}
}

// carriedMaterials maps a saved inventory through the retirement table. A
// material the content has since retired resolves to its replacement or to its
// refund rather than vanishing; only an ID this build has never heard of is
// dropped, because there is nothing left to honour it with.
func (w *World) carriedMaterials(saved map[string]int) map[string]int {
	carried := make(map[string]int, len(saved))
	ids := make([]string, 0, len(saved))
	for id := range saved {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		count := saved[id]
		if count <= 0 {
			continue
		}
		resolved, ok := w.tuning.Tables.Resolve("material", id)
		if !ok {
			continue
		}
		if resolved.ID != "" {
			carried[resolved.ID] += count
			continue
		}
		for material, per := range resolved.Refund {
			carried[material] += per * count
		}
	}
	return carried
}

// BeginLinger leaves a disconnected player's body in the world until the logout
// window closes. It stays a valid target the whole time, so dropping the
// connection is not an escape from a fight.
func (w *World) BeginLinger(id string, now time.Time) bool {
	p := w.players[id]
	if p == nil {
		return false
	}
	p.LingerUntil = now.Add(w.tuning.LogoutLinger)
	p.Input.Buttons, p.PreviousButtons = 0, 0
	p.Velocity, p.DashTicksLeft = Vec{}, 0
	return true
}

// ExpiredLingering lists the bodies whose logout window has closed, in
// deterministic order. The caller saves each before removing it.
func (w *World) ExpiredLingering(now time.Time) []string {
	expired := make([]string, 0)
	for _, id := range sortedPlayerIDs(w.players) {
		if p := w.players[id]; p.Lingering() && !now.Before(p.LingerUntil) {
			expired = append(expired, id)
		}
	}
	return expired
}

// StateOf captures what must survive a disconnect. A dead player is saved
// unplaced, so the next join enters at the hub spawn — the same destination the
// current instant respawn uses; Phase 4.2 replaces both with a chosen outpost.
func (w *World) StateOf(id string, now time.Time) (model.CharacterState, bool) {
	p := w.players[id]
	if p == nil || p.AdminSpawned {
		return model.CharacterState{}, false
	}
	materials := make(map[string]int, len(p.Materials))
	for material, count := range p.Materials {
		materials[material] = count
	}
	return model.CharacterState{
		Position:  model.Point{X: p.Position.X, Y: p.Position.Y},
		Placed:    p.Alive,
		LastSeen:  now,
		Materials: materials,
		Outposts:  append([]string(nil), p.Outposts...),
		Loadout:   p.Loadout.Clone(),
	}, true
}

// States captures every present player's persistable state for one save pass,
// lingering bodies included — their position is the one that must be kept.
func (w *World) States(now time.Time) map[string]model.CharacterState {
	states := make(map[string]model.CharacterState, len(w.players))
	for _, id := range sortedPlayerIDs(w.players) {
		if state, ok := w.StateOf(id, now); ok {
			states[id] = state
		}
	}
	return states
}

// craftedItems carries a character's saved items onto today's content. An item
// whose weapon row was withdrawn is dropped from what may be equipped rather
// than resolved onto a replacement: the components filling its slots belong to
// that row's blueprint, and silently rebuilding it on another one would hand the
// character a weapon it never made.
func (w *World) craftedItems(saved []model.CraftedItem) []model.CraftedItem {
	items := make([]model.CraftedItem, 0, len(saved))
	for _, item := range saved {
		resolved, ok := w.tuning.Tables.Resolve("weapon", item.Weapon)
		if !ok || resolved.ID == "" {
			continue
		}
		item.Weapon = resolved.ID
		// Component IDs and slot names may both change when a blueprint is
		// revamped. Resolve each persisted part and place it in the live row's
		// slot, so old saves remain usable without carrying legacy slots into the
		// current crafting UI.
		parts := map[string]string{}
		for _, oldSlot := range sortedKeys(item.Components) {
			part, live := w.tuning.Tables.Resolve("component", item.Components[oldSlot])
			if !live || part.ID == "" {
				continue
			}
			component := w.tuning.Tables.Components.Components[part.ID]
			if component.Blueprint == w.tuning.Tables.Weapons[item.Weapon].Blueprint {
				parts[component.Slot] = component.ID
			}
		}
		item.Components = parts
		items = append(items, item.Clone())
	}
	return items
}

// inventory is what the player may equip from: its permanent ledger and the
// crafted instances it owns.
func (w *World) inventory(p *Player) crafting.Inventory {
	return crafting.Inventory{Ledger: p.Unlocks, Items: p.Items}
}

// weapon resolves the player's equipped weapon from the tables each time it is
// needed, so nothing caches a stat snapshot. When the slot holds a crafted
// instance, its components are applied on top of the row — again every time,
// which is what lets a balance edit retune a crafted item in place.
func (w *World) weapon(p *Player) (tuning.Weapon, bool) {
	weapon, item, ok := w.inventory(p).Equipped(w.tuning.Tables, p.Loadout.Weapon)
	if !ok {
		return tuning.Weapon{}, false
	}
	weapon, _ = crafting.Apply(w.tuning.Tables, weapon, tuning.Ability{}, item.Components)
	return weapon, true
}

// bar is the player's action bar, resolved from the tables on every use for the
// same reason weapon is: a slot holds an ID, and what that ID means is whatever
// the tables say today.
func (w *World) bar(p *Player) []loadout.Slot {
	return loadout.Bar(w.tuning.Tables, p.Class, w.inventory(p), p.Loadout)
}

// selectedSlot is the bar position the use button acts through. A selection
// past the end of the bar — an old client, or a bar that shrank under a
// content change — falls back to the first slot rather than doing nothing.
func (w *World) selectedSlot(p *Player) (loadout.Slot, bool) {
	slots := w.bar(p)
	if len(slots) == 0 {
		return loadout.Slot{}, false
	}
	if p.Selected < 0 || p.Selected >= len(slots) {
		return slots[0], true
	}
	return slots[p.Selected], true
}

// ErrLoadoutLocked is the keystone economy rule: the equipped set is committed
// to before leaving safety and cannot be rearranged in the field, so owning
// more options improves preparation and never the power carried into one fight.
var ErrLoadoutLocked = errors.New("Your loadout is locked outside a safe zone. Return to the hub to change it.")

// ErrLoadoutUnavailable reports a body that cannot commit a change: dead, or
// lingering after a disconnect.
var ErrLoadoutUnavailable = errors.New("You cannot change your loadout right now.")

// InSafety reports whether the body stands where loadout and crafting services
// are available. Phase 3 replaces radius-from-origin with per-outpost radii.
func (w *World) InSafety(p *Player) bool {
	return p.Position.LengthSq() <= w.tuning.SafeRadius*w.tuning.SafeRadius
}

// SetLoadout commits a requested set. Respec is free — nothing is charged and
// nothing is consumed — so the only gates are the safe-zone lock and the
// legality of the set itself. A rejected request changes nothing.
func (w *World) SetLoadout(id string, requested model.Loadout, now time.Time) (model.Loadout, error) {
	p := w.players[id]
	if p == nil {
		return model.Loadout{}, ErrLoadoutUnavailable
	}
	if !p.Alive || p.Lingering() {
		return p.Loadout.Clone(), ErrLoadoutUnavailable
	}
	if !w.InSafety(p) {
		return p.Loadout.Clone(), ErrLoadoutLocked
	}
	requested.Version = w.tuning.Tables.Manifest.Version
	if err := loadout.Validate(w.tuning.Tables, p.Class, w.inventory(p), requested); err != nil {
		return p.Loadout.Clone(), err
	}
	p.Loadout = requested.Clone()
	p.RespecOwed = false
	// A committed change is a fresh kit: the new weapon arrives loaded and no
	// ability carries a lockout earned by the one it replaced. Both are only
	// reachable inside safety, so neither can be used to refresh mid-fight.
	p.Ammo, p.ReloadEnds = 0, time.Time{}
	if weapon, ok := w.weapon(p); ok {
		p.Ammo = weapon.MagazineSize
	}
	p.Cooldowns, p.NextFire = make(map[string]time.Time), now
	if slots := w.bar(p); p.Selected >= len(slots) {
		p.Selected = 0
	}
	return p.Loadout.Clone(), nil
}

// ErrCraftingLocked is the other half of the safe-zone economy rule: raw
// materials have to be hauled back to safety before they become anything.
var ErrCraftingLocked = errors.New("Crafting is only available inside a safe zone. Haul your materials back to the hub.")

// ErrCraftingUnavailable reports a body that cannot craft: dead, or lingering
// after a disconnect.
var ErrCraftingUnavailable = errors.New("You cannot craft right now.")

// CraftRequest is one requested build: a client preview and the component
// filling each required blank. The complete parts determine the weapon.
type CraftRequest struct {
	Weapon     string
	Components map[string]string
}

// Craft builds one item and charges its materials. Every gate is here rather
// than split with the caller: the safe-zone rule, the recipe's legality, and
// whether the materials are actually carried. A refused craft spends nothing and
// leaves no item — there is no partial outcome to report or roll back.
func (w *World) Craft(id string, request CraftRequest, itemID string) (model.CraftedItem, error) {
	p := w.players[id]
	if p == nil {
		return model.CraftedItem{}, ErrCraftingUnavailable
	}
	// A developer fixture is a test target, not an economy: it has no character
	// row to hang an item off and nothing about it is ever saved.
	if !p.Alive || p.Lingering() || p.AdminSpawned {
		return model.CraftedItem{}, ErrCraftingUnavailable
	}
	if !w.InSafety(p) {
		return model.CraftedItem{}, ErrCraftingLocked
	}
	if capacity := w.tuning.Tables.Progression.CraftedItemCapacity; len(p.Items) >= capacity {
		return model.CraftedItem{}, fmt.Errorf("You can only keep %d crafted weapons. Nothing was spent.", capacity)
	}
	components := filledSlots(request.Components)
	weaponID, err := crafting.Result(w.tuning.Tables, components)
	if err != nil {
		return model.CraftedItem{}, err
	}
	if request.Weapon != "" && request.Weapon != weaponID {
		return model.CraftedItem{}, fmt.Errorf("Those parts build %s, not %s.", w.tuning.Tables.Weapons[weaponID].Name, w.tuning.Tables.Weapons[request.Weapon].Name)
	}
	if err := crafting.Validate(w.tuning.Tables, p.Class, w.inventory(p), weaponID, components); err != nil {
		return model.CraftedItem{}, err
	}
	cost := crafting.Cost(w.tuning.Tables, weaponID, components)
	if short := crafting.Shortfall(cost, p.Materials); len(short) > 0 {
		return model.CraftedItem{}, w.shortfallError(short)
	}
	crafting.Spend(p.Materials, cost)
	item := model.CraftedItem{ID: itemID, CharacterID: p.ID, Weapon: weaponID, Components: components}
	p.Items = append(p.Items, item)
	return item.Clone(), nil
}

// CraftAmmunition builds one batch of special ammunition and charges its
// materials. It runs the same gates a weapon craft does — safe zone, a live
// recipe for the class, and materials actually carried — and lands in the same
// carried inventory the ability that fires it spends from. Unlike a weapon it
// leaves no item and no capacity pressure: what it makes is a finite stack that
// the launcher burns back down.
func (w *World) CraftAmmunition(id, recipeID string) (map[string]int, error) {
	p := w.players[id]
	if p == nil {
		return nil, ErrCraftingUnavailable
	}
	if !p.Alive || p.Lingering() || p.AdminSpawned {
		return nil, ErrCraftingUnavailable
	}
	if !w.InSafety(p) {
		return nil, ErrCraftingLocked
	}
	recipe, err := crafting.ValidateAmmunition(w.tuning.Tables, p.Class, recipeID)
	if err != nil {
		return nil, err
	}
	if short := crafting.Shortfall(recipe.Cost, p.Materials); len(short) > 0 {
		return nil, w.shortfallError(short)
	}
	crafting.Spend(p.Materials, recipe.Cost)
	p.Materials[recipe.Material] += recipe.Count
	return p.CarriedMaterials(), nil
}

// shortfallError names what is missing and how much of it, because "you need
// three more tempered plate" is something a player can act on and "you cannot
// afford this" is not.
func (w *World) shortfallError(short map[string]int) error {
	parts := make([]string, 0, len(short))
	for _, material := range sortedKeys(short) {
		name := w.tuning.Tables.Materials.Materials[material].Name
		if name == "" {
			name = material
		}
		parts = append(parts, fmt.Sprintf("%d more %s", short[material], name))
	}
	return fmt.Errorf("You are short %s.", strings.Join(parts, ", "))
}

// filledSlots drops empty wire pairs so they are never persisted as references
// to nothing. Recipe resolution still requires every real blueprint slot.
func filledSlots(components map[string]string) map[string]string {
	filled := make(map[string]string, len(components))
	for slot, component := range components {
		if slot != "" && component != "" {
			filled[slot] = component
		}
	}
	return filled
}

// GrantMaterials adds to what a body carries and reports the inventory it
// leaves behind. It is the developer-mode seam only: harvesting (Phase 4.1) is
// how a material legitimately enters the world, and the HTTP layer authorizes
// the caller before this is reached.
func (w *World) GrantMaterials(id string, grants map[string]int) (map[string]int, error) {
	p := w.players[id]
	if p == nil {
		return nil, errors.New("game: player is not in the world")
	}
	bound := w.tuning.Tables.Materials.AdminGrant
	for _, material := range sortedKeys(grants) {
		count := grants[material]
		if !w.tuning.Tables.Live("material", material) {
			return nil, fmt.Errorf("unknown material %q", material)
		}
		if bound.Minimum == nil || bound.Maximum == nil || float64(count) < *bound.Minimum || float64(count) > *bound.Maximum {
			return nil, fmt.Errorf("grant of %d %s is outside the permitted range", count, material)
		}
		p.Materials[material] += count
	}
	return p.CarriedMaterials(), nil
}

// CarriedMaterials is a copy of what the body holds, safe for a caller to keep.
// GrantProgress sets a body's level and grants everything the levels it now
// holds unlock. It is the developer-mode seam only: a player kill is the one
// legitimate XP trigger until Phase 4.3, so without this nothing above the
// opening kit can be reached — and therefore exercised — on a fresh server. The
// HTTP layer authorizes the caller before this is reached.
//
// Lowering a level never confiscates an unlock: the ledger is permanent by
// design, and a grant is not a loan.
func (w *World) GrantProgress(id string, level int) (model.Progress, error) {
	p := w.players[id]
	if p == nil {
		return model.Progress{}, errors.New("game: player is not in the world")
	}
	bound := w.tuning.Tables.Progression.AdminGrant
	if bound.Minimum == nil || bound.Maximum == nil || float64(level) < *bound.Minimum || float64(level) > *bound.Maximum {
		return model.Progress{}, fmt.Errorf("level %d is outside the permitted range", level)
	}
	p.Level, p.XP = level, 0
	p.Unlocks, _ = p.Unlocks.With(w.tuning.Tables.UnlocksThrough(level)...)
	p.ProgressDirty = true
	return progression.Progress(p.Level, p.XP, p.Unlocks), nil
}

func (p *Player) CarriedMaterials() map[string]int {
	carried := make(map[string]int, len(p.Materials))
	for material, count := range p.Materials {
		carried[material] = count
	}
	return carried
}

// Carried reports what a body holds and the crafted items it owns, which is what
// the crafting and inventory surfaces are drawn from.
func (w *World) Carried(id string) (map[string]int, []model.CraftedItem, bool) {
	p := w.players[id]
	if p == nil {
		return nil, nil, false
	}
	items := make([]model.CraftedItem, 0, len(p.Items))
	for _, item := range p.Items {
		items = append(items, item.Clone())
	}
	return p.CarriedMaterials(), items, true
}

func (w *World) RemovePlayer(id string) {
	if p := w.players[id]; p != nil && w.occupants[p.AccountID] == id {
		delete(w.occupants, p.AccountID)
	}
	delete(w.players, id)
	delete(w.history, id)
	for telegraphID, telegraph := range w.telegraphs {
		if telegraph.OwnerID == id {
			delete(w.telegraphs, telegraphID)
		}
	}
	w.combat.resetTarget(id)
}

func (w *World) ApplyInput(id string, input protocol.Input) bool {
	p := w.players[id]
	if p == nil || input.Sequence <= p.Input.Sequence {
		return false
	}
	if math.IsNaN(float64(input.AimX)) || math.IsNaN(float64(input.AimY)) {
		return false
	}
	p.Input = input
	return true
}

func (w *World) Respawn(id string, now time.Time) bool {
	p := w.players[id]
	if p == nil || p.Alive {
		return false
	}
	p.Position, p.Velocity, p.DashDirection, p.DashTicksLeft = Vec{}, Vec{}, Vec{}, 0
	p.cancelDelete()
	p.restoreHealth()
	p.Mana = w.tuning.MaxMana
	if weapon, ok := w.weapon(p); ok {
		p.Ammo = weapon.MagazineSize
	}
	// A fresh body carries neither the statuses that killed it nor the
	// cooldowns it died holding.
	p.Effects, p.Cooldowns = nil, make(map[string]time.Time)
	p.NextFire, p.ReloadEnds, p.DashReady = now, now, now
	w.combat.resetTarget(id)
	w.recordHistory(p, now)
	return true
}

func (w *World) Step(now time.Time) {
	dt := 1 / float64(w.tuning.TickRate)
	w.tick++
	ids := sortedPlayerIDs(w.players)
	for _, id := range ids {
		w.stepPlayer(w.players[id], now, dt)
	}
	w.stepTelegraphs(now)
	w.stepProjectiles(now, dt)
	w.stepDeployables(now)
	w.reapDeleted(now)
	for _, id := range ids {
		if p := w.players[id]; p != nil {
			w.recordHistory(p, now)
		}
	}
}

func (w *World) reapDeleted(now time.Time) {
	for id, projectile := range w.projectiles {
		if projectile.deleteComplete(now) {
			delete(w.projectiles, id)
		}
	}
	for id, telegraph := range w.telegraphs {
		if telegraph.deleteComplete(now) {
			delete(w.telegraphs, id)
		}
	}
	for id, deployable := range w.deployables {
		if deployable.deleteComplete(now) {
			delete(w.deployables, id)
		}
	}
	for index, item := range w.worldItems {
		if item != nil && item.deleteComplete(now) {
			w.worldItems[index] = nil
		}
	}
	for id, player := range w.players {
		// Connected characters remain as ordinary dead bodies until respawn.
		if player.AdminSpawned && player.deleteComplete(now) {
			w.RemovePlayer(id)
		}
	}
}

func (w *World) stepPlayer(p *Player, now time.Time, dt float64) {
	// Statuses run before the body acts, and on a lingering body too: a burn
	// left on someone who disconnects keeps burning.
	w.stepEffects(p, now)
	// A lingering body is a target, not an actor: it holds its ground, takes
	// damage, and neither moves nor fires until the logout window closes.
	if !p.Alive || p.Lingering() {
		p.Velocity, p.DashTicksLeft = Vec{}, 0
		p.Scoped, p.Guarding = false, false
		p.Acknowledged = p.Input.Sequence
		return
	}
	// The selected bar slot travels with the input, so the server resolves the
	// use button against the same slot the player was looking at. An
	// out-of-range index is clamped rather than rejected.
	if slots := len(w.bar(p)); slots > 0 {
		p.Selected = int(p.Input.SelectedSlot) % slots
	}
	aim := Vec{float64(p.Input.AimX), float64(p.Input.AimY)}.Normalized()
	if aim.LengthSq() > 0 {
		p.Aim = aim
	}
	move := Vec{}
	if p.Input.Buttons&ButtonUp != 0 {
		move.Y--
	}
	if p.Input.Buttons&ButtonDown != 0 {
		move.Y++
	}
	if p.Input.Buttons&ButtonLeft != 0 {
		move.X--
	}
	if p.Input.Buttons&ButtonRight != 0 {
		move.X++
	}
	move = move.Normalized()
	// A stun suppresses everything the body does; a root only takes its
	// movement, leaving it able to aim, reload, and act.
	stunned, rooted := w.stunned(p), w.rooted(p)
	// The two committed stances are derived from held input every tick, so
	// nothing can be left raised by a dropped frame. A stun drops both.
	guard, _ := w.guard(p)
	p.Guarding = guard != nil && !stunned && p.Input.Buttons&ButtonFire != 0
	p.Scoped = !stunned && !p.Guarding && p.Input.Buttons&ButtonScope != 0 && w.scope(p) != nil
	if p.Input.Buttons&ButtonDash != 0 && p.PreviousButtons&ButtonDash == 0 && !now.Before(p.DashReady) && !stunned && !rooted && p.Mass >= 0 {
		p.DashDirection = move
		if p.DashDirection.LengthSq() == 0 {
			p.DashDirection = p.Aim
		}
		p.DashTicksLeft = w.tuning.dashTicks()
		p.DashReady = now.Add(w.tuning.DashCooldown)
	}
	switch knocked, knockedBack := w.knockback(p); {
	case p.Mass < 0:
		p.Velocity, p.DashTicksLeft = Vec{}, 0
	case knockedBack:
		// A knockback overrides input and cancels an in-flight dash: control
		// beats mobility for as long as it runs.
		p.Velocity, p.DashTicksLeft = knocked, 0
	case stunned || rooted:
		p.Velocity, p.DashTicksLeft = Vec{}, 0
	case p.DashTicksLeft > 0:
		p.Velocity = p.DashDirection.Mul(w.tuning.dashSpeed())
		p.DashTicksLeft--
	default:
		p.Velocity = move.Mul(w.tuning.PlayerSpeed * p.SpeedMultiplier * w.movementScale(p) * w.handlingScale(p))
	}
	p.Position = w.moveCircle(p.Position, p.Velocity.Mul(dt), p.circleRadius())
	if p.Class == model.Mage {
		p.Mana = math.Min(w.tuning.MaxMana, p.Mana+w.tuning.ManaRegen*dt)
	}
	// Magazine size and reload time are weapon properties; a weapon without a
	// magazine (a staff) never enters the reload path.
	if weapon, ok := w.weapon(p); ok && weapon.MagazineSize > 0 && !stunned {
		if !p.ReloadEnds.IsZero() && !now.Before(p.ReloadEnds) {
			p.Ammo, p.ReloadEnds = weapon.MagazineSize, time.Time{}
		}
		if p.Input.Buttons&ButtonReload != 0 && p.ReloadEnds.IsZero() && p.Ammo < weapon.MagazineSize {
			p.ReloadEnds = now.Add(weapon.ReloadDuration())
		}
	}
	// A raised shield locks fire: the use button is what holds it up, so it
	// cannot also be what shoots through it.
	if p.Input.Buttons&ButtonFire != 0 && !stunned && !p.Guarding {
		w.useAbility(p, now)
	}
	p.PreviousButtons = p.Input.Buttons
	p.Acknowledged = p.Input.Sequence
}

func (w *World) stepProjectiles(now time.Time, dt float64) {
	ids := make([]string, 0, len(w.projectiles))
	for id := range w.projectiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := w.projectiles[id]
		if p.Deleting {
			continue
		}
		p.Remaining -= dt
		if p.Mass < 0 {
			p.Velocity = Vec{}
		}
		if p.Remaining <= 0 || w.advanceProjectile(p, dt, now, false) {
			// A thrown field lands wherever its round stopped, whether that was
			// an impact, the rim, or simply running out of throw.
			w.deployFrom(p, p.Position, now)
			delete(w.projectiles, id)
		}
	}
}

func (w *World) advanceProjectile(projectile *Projectile, dt float64, at time.Time, historical bool) bool {
	from, to := projectile.Position, projectile.Position.Add(projectile.Velocity.Mul(dt))
	for _, item := range w.worldItems {
		if item == nil {
			continue
		}
		if item.intersectsSegment(from, to, projectile.circleRadius()) {
			item.TakeDamage(projectile.hitDamage())
			w.resolveBlast(projectile, from, at)
			return true
		}
	}
	for _, id := range sortedPlayerIDs(w.players) {
		target := w.players[id]
		if id == projectile.OwnerID || !target.Alive {
			continue
		}
		position := target.Position
		if historical {
			position = w.positionAt(id, at)
		}
		if !segmentCircle(from, to, position, projectile.circleRadius()+target.circleRadius()) {
			continue
		}
		// A raised shield stops what flies into its arc without stopping what
		// goes off around it, so the round is consumed and any blast it carries
		// still resolves where it was stopped.
		if w.blockedBy(target, from) {
			w.resolveBlast(projectile, from, at)
			return true
		}
		// PvP protection covers the hit whole: no damage, and no status either.
		// A slow or a knockback landed from inside safety would be exactly the
		// offensive use of a safe zone the invariant forbids.
		if w.hostileReach(w.players[projectile.OwnerID], position) {
			w.damage(target, projectile.hitDamage(), projectile.OwnerID, at)
			w.applyEffects(target, projectile.Effects, projectile.OwnerID, to.Sub(from), at)
		}
		w.resolveBlast(projectile, from, at)
		return true
	}
	projectile.Position = to
	projectile.Travelled += math.Sqrt(to.Sub(from).LengthSq())
	// A hard maximum range stops a round the way the rim does: it is the
	// weapon's reach, not its lifetime, and beyond it nothing lands at all.
	if projectile.Spec.MaxRange > 0 && projectile.Travelled >= projectile.Spec.MaxRange {
		return true
	}
	return to.LengthSq() > w.tuning.WorldRadius*w.tuning.WorldRadius
}

// resolveBlast detonates an impact that carries an area, and does nothing for
// an ordinary round.
func (w *World) resolveBlast(projectile *Projectile, at Vec, when time.Time) {
	if projectile.Blast != nil {
		w.detonate(projectile, at, when)
	}
}

// damage is the one path health is lost through: shields absorb first, the
// contribution ledger records only effective health damage, and a body that
// reaches zero dies. The lethal event freezes the full per-life ledger for
// later drop ownership and ranking consumers.
func (w *World) damage(target *Player, amount float64, sourceID string, at time.Time) {
	if !target.Alive || amount <= 0 {
		return
	}
	amount = w.absorb(target, amount)
	if amount <= 0 {
		return
	}
	applied, destroyed := target.TakeDamage(amount)
	w.combat.recordDamage(at, sourceID, target.ID, applied, destroyed)
	if destroyed {
		target.Velocity, target.Effects, target.DashTicksLeft = Vec{}, nil, 0
		w.cancelTelegraphs(target.ID, at)
		w.creditKill(target)
	}
}

// creditKill awards the kill's XP to whoever the combat log credits — most
// damage dealt, not the last hit — so a squad's finisher does not take the
// progression its damage dealer earned. Developer fixtures are excluded on both
// sides: an admin-spawned body is a test target, not an economy.
func (w *World) creditKill(target *Player) {
	if target.AdminSpawned {
		return
	}
	kill, ok := w.combat.lastKill(target.ID)
	if !ok || kill.CreditID == "" || kill.CreditID == target.ID {
		return
	}
	killer := w.players[kill.CreditID]
	if killer == nil || killer.AdminSpawned {
		return
	}
	w.awardXP(killer, tuning.SourcePlayerKill)
}

// awardXP credits one occurrence of a source and grants whatever the levels it
// crossed unlock. Nothing here touches combat power: a level buys access to more
// options, never a bigger number, which is what keeps the band compressed.
func (w *World) awardXP(p *Player, source string) {
	award := w.tuning.Tables.Progression.Award(source)
	if award <= 0 || p.AdminSpawned {
		return
	}
	level, xp, granted := progression.Advance(w.tuning.Tables, p.Level, p.XP, award)
	p.Level, p.XP = level, xp
	if len(granted) > 0 {
		p.Unlocks, _ = p.Unlocks.With(granted...)
	}
	p.ProgressDirty = true
}

// Progress is the character's permanent axis as it stands on the body.
func (w *World) Progress(id string) (model.Progress, bool) {
	p := w.players[id]
	if p == nil || p.AdminSpawned {
		return model.Progress{}, false
	}
	return progression.Progress(p.Level, p.XP, p.Unlocks), true
}

// DrainProgress reports every body whose permanent progression has changed since
// the last drain and clears the marks, in deterministic order. The engine
// persists each and tells its owner; the world never writes or sends.
func (w *World) DrainProgress() map[string]model.Progress {
	changed := make(map[string]model.Progress)
	for _, id := range sortedPlayerIDs(w.players) {
		p := w.players[id]
		if !p.ProgressDirty {
			continue
		}
		p.ProgressDirty = false
		if progress, ok := w.Progress(id); ok {
			changed[id] = progress
		}
	}
	return changed
}

func (w *World) moveCircle(from, delta Vec, radius float64) Vec {
	result := Vec{from.X + delta.X, from.Y}
	if w.collides(result, radius) {
		result.X = from.X
	}
	result.Y += delta.Y
	if w.collides(result, radius) {
		result.Y = from.Y
	}
	limit := w.tuning.WorldRadius - radius
	if length := math.Sqrt(result.LengthSq()); length > limit {
		result = result.Mul(limit / length)
	}
	return result
}

func (w *World) collides(position Vec, radius float64) bool {
	for _, item := range w.worldItems {
		if item == nil {
			continue
		}
		if item.intersectsCircle(position, radius) {
			return true
		}
	}
	return false
}

func (w *World) recordHistory(p *Player, now time.Time) {
	samples := append(w.history[p.ID], historySample{at: now, position: p.Position})
	cutoff := now.Add(-w.tuning.MaxRewind - 50*time.Millisecond)
	first := 0
	for first < len(samples)-1 && samples[first].at.Before(cutoff) {
		first++
	}
	w.history[p.ID] = samples[first:]
}

func (w *World) positionAt(id string, at time.Time) Vec {
	samples := w.history[id]
	if len(samples) == 0 {
		if p := w.players[id]; p != nil {
			return p.Position
		}
		return Vec{}
	}
	if at.Before(samples[0].at) {
		return samples[0].position
	}
	for i := 1; i < len(samples); i++ {
		if at.After(samples[i].at) {
			continue
		}
		a, b := samples[i-1], samples[i]
		span := b.at.Sub(a.at)
		if span <= 0 {
			return b.position
		}
		t := float64(at.Sub(a.at)) / float64(span)
		return a.position.Add(b.position.Sub(a.position).Mul(t))
	}
	return samples[len(samples)-1].position
}

// generateWorldItems materializes authored fixtures first, then lays out
// deterministic trees around them. A dense slice is both faster than an
// interface collection now and shaped like a future ECS component column.
func generateWorldItems(tables *tuning.Tables) []*Entity {
	world, trees := tables.World, tables.World.Trees
	result := make([]*Entity, 0, len(world.Fixtures)+trees.Count)
	for _, fixture := range world.Fixtures {
		entity := newEntity(fixture.ID, fixture.Entity, Vec{fixture.Position[0], fixture.Position[1]}, tables.Entities[fixture.Entity], EntityOverrides{})
		result = append(result, &entity)
	}
	safeRadius := world.SafeRadius()
	// Trees start InnerMargin outside the safe radius and stop OuterMargin
	// short of the rim, so neither the hub nor the world edge is walled in.
	reach := world.Radius - safeRadius - trees.OuterMargin
	if trees.Count <= 0 || reach <= trees.InnerMargin || trees.RadiusSpread < 1 {
		return result
	}
	base := tables.Entities["tree"]
	baseEntity := newEntity("", "tree", Vec{}, base, EntityOverrides{})
	baseRadius := baseEntity.circleRadius()
	state := trees.Seed
	placed := 0
	for placed < trees.Count {
		state = state*6364136223846793005 + 1442695040888963407
		a := float64(state%62832) / 10000
		state = state*6364136223846793005 + 1442695040888963407
		r := safeRadius + trees.InnerMargin + float64(state%uint64(reach))
		position := Vec{math.Cos(a) * r, math.Sin(a) * r}
		radius := baseRadius + float64(state%uint64(trees.RadiusSpread))
		clear := true
		for _, other := range result {
			if other.intersectsCircle(position, radius+trees.Spacing) {
				clear = false
				break
			}
		}
		if clear {
			objects := collisionObjectsFromTuning(base.CollisionObjects)
			objects[0].Radius = radius
			entity := newEntity(fmt.Sprintf("tree-%02d", placed), "tree", position, base, EntityOverrides{CollisionObjects: &objects})
			result = append(result, &entity)
			placed++
		}
	}
	return result
}

func segmentCircle(a, b, center Vec, radius float64) bool {
	ab := b.Sub(a)
	denominator := ab.LengthSq()
	if denominator == 0 {
		return a.Sub(center).LengthSq() <= radius*radius
	}
	t := center.Sub(a).X*ab.X + center.Sub(a).Y*ab.Y
	t = math.Max(0, math.Min(1, t/denominator))
	closest := a.Add(ab.Mul(t))
	return closest.Sub(center).LengthSq() <= radius*radius
}

func hash(value string) uint64 {
	var h uint64 = 1469598103934665603
	for i := range value {
		h ^= uint64(value[i])
		h *= 1099511628211
	}
	return h
}

func sortedPlayerIDs(players map[string]*Player) []string {
	return sortedKeys(players)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
