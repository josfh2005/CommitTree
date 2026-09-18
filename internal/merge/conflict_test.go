package merge

import (
	"strings"
	"testing"
)

const threeWay = `package main

import "fmt"

func main() {
<<<<<<< HEAD
	fmt.Println("ours")
||||||| base
	fmt.Println("base")
=======
	fmt.Println("theirs")
>>>>>>> feature
}
`

func TestParseThreeWayHunk(t *testing.T) {
	hunks, err := Parse(threeWay)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hunks))
	}
	h := hunks[0]
	if h.Ours != "\tfmt.Println(\"ours\")\n" {
		t.Errorf("ours = %q", h.Ours)
	}
	if h.Base != "\tfmt.Println(\"base\")\n" {
		t.Errorf("base = %q", h.Base)
	}
	if h.Theirs != "\tfmt.Println(\"theirs\")\n" {
		t.Errorf("theirs = %q", h.Theirs)
	}
	if !strings.HasSuffix(h.Before, "func main() {\n") {
		t.Errorf("before = %q", h.Before)
	}
	if h.After != "}\n" {
		t.Errorf("after = %q", h.After)
	}
}

func TestParseTwoWayHunkHasNoBase(t *testing.T) {
	hunks, err := Parse("a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\nb\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 || hunks[0].Base != "" {
		t.Fatalf("got %d hunks, base %q", len(hunks), hunks[0].Base)
	}
}

func TestParseSeveralHunksAreIndexed(t *testing.T) {
	content := "<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> x\nmiddle\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> x\n"
	hunks, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 2 || hunks[0].Index != 0 || hunks[1].Index != 1 {
		t.Fatalf("got %d hunks with indexes %d,%d", len(hunks), hunks[0].Index, hunks[1].Index)
	}
	if hunks[0].Ours != "1\n" || hunks[1].Theirs != "4\n" {
		t.Errorf("wrong sides: %q %q", hunks[0].Ours, hunks[1].Theirs)
	}
}

// A markdown setext underline is a run of "=" longer or shorter than the
// seven-character marker, and must not be mistaken for the separator.
func TestParseIgnoresMarkdownUnderline(t *testing.T) {
	content := "<<<<<<< HEAD\nTitle\n========\nbody\n=======\ntheirs\n>>>>>>> x\n"
	hunks, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	if hunks[0].Ours != "Title\n========\nbody\n" {
		t.Errorf("ours = %q", hunks[0].Ours)
	}
}

func TestParseCRLF(t *testing.T) {
	hunks, err := Parse("<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> x\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if hunks[0].Ours != "ours\r\n" {
		t.Errorf("ours = %q", hunks[0].Ours)
	}
}

func TestParseUnterminatedIsAnError(t *testing.T) {
	if _, err := Parse("<<<<<<< HEAD\nours\n=======\ntheirs\n"); err == nil {
		t.Fatal("want an error for an unterminated conflict")
	}
}

func TestParseCleanFileHasNoHunks(t *testing.T) {
	hunks, err := Parse("nothing to see\n")
	if err != nil || len(hunks) != 0 {
		t.Fatalf("hunks = %v, err = %v", hunks, err)
	}
}

func TestHasMarkers(t *testing.T) {
	if !HasMarkers("a\n<<<<<<< HEAD\n") {
		t.Error("want true for a file with an opening marker")
	}
	if HasMarkers("a\n========\nb\n") {
		t.Error("want false for a markdown underline")
	}
}
