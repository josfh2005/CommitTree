package graph_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/graph"
)

// nodes parses "hash:parent1,parent2" specs.
func nodes(specs ...string) []graph.Node {
	out := make([]graph.Node, 0, len(specs))
	for _, s := range specs {
		hash, parents, _ := strings.Cut(s, ":")
		n := graph.Node{Hash: hash, Parents: []string{}}
		if parents != "" {
			n.Parents = strings.Split(parents, ",")
		}
		out = append(out, n)
	}
	return out
}

func line(from, to, color int) graph.Edge {
	return graph.Edge{From: from, To: to, Color: color, Kind: graph.Line}
}

func row(lane, color int, edges ...graph.Edge) graph.Row {
	if edges == nil {
		edges = []graph.Edge{}
	}
	return graph.Row{Lane: lane, Color: color, Edges: edges}
}

func assertRows(t *testing.T, got, want []graph.Row) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("row %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func hasEdge(r graph.Row, e graph.Edge) bool {
	for _, x := range r.Edges {
		if x == e {
			return true
		}
	}
	return false
}

func TestLinearHistory(t *testing.T) {
	got := graph.New().Add(nodes("c:b", "b:a", "a"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0)),
		row(0, 0, line(0, 0, 0)),
	})
}

func TestBranchAndMerge(t *testing.T) {
	got := graph.New().Add(nodes("M:B1,F1", "F1:B0", "B1:B0", "B0"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(1, 1, line(0, 0, 0), line(0, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1)),
	})
}

func TestOctopusMerge(t *testing.T) {
	got := graph.New().Add(nodes("M:A,B,C", "A:R", "B:R", "C:R", "R"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0), line(0, 1, 1), line(0, 2, 2)),
		row(1, 1, line(0, 0, 0), line(1, 1, 1), line(2, 2, 2)),
		row(2, 2, line(0, 0, 0), line(1, 1, 1), line(2, 2, 2)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1), line(2, 0, 2)),
	})
}

func TestDisjointHistoriesReuseLaneWithNewColor(t *testing.T) {
	got := graph.New().Add(nodes("X1:X0", "X0", "Y1:Y0", "Y0"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0)),
		row(0, 1),
		row(0, 1, line(0, 0, 1)),
	})
}

func TestMergeJoinsExistingLane(t *testing.T) {
	got := graph.New().Add(nodes("S:B", "M:A,B", "A:B", "B"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(1, 1, line(0, 0, 0)),
		row(1, 1, line(0, 0, 0), line(1, 0, 0), line(1, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1)),
	})
}

func TestLongLaneIsCutWithArrows(t *testing.T) {
	specs := []string{"M:c1,F"}
	for i := 1; i <= 35; i++ {
		parent := fmt.Sprintf("c%d", i+1)
		if i == 35 {
			parent = "R"
		}
		specs = append(specs, fmt.Sprintf("c%d:%s", i, parent))
	}
	specs = append(specs, "F:R", "R")
	rows := graph.New().Add(nodes(specs...))

	if !hasEdge(rows[1], line(0, 1, 1)) {
		t.Fatalf("row 1: missing fork edge: %+v", rows[1].Edges)
	}
	for i := 2; i <= graph.MaxStraight; i++ {
		if !hasEdge(rows[i], line(1, 1, 1)) {
			t.Fatalf("row %d: missing straight line on lane 1: %+v", i, rows[i].Edges)
		}
	}
	arrowRow := graph.MaxStraight + 1
	down := graph.Edge{From: 1, To: 1, Color: 1, Kind: graph.ArrowDown, Target: "F"}
	if !hasEdge(rows[arrowRow], down) {
		t.Fatalf("row %d: missing arrow down: %+v", arrowRow, rows[arrowRow].Edges)
	}
	for i := arrowRow + 1; i <= 35; i++ {
		for _, e := range rows[i].Edges {
			if e.From == 1 || e.To == 1 {
				t.Fatalf("row %d: cut lane still drawn: %+v", i, e)
			}
		}
	}
	fRow := rows[36]
	up := graph.Edge{From: 1, To: 1, Color: 1, Kind: graph.ArrowUp, Target: "M"}
	if fRow.Lane != 1 || !hasEdge(fRow, up) {
		t.Fatalf("F row = %+v", fRow)
	}
	if len(rows[37].Edges) != 2 {
		t.Fatalf("root row edges = %+v", rows[37].Edges)
	}
}

func TestCutLaneFreesItsColumn(t *testing.T) {
	specs := []string{"M:c1,F"}
	for i := 1; i <= 35; i++ {
		parent := fmt.Sprintf("c%d", i+1)
		if i == 35 {
			parent = "R"
		}
		specs = append(specs, fmt.Sprintf("c%d:%s", i, parent))
		if i == 32 {
			specs = append(specs, "N:c33")
		}
	}
	specs = append(specs, "F:R", "R")
	rows := graph.New().Add(nodes(specs...))

	// Lane 1 (waiting for F) is cut at row MaxStraight+1; the new branch tip N
	// two rows later must reuse that column instead of opening column 2.
	n := rows[33]
	if n.Lane != 1 {
		t.Fatalf("N lane = %d, want 1 (freed by the cut line)", n.Lane)
	}
	up := graph.Edge{Kind: graph.ArrowUp, Target: "M"}
	found := false
	for _, e := range rows[37].Edges {
		if e.Kind == up.Kind && e.Target == up.Target {
			found = true
		}
	}
	if !found {
		t.Fatalf("F row missing arrow up: %+v", rows[37])
	}
}

func TestLayoutResumesAcrossPages(t *testing.T) {
	all := nodes("M:B1,F1", "F1:B0", "B1:B0", "B0")
	want := graph.New().Add(all)

	l := graph.New()
	got := append(l.Add(all[:2]), l.Add(all[2:])...)

	assertRows(t, got, want)
}
