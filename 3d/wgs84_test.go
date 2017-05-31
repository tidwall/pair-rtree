package rtree

import (
	"testing"
)

func TestSphereConversion(t *testing.T) {
}

func BenchmarkWGS84Conversion(t *testing.B) {
	p := [3]float64{-115, 33, 110}
	for i := 0; i < t.N; i++ {
		lonLatElevToXYZ_WGS84(p)
	}
}
func BenchmarkSphereConversion(t *testing.B) {
	p := [3]float64{-115, 33, 110}
	for i := 0; i < t.N; i++ {
		lonLatElevToXYZ_Sphere(p)
	}
}
