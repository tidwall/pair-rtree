package rtree

import (
	"math"
	"sync"

	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
	rtree2 "github.com/tidwall/pair-rtree/2d"
	rtree3 "github.com/tidwall/pair-rtree/3d"
)

type RTree struct {
	tr2 *rtree2.RTree
	tr3 *rtree3.RTree
}

func New() *RTree {
	return &RTree{
		tr2: rtree2.New(),
		tr3: rtree3.New(),
	}
}

func (tr *RTree) Insert(item pair.Pair) {
	if geobin.WrapBinary(item.Value()).Dims() == 2 {
		tr.tr2.Insert(item)
	} else {
		tr.tr3.Insert(item)
	}
}

func (tr *RTree) Remove(item pair.Pair) {
	if geobin.WrapBinary(item.Value()).Dims() == 2 {
		tr.tr2.Remove(item)
	} else {
		tr.tr3.Remove(item)
	}
}

func (tr *RTree) Search(box pair.Pair, iter func(item pair.Pair) bool) bool {
	dims := geobin.WrapBinary(box.Value()).Dims()
	min, max := geobin.WrapBinary(box.Value()).Rect()
	if dims == 2 {
		if !tr.tr2.Search(box, iter) {
			return false
		}
		box = pair.New(nil, geobin.Make3DRect(min[0], min[1], math.Inf(-1), max[0], max[1], math.Inf(+1)).Binary())
		return tr.tr3.Search(box, iter)
	} else {
		if min[2] <= 0 && max[2] >= 0 {
			if !tr.tr2.Search(box, iter) {
				return false
			}
		}
		return tr.tr3.Search(box, iter)
	}
}
func (tr *RTree) Count() int {
	return tr.tr2.Count() + tr.tr3.Count()
}
func (tr *RTree) KNN(pos pair.Pair, iter func(item pair.Pair, dist float64) bool) bool {
	empty2 := tr.isEmpty(2)
	empty3 := tr.isEmpty(3)
	if empty2 && empty3 {
		return true
	}
	p := geobin.WrapBinary(pos.Value()).Position()
	if empty3 {
		// only 2d
		return tr.tr2.KNN(p.X, p.Y, iter)
	}
	if empty2 {
		// only 3d
		return tr.tr3.KNN(p.X, p.Y, p.Z, iter)
	}
	// mux 3d and 2d
	type ctx struct {
		item pair.Pair
		dist float64
		next chan bool
		dims int
	}
	type qitem struct {
		item pair.Pair
		dist float64
	}

	var queues [2][]qitem
	var dones int
	var exit bool
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	fn := func(idx int) func(pair.Pair, float64) bool {
		return func(item pair.Pair, dist float64) bool {
			mu.Lock()
			if exit {
				mu.Unlock()
				return false
			}
			queues[idx] = append(queues[idx], qitem{item, dist})
			cond.Broadcast()
			mu.Unlock()
			return true
		}
	}
	qdone := func(_ bool) {
		mu.Lock()
		dones++
		cond.Broadcast()
		mu.Unlock()
	}
	go func() { qdone(tr.tr2.KNN(p.X, p.Y, fn(0))) }()
	go func() { qdone(tr.tr3.KNN(p.X, p.Y, p.Z, fn(1))) }()
	for {
		mu.Lock()
		for len(queues[0]) > 0 && len(queues[1]) > 0 {
			var qi qitem
			if queues[0][0].dist < queues[1][0].dist {
				qi = queues[0][0]
				queues[0] = queues[0][1:]
			} else {
				qi = queues[1][0]
				queues[1] = queues[1][1:]
			}
			if !iter(qi.item, qi.dist) {
				exit = true
				mu.Unlock()
				return false
			}
		}
		if dones == 2 {
			if !exit {
				for i := 0; i < 2; i++ {
					for _, qi := range queues[i] {
						if !iter(qi.item, qi.dist) {
							mu.Unlock()
							return false
						}
					}
				}
				exit = true
			}
			mu.Unlock()
			break
		}
		cond.Wait()
		mu.Unlock()
	}
	return true
}

func (tr *RTree) isEmpty(which int) bool {
	empty := true
	if which == 2 {
		tr.tr2.Traverse(func(min, max [2]float64, level int, item pair.Pair) bool {
			if level == 0 && !item.Zero() {
				empty = false
				return false
			}
			return true
		})
	} else if which == 3 {
		tr.tr3.Traverse(func(min, max [3]float64, level int, item pair.Pair) bool {
			if level == 0 && !item.Zero() {
				empty = false
				return false
			}
			return true
		})
	}
	return empty
}
func (tr *RTree) Scan(iter func(item pair.Pair) bool) bool {
	if !tr.tr2.Scan(iter) {
		return false
	}
	return tr.tr3.Scan(iter)
}
func (tr *RTree) Bounds() (min, max [3]float64) {
	empty2 := tr.isEmpty(2)
	empty3 := tr.isEmpty(3)
	if empty2 && empty3 {
		return [3]float64{0, 0, 0}, [3]float64{0, 0, 0}
	}
	if empty3 {
		min, max := tr.tr2.Bounds()
		return [3]float64{min[0], min[1], 0}, [3]float64{max[0], max[1], 0}
	}
	if empty2 {
		return tr.tr3.Bounds()
	}
	min, max = tr.tr3.Bounds()
	min2, max2 := tr.tr2.Bounds()
	if min2[0] < min[0] {
		min[0] = min2[0]
	}
	if max2[0] > max[0] {
		max[0] = max2[0]
	}
	if min2[1] < min[1] {
		min[1] = min2[1]
	}
	if max2[1] > max[1] {
		max[1] = max2[1]
	}
	return min, max
}
