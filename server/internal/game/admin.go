package game

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

// AdminSpawn names an entity archetype from entities.json. Config is keyed by
// stable component.attribute bindings and Position is the clicked world point.
type AdminSpawn struct {
	ID       string
	Position Vec
	Config   map[string]string
}

// AdminEntityState is the generic editor payload. The UI resolves DefinitionID
// back to the shared tuning schema and fills it with these live values.
type AdminEntityState struct {
	ID           string            `json:"id"`
	DefinitionID string            `json:"definition_id"`
	Values       map[string]string `json:"values"`
}

type adminTarget struct {
	entity     *Entity
	player     *Player
	projectile *Projectile
	telegraph  *Telegraph
	deployable *Deployable
}

type adminAttributeAdapter struct {
	get func(adminTarget) (string, bool)
	set func(adminTarget, string) error
}

// adminAttributeRegistry is explicit by design: tuning and HTTP speak stable
// component.attribute IDs, while this one table knows today's storage layout.
// An ECS migration replaces these adapters, not the tuning schema or UI.
var adminAttributeRegistry = map[string]adminAttributeAdapter{
	"transform.position": {
		get: func(t adminTarget) (string, bool) {
			if t.entity == nil {
				return "", false
			}
			return formatAdminPosition(t.entity.Position), true
		},
		set: func(t adminTarget, value string) error {
			if t.entity == nil {
				return unsupportedAttribute("transform.position")
			}
			position, err := parseAdminPosition(value)
			if err != nil {
				return err
			}
			t.entity.Position = position
			return nil
		},
	},
	"deployable.radius": {
		get: func(t adminTarget) (string, bool) {
			if t.deployable == nil {
				return "", false
			}
			return formatNumber(t.deployable.Field.Radius), true
		},
		set: func(t adminTarget, value string) error {
			if t.deployable == nil {
				return unsupportedAttribute("deployable.radius")
			}
			radius, err := strconv.ParseFloat(value, 64)
			if err != nil || radius <= 0 {
				return fmt.Errorf("radius %q is not a positive number", value)
			}
			t.deployable.Field.Radius = radius
			return nil
		},
	},
	"transform.heading_degrees": {
		get: func(t adminTarget) (string, bool) {
			var direction Vec
			switch {
			case t.projectile != nil:
				direction = t.projectile.Velocity
			case t.telegraph != nil:
				direction = t.telegraph.Direction
			default:
				return "", false
			}
			return formatNumber(math.Atan2(direction.Y, direction.X) * 180 / math.Pi), true
		},
		set: func(t adminTarget, value string) error {
			direction := adminDirection(value)
			switch {
			case t.projectile != nil:
				speed := math.Sqrt(t.projectile.Velocity.LengthSq())
				t.projectile.Velocity = direction.Mul(speed)
			case t.telegraph != nil:
				t.telegraph.Direction = direction
			default:
				return unsupportedAttribute("transform.heading_degrees")
			}
			return nil
		},
	},
	"physics.mass":      numberAdapter(func(t adminTarget) *float64 { return &t.entity.Mass }),
	"vitals.max_health": numberAdapter(func(t adminTarget) *float64 { return &t.entity.MaxHealth }),
	"vitals.health": {
		get: func(t adminTarget) (string, bool) {
			if t.entity == nil {
				return "", false
			}
			return formatNumber(t.entity.Health), true
		},
		set: func(t adminTarget, value string) error {
			if t.entity == nil {
				return unsupportedAttribute("vitals.health")
			}
			t.entity.Health, _ = strconv.ParseFloat(value, 64)
			t.entity.Alive = t.entity.Health != 0
			if !t.entity.Alive {
				t.entity.Velocity = Vec{}
			}
			return nil
		},
	},
	"player.name": {
		get: func(t adminTarget) (string, bool) {
			if t.player == nil {
				return "", false
			}
			return t.player.Name, true
		},
		set: func(t adminTarget, value string) error {
			if t.player == nil {
				return unsupportedAttribute("player.name")
			}
			t.player.Name = value
			return nil
		},
	},
	"player.class": {
		get: func(t adminTarget) (string, bool) {
			if t.player == nil {
				return "", false
			}
			return string(t.player.Class), true
		},
		set: func(t adminTarget, value string) error {
			if t.player == nil {
				return unsupportedAttribute("player.class")
			}
			t.player.Class = model.Class(value)
			return nil
		},
	},
	"player.speed_multiplier": {
		get: func(t adminTarget) (string, bool) {
			if t.player == nil {
				return "", false
			}
			return formatNumber(t.player.SpeedMultiplier), true
		},
		set: func(t adminTarget, value string) error {
			if t.player == nil {
				return unsupportedAttribute("player.speed_multiplier")
			}
			t.player.SpeedMultiplier, _ = strconv.ParseFloat(value, 64)
			return nil
		},
	},
	"player.view_distance": {
		get: func(t adminTarget) (string, bool) {
			if t.player == nil {
				return "", false
			}
			return formatNumber(t.player.ViewDistance), true
		},
		set: func(t adminTarget, value string) error {
			if t.player == nil {
				return unsupportedAttribute("player.view_distance")
			}
			t.player.ViewDistance, _ = strconv.ParseFloat(value, 64)
			return nil
		},
	},
	"render.element": {
		get: func(t adminTarget) (string, bool) {
			switch {
			case t.projectile != nil:
				if t.projectile.Element == "" {
					return "none", true
				}
				return t.projectile.Element, true
			case t.telegraph != nil:
				return t.telegraph.Element, true
			}
			return "", false
		},
		set: func(t adminTarget, value string) error {
			if value == "none" {
				value = ""
			}
			switch {
			case t.projectile != nil:
				t.projectile.Element = value
			case t.telegraph != nil:
				t.telegraph.Element = value
			default:
				return unsupportedAttribute("render.element")
			}
			return nil
		},
	},
}

func numberAdapter(pointer func(adminTarget) *float64) adminAttributeAdapter {
	return adminAttributeAdapter{
		get: func(target adminTarget) (string, bool) {
			if target.entity == nil {
				return "", false
			}
			return formatNumber(*pointer(target)), true
		},
		set: func(target adminTarget, value string) error {
			if target.entity == nil {
				return unsupportedAttribute("number")
			}
			*pointer(target), _ = strconv.ParseFloat(value, 64)
			return nil
		},
	}
}

func unsupportedAttribute(attribute string) error {
	return fmt.Errorf("attribute %q does not apply to this entity", attribute)
}
func formatNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func (w *World) adminSpawn(request AdminSpawn, now time.Time) error {
	definition, ok := w.tuning.Tables.Entities[request.ID]
	if !ok || !definition.Admin.Spawnable {
		return fmt.Errorf("unknown spawnable %q", request.ID)
	}
	prototype := newEntity("", request.ID, Vec{}, definition, EntityOverrides{})
	extent := prototype.boundingRadius()
	if !adminPosition(request.Position, w.tuning.WorldRadius-extent) {
		return fmt.Errorf("placement is outside the world")
	}
	values, err := adminValues(definition.Admin.Fields, "spawn", request.Config)
	if err != nil {
		return err
	}
	switch request.ID {
	case "player":
		if !w.standable(request.Position) {
			return fmt.Errorf("a player cannot be placed inside cover")
		}
		return w.adminPlayer(definition, request.Position, values, now)
	case "projectile":
		return w.adminProjectile(request.Position, values)
	case "telegraph":
		return w.adminTelegraph(request.Position, values, now)
	case "smoke", "cinder", "firestorm", "rime-aura", "ice-trap", "blizzard":
		return w.adminDeployable(request.Position, values, now)
	case "tree", "wall", "stone-wall":
		w.nextAdminEntity++
		entity := newEntity(fmt.Sprintf("admin-%s-%d", request.ID, w.nextAdminEntity), request.ID, request.Position, definition, EntityOverrides{})
		entity.AdminSpawned = true
		w.worldItems = append(w.worldItems, &entity)
		return nil
	default:
		return fmt.Errorf("spawnable %q has no world factory", request.ID)
	}
}

func (w *World) adminPlayer(definition tuning.EntityDefinition, position Vec, values map[string]string, now time.Time) error {
	w.nextAdminPlayer++
	id := fmt.Sprintf("admin-player-%d", w.nextAdminPlayer)
	p := w.AddPlayer(model.Character{ID: id, Name: values["player.name"], Class: model.Class(values["player.class"])}, now)
	p.Position, p.AdminSpawned = position, true
	p.DefinitionID = "player"
	if value := values["player.speed_multiplier"]; value != "" {
		_ = adminAttributeRegistry["player.speed_multiplier"].set(adminTarget{entity: &p.Entity, player: p}, value)
	}
	w.recordHistory(p, now)
	return nil
}

func (w *World) adminProjectile(position Vec, values map[string]string) error {
	ability := w.tuning.Tables.Abilities[values["projectile.ability"]]
	if ability.Projectile == nil {
		return fmt.Errorf("ability %q has no projectile", ability.ID)
	}
	direction := adminDirection(values["transform.heading_degrees"])
	// The delivery spec rides along like it does on a fired round, so a spawned
	// fixture falls off, expires, and blasts exactly as the real one would.
	projectile := &Projectile{
		Element: values["render.element"], Damage: w.pelletDamage(ability),
		Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
		Spec: *ability.Projectile, Blast: ability.Blast, Deploy: ability.Deployable,
	}
	if ability.Blast != nil {
		projectile.BlastEffects = ability.Blast.Effects
	}
	if projectile.Element == "none" {
		projectile.Element = ""
	}
	projectile.Entity = w.newProjectileEntity(fmt.Sprintf("p-%d", w.nextProjectile), position, direction.Mul(ability.Projectile.Speed), ability.Projectile.Radius)
	projectile.Kind, projectile.AdminSpawned = ability.Projectile.Kind, true
	w.nextProjectile++
	w.projectiles[projectile.ID] = projectile
	return nil
}

func (w *World) adminTelegraph(position Vec, values map[string]string, now time.Time) error {
	ability := w.tuning.Tables.Abilities[values["telegraph.ability"]]
	element := values["render.element"]
	telegraph := w.startTelegraph("", element, position, adminDirection(values["transform.heading_degrees"]), ability, now)
	if telegraph == nil {
		return fmt.Errorf("ability %q has no telegraph", ability.ID)
	}
	telegraph.AdminSpawned = true
	return nil
}

// adminDeployable drops one authored field where it was clicked, so smoke can
// be placed and looked through without a Gunslinger having to throw it.
func (w *World) adminDeployable(position Vec, values map[string]string, now time.Time) error {
	ability := w.tuning.Tables.Abilities[values["deployable.ability"]]
	if ability.Deployable == nil {
		return fmt.Errorf("ability %q deploys nothing", ability.ID)
	}
	deployable := w.deploy("", *ability.Deployable, position, values["render.element"], now)
	if deployable == nil {
		return fmt.Errorf("ability %q deploys unknown archetype %q", ability.ID, ability.Deployable.Kind)
	}
	deployable.AdminSpawned = true
	return nil
}

func (w *World) adminInspect(entityID string) (AdminEntityState, error) {
	target, ok := w.adminTarget(entityID)
	if !ok {
		return AdminEntityState{}, fmt.Errorf("entity %q is not in the world", entityID)
	}
	definition, ok := w.tuning.Tables.Entities[target.entity.DefinitionID]
	if !ok {
		return AdminEntityState{}, fmt.Errorf("entity %q has no editable definition", entityID)
	}
	state := AdminEntityState{ID: entityID, DefinitionID: target.entity.DefinitionID, Values: map[string]string{}}
	for _, field := range definition.Admin.Fields {
		if field.Scope == "spawn" {
			continue
		}
		if adapter, found := adminAttributeRegistry[field.Attribute]; found {
			if value, applies := adapter.get(target); applies {
				state.Values[field.Attribute] = value
			}
		}
	}
	return state, nil
}

func (w *World) adminEdit(entityID string, requested map[string]string, now time.Time) (AdminEntityState, error) {
	target, ok := w.adminTarget(entityID)
	if !ok || target.entity.Deleting {
		return AdminEntityState{}, fmt.Errorf("entity %q is not editable", entityID)
	}
	definition := w.tuning.Tables.Entities[target.entity.DefinitionID]
	values, err := adminValues(definition.Admin.Fields, "edit", requested)
	if err != nil {
		return AdminEntityState{}, err
	}
	position := target.entity.Position
	if value, found := values["transform.position"]; found {
		position, _ = parseAdminPosition(value)
	}
	if !adminPosition(position, w.tuning.WorldRadius-target.entity.boundingRadius()) {
		return AdminEntityState{}, fmt.Errorf("position is outside the world")
	}
	moved := position != target.entity.Position
	if target.player != nil && moved && !w.standable(position) {
		return AdminEntityState{}, fmt.Errorf("a player cannot be moved inside cover")
	}
	for attribute, value := range values {
		adapter, found := adminAttributeRegistry[attribute]
		if !found || adapter.set == nil {
			return AdminEntityState{}, fmt.Errorf("attribute %q has no world adapter", attribute)
		}
		if err := adapter.set(target, value); err != nil {
			return AdminEntityState{}, err
		}
	}
	if target.player != nil && moved {
		w.history[target.player.ID] = nil
		w.recordHistory(target.player, now)
	}
	return w.adminInspect(entityID)
}

func (w *World) adminDelete(entityID string, now time.Time) error {
	target, ok := w.adminTarget(entityID)
	if !ok {
		return fmt.Errorf("entity %q is not in the world", entityID)
	}
	if target.player != nil {
		if target.player.Alive {
			target.player.Health, target.player.Alive = 0, false
			target.player.Velocity, target.player.Effects, target.player.DashTicksLeft = Vec{}, nil, 0
			w.cancelTelegraphs(target.player.ID, now)
		}
	}
	target.entity.Delete(now)
	return nil
}

// setAdminAttributes preserves the old caller-only seam while routing it
// through the same registry used by generic entity editing.
func (w *World) setAdminAttributes(playerID string, requested map[string]float64) error {
	values := make(map[string]string, len(requested))
	for key, value := range requested {
		attribute := map[string]string{"speed_multiplier": "player.speed_multiplier", "view_distance": "player.view_distance"}[key]
		if attribute == "" {
			return fmt.Errorf("unknown player attribute %q", key)
		}
		values[attribute] = formatNumber(value)
	}
	_, err := w.adminEdit(playerID, values, time.Now())
	return err
}

func (w *World) adminTarget(id string) (adminTarget, bool) {
	if value := w.players[id]; value != nil {
		return adminTarget{entity: &value.Entity, player: value}, true
	}
	if value := w.projectiles[id]; value != nil {
		return adminTarget{entity: &value.Entity, projectile: value}, true
	}
	if value := w.telegraphs[id]; value != nil {
		return adminTarget{entity: &value.Entity, telegraph: value}, true
	}
	if value := w.deployables[id]; value != nil {
		return adminTarget{entity: &value.Entity, deployable: value}, true
	}
	for _, value := range w.worldItems {
		if value != nil && value.ID == id {
			return adminTarget{entity: value}, true
		}
	}
	return adminTarget{}, false
}

func adminValues(fields []tuning.AdminField, scope string, requested map[string]string) (map[string]string, error) {
	allowed := map[string]tuning.AdminField{}
	for _, field := range fields {
		if field.Scope == scope || field.Scope == "both" {
			allowed[field.Attribute] = field
		}
	}
	for attribute := range requested {
		if _, ok := allowed[attribute]; !ok {
			return nil, fmt.Errorf("unknown %s attribute %q", scope, attribute)
		}
	}
	values := map[string]string{}
	for attribute, field := range allowed {
		raw, provided := requested[attribute]
		if !provided {
			if scope == "edit" {
				continue
			}
			raw = field.Default
		}
		value, err := validateAdminValue(field, raw)
		if err != nil {
			return nil, err
		}
		values[attribute] = value
	}
	if scope == "edit" && len(values) == 0 {
		return nil, fmt.Errorf("no editable attributes were supplied")
	}
	return values, nil
}

func validateAdminValue(field tuning.AdminField, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch field.Input {
	case "number", "rotation":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || field.Minimum == nil || field.Maximum == nil || value < *field.Minimum || value > *field.Maximum {
			return "", fmt.Errorf("attribute %q must be between %g and %g", field.Attribute, pointerValue(field.Minimum), pointerValue(field.Maximum))
		}
		return formatNumber(value), nil
	case "position":
		position, err := parseAdminPosition(raw)
		if err != nil || field.Minimum == nil || field.Maximum == nil || position.X < *field.Minimum || position.X > *field.Maximum || position.Y < *field.Minimum || position.Y > *field.Maximum {
			return "", fmt.Errorf("attribute %q must contain two coordinates between %g and %g", field.Attribute, pointerValue(field.Minimum), pointerValue(field.Maximum))
		}
		return formatAdminPosition(position), nil
	case "text":
		if len(raw) > field.MaxLength {
			return "", fmt.Errorf("attribute %q exceeds %d characters", field.Attribute, field.MaxLength)
		}
		return raw, nil
	case "select":
		for _, option := range field.Options {
			if raw == option.Value {
				return raw, nil
			}
		}
		return "", fmt.Errorf("attribute %q has an invalid option", field.Attribute)
	default:
		return "", fmt.Errorf("attribute %q has unsupported input %q", field.Attribute, field.Input)
	}
}

func pointerValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
func parseAdminPosition(raw string) (Vec, error) {
	values := []float64{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil || len(values) != 2 || math.IsNaN(values[0]) || math.IsNaN(values[1]) || math.IsInf(values[0], 0) || math.IsInf(values[1], 0) {
		return Vec{}, fmt.Errorf("position must be a finite [x,y] vector")
	}
	return Vec{X: values[0], Y: values[1]}, nil
}
func formatAdminPosition(position Vec) string {
	return "[" + formatNumber(position.X) + "," + formatNumber(position.Y) + "]"
}
func adminDirection(raw string) Vec {
	degrees, _ := strconv.ParseFloat(raw, 64)
	radians := degrees * math.Pi / 180
	return Vec{X: math.Cos(radians), Y: math.Sin(radians)}
}
func adminPosition(position Vec, radius float64) bool {
	return !math.IsNaN(position.X) && !math.IsNaN(position.Y) && !math.IsInf(position.X, 0) && !math.IsInf(position.Y, 0) && position.LengthSq() <= radius*radius
}
