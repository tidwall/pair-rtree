package rtree

import (
	"unsafe"

	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
	"github.com/tidwall/tinyqueue"
)

type queueItem struct {
	node   unsafe.Pointer
	isItem bool
	dist   float64
}

func (item *queueItem) Less(b tinyqueue.Item) bool {
	return item.dist < b.(*queueItem).dist
}

func (tr *RTree) KNN(x, y float64, iter func(item pair.Pair, dist float64) bool) bool {
	node := tr.data
	queue := tinyqueue.New(nil)
	for node != nil {
		for _, child := range node.children {
			var min, max [2]float64
			if node.leaf {
				item := pair.FromPointer(child)
				omin, omax := geobin.WrapBinary(item.Value()).Rect(tr.t)
				min[0], min[1] = omin[0], omin[1]
				max[0], max[1] = omax[0], omax[1]
			} else {
				node := (*treeNode)(child)
				min[0], min[1] = node.minX, node.minY
				max[0], max[1] = node.maxX, node.maxY
			}
			queue.Push(&queueItem{
				node:   child,
				isItem: node.leaf,
				dist:   boxDist(x, y, min, max),
			})
		}
		for queue.Len() > 0 && queue.Peek().(*queueItem).isItem {
			item := queue.Pop().(*queueItem)
			candidate := item.node
			if !iter(pair.FromPointer(candidate), item.dist) {
				return false
			}
		}
		last := queue.Pop()
		if last != nil {
			node = (*treeNode)(last.(*queueItem).node)
		} else {
			node = nil
		}
	}
	return true
}

func boxDist(x, y float64, min, max [2]float64) float64 {
	dx := axisDist(x, min[0], max[0])
	dy := axisDist(y, min[1], max[1])
	return dx*dx + dy*dy
}
func axisDist(k, min, max float64) float64 {
	if k < min {
		return min - k
	}
	if k <= max {
		return 0
	}
	return k - max
}
