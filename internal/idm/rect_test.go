package idm

import (
	"math"
	"testing"
)

func TestRectDimensions(t *testing.T) {
	r := Rect{X0: 10, Y0: 20, X1: 40, Y1: 25}

	if got := r.Width(); got != 30 {
		t.Errorf("Width() = %v, want 30", got)
	}
	if got := r.Height(); got != 5 {
		t.Errorf("Height() = %v, want 5", got)
	}
	if got := r.Area(); got != 150 {
		t.Errorf("Area() = %v, want 150", got)
	}
}

func TestRectIsZero(t *testing.T) {
	if !(Rect{}).IsZero() {
		t.Error("zero Rect reported as non-zero")
	}
	if (Rect{X1: 1}).IsZero() {
		t.Error("non-zero Rect reported as zero")
	}
}

func TestRectValid(t *testing.T) {
	tests := []struct {
		name string
		rect Rect
		want bool
	}{
		{"ordered", Rect{X0: 1, Y0: 2, X1: 3, Y1: 4}, true},
		{"zero size", Rect{X0: 5, Y0: 5, X1: 5, Y1: 5}, true},
		{"zero value", Rect{}, true},
		{"x inverted", Rect{X0: 3, Y0: 2, X1: 1, Y1: 4}, false},
		{"y inverted", Rect{X0: 1, Y0: 4, X1: 3, Y1: 2}, false},
		{"nan", Rect{X0: math.NaN(), Y1: 1}, false},
		{"inf", Rect{X0: 0, Y0: 0, X1: math.Inf(1), Y1: 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rect.Valid(); got != tt.want {
				t.Errorf("%v.Valid() = %v, want %v", tt.rect, got, tt.want)
			}
		})
	}
}

func TestRectIntersects(t *testing.T) {
	base := Rect{X0: 0, Y0: 0, X1: 10, Y1: 10}

	tests := []struct {
		name string
		o    Rect
		want bool
	}{
		{"overlapping", Rect{X0: 5, Y0: 5, X1: 15, Y1: 15}, true},
		{"contained", Rect{X0: 2, Y0: 2, X1: 3, Y1: 3}, true},
		{"identical", base, true},
		{"touching edge does not count", Rect{X0: 10, Y0: 0, X1: 20, Y1: 10}, false},
		{"apart horizontally", Rect{X0: 20, Y0: 0, X1: 30, Y1: 10}, false},
		{"apart vertically", Rect{X0: 0, Y0: 20, X1: 10, Y1: 30}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.Intersects(tt.o); got != tt.want {
				t.Errorf("Intersects(%v) = %v, want %v", tt.o, got, tt.want)
			}
			if got := tt.o.Intersects(base); got != tt.want {
				t.Errorf("reverse Intersects(%v) = %v, want %v", tt.o, got, tt.want)
			}
		})
	}
}

func TestRectVerticalOverlap(t *testing.T) {
	tall := Rect{X0: 0, Y0: 0, X1: 10, Y1: 100}

	tests := []struct {
		name string
		o    Rect
		want float64
	}{
		{"same band", Rect{X0: 20, Y0: 40, X1: 30, Y1: 60}, 20},
		{"partial", Rect{X0: 20, Y0: 90, X1: 30, Y1: 110}, 10},
		{"above", Rect{X0: 20, Y0: 100, X1: 30, Y1: 120}, 0},
		{"below", Rect{X0: 20, Y0: -20, X1: 30, Y1: 0}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tall.VerticalOverlap(tt.o); got != tt.want {
				t.Errorf("VerticalOverlap(%v) = %v, want %v", tt.o, got, tt.want)
			}
			if got := tt.o.VerticalOverlap(tall); got != tt.want {
				t.Errorf("reverse VerticalOverlap(%v) = %v, want %v", tt.o, got, tt.want)
			}
		})
	}
}

func TestRectHorizontalGap(t *testing.T) {
	left := Rect{X0: 0, Y0: 0, X1: 10, Y1: 10}

	tests := []struct {
		name string
		o    Rect
		want float64
	}{
		{"gap to the right", Rect{X0: 14, Y0: 0, X1: 20, Y1: 10}, 4},
		{"gap to the left", Rect{X0: -6, Y0: 0, X1: 0, Y1: 10}, 0},
		{"overlapping", Rect{X0: 5, Y0: 0, X1: 15, Y1: 10}, 0},
		{"touching", Rect{X0: 10, Y0: 0, X1: 20, Y1: 10}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := left.HorizontalGap(tt.o); got != tt.want {
				t.Errorf("HorizontalGap(%v) = %v, want %v", tt.o, got, tt.want)
			}
		})
	}
}
