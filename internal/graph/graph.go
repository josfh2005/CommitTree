// Package graph assigns commits to lanes and computes the line segments of a
// commit graph. It knows nothing about git: callers pass nodes in topological
// order (children before parents) and draw the returned rows.
package graph

// MaxStraight is how many rows a line may pass through before it is cut and
// drawn as a pair of arrows.
const MaxStraight = 30

type Node struct {
	Hash    string
	Parents []string
}

type EdgeKind int

const (
	Line EdgeKind = iota
	ArrowDown
	ArrowUp
)

// Edge is a segment from the previous row's center (column From) to this
// row's center (column To). Arrow edges name the commit they point toward.
type Edge struct {
	From   int      `json:"from"`
	To     int      `json:"to"`
	Color  int      `json:"color"`
	Kind   EdgeKind `json:"kind"`
	Target string   `json:"target,omitempty"`
}

type Row struct {
	Lane  int    `json:"lane"`
	Color int    `json:"color"`
	Edges []Edge `json:"edges"`
}

type lane struct {
	want   string // commit this line leads to
	source string // commit this line started at
	color  int
	from   []int // columns on the previous row that feed this line
	run    int   // rows passed without arriving
	hidden bool  // cut: slot reserved, nothing drawn
}

type Layout struct {
	lanes     []*lane
	nextColor int
}

func New() *Layout { return &Layout{} }

// Add lays out the next nodes, continuing from previous calls.
func (l *Layout) Add(nodes []Node) []Row {
	rows := make([]Row, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, l.add(n))
	}
	return rows
}

func (l *Layout) add(n Node) Row {
	var targets []int
	for j, ln := range l.lanes {
		if ln != nil && ln.want == n.Hash {
			targets = append(targets, j)
		}
	}
	var col, color int
	if len(targets) > 0 {
		col, color = targets[0], l.lanes[targets[0]].color
	} else {
		col, color = l.freeSlot(nil), l.newColor()
	}

	edges := []Edge{}
	for j, ln := range l.lanes {
		if ln == nil {
			continue
		}
		switch {
		case ln.want == n.Hash && ln.hidden:
			edges = append(edges, Edge{From: col, To: col, Color: ln.color, Kind: ArrowUp, Target: ln.source})
		case ln.want == n.Hash:
			for _, f := range ln.from {
				edges = append(edges, Edge{From: f, To: col, Color: ln.color, Kind: Line})
			}
		case ln.hidden:
			// Cut line: the slot stays reserved but nothing is drawn.
		default:
			ln.run++
			if ln.run > MaxStraight {
				ln.hidden = true
				edges = append(edges, Edge{From: j, To: j, Color: ln.color, Kind: ArrowDown, Target: ln.want})
				continue
			}
			for _, f := range ln.from {
				edges = append(edges, Edge{From: f, To: j, Color: ln.color, Kind: Line})
			}
		}
	}

	freed := make(map[int]bool, len(targets))
	for _, j := range targets {
		l.lanes[j] = nil
		freed[j] = true
	}
	for j, ln := range l.lanes {
		if ln != nil && !ln.hidden {
			ln.from = []int{j}
		}
	}

	if len(n.Parents) > 0 {
		l.set(col, &lane{want: n.Parents[0], source: n.Hash, color: color, from: []int{col}})
		for _, p := range n.Parents[1:] {
			if p == n.Parents[0] {
				continue
			}
			if j := l.find(p); j >= 0 {
				ln := l.lanes[j]
				if ln.hidden {
					ln.hidden, ln.from = false, nil
				}
				ln.from = append(ln.from, col)
				ln.run = 0
				continue
			}
			l.set(l.freeSlot(freed), &lane{want: p, source: n.Hash, color: l.newColor(), from: []int{col}})
		}
	}
	l.trim()
	return Row{Lane: col, Color: color, Edges: edges}
}

func (l *Layout) freeSlot(exclude map[int]bool) int {
	for j, ln := range l.lanes {
		if ln == nil && !exclude[j] {
			return j
		}
	}
	return len(l.lanes)
}

func (l *Layout) set(j int, ln *lane) {
	for len(l.lanes) <= j {
		l.lanes = append(l.lanes, nil)
	}
	l.lanes[j] = ln
}

func (l *Layout) find(hash string) int {
	for j, ln := range l.lanes {
		if ln != nil && ln.want == hash {
			return j
		}
	}
	return -1
}

func (l *Layout) trim() {
	for len(l.lanes) > 0 && l.lanes[len(l.lanes)-1] == nil {
		l.lanes = l.lanes[:len(l.lanes)-1]
	}
}

func (l *Layout) newColor() int {
	c := l.nextColor
	l.nextColor++
	return c
}
