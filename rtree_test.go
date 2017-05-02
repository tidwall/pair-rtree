package rtree

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/json-iterator/go/assert"
	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
)

func TestTree2DPoints(t *testing.T) {
	testRandom(t, 10000, 0, 0) // 2d points
}
func TestTree3DPoints(t *testing.T) {
	testRandom(t, 10000, 1, 1) // 3d points
}
func TestTree2DRects(t *testing.T) {
	testRandom(t, 10000, 2, 2) // 2d rects
}
func TestTree3DRects(t *testing.T) {
	testRandom(t, 10000, 3, 3) // 3d rects
}
func TestTree2D3DPoints(t *testing.T) {
	testRandom(t, 10000, 0, 1) // 2d/3d points
}
func TestTree2D3DRect(t *testing.T) {
	testRandom(t, 10000, 2, 3) // 2d/3d rects
}
func TestTreeMixed(t *testing.T) {
	testRandom(t, 10000, 0, 3) // all mixed
}
func testRandom(t *testing.T, n, lb, ub int) {
	rand.Seed(time.Now().UnixNano())
	var objs []pair.Pair
	for i := 0; i < n; i++ {
		var obj pair.Pair
		v := rand.Int() % (ub - lb + 1)
		switch v + lb {
		case 0:
			obj = rand2DPoint()
		case 1:
			obj = rand3DPoint()
		case 2:
			obj = rand2DRect()
		case 3:
			obj = rand3DRect()
		}
		objs = append(objs, obj)
	}
	tr := New()
	min, max := tr.Bounds()
	assert.Equal(t, [3]float64{0, 0, 0}, min)
	assert.Equal(t, [3]float64{0, 0, 0}, max)

	start := time.Now()
	for _, obj := range objs {
		tr.Insert(obj)
	}
	dur := time.Since(start)
	fmt.Printf("Inserted %d random objects in %s (%.0f/objs sec)\n", len(objs), dur, float64(len(objs))/dur.Seconds())

	// verify mbr
	min = [3]float64{math.Inf(+1), math.Inf(+1), math.Inf(+1)}
	max = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
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

	// search
	testSearch(t, tr, objs, 0.20, true)
	//testSearch(t, tr, objs, 0.50, true)
	//testSearch(t, tr, objs, 1.00, true)

	//// knn
	testKNN(t, tr, objs, int(float64(n)*0.001), true)
	testKNN(t, tr, objs, int(float64(n)*0.01), true)
	testKNN(t, tr, objs, int(float64(n)*0.1), true)
	testKNN(t, tr, objs, n*2, true) // all of them

	// remove all objects
	indexes := rand.Perm(len(objs))
	start = time.Now()
	for _, i := range indexes {
		tr.Remove(objs[i])
	}
	durRemove := time.Since(start)
	assert.Equal(t, 0, tr.Count())
	fmt.Printf("Removed %d objects in %dms -- %d ops/sec\n",
		len(objs), int(durRemove.Seconds()*1000),
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
	pos := makePointPair3("", x, y, z)

	// gather the results, make sure that is matches exactly
	var arr1 []pair.Pair
	var dists1 []float64
	pdist := math.Inf(-1)
	tr.KNN(pos, func(item pair.Pair, dist float64) bool {
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
		io := geobin.WrapBinary(nobjs[i].Value())
		jo := geobin.WrapBinary(nobjs[j].Value())
		imin, imax := io.Rect()
		jmin, jmax := jo.Rect()
		// boxDist is a private function.
		var idist, jdist float64
		if io.Dims() == 2 {
			idist = testBoxDist2(x, y, imin, imax)
		} else {
			idist = testBoxDist3(x, y, z, imin, imax)
		}
		if jo.Dims() == 2 {
			jdist = testBoxDist2(x, y, jmin, jmax)
		} else {
			jdist = testBoxDist3(x, y, z, jmin, jmax)
		}
		return idist < jdist
	})

	arr2 := nobjs[:len(arr1)]
	var dists2 []float64
	for i := 0; i < len(arr2); i++ {
		o := geobin.WrapBinary(arr2[i].Value())
		min, max := o.Rect()
		var dist float64
		if o.Dims() == 2 {
			dist = testBoxDist2(x, y, min, max)
		} else {
			dist = testBoxDist3(x, y, z, min, max)
		}
		dists2 = append(dists2, dist)
	}
	// only compare the distances, not the objects because rectangles with
	// a dist of zero will not be ordered.
	assert.Equal(t, dists1, dists2)

}
func testBoxDist2(x, y float64, min, max [3]float64) float64 {
	dx := textAxisDist(x, min[0], max[0])
	dy := textAxisDist(y, min[1], max[1])
	return dx*dx + dy*dy
}
func testBoxDist3(x, y, z float64, min, max [3]float64) float64 {
	dx := textAxisDist(x, min[0], max[0])
	dy := textAxisDist(y, min[1], max[1])
	dz := textAxisDist(z, min[2], max[2])
	return dx*dx + dy*dy + dz*dz
}
func textAxisDist(k, min, max float64) float64 {
	if k < min {
		return min - k
	}
	if k <= max {
		return 0
	}
	return k - max
}
func rand2DPoint() pair.Pair {
	x := rand.Float64()*360 - 180
	y := rand.Float64()*180 - 90
	return makePointPair2("", x, y)
}
func rand2DRect() pair.Pair {
	x := rand.Float64()*340 - 170
	y := rand.Float64()*160 - 80
	minx := x - rand.Float64()*10
	miny := y - rand.Float64()*10
	maxx := x + rand.Float64()*10
	maxy := y + rand.Float64()*10
	return makeBoundsPair2("", minx, miny, maxx, maxy)
}
func rand3DPoint() pair.Pair {
	x := rand.Float64()*360 - 180
	y := rand.Float64()*180 - 90
	z := rand.Float64()*100 - 50
	return makePointPair3("", x, y, z)
}
func rand3DRect() pair.Pair {
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
func makePointPair3(key string, x, y, z float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make3DPoint(x, y, z).Binary())
}
func makeBoundsPair3(key string, minx, miny, minz, maxx, maxy, maxz float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make3DRect(minx, miny, minz, maxx, maxy, maxz).Binary())
}
func makePointPair2(key string, x, y float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make2DPoint(x, y).Binary())
}
func makeBoundsPair2(key string, minx, miny, maxx, maxy float64) pair.Pair {
	return pair.New([]byte(key), geobin.Make2DRect(minx, miny, maxx, maxy).Binary())
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
		ok := testIntersects(obj, box)
		if ok {
			arr2 = append(arr2, obj)
		}
		/*
			var found bool
			for _, o := range arr1 {
				if o == obj {
					found = true
					break
				}
			}
			var res string
			if ok {
				if found {
					res = "[++]" // search and test found
				} else {
					res = "[-+]"
				}
			} else {
				if found {
					res = "[+-]" // search found, but test did not
				} else {
					res = "[--]"
				}
			}
			if res != "[--]" {
				fmt.Printf("%02d: %s %s %s\n", i, res, rectString(box), rectString(obj))
			}
		*/
	}
	assert.Equal(t, len(arr1), len(arr2))
	var missing int
	for _, o1 := range arr1 {
		var found bool
		for _, o2 := range arr2 {
			if o2 == o1 {
				found = true
				break
			}
		}
		if !found {
			missing++
		}
	}
	if missing > 0 {
		t.Fatalf("missing %d\n", missing)
	}
}

func rectString(item pair.Pair) string {
	dims := geobin.WrapBinary(item.Value()).Dims()
	min, max := geobin.WrapBinary(item.Value()).Rect()
	if dims == 2 {
		return fmt.Sprintf("[%7.2f %7.2f %7.2f %7.2f]", min[0], min[1], max[0], max[1])
	}
	return fmt.Sprintf("[%7.2f %7.2f %7.2f %7.2f %7.2f %7.2f]", min[0], min[1], min[2], max[0], max[1], max[2])
}

func testIntersects(obj, box pair.Pair) bool {
	odims := geobin.WrapBinary(obj.Value()).Dims()
	omin, omax := geobin.WrapBinary(obj.Value()).Rect()
	bdims := geobin.WrapBinary(box.Value()).Dims()
	bmin, bmax := geobin.WrapBinary(box.Value()).Rect()
	if odims == 2 {
		if bdims == 2 {
			return testIntersects2(omin, omax, bmin, bmax)
		} else {
			if bmin[2] <= 0 && bmax[2] >= 0 {
				return testIntersects2(omin, omax, bmin, bmax)
			} else {
				return false
			}
		}
	} else if odims == 3 {
		if bdims == 3 {
			return testIntersects3(omin, omax, bmin, bmax)
		} else {
			bmin[2], bmax[2] = math.Inf(-1), math.Inf(+1)
			return testIntersects3(omin, omax, bmin, bmax)
		}
	}
	return false
}

func testIntersects2(amin, amax, bmin, bmax [3]float64) bool {
	return bmin[0] <= amax[0] && bmin[1] <= amax[1] &&
		bmax[0] >= amin[0] && bmax[1] >= amin[1]
}

func testIntersects3(amin, amax, bmin, bmax [3]float64) bool {
	return bmin[0] <= amax[0] && bmin[1] <= amax[1] && bmin[2] <= amax[2] &&
		bmax[0] >= amin[0] && bmax[1] >= amin[1] && bmax[2] >= amin[2]
}
