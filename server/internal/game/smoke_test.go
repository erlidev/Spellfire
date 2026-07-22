package game

import "testing"

func TestSmokeUsesFiveBoundedOverlappingCircles(t *testing.T) {
	circles := smokeCircles(Vec{}, 100)
	if len(circles) != 5 {
		t.Fatalf("smoke has %d circles, want 5", len(circles))
	}
	for _, circle := range circles {
		if circle.Center.LengthSq() > (100-circle.Radius)*(100-circle.Radius) {
			t.Fatalf("smoke circle at %#v with radius %g exceeds authored radius", circle.Center, circle.Radius)
		}
	}
}

func TestViewerInsideSmokeSeesOnlyUnionOfContainingCircles(t *testing.T) {
	circles := smokeCircles(Vec{}, 100)
	viewer := Vec{35, 0} // inside both center and right lobes
	if smokeOccluded(viewer, Vec{85, 0}, circles) {
		t.Fatal("point inside a smoke circle containing the viewer was occluded")
	}
	if !smokeOccluded(viewer, Vec{-85, 0}, circles) {
		t.Fatal("point outside the viewer's two containing circles remained visible")
	}
	if !smokeOccluded(Vec{-150, 0}, Vec{150, 0}, circles) {
		t.Fatal("crossing the smoke from outside did not occlude")
	}
}
