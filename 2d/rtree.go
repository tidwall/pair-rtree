package rtree

import (
	"math"
	"sort"
	"unsafe"

	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
)

var mathInfNeg = math.Inf(-1)
var mathInfPos = math.Inf(+1)

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

const defaultMaxEntries = 9

type treeNode struct {
	minX, minY float64
	maxX, maxY float64
	children   []unsafe.Pointer
	leaf       bool
	height     int8
}

func (a *treeNode) extend(b *treeNode) {
	a.minX = mathMin(a.minX, b.minX)
	a.maxX = mathMax(a.maxX, b.maxX)
	a.minY = mathMin(a.minY, b.minY)
	a.maxY = mathMax(a.maxY, b.maxY)
}

func (a *treeNode) intersectionArea(b *treeNode) float64 {
	var minX = mathMax(a.minX, b.minX)
	var maxX = mathMin(a.maxX, b.maxX)
	var minY = mathMax(a.minY, b.minY)
	var maxY = mathMin(a.maxY, b.maxY)
	return mathMax(0, maxX-minX) * mathMax(0, maxY-minY)
}
func (a *treeNode) area() float64 {
	return (a.maxX - a.minX) * (a.maxY - a.minY)
}
func (a *treeNode) enlargedArea(b *treeNode) float64 {
	return (mathMax(b.maxX, a.maxX) - mathMin(b.minX, a.minX)) *
		(mathMax(b.maxY, a.maxY) - mathMin(b.minY, a.minY))
}
func (a *treeNode) intersects(b *treeNode) bool {
	return b.minX <= a.maxX && b.minY <= a.maxY &&
		b.maxX >= a.minX && b.maxY >= a.minY
}
func (a *treeNode) contains(b *treeNode) bool {
	return a.minX <= b.minX && a.minY <= b.minY &&
		b.maxX <= a.maxX && b.maxY <= a.maxY
}
func (a *treeNode) margin() float64 {
	return (a.maxX - a.minX) + (a.maxY - a.minY)
}

type RTree struct {
	maxEntries int
	minEntries int
	data       *treeNode
	reusePath  []*treeNode
}

func New() *RTree {
	tr := &RTree{}
	maxEntries := defaultMaxEntries
	tr.maxEntries = int(mathMax(4, float64(maxEntries)))
	tr.minEntries = int(mathMax(2, math.Ceil(float64(tr.maxEntries)*0.4)))
	tr.data = createNode(nil)
	return tr
}

func createNode(children []unsafe.Pointer) *treeNode {
	return &treeNode{
		children: children,
		height:   1,
		leaf:     true,
		minX:     mathInfPos,
		minY:     mathInfPos,
		maxX:     mathInfNeg,
		maxY:     mathInfNeg,
	}
}
func fillBBox(item pair.Pair, bbox *treeNode) {
	min, max := geobin.WrapBinary(item.Value()).Rect()
	bbox.minX, bbox.minY, bbox.maxX, bbox.maxY = min[0], min[1], max[0], max[1]
}
func (tr *RTree) Insert(item pair.Pair) {
	min, max := geobin.WrapBinary(item.Value()).Rect()
	tr.insertBBox(item, min[0], min[1], max[0], max[1])
}
func (tr *RTree) insertBBox(item pair.Pair, minX, minY, maxX, maxY float64) {
	var bbox treeNode
	bbox.minX, bbox.minY = minX, minY
	bbox.maxX, bbox.maxY = maxX, maxY
	tr.insert(&bbox, item, tr.data.height-1, false)
}

func (tr *RTree) insert(bbox *treeNode, item pair.Pair, level int8, isNode bool) {
	tr.reusePath = tr.reusePath[:0]
	node, insertPath := tr.chooseSubtree(bbox, tr.data, level, tr.reusePath)
	node.children = append(node.children, item.Pointer())
	node.extend(bbox)
	for level >= 0 {
		if len(insertPath[level].children) > tr.maxEntries {
			insertPath = tr.split(insertPath, level)
			level--
		} else {
			break
		}
	}
	tr.adjustParentBBoxes(bbox, insertPath, level)
	tr.reusePath = insertPath
}

func (tr *RTree) adjustParentBBoxes(bbox *treeNode, path []*treeNode, level int8) {
	// adjust bboxes along the given tree path
	for i := level; i >= 0; i-- {
		path[i].extend(bbox)
	}
}
func (tr *RTree) split(insertPath []*treeNode, level int8) []*treeNode {
	var node = insertPath[level]
	var M = len(node.children)
	var m = tr.minEntries

	tr.chooseSplitAxis(node, m, M)
	splitIndex := tr.chooseSplitIndex(node, m, M)

	spliced := make([]unsafe.Pointer, len(node.children)-splitIndex)
	copy(spliced, node.children[splitIndex:])

	newChildren := make([]unsafe.Pointer, splitIndex)
	copy(newChildren, node.children[:splitIndex])
	node.children = newChildren

	newNode := createNode(spliced)
	newNode.height = node.height
	newNode.leaf = node.leaf

	calcBBox(node)
	calcBBox(newNode)

	if level != 0 {
		insertPath[level-1].children = append(insertPath[level-1].children, unsafe.Pointer(newNode))
	} else {
		tr.splitRoot(node, newNode)
	}
	return insertPath
}
func (tr *RTree) splitRoot(node, newNode *treeNode) {
	tr.data = createNode([]unsafe.Pointer{unsafe.Pointer(node), unsafe.Pointer(newNode)})
	tr.data.height = node.height + 1
	tr.data.leaf = false
	calcBBox(tr.data)
}
func (tr *RTree) chooseSplitIndex(node *treeNode, m, M int) int {
	var i int
	var bbox1, bbox2 *treeNode
	var overlap, area, minOverlap, minArea float64
	var index int

	minArea = mathInfPos
	minOverlap = minArea

	for i = m; i <= M-m; i++ {
		bbox1 = distBBox(node, 0, i, nil)
		bbox2 = distBBox(node, i, M, nil)

		overlap = bbox1.intersectionArea(bbox2)
		area = bbox1.area() + bbox2.area()

		// choose distribution with minimum overlap
		if overlap < minOverlap {
			minOverlap = overlap
			index = i

			if area < minArea {
				minArea = area
			}
		} else if overlap == minOverlap {
			// otherwise choose distribution with minimum area
			if area < minArea {
				minArea = area
				index = i
			}
		}
	}
	return index
}

func (tr *RTree) chooseSplitAxis(node *treeNode, m, M int) {
	var xMargin = tr.allDistMargin(node, m, M, 1)
	var yMargin = tr.allDistMargin(node, m, M, 2)
	if xMargin < yMargin { // xy
		sortNodes(node, 1)
	}
}

type leafByDim struct {
	node *treeNode
	dim  int
}

func (arr *leafByDim) Len() int { return len(arr.node.children) }
func (arr *leafByDim) Less(i, j int) bool {
	var a, b treeNode
	fillBBox(pair.FromPointer(arr.node.children[i]), &a)
	fillBBox(pair.FromPointer(arr.node.children[j]), &b)
	if arr.dim == 1 {
		return a.minX < b.minX
	}
	return a.minY < b.minY
}
func (arr *leafByDim) Swap(i, j int) {
	arr.node.children[i], arr.node.children[j] = arr.node.children[j], arr.node.children[i]
}

type nodeByDim struct {
	node *treeNode
	dim  int
}

func (arr *nodeByDim) Len() int { return len(arr.node.children) }
func (arr *nodeByDim) Less(i, j int) bool {
	a := (*treeNode)(arr.node.children[i])
	b := (*treeNode)(arr.node.children[j])
	if arr.dim == 1 {
		return a.minX < b.minX
	}
	return a.minY < b.minY
}
func (arr *nodeByDim) Swap(i, j int) {
	arr.node.children[i], arr.node.children[j] = arr.node.children[j], arr.node.children[i]
}
func sortNodes(node *treeNode, dim int) {
	if node.leaf {
		sort.Sort(&leafByDim{node: node, dim: dim})
	} else {
		sort.Sort(&nodeByDim{node: node, dim: dim})
	}
}

func (tr *RTree) allDistMargin(node *treeNode, m, M int, dim int) float64 {
	sortNodes(node, dim)
	var leftBBox = distBBox(node, 0, m, nil)
	var rightBBox = distBBox(node, M-m, M, nil)
	var margin = leftBBox.margin() + rightBBox.margin()

	var i int

	if node.leaf {
		var child treeNode
		for i = m; i < M-m; i++ {
			fillBBox(pair.FromPointer(node.children[i]), &child)
			leftBBox.extend(&child)
			margin += leftBBox.margin()
		}
		for i = M - m - 1; i >= m; i-- {
			fillBBox(pair.FromPointer(node.children[i]), &child)
			leftBBox.extend(&child)
			margin += rightBBox.margin()
		}
	} else {
		for i = m; i < M-m; i++ {
			child := (*treeNode)(node.children[i])
			leftBBox.extend(child)
			margin += leftBBox.margin()
		}
		for i = M - m - 1; i >= m; i-- {
			child := (*treeNode)(node.children[i])
			leftBBox.extend(child)
			margin += rightBBox.margin()
		}
	}
	return margin
}
func (tr *RTree) chooseSubtree(bbox, node *treeNode, level int8, path []*treeNode) (*treeNode, []*treeNode) {
	var targetNode *treeNode
	var area, enlargement, minArea, minEnlargement float64
	for {
		path = append(path, node)
		if node.leaf || int8(len(path)-1) == level {
			break
		}
		minEnlargement = mathInfPos
		minArea = minEnlargement
		for _, ptr := range node.children {
			child := (*treeNode)(ptr)
			area = child.area()
			enlargement = bbox.enlargedArea(child) - area
			if enlargement < minEnlargement {
				minEnlargement = enlargement
				if area < minArea {
					minArea = area
				}
				targetNode = child
			} else if enlargement == minEnlargement {
				if area < minArea {
					minArea = area
					targetNode = child
				}
			}
		}
		if targetNode != nil {
			node = targetNode
		} else if len(node.children) > 0 {
			node = (*treeNode)(node.children[0])
		} else {
			node = nil
		}
	}
	return node, path
}

func calcBBox(node *treeNode) {
	distBBox(node, 0, len(node.children), node)
}
func distBBox(node *treeNode, k, p int, destNode *treeNode) *treeNode {
	if destNode == nil {
		destNode = createNode(nil)
	} else {
		destNode.minX = mathInfPos
		destNode.minY = mathInfPos
		destNode.maxX = mathInfNeg
		destNode.maxY = mathInfNeg
	}

	for i := k; i < p; i++ {
		ptr := node.children[i]
		if node.leaf {
			var child treeNode
			fillBBox(pair.FromPointer(ptr), &child)
			destNode.extend(&child)
		} else {
			child := (*treeNode)(ptr)
			destNode.extend(child)
		}
	}
	return destNode
}

func (tr *RTree) Search(bbox pair.Pair, iter func(item pair.Pair) bool) bool {
	min, max := geobin.WrapBinary(bbox.Value()).Rect()
	return tr.searchBBox(min[0], min[1], max[0], max[1], iter)
}

func (tr *RTree) searchBBox(minX, minY, maxX, maxY float64,
	iter func(item pair.Pair) bool) bool {
	var bboxn treeNode
	bboxn.minX, bboxn.minY = minX, minY
	bboxn.maxX, bboxn.maxY = maxX, maxY
	if !tr.data.intersects(&bboxn) {
		return true
	}
	return search(tr.data, &bboxn, iter)
}

func search(node, bbox *treeNode, iter func(item pair.Pair) bool) bool {
	if node.leaf {
		for i := 0; i < len(node.children); i++ {
			item := pair.FromPointer(node.children[i])
			var child treeNode
			fillBBox(item, &child)
			if bbox.intersects(&child) {
				if !iter(item) {
					return false
				}
			}
		}
	} else {
		for i := 0; i < len(node.children); i++ {
			child := (*treeNode)(node.children[i])
			if bbox.intersects(child) {
				if !search(child, bbox, iter) {
					return false
				}
			}
		}
	}
	return true
}

func (tr *RTree) Remove(item pair.Pair) {
	min, max := geobin.WrapBinary(item.Value()).Rect()
	tr.removeBBox(item, min[0], min[1], max[0], max[1])
}

func (tr *RTree) removeBBox(item pair.Pair, minX, minY, maxX, maxY float64) {
	var bbox treeNode
	bbox.minX, bbox.minY = minX, minY
	bbox.maxX, bbox.maxY = maxX, maxY
	path := tr.reusePath[:0]

	var node = tr.data
	var indexes []int

	var i int
	var parent *treeNode
	var index int
	var goingUp bool

	for node != nil || len(path) != 0 {
		if node == nil {
			node = path[len(path)-1]
			path = path[:len(path)-1]
			if len(path) == 0 {
				parent = nil
			} else {
				parent = path[len(path)-1]
			}
			i = indexes[len(indexes)-1]
			indexes = indexes[:len(indexes)-1]
			goingUp = true
		}

		if node.leaf {
			index = findItem(item, node)
			if index != -1 {
				// item found, remove the item and condense tree upwards
				copy(node.children[index:], node.children[index+1:])
				node.children[len(node.children)-1] = nil
				node.children = node.children[:len(node.children)-1]
				path = append(path, node)
				tr.condense(path)
				goto done
			}
		}
		if !goingUp && !node.leaf && node.contains(&bbox) { // go down
			path = append(path, node)
			indexes = append(indexes, i)
			i = 0
			parent = node
			node = (*treeNode)(node.children[0])
		} else if parent != nil { // go right
			i++
			if i == len(parent.children) {
				node = nil
			} else {
				node = (*treeNode)(parent.children[i])
			}
			goingUp = false
		} else {
			node = nil
		}
	}
done:
	tr.reusePath = path
	return
}
func (tr *RTree) condense(path []*treeNode) {
	// go through the path, removing empty nodes and updating bboxes
	var siblings []unsafe.Pointer
	for i := len(path) - 1; i >= 0; i-- {
		if len(path[i].children) == 0 {
			if i > 0 {
				siblings = path[i-1].children
				index := -1
				for j := 0; j < len(siblings); j++ {
					if siblings[j] == unsafe.Pointer(path[i]) {
						index = j
						break
					}
				}
				copy(siblings[index:], siblings[index+1:])
				siblings[len(siblings)-1] = nil
				siblings = siblings[:len(siblings)-1]
				path[i-1].children = siblings
			} else {
				tr.data = createNode(nil) // clear tree
			}
		} else {
			calcBBox(path[i])
		}
	}
}
func findItem(item pair.Pair, node *treeNode) int {
	ptr := item.Pointer()
	for i := 0; i < len(node.children); i++ {
		if node.children[i] == ptr {
			return i
		}
	}
	return -1
}
func (tr *RTree) Count() int {
	return count(tr.data)
}
func count(node *treeNode) int {
	if node.leaf {
		return len(node.children)
	}
	var n int
	for _, ptr := range node.children {
		n += count((*treeNode)(ptr))
	}
	return n
}

func (tr *RTree) Traverse(iter func(min, max [2]float64, level int, item pair.Pair) bool) {
	traverse(tr.data, iter)
}

func traverse(node *treeNode, iter func(min, max [2]float64, level int, item pair.Pair) bool) bool {
	if !iter(
		[2]float64{node.minX, node.minY},
		[2]float64{node.maxX, node.maxY},
		int(node.height), pair.Pair{},
	) {
		return false
	}
	if node.leaf {
		for _, ptr := range node.children {
			item := pair.FromPointer(ptr)
			var bbox treeNode
			fillBBox(item, &bbox)
			if !iter(
				[2]float64{bbox.minX, bbox.minY},
				[2]float64{bbox.maxX, bbox.maxY},
				0, item,
			) {
				return false
			}
		}
	} else {
		for _, ptr := range node.children {
			if !traverse((*treeNode)(ptr), iter) {
				return false
			}
		}
	}
	return true
}

func (tr *RTree) Scan(iter func(item pair.Pair) bool) bool {
	return scan(tr.data, iter)
}

func scan(node *treeNode, iter func(item pair.Pair) bool) bool {
	if node.leaf {
		for _, ptr := range node.children {
			if !iter(pair.FromPointer(ptr)) {
				return false
			}
		}
	} else {
		for _, ptr := range node.children {
			if !scan((*treeNode)(ptr), iter) {
				return false
			}
		}
	}
	return true
}

func (tr *RTree) Bounds() (min, max [2]float64) {
	if len(tr.data.children) == 0 {
		return [2]float64{0, 0}, [2]float64{0, 0}
	}
	return [2]float64{tr.data.minX, tr.data.minY},
		[2]float64{tr.data.maxX, tr.data.maxY}
}
