package tuning

import (
	"sort"

	"spellfire/server/internal/worldfield"
)

// This file is the only adapter between the tables and the world field. The
// field itself takes primitives and imports nothing, so the loader can validate
// a generated seed without the field having to know what a table is, and the
// browser can build the identical field from the identical rows.

// Field is the deterministic world field these tables describe: danger, biome,
// grade, and region by position.
func (t *Tables) Field() *worldfield.Field {
	if t.field == nil {
		t.field = worldfield.New(t.FieldParams())
	}
	return t.field
}

// FieldParams projects the world and biome tables onto the field's inputs.
func (t *Tables) FieldParams() worldfield.Params {
	field := t.World.Field
	params := worldfield.Params{
		Radius:          t.World.Radius,
		Seed:            field.Seed,
		RegionCell:      field.RegionCell,
		RegionJitter:    field.RegionJitter,
		RadialReference: field.RadialReference,
		WarpCell:        field.WarpCell,
		WarpAmplitude:   field.WarpAmplitude,
		BlendWidth:      field.BlendWidth,
		Biomes:          sortedKeys(t.Biomes),
		Bands:           t.FieldBands(),
		Grades:          t.FieldGrades(),
	}
	return params
}

// FieldBands projects the danger rows in the order they are authored, which is
// outward from the hub — the order the field's radial lookup depends on.
func (t *Tables) FieldBands() []worldfield.Band {
	bands := make([]worldfield.Band, 0, len(t.World.DangerBands))
	for _, band := range t.World.DangerBands {
		bands = append(bands, worldfield.Band{
			ID: band.ID, Name: band.Name, PvP: band.PvP, MaterialGrade: band.MaterialGrade,
			Shape: band.Shape, Summary: band.Summary, Tier: band.Tier, OuterRadius: band.OuterRadius,
		})
	}
	return bands
}

// FieldGrades projects the reward curve, resolving each threshold's grade to
// the rarity tier it prices.
func (t *Tables) FieldGrades() worldfield.GradeCurve {
	curve := worldfield.GradeCurve{}
	for _, point := range t.World.Field.GradeCurve.Points {
		curve.Points = append(curve.Points, worldfield.GradePoint{At: point[0], Richness: point[1]})
	}
	for _, threshold := range t.World.Field.GradeCurve.Thresholds {
		curve.Thresholds = append(curve.Thresholds, worldfield.GradeThreshold{
			ID: threshold.Grade, Tier: t.Materials.Grades[threshold.Grade].Tier, At: threshold.At,
		})
	}
	return curve
}

// MaterialsAt lists what ground of a biome and grade tier can yield, in stable
// order. Two rules decide it, and between them they are the whole of "biome
// determines type, radius determines grade":
//
//   - A universal kind — structural stock, stave wood, reagents — is available
//     in every biome, so no build is ever hard-locked by geography.
//   - A biome-gated kind is available only in its own biome.
//
// Grade is a ceiling rather than an equality: richer ground yields everything
// the poorer ground did, plus its own grade, so walking outward adds options
// instead of trading them. Crafted kinds are excluded because nothing in the
// world produces them — a recipe does.
func (t *Tables) MaterialsAt(biome string, tier int) []string {
	found := make([]string, 0)
	for id, material := range t.Materials.Materials {
		kind, ok := t.Materials.Kinds[material.Kind]
		if !ok || kind.Source == "craft" {
			continue
		}
		grade, ok := t.Materials.Grades[material.Grade]
		if !ok || grade.Tier > tier {
			continue
		}
		if !kind.Universal && material.Biome != biome {
			continue
		}
		found = append(found, id)
	}
	sort.Strings(found)
	return found
}

// MaterialsAtPosition is MaterialsAt resolved through the field, which is the
// form harvesting will ask in: what can this patch of ground give me.
func (t *Tables) MaterialsAtPosition(x, y float64) []string {
	field := t.Field()
	return t.MaterialsAt(field.BiomeAt(x, y).ID, field.GradeAt(x, y).Tier)
}
