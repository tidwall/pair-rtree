package rtree

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/fogleman/ln/ln"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
)

func makePointPair3(key string, x, y, z float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make3DPoint(x, y, z).Binary())
}
func makeBoundsPair3(key string, minx, miny, minz, maxx, maxy, maxz float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make3DRect(minx, miny, minz, maxx, maxy, maxz).Binary())
}
func TestBasic(t *testing.T) {
	tr := New(nil)
	p1 := makePointPair3("key1", -115, 33, 1)
	p2 := makePointPair3("key2", -113, 35, 2)
	tr.Insert(p1)
	tr.Insert(p2)
	assert.Equal(t, 2, tr.Count())

	var points []pair.Pair
	tr.Search(makeBoundsPair3("", -116, 32, -1, -114, 34, 1), func(item pair.Pair) bool {
		points = append(points, item)
		return true
	})
	assert.Equal(t, 1, len(points))
	tr.Remove(p1)
	assert.Equal(t, 1, tr.Count())

	points = nil
	tr.Search(makeBoundsPair3("", -116, 33, 10, -114, 34, 11), func(item pair.Pair) bool {
		points = append(points, item)
		return true
	})
	assert.Equal(t, 0, len(points))
	tr.Remove(p2)
	assert.Equal(t, 0, tr.Count())
}

func getMemStats() runtime.MemStats {
	runtime.GC()
	time.Sleep(time.Millisecond)
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms
}
func makeRandom(what string) pair.Pair {
	if what == "point" {
		x := rand.Float64()*360 - 180
		y := rand.Float64()*180 - 90
		z := rand.Float64()*100 - 50
		return makePointPair3("", x, y, z)
	} else if what == "rect" {
		x := rand.Float64()*340 - 170
		y := rand.Float64()*160 - 80
		z := rand.Float64()*80 - 30
		minx := x - rand.Float64()*10
		miny := y - rand.Float64()*10
		minz := z - rand.Float64()*10
		maxx := x + rand.Float64()*10
		maxy := y + rand.Float64()*10
		maxz := z + rand.Float64()*10
		return makeBoundsPair3("", minx, miny, minz, maxx, maxy, maxz)
	}
	panic("??")
}

func TestRandomPoints(t *testing.T) {
	testRandom(t, "point", 10000)
}

func TestRandomRects(t *testing.T) {
	testRandom(t, "rect", 10000)
}

func testRandom(t *testing.T, which string, n int) {
	rand.Seed(time.Now().UnixNano())
	tr := New(nil)
	min, max := tr.Bounds()
	assert.Equal(t, [3]float64{0, 0, 0}, min)
	assert.Equal(t, [3]float64{0, 0, 0}, max)

	// create random objects
	m1 := getMemStats()
	objs := make([]pair.Pair, n)
	for i := 0; i < n; i++ {
		objs[i] = makeRandom(which)
	}

	// insert the objects into tree
	m2 := getMemStats()
	start := time.Now()
	for _, r := range objs {
		tr.Insert(r)
	}
	durInsert := time.Since(start)
	m3 := getMemStats()
	assert.Equal(t, len(objs), tr.Count())
	fmt.Printf("Inserted %d random %ss in %dms -- %d ops/sec\n",
		len(objs), which, int(durInsert.Seconds()*1000),
		int(float64(len(objs))/durInsert.Seconds()))
	fmt.Printf("  total cost is %d bytes/%s -- tree overhead %d%%\n",
		int(m3.HeapAlloc-m1.HeapAlloc)/len(objs),
		which,
		int((float64(m3.HeapAlloc-m2.HeapAlloc)/float64(len(objs)))/
			(float64(m3.HeapAlloc-m1.HeapAlloc)/float64(len(objs)))*100))

	// count all nodes and leaves
	var nodes int
	var leaves int
	var maxLevel int
	tr.Traverse(func(min, max [3]float64, level int, item pair.Pair) bool {
		if level != 0 {
			nodes++
		}
		if level == 1 {
			leaves++
		}
		if level > maxLevel {
			maxLevel = level
		}
		return true
	})
	fmt.Printf("  nodes: %d, leaves: %d, level: %d\n", nodes, leaves, maxLevel)

	// verify mbr
	min = [3]float64{math.Inf(+1), math.Inf(+1), math.Inf(+1)}
	max = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, o := range objs {
		minb, maxb := geobin.WrapBinary(o.Value()).Rect(nil)
		for i := 0; i < len(min); i++ {
			if minb[i] < min[i] {
				min[i] = minb[i]
			}
			if maxb[i] > max[i] {
				max[i] = maxb[i]
			}
		}
	}
	minb, maxb := tr.Bounds()
	assert.Equal(t, min, minb)
	assert.Equal(t, max, maxb)

	// scan
	var arr []pair.Pair
	tr.Scan(func(item pair.Pair) bool {
		arr = append(arr, item)
		return true
	})
	assert.True(t, testHasSameItems(objs, arr))

	// search
	testSearch(t, tr, objs, 0.10, true)
	testSearch(t, tr, objs, 0.50, true)
	testSearch(t, tr, objs, 1.00, true)

	// knn
	testKNN(t, tr, objs, 100, true)
	testKNN(t, tr, objs, 1000, true)
	testKNN(t, tr, objs, 10000, true)
	testKNN(t, tr, objs, n*2, true) // all of them

	// remove all objects
	indexes := rand.Perm(len(objs))
	start = time.Now()
	for _, i := range indexes {
		tr.Remove(objs[i])
	}
	durRemove := time.Since(start)
	assert.Equal(t, 0, tr.Count())
	fmt.Printf("Removed %d %ss in %dms -- %d ops/sec\n",
		len(objs), which, int(durRemove.Seconds()*1000),
		int(float64(len(objs))/durRemove.Seconds()))

	min, max = tr.Bounds()
	assert.Equal(t, [3]float64{0, 0, 0}, min)
	assert.Equal(t, [3]float64{0, 0, 0}, max)
}
func testKNN(t *testing.T, tr *RTree, objs []pair.Pair, n int, check bool) {
	min, max := tr.Bounds()
	x := (max[0] + min[0]) / 2
	y := (max[1] + min[1]) / 2
	z := (max[2] + min[2]) / 2

	// gather the results, make sure that is matches exactly
	var arr1 []pair.Pair
	var dists1 []float64
	pdist := math.Inf(-1)
	tr.KNN(x, y, z, func(item pair.Pair, dist float64) bool {
		if len(arr1) == n {
			return false
		}
		arr1 = append(arr1, item)
		dists1 = append(dists1, dist)
		if dist < pdist {
			panic("dist out of order")
		}
		pdist = dist
		return true
	})
	assert.True(t, n > len(objs) || n == len(arr1))

	// get the KNN for the original array
	nobjs := make([]pair.Pair, len(objs))
	copy(nobjs, objs)
	sort.Slice(nobjs, func(i, j int) bool {
		imin, imax := geobin.WrapBinary(nobjs[i].Value()).Rect(nil)
		jmin, jmax := geobin.WrapBinary(nobjs[j].Value()).Rect(nil)
		idist := testBoxDist(x, y, z, imin, imax)
		jdist := testBoxDist(x, y, z, jmin, jmax)
		return idist < jdist
	})
	arr2 := nobjs[:len(arr1)]
	var dists2 []float64
	for i := 0; i < len(arr2); i++ {
		min, max := geobin.WrapBinary(arr2[i].Value()).Rect(nil)
		dist := testBoxDist(x, y, z, min, max)
		dists2 = append(dists2, dist)
	}
	// only compare the distances, not the objects because rectangles with
	// a dist of zero will not be ordered.
	assert.Equal(t, dists1, dists2)

}
func testBoxDist(x, y, z float64, min, max [3]float64) float64 {
	dx := testAxisDist(x, min[0], max[0])
	dy := testAxisDist(y, min[1], max[1])
	dz := testAxisDist(z, min[2], max[2])
	return dx*dx + dy*dy + dz*dz
}
func testAxisDist(k, min, max float64) float64 {
	if k < min {
		return min - k
	}
	if k <= max {
		return 0
	}
	return k - max
}
func testSearch(t *testing.T, tr *RTree, objs []pair.Pair, percent float64, check bool) {
	min, max := tr.Bounds()
	minx := ((max[0]+min[0])/2 - ((max[0]-min[0])*percent)/2)
	maxx := ((max[0]+min[0])/2 + ((max[0]-min[0])*percent)/2)
	miny := ((max[1]+min[1])/2 - ((max[1]-min[1])*percent)/2)
	maxy := ((max[1]+min[1])/2 + ((max[1]-min[1])*percent)/2)
	minz := ((max[2]+min[2])/2 - ((max[2]-min[2])*percent)/2)
	maxz := ((max[2]+min[2])/2 + ((max[2]-min[2])*percent)/2)
	box := makeBoundsPair3("", minx, miny, minz, maxx, maxy, maxz)
	var arr1 []pair.Pair
	tr.Search(box, func(item pair.Pair) bool {
		if check {
			arr1 = append(arr1, item)
		}
		return true
	})
	if !check {
		return
	}
	var arr2 []pair.Pair
	for _, obj := range objs {
		if testIntersects(obj, box) {
			arr2 = append(arr2, obj)
		}
	}
	assert.Equal(t, len(arr1), len(arr2))
	for _, o1 := range arr1 {
		var found bool
		for _, o2 := range arr2 {
			if o2 == o1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("not found")
		}
	}
}

func testIntersects(obj, box pair.Pair) bool {
	amin, amax := geobin.WrapBinary(obj.Value()).Rect(nil)
	bmin, bmax := geobin.WrapBinary(box.Value()).Rect(nil)
	return bmin[0] <= amax[0] && bmin[1] <= amax[1] && bmin[2] <= amax[2] &&
		bmax[0] >= amin[0] && bmax[1] >= amin[1] && bmax[2] >= amin[2]
}
func testHasSameItems(a1, a2 []pair.Pair) bool {
	if len(a1) != len(a2) {
		return false
	}
	for _, p1 := range a1 {
		var found bool
		for _, p2 := range a2 {
			if p1 == p2 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestOutput3DPNG(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	tr := New(nil)
	for i := 0; i < 7500; i++ {
		x := rand.Float64()*2 - 1
		y := rand.Float64()*2 - 1
		z := rand.Float64()*2 - 1
		tr.Insert(makePointPair3("", x, y, z))
	}

	scene := ln.Scene{}
	tr.Traverse(func(min, max [3]float64, level int, item pair.Pair) bool {
		if level > 0 {
			scene.Add(ln.NewCube(
				ln.Vector{min[0] - 0.01, min[1] - 0.01, min[2] - 0.01},
				ln.Vector{max[0] + 0.01, max[1] + 0.01, max[2] + 0.01},
			))
		}
		return true
	})

	// define camera parameters
	eye := ln.Vector{4, 3, 2}    // camera position
	center := ln.Vector{0, 0, 0} // camera looks at
	up := ln.Vector{0, 0, 1}     // up direction

	// define rendering parameters
	width := 1024.0  // rendered width
	height := 1024.0 // rendered height
	fovy := 50.0     // vertical field of view, degrees
	znear := 0.1     // near z plane
	zfar := 10.0     // far z plane
	step := 0.5      // how finely to chop the paths for visibility testing

	// compute 2D paths that depict the 3D scene
	paths := scene.Render(eye, center, up, width, height, fovy, znear, zfar, step)

	// render the paths in an image
	paths.WriteToPNG("out3d.png", width, height)
}

func BenchmarkInsert(b *testing.B) {
	rand.Seed(0)
	var points []pair.Pair
	for i := 0; i < b.N; i++ {
		x := rand.Float64()*360 - 180
		y := rand.Float64()*180 - 90
		z := rand.Float64()*40 - 20
		points = append(points, makePointPair3("", x, y, z))
	}
	b.ReportAllocs()
	b.ResetTimer()
	tr := New(nil)
	for i := 0; i < b.N; i++ {
		tr.Insert(points[i])
	}
}
