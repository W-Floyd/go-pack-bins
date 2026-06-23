package packapi

import (
	"context"
	"strconv"
	"testing"
)

// smallItems builds n feasible items for a mode (small enough that every advertised
// algorithm — including the exact/brute solvers — handles them quickly).
func smallItems(mode string, n int) []ItemSpec {
	out := make([]ItemSpec, n)
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i)
		switch mode {
		case "1d":
			out[i] = ItemSpec{ID: id, Width: 2}
		case "2d":
			out[i] = ItemSpec{ID: id, Width: 20, Height: 20, AllowRotate: true}
		default:
			out[i] = ItemSpec{ID: id, Width: 2, Height: 2, Depth: 2, AllowRotate: true}
		}
	}
	return out
}

// TestAdvertisedAlgosSolve guards against drift between what the algorithm registry
// (AlgoCapabilities, served to the front-ends) advertises and what dispatch can
// actually solve: every advertised (mode, algorithm) must pack a small feasible
// instance with no error and nothing left unplaced. A dropdown entry that lost its
// dispatch path — or a new algorithm added to dispatch but not advertised — is
// caught here rather than by a user.
func TestAdvertisedAlgosSolve(t *testing.T) {
	bins := map[string]BinSpec{
		"1d": {Width: 10},
		"2d": {Width: 100, Height: 100},
		"3d": {Width: 10, Height: 10, Depth: 10},
	}
	caps := AlgoCapabilities()
	for mode, algos := range caps.Modes {
		for _, a := range algos {
			t.Run(mode+"/"+a.ID, func(t *testing.T) {
				req := PackRequest{Mode: mode, Algorithm: a.ID, Bin: bins[mode], Items: smallItems(mode, 5)}
				// pref is the preference-fit algorithm; it expects at least one objective.
				if a.ID == "pref" {
					req.Preferences = []PreferenceSpec{{Scalar: "", Mode: "balance", Weight: 1}}
				}
				resp := PackCtx(context.Background(), req)
				if resp.Error != "" {
					t.Fatalf("advertised but errored: %s", resp.Error)
				}
				if len(resp.Unplaced) != 0 {
					t.Fatalf("left %d of 5 feasible items unplaced", len(resp.Unplaced))
				}
			})
		}
	}
}
