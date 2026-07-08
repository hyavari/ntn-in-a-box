package tui

import (
	"fmt"
	"sync"
	"testing"
)

func TestRingBuffer_WriteBelowCapacity(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write("a")
	rb.Write("b")
	rb.Write("c")

	if rb.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", rb.Len())
	}
	got := rb.All()
	want := []string{"a", "b", "c"}
	assertLines(t, got, want)
}

func TestRingBuffer_WriteAtCapacity(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Write("a")
	rb.Write("b")
	rb.Write("c")

	if rb.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", rb.Len())
	}
	got := rb.All()
	want := []string{"a", "b", "c"}
	assertLines(t, got, want)
}

func TestRingBuffer_WriteOverCapacity(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Write("a")
	rb.Write("b")
	rb.Write("c")
	rb.Write("d")
	rb.Write("e")

	if rb.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", rb.Len())
	}
	got := rb.All()
	want := []string{"c", "d", "e"}
	assertLines(t, got, want)
}

func TestRingBuffer_LinesWindow(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := range 10 {
		rb.Write(fmt.Sprintf("line%d", i))
	}
	// Buffer holds: line5, line6, line7, line8, line9

	got := rb.Lines(1, 3)
	want := []string{"line6", "line7", "line8"}
	assertLines(t, got, want)
}

func TestRingBuffer_LinesStartBeyondLen(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write("a")
	rb.Write("b")

	got := rb.Lines(5, 3)
	if len(got) != 0 {
		t.Fatalf("Lines(5, 3) = %v, want empty", got)
	}
}

func TestRingBuffer_LinesCountBeyondLen(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write("a")
	rb.Write("b")

	got := rb.Lines(0, 10)
	want := []string{"a", "b"}
	assertLines(t, got, want)
}

func TestRingBuffer_ConcurrentWrites(t *testing.T) {
	rb := NewRingBuffer(100)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 50 {
				rb.Write(fmt.Sprintf("g%d-%d", id, j))
			}
		}(i)
	}
	wg.Wait()

	// 10 goroutines × 50 = 500 writes, buffer holds last 100.
	if rb.Len() != 100 {
		t.Fatalf("Len() = %d, want 100", rb.Len())
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d\n  got:  %v\n  want: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
