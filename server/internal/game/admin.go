package game

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/tuning"
)

// AdminSpawn is a request already authenticated by the HTTP layer. ID names a
// row in admin_tools.json; Config contains only the selected row's editable
// fields, and Position is the world coordinate the administrator clicked.
type AdminSpawn struct {
	ID       string
	Position Vec
	Config   map[string]string
}

type adminValue struct {
	number float64
	text   string
}

func (w *World) adminSpawn(request AdminSpawn, now time.Time) error {
	row, ok := w.tuning.Tables.AdminTools.Spawnables[request.ID]
	if !ok {
		return fmt.Errorf("unknown spawnable %q", request.ID)
	}
	if !adminPosition(request.Position, w.tuning.WorldRadius) {
		return fmt.Errorf("placement is outside the world")
	}
	values, err := adminValues(row.Fields, request.Config)
	if err != nil {
		return err
	}
	switch row.Kind {
	case "player":
		if !w.standable(request.Position) {
			return fmt.Errorf("a player cannot be placed inside cover")
		}
		return w.adminPlayer(row, request.Position, values, now)
	case "projectile":
		return w.adminProjectile(row, request.Position, values)
	case "telegraph":
		return w.adminTelegraph(row, request.Position, values, now)
	default:
		return fmt.Errorf("spawnable %q has unsupported kind %q", request.ID, row.Kind)
	}
}

func (w *World) adminPlayer(row tuning.AdminSpawnable, position Vec, values map[string]adminValue, now time.Time) error {
	w.nextAdminPlayer++
	id := fmt.Sprintf("admin-player-%d", w.nextAdminPlayer)
	name := values["name"].text
	if name == "" {
		name = row.Name
	}
	p := w.AddPlayer(model.Character{ID: id, Name: name, Class: model.Class(row.Class)}, now)
	p.Position, p.AdminSpawned = position, true
	if speed, ok := values["speed_multiplier"]; ok {
		p.SpeedMultiplier = speed.number
	}
	w.recordHistory(p, now)
	return nil
}

func (w *World) adminProjectile(row tuning.AdminSpawnable, position Vec, values map[string]adminValue) error {
	ability := w.tuning.Tables.Abilities[row.Ability]
	if ability.Projectile == nil {
		return fmt.Errorf("spawnable %q has no projectile", row.Name)
	}
	direction := adminDirection(values)
	projectile := &Projectile{
		Element: row.Element,
		Damage:  w.tuning.Tables.BandDamage(ability.DamageBand), Remaining: ability.Projectile.LifeSeconds, Effects: ability.Effects,
	}
	projectile.Entity = w.newProjectileEntity(fmt.Sprintf("p-%d", w.nextProjectile), position, direction.Mul(ability.Projectile.Speed), ability.Projectile.Radius)
	projectile.Kind = ability.Projectile.Kind
	w.nextProjectile++
	w.projectiles[projectile.ID] = projectile
	return nil
}

func (w *World) adminTelegraph(row tuning.AdminSpawnable, position Vec, values map[string]adminValue, now time.Time) error {
	ability := w.tuning.Tables.Abilities[row.Ability]
	if w.startTelegraph("", row.Element, position, adminDirection(values), ability, now) == nil {
		return fmt.Errorf("spawnable %q has no telegraph", row.Name)
	}
	return nil
}

func (w *World) setAdminAttributes(playerID string, requested map[string]float64) error {
	p := w.players[playerID]
	if p == nil || !p.Alive {
		return fmt.Errorf("player is not in the world")
	}
	values, err := adminNumberValues(w.tuning.Tables.AdminTools.Attributes, requested)
	if err != nil {
		return err
	}
	for id, value := range values {
		switch id {
		case "speed_multiplier":
			p.SpeedMultiplier = value
		case "view_distance":
			p.ViewDistance = value
		default:
			return fmt.Errorf("attribute %q has no world executor", id)
		}
	}
	return nil
}

func adminValues(fields []tuning.AdminToolField, requested map[string]string) (map[string]adminValue, error) {
	allowed := make(map[string]tuning.AdminToolField, len(fields))
	for _, field := range fields {
		allowed[field.ID] = field
	}
	for id := range requested {
		if _, ok := allowed[id]; !ok {
			return nil, fmt.Errorf("unknown configuration field %q", id)
		}
	}
	values := make(map[string]adminValue, len(fields))
	for _, field := range fields {
		raw, provided := requested[field.ID]
		switch field.Kind {
		case "number":
			value := field.DefaultNumber
			if provided {
				parsed, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
					return nil, fmt.Errorf("configuration field %q must be a number", field.ID)
				}
				value = parsed
			}
			if value < field.Minimum || value > field.Maximum {
				return nil, fmt.Errorf("configuration field %q must be between %g and %g", field.ID, field.Minimum, field.Maximum)
			}
			values[field.ID] = adminValue{number: value}
		case "text":
			value := field.DefaultText
			if provided {
				value = strings.TrimSpace(raw)
			}
			if len(value) > field.MaxLength {
				return nil, fmt.Errorf("configuration field %q exceeds %d characters", field.ID, field.MaxLength)
			}
			values[field.ID] = adminValue{text: value}
		default:
			return nil, fmt.Errorf("configuration field %q has unsupported kind", field.ID)
		}
	}
	return values, nil
}

func adminNumberValues(fields map[string]tuning.AdminToolField, requested map[string]float64) (map[string]float64, error) {
	if len(requested) == 0 {
		return nil, fmt.Errorf("no attributes were supplied")
	}
	values := make(map[string]float64, len(requested))
	for id, value := range requested {
		field, ok := fields[id]
		if !ok || field.Kind != "number" {
			return nil, fmt.Errorf("unknown numeric attribute %q", id)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < field.Minimum || value > field.Maximum {
			return nil, fmt.Errorf("attribute %q must be between %g and %g", id, field.Minimum, field.Maximum)
		}
		values[id] = value
	}
	return values, nil
}

func adminDirection(values map[string]adminValue) Vec {
	degrees := values["heading_degrees"].number
	radians := degrees * math.Pi / 180
	return Vec{X: math.Cos(radians), Y: math.Sin(radians)}
}

func adminPosition(position Vec, radius float64) bool {
	return !math.IsNaN(position.X) && !math.IsNaN(position.Y) && !math.IsInf(position.X, 0) && !math.IsInf(position.Y, 0) && position.LengthSq() <= radius*radius
}
