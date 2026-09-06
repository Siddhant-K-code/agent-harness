package contextwindow

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func TestCompactionRetainsRecentExchangeAndWaitsForGrowth(t *testing.T) {
	w := Window{Turns: [][]responses.ResponseInputItemUnionParam{{}, {}, {}}}
	if !w.ShouldCompact(1600, 2000, 75, 1) {
		t.Fatal("first eligible compaction skipped")
	}
	w.LastAttemptTokens = 1600
	if w.ShouldCompact(1650, 2000, 75, 1) {
		t.Fatal("repeated compaction without meaningful growth")
	}
	if !w.ShouldCompact(1800, 2000, 75, 1) {
		t.Fatal("growth threshold did not retrigger")
	}
	if !w.ShouldCompact(2001, 2000, 75, 1) {
		t.Fatal("hard context overflow ignored")
	}
	w.Turns = w.Turns[:1]
	if w.ShouldCompact(2001, 2000, 75, 1) {
		t.Fatal("last recent exchange was eligible for compaction")
	}
}
