package rtree

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/fogleman/gg"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
)

func makePointPair2(key string, x, y float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make2DPoint(x, y).Binary())
}

func makeBoundsPair2(key string, minx, miny, maxx, maxy float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make2DRect(minx, miny, maxx, maxy).Binary())
}

func TestBasic(t *testing.T) {
	tr := New()
	p1 := makePointPair2("key1", -115, 33)
	p2 := makePointPair2("key2", -113, 35)
	tr.Insert(p1)
	tr.Insert(p2)
	assert.Equal(t, 2, tr.Count())

	var points []pair.Pair
	tr.Search(makeBoundsPair2("", -116, 32, -114, 34), func(item pair.Pair) bool {
		points = append(points, item)
		return true
	})
	assert.Equal(t, 1, len(points))
	tr.Remove(p1)
	assert.Equal(t, 1, tr.Count())

	points = nil
	tr.Search(makeBoundsPair2("", -116, 33, -114, 34), func(item pair.Pair) bool {
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
		return makePointPair2("", x, y)
	} else if what == "rect" {
		x := rand.Float64()*340 - 170
		y := rand.Float64()*160 - 80
		minx := x - rand.Float64()*10
		miny := y - rand.Float64()*10
		maxx := x + rand.Float64()*10
		maxy := y + rand.Float64()*10
		return makeBoundsPair2("", minx, miny, maxx, maxy)
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
	tr := New()
	min, max := tr.Bounds()
	assert.Equal(t, [2]float64{0, 0}, min)
	assert.Equal(t, [2]float64{0, 0}, max)

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
	tr.Traverse(func(min, max [2]float64, level int, item pair.Pair) bool {
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
	min = [2]float64{math.Inf(+1), math.Inf(+1)}
	max = [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, o := range objs {
		minb, maxb := geobin.WrapBinary(o.Value()).Rect()
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
	assert.Equal(t, [2]float64{0, 0}, min)
	assert.Equal(t, [2]float64{0, 0}, max)
}

func testKNN(t *testing.T, tr *RTree, objs []pair.Pair, n int, check bool) {
	min, max := tr.Bounds()
	x := (max[0] + min[0]) / 2
	y := (max[1] + min[1]) / 2

	// gather the results, make sure that is matches exactly
	var arr1 []pair.Pair
	var dists1 []float64
	pdist := math.Inf(-1)
	tr.KNN(x, y, func(item pair.Pair, dist float64) bool {
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
		imin, imax := geobin.WrapBinary(nobjs[i].Value()).Rect()
		jmin, jmax := geobin.WrapBinary(nobjs[j].Value()).Rect()
		idist := testBoxDist(x, y, [2]float64{imin[0], imin[1]}, [2]float64{imax[0], imax[1]})
		jdist := testBoxDist(x, y, [2]float64{jmin[0], jmin[1]}, [2]float64{jmax[0], jmax[1]})
		return idist < jdist
	})
	arr2 := nobjs[:len(arr1)]
	var dists2 []float64
	for i := 0; i < len(arr2); i++ {
		min, max := geobin.WrapBinary(arr2[i].Value()).Rect()
		dist := testBoxDist(x, y, [2]float64{min[0], min[1]}, [2]float64{max[0], max[1]})
		dists2 = append(dists2, dist)
	}
	// only compare the distances, not the objects because rectangles with
	// a dist of zero will not be ordered.
	assert.Equal(t, dists1, dists2)

}
func testBoxDist(x, y float64, min, max [2]float64) float64 {
	dx := testAxisDist(x, min[0], max[0])
	dy := testAxisDist(y, min[1], max[1])
	return dx*dx + dy*dy
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
	box := makeBoundsPair2("", minx, miny, maxx, maxy)
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
	amin, amax := geobin.WrapBinary(obj.Value()).Rect()
	bmin, bmax := geobin.WrapBinary(box.Value()).Rect()
	return bmin[0] <= amax[0] && bmin[1] <= amax[1] &&
		bmax[0] >= amin[0] && bmax[1] >= amin[1]
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
func TestOutput2DPNG(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	tr := New()
	for i := 0; i < 7500; i++ {
		x := rand.Float64()*360 - 180
		y := rand.Float64()*180 - 90
		tr.Insert(makePointPair2("", x, y))
	}

	var w, h float64
	var scale float64 = 3.0
	var dc *gg.Context
	tr.Traverse(func(min, max [2]float64, level int, item pair.Pair) bool {
		if dc == nil {
			w, h = (max[0]-min[0])*scale, (max[1]-min[1])*scale
			dc = gg.NewContext(int(w), int(h))
			dc.DrawRectangle(0, 0, w+1, h+1)
			dc.SetRGB(0, 0, 0)
			dc.Fill()
			dc.SetLineWidth(0.2 * scale)
		}
		switch level {
		default:
			dc.SetRGB(0, 0, 0)
		case 0:
			dc.SetRGB(1, 0, 0)
		case 1:
			dc.SetRGB(0, 1, 0)
		case 2:
			dc.SetRGB(0, 0, 1)
		case 3:
			dc.SetRGB(1, 1, 0)
		case 4:
			dc.SetRGB(1, 0, 1)
		case 5:
			dc.SetRGB(0, 1, 1)
		case 6:
			dc.SetRGB(0.5, 0, 0)
		case 7:
			dc.SetRGB(0, 0.5, 0)
		}
		if level == 0 {
			dc.DrawRectangle(min[0]*scale+w/2-1, min[1]*scale+h/2-1, (max[0]-min[0])*scale+1, (max[1]-min[1])*scale+1)
			dc.Fill()
		} else {
			dc.DrawRectangle(min[0]*scale+w/2, min[1]*scale+h/2, (max[0]-min[0])*scale, (max[1]-min[1])*scale)
			dc.Stroke()
		}
		return true
	})
	dc.SavePNG("out2d.png")
}

func BenchmarkInsert(b *testing.B) {
	rand.Seed(time.Now().UnixNano())
	var points []pair.Pair
	for i := 0; i < b.N; i++ {
		x := rand.Float64()*360 - 180
		y := rand.Float64()*180 - 90
		points = append(points, makePointPair2("", x, y))
	}
	b.ReportAllocs()
	b.ResetTimer()
	tr := New()
	for i := 0; i < b.N; i++ {
		tr.Insert(points[i])
	}
}
