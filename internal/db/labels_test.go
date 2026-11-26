package db

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestGenerateLabel(t *testing.T) {
	re := regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		label := GenerateLabel()
		if !re.MatchString(label) {
			t.Fatalf("label %q does not match adjective-noun-NNNN", label)
		}
		parts := strings.Split(label, "-")
		if !slices.Contains(adjectives, parts[0]) || !slices.Contains(nouns, parts[1]) {
			t.Fatalf("label %q uses words outside the lists", label)
		}
		if n, _ := strconv.Atoi(parts[2]); n < 1000 || n > 9999 {
			t.Fatalf("label %q number out of range", label)
		}
		seen[label] = true
	}
	if len(seen) < 990 {
		t.Errorf("only %d unique labels in 1000, generator looks broken", len(seen))
	}
}

func TestWordListsAreDashFree(t *testing.T) {
	// Labels are split on "-", so words must not contain one.
	for _, w := range append(slices.Clone(adjectives), nouns...) {
		if strings.Contains(w, "-") || w == "" {
			t.Errorf("bad word %q", w)
		}
	}
}
