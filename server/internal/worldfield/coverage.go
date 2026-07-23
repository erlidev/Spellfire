package worldfield

import (
	"fmt"
	"math"
	"sort"
)

// Coverage is the enforced form of "geography never hard-locks a build". The
// biome field is generated rather than authored, so nothing guarantees by
// construction that a Fringe player can reach Frost material or that the
// Deadlands is not all one element. Rather than trust the seed, the loader
// samples the field and refuses one that leaves a gap.
//
// Sampling is a square lattice clipped to the disc rather than a polar sweep:
// a polar sweep would need trigonometry, and every sample here has to be the
// same number the simulation and the browser compute.
type Coverage struct {
	// Share is band ID → biome ID → fraction of that band's area, and Samples
	// is how many points landed in each band.
	Share   map[string]map[string]float64
	Samples map[string]int
}

// Sample walks a lattice of `resolution` points across the world's bounding
// square and reports the biome mix of every danger band.
func (f *Field) Sample(resolution int) Coverage {
	coverage := Coverage{Share: map[string]map[string]float64{}, Samples: map[string]int{}}
	if resolution < 2 || f.p.Radius <= 0 || len(f.p.Bands) == 0 {
		return coverage
	}
	counts := map[string]map[string]int{}
	for _, band := range f.p.Bands {
		counts[band.ID] = map[string]int{}
		coverage.Samples[band.ID] = 0
	}
	step := 2 * f.p.Radius / float64(resolution-1)
	for row := 0; row < resolution; row++ {
		y := -f.p.Radius + float64(row)*step
		for column := 0; column < resolution; column++ {
			x := -f.p.Radius + float64(column)*step
			if x*x+y*y > f.p.Radius*f.p.Radius {
				continue
			}
			band := f.DangerAt(x, y)
			counts[band.ID][f.BiomeAt(x, y).ID]++
			coverage.Samples[band.ID]++
		}
	}
	for band, mix := range counts {
		total := coverage.Samples[band]
		shares := map[string]float64{}
		for biome, count := range mix {
			if total > 0 {
				shares[biome] = float64(count) / float64(total)
			}
		}
		coverage.Share[band] = shares
	}
	return coverage
}

// Problems reports every way the sampled field fails the coverage contract, in
// stable order. A band with no material grade — the hub — is exempt: nothing is
// harvested there, so its biome mix decides nothing.
//
// Bands too small to hold a fair share of every biome are exempt in the same
// spirit: `minimum` is checked only where the band actually has room for it.
func (c Coverage) Problems(bands []Band, biomes []string, minimum float64, minimumSamples int) []string {
	problems := []string{}
	sorted := append([]string(nil), biomes...)
	sort.Strings(sorted)
	for _, band := range bands {
		if band.MaterialGrade == "" {
			continue
		}
		if c.Samples[band.ID] < minimumSamples {
			problems = append(problems, fmt.Sprintf(
				"world: danger band %q was sampled %d times, below the %d needed to judge its biome mix; raise field.coverage.resolution",
				band.ID, c.Samples[band.ID], minimumSamples))
			continue
		}
		for _, biome := range sorted {
			share := c.Share[band.ID][biome]
			if share <= 0 {
				problems = append(problems, fmt.Sprintf(
					"world: seed leaves biome %q absent from danger band %q; geography would hard-lock every build that needs its material — re-roll field.seed",
					biome, band.ID))
				continue
			}
			if share < minimum {
				problems = append(problems, fmt.Sprintf(
					"world: biome %q covers %.1f%% of danger band %q, below the %.1f%% floor; re-roll field.seed",
					biome, share*100, band.ID, minimum*100))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// Balance is how far the field's overall biome mix strays from an even split,
// as the largest absolute deviation from 1/n. It is reported rather than
// enforced: a seed may legitimately favour one element world-wide, but a
// wildly skewed one is worth seeing in a test.
func (c Coverage) Balance(bands []Band, biomes []string) float64 {
	totals, all := map[string]float64{}, 0.0
	for _, band := range bands {
		for biome, share := range c.Share[band.ID] {
			totals[biome] += share * float64(c.Samples[band.ID])
			all += share * float64(c.Samples[band.ID])
		}
	}
	if all == 0 || len(biomes) == 0 {
		return 0
	}
	even, worst := 1/float64(len(biomes)), 0.0
	for _, biome := range biomes {
		worst = math.Max(worst, math.Abs(totals[biome]/all-even))
	}
	return worst
}
