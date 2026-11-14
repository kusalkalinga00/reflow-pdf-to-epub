package idm

import "math"

// Rect is an axis-aligned rectangle in PDF user space, where the origin is the
// bottom-left corner of the page and y increases upwards. X0 and Y0 are the
// minimum corner; X1 and Y1 are the maximum.
type Rect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// Width returns the horizontal extent.
func (r Rect) Width() float64 { return r.X1 - r.X0 }

// Height returns the vertical extent.
func (r Rect) Height() float64 { return r.Y1 - r.Y0 }

// Area returns the enclosed area. It is zero or negative for an invalid rect.
func (r Rect) Area() float64 { return r.Width() * r.Height() }

// IsZero reports whether the rect is the zero value.
func (r Rect) IsZero() bool { return r == Rect{} }

// Valid reports whether the rect is well formed: finite coordinates with the
// minimum corner first. It does not check that the rect lies on its page,
// because real PDFs draw slightly outside the media box.
func (r Rect) Valid() bool {
	for _, v := range [...]float64{r.X0, r.Y0, r.X1, r.Y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return r.X0 <= r.X1 && r.Y0 <= r.Y1
}

// Intersects reports whether the two rects share interior area. Rects that only
// touch along an edge do not intersect, which is what layout clustering wants
// when deciding whether two blocks are adjacent.
func (r Rect) Intersects(o Rect) bool {
	return r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}

// VerticalOverlap returns the height of the shared vertical band, or 0 when
// there is none. Line clustering uses it to decide whether two glyphs sit on
// the same baseline band.
func (r Rect) VerticalOverlap(o Rect) float64 {
	top := math.Min(r.Y1, o.Y1)
	bottom := math.Max(r.Y0, o.Y0)
	if top <= bottom {
		return 0
	}
	return top - bottom
}

// HorizontalGap returns the horizontal distance between the rects, or 0 when
// they overlap horizontally. Column detection uses it to find the gutter.
func (r Rect) HorizontalGap(o Rect) float64 {
	switch {
	case r.X1 < o.X0:
		return o.X0 - r.X1
	case o.X1 < r.X0:
		return r.X0 - o.X1
	default:
		return 0
	}
}
