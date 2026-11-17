package corpus

import (
	"strings"
	"testing"
)

func TestStripRepeatedEdgesRemovesRunningHeadersAndFooters(t *testing.T) {
	pages := []string{
		"Devops Notes\nFirst page body.\nSlide 2",
		"Devops Notes\nSecond page body.\nSlide 3",
		"Devops Notes\nThird page body.\nSlide 4",
		"Devops Notes\nFourth page body.\nSlide 5",
	}

	got := stripRepeatedEdges(strings.Join(pages, "\f"))

	for _, unwanted := range []string{"Devops Notes", "Slide 2", "Slide 5"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("reference still contains %q:\n%s", unwanted, got)
		}
	}
	for _, wanted := range []string{"First page body.", "Fourth page body."} {
		if !strings.Contains(got, wanted) {
			t.Errorf("reference lost %q:\n%s", wanted, got)
		}
	}
}

func TestStripRepeatedEdgesKeepsUniqueLines(t *testing.T) {
	text := "Title page\nOnly page here."

	if got := stripRepeatedEdges(text); got != text {
		t.Errorf("a short document was modified: %q", got)
	}
}

func TestMaskDigits(t *testing.T) {
	if got, want := maskDigits("Slide 42"), "slide #"; got != want {
		t.Errorf("maskDigits() = %q, want %q", got, want)
	}
	if got, want := maskDigits("No  digits 12 here"), "no digits # here"; got != want {
		t.Errorf("maskDigits() = %q, want %q", got, want)
	}
}
