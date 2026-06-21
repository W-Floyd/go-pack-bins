package packapi

import (
	"context"
	"strings"
	"testing"
)

// makeReq builds a small 3-D request with n unit-ish items.
func makeReq(algorithm string, n int) PackRequest {
	items := make([]ItemSpec, n)
	for i := range items {
		items[i] = ItemSpec{ID: itoa(i), Width: 2, Height: 2, Depth: 2, AllowRotate: true}
	}
	return PackRequest{
		Mode:      "3d",
		Algorithm: algorithm,
		Bin:       BinSpec{Width: 10, Height: 10, Depth: 10},
		Items:     items,
	}
}

func itoa(i int) string {
	if i == 0 {
		return "i0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return "i" + string(b)
}

// A context cancelled before the solve starts must abort with a context error
// rather than producing a full result.
func TestPackCtxCancelledUpFront(t *testing.T) {
	for _, algo := range []string{"ff", "ffd", "auto", "bc", "joint"} {
		t.Run(algo, func(t *testing.T) {
			req := makeReq(algo, 40)
			if algo == "bc" {
				req.Mode = "1d" // bin-completion is 1-D only
				req.Bin = BinSpec{Width: 10}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // already cancelled
			resp := PackCtx(ctx, req)
			if !strings.Contains(resp.Error, "context canceled") {
				t.Fatalf("algo %s: expected context-cancelled error, got error=%q bins=%d", algo, resp.Error, resp.BinsUsed)
			}
		})
	}
}

// A live (non-cancelled) context must produce a normal result.
func TestPackCtxLiveSucceeds(t *testing.T) {
	resp := PackCtx(context.Background(), makeReq("auto", 20))
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.BinsUsed == 0 || len(resp.Placements) == 0 {
		t.Fatalf("expected a packed result, got bins=%d placements=%d", resp.BinsUsed, len(resp.Placements))
	}
}
