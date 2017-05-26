package rtree

import (
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unsafe"

	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
	"github.com/tidwall/pinhole"
)

type transformer func(minIn, maxIn [3]float64) (minOut, maxOut [3]float64)

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

type treeNode struct {
	minX, minY, minZ float64
	maxX, maxY, maxZ float64
	children         []unsafe.Pointer
	leaf             bool
	height           int8
}

func (a *treeNode) extend(b *treeNode) {
	a.minX = mathMin(a.minX, b.minX)
	a.maxX = mathMax(a.maxX, b.maxX)
	a.minY = mathMin(a.minY, b.minY)
	a.maxY = mathMax(a.maxY, b.maxY)
	a.minZ = mathMin(a.minZ, b.minZ)
	a.maxZ = mathMax(a.maxZ, b.maxZ)
}

func (a *treeNode) intersectionArea(b *treeNode) float64 {
	var minX = mathMax(a.minX, b.minX)
	var maxX = mathMin(a.maxX, b.maxX)
	var minY = mathMax(a.minY, b.minY)
	var maxY = mathMin(a.maxY, b.maxY)
	var minZ = mathMax(a.minZ, b.minZ)
	var maxZ = mathMin(a.maxZ, b.maxZ)
	return mathMax(0, maxX-minX) * mathMax(0, maxY-minY) * mathMax(0, maxZ-minZ)
}
func (a *treeNode) area() float64 {
	return (a.maxX - a.minX) * (a.maxY - a.minY) * (a.maxZ - a.minZ)
}
func (a *treeNode) enlargedArea(b *treeNode) float64 {
	return (mathMax(b.maxX, a.maxX) - mathMin(b.minX, a.minX)) *
		(mathMax(b.maxY, a.maxY) - mathMin(b.minY, a.minY)) *
		(mathMax(b.maxZ, a.maxZ) - mathMin(b.minZ, a.minZ))
}

func (a *treeNode) intersects(b *treeNode) bool {
	return b.minX <= a.maxX && b.minY <= a.maxY && b.minZ <= a.maxZ &&
		b.maxX >= a.minX && b.maxY >= a.minY && b.maxZ >= a.minZ
}
func (a *treeNode) contains(b *treeNode) bool {
	return a.minX <= b.minX && a.minY <= b.minY && a.minZ <= b.minZ &&
		b.maxX <= a.maxX && b.maxY <= a.maxY && b.maxZ <= a.maxZ
}

func (a *treeNode) margin() float64 {
	return (a.maxX - a.minX) + (a.maxY - a.minY) + (a.maxZ - a.minZ)
}

type Options struct {
	MaxEntries  int
	Transformer func(minIn, maxIn [3]float64) (minOut, maxOut [3]float64)
}

var DefaultOptions = &Options{
	MaxEntries:  9,
	Transformer: nil,
}

type RTree struct {
	maxEntries int
	minEntries int
	t          transformer
	data       *treeNode
	reusePath  []*treeNode
}

func New(opts *Options) *RTree {
	tr := &RTree{}
	if opts == nil {
		opts = DefaultOptions
	}
	tr.t = opts.Transformer
	tr.maxEntries = int(mathMax(4, float64(opts.MaxEntries)))
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
		minZ:     mathInfPos,
		maxX:     mathInfNeg,
		maxY:     mathInfNeg,
		maxZ:     mathInfNeg,
	}
}
func fillBBox(item pair.Pair, bbox *treeNode, t transformer) {
	min, max := geobin.WrapBinary(item.Value()).Rect(t)
	bbox.minX, bbox.minY, bbox.minZ = min[0], min[1], min[2]
	bbox.maxX, bbox.maxY, bbox.maxZ = max[0], max[1], max[2]
}
func (tr *RTree) Insert(item pair.Pair) {
	min, max := geobin.WrapBinary(item.Value()).Rect(tr.t)
	tr.insertBBox(item, min[0], min[1], min[2], max[0], max[1], max[2])
}
func (tr *RTree) insertBBox(item pair.Pair, minX, minY, minZ, maxX, maxY, maxZ float64) {
	var bbox treeNode
	bbox.minX, bbox.minY, bbox.minZ = minX, minY, minZ
	bbox.maxX, bbox.maxY, bbox.maxZ = maxX, maxY, maxZ
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
	node.children = node.children[:splitIndex]

	newNode := createNode(spliced)
	newNode.height = node.height
	newNode.leaf = node.leaf

	calcBBox(node, tr.t)
	calcBBox(newNode, tr.t)

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
	calcBBox(tr.data, tr.t)
}
func (tr *RTree) chooseSplitIndex(node *treeNode, m, M int) int {
	var i int
	var bbox1, bbox2 *treeNode
	var overlap, area, minOverlap, minArea float64
	var index int

	minArea = mathInfPos
	minOverlap = minArea

	for i = m; i <= M-m; i++ {
		bbox1 = distBBox(node, 0, i, nil, tr.t)
		bbox2 = distBBox(node, i, M, nil, tr.t)

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
	var zMargin = tr.allDistMargin(node, m, M, 3)
	if xMargin < yMargin { // xyz, xzy, zxy
		if xMargin < zMargin { // xyz, xzy
			sortNodes(node, 1, tr.t)
		}
	} else if yMargin < zMargin { // yxz, yzx
		sortNodes(node, 2, tr.t)
	}
}

type leafByDim struct {
	node *treeNode
	dim  int
	t    transformer
}

func (arr *leafByDim) Len() int { return len(arr.node.children) }
func (arr *leafByDim) Less(i, j int) bool {
	var a, b treeNode
	fillBBox(pair.FromPointer(arr.node.children[i]), &a, arr.t)
	fillBBox(pair.FromPointer(arr.node.children[j]), &b, arr.t)
	if arr.dim == 1 {
		return a.minX < b.minX
	}
	if arr.dim == 2 {
		return a.minY < b.minY
	}
	if arr.dim == 3 {
		return a.minZ < b.minZ
	}
	return false
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
	if arr.dim == 2 {
		return a.minY < b.minY
	}
	if arr.dim == 3 {
		return a.minZ < b.minZ
	}
	return false
}
func (arr *nodeByDim) Swap(i, j int) {
	arr.node.children[i], arr.node.children[j] = arr.node.children[j], arr.node.children[i]
}
func sortNodes(node *treeNode, dim int, t transformer) {
	if node.leaf {
		sort.Sort(&leafByDim{node: node, dim: dim, t: t})
	} else {
		sort.Sort(&nodeByDim{node: node, dim: dim})
	}
}

func (tr *RTree) allDistMargin(node *treeNode, m, M int, dim int) float64 {
	sortNodes(node, dim, tr.t)
	var leftBBox = distBBox(node, 0, m, nil, tr.t)
	var rightBBox = distBBox(node, M-m, M, nil, tr.t)
	var margin = leftBBox.margin() + rightBBox.margin()

	var i int

	if node.leaf {
		var child treeNode
		for i = m; i < M-m; i++ {
			fillBBox(pair.FromPointer(node.children[i]), &child, tr.t)
			leftBBox.extend(&child)
			margin += leftBBox.margin()
		}
		for i = M - m - 1; i >= m; i-- {
			fillBBox(pair.FromPointer(node.children[i]), &child, tr.t)
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

func calcBBox(node *treeNode, t transformer) {
	distBBox(node, 0, len(node.children), node, t)
}
func distBBox(node *treeNode, k, p int, destNode *treeNode, t transformer) *treeNode {
	if destNode == nil {
		destNode = createNode(nil)
	} else {
		destNode.minX = mathInfPos
		destNode.minY = mathInfPos
		destNode.minZ = mathInfPos
		destNode.maxX = mathInfNeg
		destNode.maxY = mathInfNeg
		destNode.maxZ = mathInfNeg
	}

	for i := k; i < p; i++ {
		ptr := node.children[i]
		if node.leaf {
			var child treeNode
			fillBBox(pair.FromPointer(ptr), &child, t)
			destNode.extend(&child)
		} else {
			child := (*treeNode)(ptr)
			destNode.extend(child)
		}
	}
	return destNode
}

func (tr *RTree) Search(bbox pair.Pair, iter func(item pair.Pair) bool) bool {
	min, max := geobin.WrapBinary(bbox.Value()).Rect(tr.t)
	return tr.searchBBox(min[0], min[1], min[2], max[0], max[1], max[2], iter)
}

func (tr *RTree) searchBBox(minX, minY, minZ, maxX, maxY, maxZ float64,
	iter func(item pair.Pair) bool) bool {
	var bboxn treeNode
	bboxn.minX, bboxn.minY, bboxn.minZ = minX, minY, minZ
	bboxn.maxX, bboxn.maxY, bboxn.maxZ = maxX, maxY, maxZ
	if !tr.data.intersects(&bboxn) {
		return true
	}
	return search(tr.data, &bboxn, iter, tr.t)
}

func search(node, bbox *treeNode, iter func(item pair.Pair) bool, t transformer) bool {
	if node.leaf {
		for i := 0; i < len(node.children); i++ {
			item := pair.FromPointer(node.children[i])
			var child treeNode
			fillBBox(item, &child, t)
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
				if !search(child, bbox, iter, t) {
					return false
				}
			}
		}
	}
	return true
}

func (tr *RTree) Remove(item pair.Pair) {
	min, max := geobin.WrapBinary(item.Value()).Rect(tr.t)
	tr.removeBBox(item, min[0], min[1], min[2], max[0], max[1], max[2])
}

func (tr *RTree) removeBBox(item pair.Pair, minX, minY, minZ, maxX, maxY, maxZ float64) {
	var bbox treeNode
	bbox.minX, bbox.minY, bbox.minZ = minX, minY, minZ
	bbox.maxX, bbox.maxY, bbox.maxZ = maxX, maxY, maxZ
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
			calcBBox(path[i], tr.t)
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

func (tr *RTree) Traverse(iter func(min, max [3]float64, level int, item pair.Pair) bool) {
	traverse(tr.data, iter, tr.t)
}

func traverse(node *treeNode, iter func(min, max [3]float64, level int, item pair.Pair) bool, t transformer) bool {
	if !iter(
		[3]float64{node.minX, node.minY, node.minZ},
		[3]float64{node.maxX, node.maxY, node.maxZ},
		int(node.height), pair.Pair{},
	) {
		return false
	}
	if node.leaf {
		for _, ptr := range node.children {
			item := pair.FromPointer(ptr)
			var bbox treeNode
			fillBBox(item, &bbox, t)
			if !iter(
				[3]float64{bbox.minX, bbox.minY, bbox.minZ},
				[3]float64{bbox.maxX, bbox.maxY, bbox.maxZ},
				0, item,
			) {
				return false
			}
		}
	} else {
		for _, ptr := range node.children {
			if !traverse((*treeNode)(ptr), iter, t) {
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

func (tr *RTree) Bounds() (min, max [3]float64) {
	if len(tr.data.children) == 0 {
		return [3]float64{0, 0, 0}, [3]float64{0, 0, 0}
	}
	return [3]float64{tr.data.minX, tr.data.minY, tr.data.minZ},
		[3]float64{tr.data.maxX, tr.data.maxY, tr.data.maxZ}
}

// Load bulk loads items. For now it only loads each item one at a time.
// In the future it should use the OMT algorithm.
func (tr *RTree) Load(items []pair.Pair) {
	for _, item := range items {
		tr.Insert(item)
	}
}

func (tr *RTree) SavePNG(path string, width, height int, scale float64, showNodes bool, withGIF bool, printer io.Writer) error {
	p := pinhole.New()
	tr.Traverse(func(min, max [3]float64, level int, item pair.Pair) bool {
		p.Begin()
		if level > 0 && showNodes {
			p.DrawCube(min[0], min[1], min[2], max[0], max[1], max[2])
			switch level {
			default:
				p.Colorize(color.RGBA{96, 96, 96, 128})
			case 1:
				p.Colorize(color.RGBA{32, 64, 32, 64})
			case 2:
				p.Colorize(color.RGBA{48, 48, 96, 96})
			case 3:
				p.Colorize(color.RGBA{96, 128, 128, 128})
			case 4:
				p.Colorize(color.RGBA{128, 128, 196, 196})
			}
		} else {
			p.DrawDot(min[0], min[1], min[2], 0.04)
			p.Colorize(color.White)
		}
		p.End()
		return true
	})
	p.Center()
	p.Scale(scale, scale, scale)
	// render the paths in an image
	opts := *pinhole.DefaultImageOptions
	opts.LineWidth = 0.045
	opts.BGColor = color.Black
	if err := p.SavePNG(path, width, height, &opts); err != nil {
		return err
	}
	if printer != nil {
		fmt.Fprintf(printer, "wrote %s\n", path)
	}
	if withGIF {
		var palette = palette.WebSafe
		outGif := &gif.GIF{}
		for i := 0; i < 60; i++ {
			p.Rotate(0, math.Pi*2/60.0, 0)
			inPng := p.Image(width, height, &opts)
			inGif := image.NewPaletted(inPng.Bounds(), palette)
			draw.Draw(inGif, inPng.Bounds(), inPng, image.Point{}, draw.Src)
			outGif.Image = append(outGif.Image, inGif)
			outGif.Delay = append(outGif.Delay, 0)
			if printer != nil {
				fmt.Fprintf(printer, "wrote gif frame %d/%d\n", i, 60)
			}
		}
		if strings.HasSuffix(path, ".png") {
			path = path[:len(path)-4] + ".gif"
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := gif.EncodeAll(f, outGif); err != nil {
			return err
		}
		if printer != nil {
			fmt.Fprintf(printer, "wrote %s\n", path)
		}
	}
	return nil
}
