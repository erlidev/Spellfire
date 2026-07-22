package game

import (
	"math"
	"time"

	"spellfire/server/internal/loadout"
	"spellfire/server/internal/tuning"
)

// Gunplay is the Gunslinger's mastery axis: where a shot actually goes, what a
// committed stance costs, and how far a round stays lethal. Nothing here reads
// a class — a weapon either declares a recoil pattern, a spread, a scope, or a
// guard, or it does not — so a Mage staff runs the same path and simply has none
// of them.
//
// Two rules hold the whole file together. Recoil is a fixed left/right pattern
// unique to each gun, so a burst is a shape a player learns rather than a random
// cone; and move-spread is a deterministic draw, seeded from the shooter and its
// own shot count, so a test can reproduce a shot exactly while a player cannot
// predict one.

// guard is the barrier the selected slot holds, and the ability it belongs to.
// It is nil for every slot that is not a raised shield.
func (w *World) guard(p *Player) (*tuning.Guard, tuning.Ability) {
	ability, ok := w.ability(p)
	if !ok || ability.Guard == nil {
		return nil, tuning.Ability{}
	}
	return ability.Guard, ability
}

// scope is the committed aiming mode of the equipped weapon, and nil for a
// weapon that has none.
func (w *World) scope(p *Player) *tuning.Scope {
	weapon, ok := w.weapon(p)
	if !ok {
		return nil
	}
	return weapon.Scope
}

// handlingScale is what the equipped kit does to movement: the weight class the
// weapon is balanced on, the scope it is looking through, and the shield it is
// holding up. It is separate from movementScale, which is what statuses do,
// because the two compose rather than compete — a slowed body carrying an LMG
// pays both.
func (w *World) handlingScale(p *Player) float64 {
	scale := 1.0
	if weapon, ok := w.weapon(p); ok {
		scale *= w.tuning.Tables.WeightOf(weapon).MovementMultiplier
		if p.Scoped && weapon.Scoped() {
			scale *= weapon.Scope.MovementMultiplier
		}
	}
	if guard, _ := w.guard(p); guard != nil && p.Guarding {
		scale *= guard.MovementMultiplier
	}
	return scale
}

// carriedGuard is the barrier the body is holding durability for, whatever slot
// it currently has selected. It is nil until a shield has been raised at least
// once.
func (w *World) carriedGuard(p *Player) *tuning.Guard {
	if p.ShieldAbility == "" {
		return nil
	}
	ability, ok := w.tuning.Tables.Abilities[p.ShieldAbility]
	if !ok {
		return nil
	}
	return ability.Guard
}

// stepGuard keeps the raised barrier's durability current. A lowered shield
// recovers after a quiet window; a raised one never repairs itself, so holding
// it up under fire is what spends it. It returns to service only once it is
// whole again, which is what stops a broken shield from being flickered back up
// for a single point of durability.
func (w *World) stepGuard(p *Player, now time.Time, dt float64) {
	if selected, ability := w.guard(p); selected != nil && p.ShieldAbility != ability.ID {
		// A barrier the body was not already carrying is taken up whole.
		p.ShieldAbility, p.Shield, p.ShieldBroken, p.ShieldHitAt = ability.ID, selected.Durability, false, time.Time{}
		return
	}
	// Recovery is resolved from the shield the body owns rather than the slot it
	// is looking at, so a spent shield keeps repairing while its owner fights on
	// with the gun in the next slot.
	guard := w.carriedGuard(p)
	if guard == nil || p.Guarding || p.Shield >= guard.Durability {
		return
	}
	if !p.ShieldHitAt.IsZero() && now.Sub(p.ShieldHitAt) < guard.RegenDelay() {
		return
	}
	p.Shield = math.Min(guard.Durability, p.Shield+guard.RegenPerSecond*dt)
	if p.Shield >= guard.Durability {
		p.ShieldBroken = false
	}
}

// guardHealth is the durability the body's current guard has left, and zero for
// a slot that is not a shield or a shield that is spent.
func (w *World) guardHealth(p *Player) float64 {
	guard, ability := w.guard(p)
	if guard == nil {
		return 0
	}
	// A shield the body has never raised is whole; stepGuard has not run for it
	// yet on the tick the slot is first selected.
	if p.ShieldAbility != ability.ID {
		return guard.Durability
	}
	if p.ShieldBroken {
		return 0
	}
	return p.Shield
}

// guardDurability is the full pool the body's current guard is spent from, and
// zero for a slot that is not a shield. The wire carries it beside what is left
// so a client can draw the bar without a table lookup of its own.
func (w *World) guardDurability(p *Player) float64 {
	guard, _ := w.guard(p)
	if guard == nil {
		return 0
	}
	return guard.Durability
}

// guardAbsorb spends the raised shield on one impact and reports the damage
// that carries past it. The shield's durability is its health: it stops exactly
// what it can pay for, the overflow reaches the body, and a shield drained to
// zero breaks and drops.
func (w *World) guardAbsorb(target *Player, amount float64, now time.Time) float64 {
	guard, ability := w.guard(target)
	if guard == nil {
		return amount
	}
	if target.ShieldAbility != ability.ID {
		target.ShieldAbility, target.Shield, target.ShieldBroken = ability.ID, guard.Durability, false
	}
	target.ShieldHitAt = now
	taken := math.Min(target.Shield, amount)
	target.Shield -= taken
	if target.Shield <= 0 {
		target.Shield, target.ShieldBroken, target.Guarding = 0, true, false
	}
	return amount - taken
}

// blockedBy reports whether a raised shield stops an impact arriving from a
// direction. The arc is measured against where the body is aiming, so covering
// one angle is always leaving another open, and a shield with no durability left
// stops nothing at all.
func (w *World) blockedBy(target *Player, from Vec) bool {
	if !target.Guarding {
		return false
	}
	guard, _ := w.guard(target)
	if guard == nil || w.guardHealth(target) <= 0 {
		return false
	}
	toward := from.Sub(target.Position).Normalized()
	if toward.LengthSq() == 0 {
		return false
	}
	facing := target.Aim.Normalized()
	return guard.Blocks(facing.X, facing.Y, toward.X, toward.Y)
}

// firingDirections is where the rounds of one use actually go. It walks the
// weapon's recoil pattern, adds the spread the body's own speed earned, and
// fans a multi-pellet shot over its cone. A weapon with no gunplay — a staff —
// gets the aim vector back unchanged.
//
// It advances the recoil index as a side effect, because a shot is exactly what
// walks the muzzle: recovering the pattern is the reward for stopping.
func (w *World) firingDirections(p *Player, ability tuning.Ability, now time.Time) []Vec {
	weapon, hasWeapon := w.weapon(p)
	aim := p.Aim.Normalized()
	if aim.LengthSq() == 0 {
		aim = Vec{1, 0}
	}
	offset := 0.0
	// Gunplay belongs to the weapon slot alone: a staff has no pattern to walk
	// and no spread to draw, and a gadget is thrown by hand rather than fired
	// down the barrel, so neither walks the muzzle the gun is holding.
	if hasWeapon && w.firingWeapon(p) && (len(weapon.Recoil.Pattern) > 0 || weapon.Spread.MovingDegrees > 0) {
		weight := w.tuning.Tables.WeightOf(weapon)
		offset = w.walkRecoil(p, weapon, weight, now)
		offset += w.spreadDegrees(p, weapon, weight)
		p.Shot++
		p.LastShot = now
	}
	p.Fired++
	spec := ability.Projectile
	if spec == nil || spec.PelletCount() <= 1 {
		return []Vec{rotate(aim, offset)}
	}
	// A cone is laid out deterministically from its centre, so a shotgun's
	// pattern is a shape a player can learn to place rather than a dice roll.
	pellets := spec.PelletCount()
	directions := make([]Vec, 0, pellets)
	step := spec.PelletSpreadDegrees / float64(pellets-1)
	for index := 0; index < pellets; index++ {
		directions = append(directions, rotate(aim, offset-spec.PelletSpreadDegrees/2+float64(index)*step))
	}
	return directions
}

// firingWeapon reports whether the selected slot is the weapon itself, which is
// the only thing that walks the gun's recoil pattern.
func (w *World) firingWeapon(p *Player) bool {
	slot, ok := w.selectedSlot(p)
	return ok && slot.Kind == loadout.KindWeapon
}

// walkRecoil advances the equipped weapon's pattern by one shot and reports
// where the muzzle now sits, in degrees off aim. The offset persists between
// shots and each pattern entry is a step applied to it, so a burst walks the
// weapon along a fixed shape rather than teleporting it to one of a few fixed
// angles — which is what makes the pattern learnable and the drift visible.
//
// Enough quiet settles the offset back to aim and returns the pattern to its
// first entry, so stopping is the only thing that controls a gun.
func (w *World) walkRecoil(p *Player, weapon tuning.Weapon, weight tuning.WeightClass, now time.Time) float64 {
	recovery := weapon.Recoil.Recovery()
	offset := 0.0
	if !p.LastShot.IsZero() && recovery > 0 {
		if elapsed := now.Sub(p.LastShot); elapsed >= recovery {
			p.Shot = 0
		} else {
			offset = settledRecoil(p.RecoilPeak, elapsed, recovery)
		}
	}
	offset += weapon.Recoil.DegreesAt(p.Shot) * weight.RecoilMultiplier
	// A magazine longer than the pattern repeats it, so a pattern whose steps do
	// not sum to zero would otherwise wander off aim without limit.
	if limit := weapon.Recoil.MaxDegrees() * weight.RecoilMultiplier; limit > 0 {
		offset = math.Max(-limit, math.Min(limit, offset))
	}
	p.RecoilPeak = offset
	return offset
}

// recoilDegrees is where the muzzle is pointing right now relative to aim: what
// the last shot left, decayed by however much quiet has passed since. It is a
// pure function of the last shot rather than something integrated per tick, so
// a rewound resolve and a snapshot always agree on it, and it is what the
// snapshot carries so every client can see a weapon walk and settle.
func (w *World) recoilDegrees(p *Player, now time.Time) float64 {
	if p.LastShot.IsZero() || p.RecoilPeak == 0 {
		return 0
	}
	weapon, ok := w.weapon(p)
	if !ok {
		return 0
	}
	return settledRecoil(p.RecoilPeak, now.Sub(p.LastShot), weapon.Recoil.Recovery())
}

// settledRecoil decays an offset linearly to zero over the recovery window.
func settledRecoil(peak float64, since, recovery time.Duration) float64 {
	if recovery <= 0 || since >= recovery || since < 0 {
		return 0
	}
	return peak * (1 - float64(since)/float64(recovery))
}

// spreadDegrees is how far off aim this shot may land because of how the body is
// moving. Standing is the floor a settled aim earns; travelling at full speed
// costs the weapon's full moving spread, scaled by weight and reduced by a
// scope. The magnitude is a deterministic draw from the shooter and its shot
// count, so the same shot always lands the same way in a test.
func (w *World) spreadDegrees(p *Player, weapon tuning.Weapon, weight tuning.WeightClass) float64 {
	moving := math.Sqrt(p.Velocity.LengthSq()) / math.Max(1, w.tuning.PlayerSpeed)
	if moving > 1 {
		moving = 1
	}
	standing := weapon.Spread.StandingDegrees
	widest := standing + (weapon.Spread.MovingDegrees*weight.MoveSpreadMultiplier-standing)*moving
	if widest < standing {
		widest = standing
	}
	if p.Scoped && weapon.Scoped() {
		widest *= weapon.Scope.SpreadMultiplier
	}
	if widest <= 0 {
		return 0
	}
	// A draw in [-1,1) scaled by the cone's half-width: the shot may land either
	// side of aim, never further out than the weapon allows.
	return (splitmix(hash(p.ID)+p.Fired)*2 - 1) * widest / 2
}

// splitmix is a deterministic [0,1) draw from a counter. It is a mixing function
// rather than a stream, so a shot's spread depends only on who fired it and how
// many shots they had fired — never on process start or map order.
func splitmix(state uint64) float64 {
	state += 0x9e3779b97f4a7c15
	state = (state ^ (state >> 30)) * 0xbf58476d1ce4e5b9
	state = (state ^ (state >> 27)) * 0x94d049bb133111eb
	state ^= state >> 31
	return float64(state>>11) / float64(uint64(1)<<53)
}

// rotate turns a unit vector by an angle in degrees.
func rotate(v Vec, degrees float64) Vec {
	if degrees == 0 {
		return v
	}
	radians := degrees * math.Pi / 180
	sin, cos := math.Sin(radians), math.Cos(radians)
	return Vec{v.X*cos - v.Y*sin, v.X*sin + v.Y*cos}
}

// hitscan resolves a round that lands instantly, and reports whether it did.
// It is the sniper's exception and reaches here only from a scoped weapon: the
// blackout and the movement penalty a scope costs are the counterplay that
// replaces travel time, which is what the "scoped_commit" dodge vector names.
//
// Rewind applies exactly as it does to a fired projectile: the shot is resolved
// against where the targets were when the client saw them.
func (w *World) hitscan(p *Player, ability tuning.Ability, origin, direction Vec, at time.Time) bool {
	reach := ability.Projectile.HitscanRange
	end := origin.Add(direction.Mul(reach))
	// Cover stops a round before anything behind it, so the nearest world item
	// on the line wins over any target past it.
	nearest, blocked := reach, false
	for _, item := range w.worldItems {
		if item == nil {
			continue
		}
		if distance, ok := segmentEntry(origin, end, item, reach); ok && distance < nearest {
			nearest, blocked = distance, true
		}
	}
	var struck *Player
	for _, id := range sortedPlayerIDs(w.players) {
		target := w.players[id]
		if id == p.ID || !target.Alive {
			continue
		}
		position := w.positionAt(id, at)
		if !segmentCircle(origin, end, position, target.circleRadius()) {
			continue
		}
		if distance := math.Sqrt(position.Sub(origin).LengthSq()); distance < nearest {
			nearest, struck, blocked = distance, target, false
		}
	}
	if struck == nil {
		return blocked
	}
	if w.blockedBy(struck, origin) {
		// The shield spends its durability on the round, and only what that
		// pool could not cover reaches the body.
		if through := w.guardAbsorb(struck, p.hitscanDamage(w, ability, nearest), at); through > 0 && w.hostileReach(p, struck.Position) {
			w.damage(struck, through, p.ID, at)
		}
		return true
	}
	if w.hostileReach(p, struck.Position) {
		damage := p.hitscanDamage(w, ability, nearest)
		w.damage(struck, damage, p.ID, at)
		w.applyEffects(struck, ability.Effects, p.ID, direction, at)
	}
	return true
}

// pelletDamage is what one body of a use is worth. A multi-pellet shot divides
// the band between its pellets, so a full cone connecting is worth exactly one
// band hit and a grazing one is worth less: the shotgun's identity is the
// condition it imposes, never extra damage.
func (w *World) pelletDamage(ability tuning.Ability) float64 {
	damage := w.tuning.Tables.BandDamage(ability.DamageBand) * ability.DamageScale()
	if ability.Projectile == nil {
		return damage
	}
	return damage / float64(ability.Projectile.PelletCount())
}

// hitscanDamage is what an instant round is worth at the distance it landed:
// the band, after the same falloff a travelling round would have paid.
func (p *Player) hitscanDamage(w *World, ability tuning.Ability, distance float64) float64 {
	return w.tuning.Tables.BandDamage(ability.DamageBand) * ability.DamageScale() * ability.Projectile.DamageScale(distance)
}

// segmentEntry reports how far along a segment an entity is first touched.
func segmentEntry(from, to Vec, item *Entity, length float64) (float64, bool) {
	if !item.intersectsSegment(from, to, 0) {
		return 0, false
	}
	// The exact entry point is not worth a solve here: cover either stops the
	// round or it does not, and the distance only orders competing blockers.
	return math.Min(length, math.Sqrt(item.Position.Sub(from).LengthSq())), true
}

// hostileReach reports whether an attacker standing where it is may damage a
// body standing where that is. It is the same PvP-protection rule the projectile
// resolver applies, factored out so hitscan and blast cannot drift from it.
func (w *World) hostileReach(owner *Player, target Vec) bool {
	if owner == nil {
		return false
	}
	limit := w.tuning.PvPRadius * w.tuning.PvPRadius
	return owner.Position.LengthSq() > limit && target.LengthSq() > limit
}

// hazardReach is hostileReach for something standing in the world on its own —
// a developer-placed field with no caster behind it. PvP protection still holds
// over the body it would reach; what it cannot do is consult an attacker who
// does not exist.
func (w *World) hazardReach(owner *Player, ownerID string, target Vec) bool {
	if ownerID == "" {
		limit := w.tuning.PvPRadius * w.tuning.PvPRadius
		return target.LengthSq() > limit
	}
	return w.hostileReach(owner, target)
}

// detonate resolves an impact's area. Everyone inside the radius takes the
// band's damage and the blast's effects, pushed away from where it landed. A
// raised shield does not stop it: the shield blocks what flies into its arc,
// never what goes off around it.
func (w *World) detonate(projectile *Projectile, at Vec, when time.Time) {
	w.explode(projectile.OwnerID, at, projectile.Blast.Radius, projectile.Damage, projectile.BlastEffects, when, projectile.BlinkOnHit)
}

// explode is the area resolver every blast reaches, whether a round carried it
// to an impact or a telegraph placed it on the ground. It reports whether it
// actually reached anybody, which is what a cast that pays out on landing —
// Skyfall's blink — is conditioned on.
func (w *World) explode(ownerID string, at Vec, radius, damage float64, effects []string, when time.Time, blinkOnHit bool) bool {
	owner := w.players[ownerID]
	struck := false
	for _, id := range sortedPlayerIDs(w.players) {
		target := w.players[id]
		if id == ownerID || !target.Alive {
			continue
		}
		if target.Position.Sub(at).LengthSq() > radius*radius {
			continue
		}
		if !w.hostileReach(owner, target.Position) {
			continue
		}
		struck = true
		w.damage(target, damage, ownerID, when)
		w.applyEffects(target, effects, ownerID, target.Position.Sub(at), when)
	}
	// Landing it is what grants the reposition: a blink handed out for a missed
	// area would be free mobility with a damage roll attached.
	if struck && blinkOnHit && owner != nil {
		w.teleport(owner, at, when)
	}
	return struck
}

// teleport places a body exactly where an ability says, when that point is
// somewhere it could stand. Unlike a blink it does not walk: the destination is
// a place the world has already resolved something at.
func (w *World) teleport(p *Player, to Vec, now time.Time) {
	if !p.Alive || p.Lingering() || p.Mass < 0 || !w.standable(to) {
		return
	}
	p.Position, p.Velocity, p.DashTicksLeft = to, Vec{}, 0
	w.recordHistory(p, now)
}
