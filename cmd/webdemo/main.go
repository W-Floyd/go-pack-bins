// Command webdemo serves the packing UI and a small JSON API. All packing logic
// lives in the transport-independent packapi package, so these handlers only do
// HTTP plumbing (decode → packapi → encode). The same packapi powers the
// WebAssembly build in cmd/wasm.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/W-Floyd/go-pack-bins/packapi"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/api/pack", handlePack)
	http.HandleFunc("/api/pack/stream", handlePackStream)
	http.HandleFunc("/api/pack/nested", handleNestedPack)
	http.HandleFunc("/api/voids", handleVoids)
	http.HandleFunc("/api/algos", handleAlgos)
	http.HandleFunc("/api/presets", handlePresets)
	http.HandleFunc("/api/generate", handleGenerate)
	log.Println("Listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}

// cors sets the shared CORS headers and reports whether the request was a
// preflight OPTIONS (already handled) so the caller can return early.
func cors(w http.ResponseWriter, r *http.Request) (preflight bool) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return true
	}
	return false
}

// handleAlgos serves the algorithm-capabilities document the frontend fetches on
// load to self-configure (dropdowns, tunables, decoder selector, feature panels).
func handleAlgos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	json.NewEncoder(w).Encode(packapi.AlgoCapabilities())
}

// handlePresets serves the ready-made demo setups the frontend's "Load preset"
// menu offers (curated demos + benchmark instances from benchmarks.json).
func handlePresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	json.NewEncoder(w).Encode(packapi.Presets())
}

// handleGenerate materialises a generator preset's items on demand (mode/kind/n/seed
// query params), so large/benchmark presets ship as a tiny spec rather than a giant
// item list. Generation lives in packapi (single source, shared with benchmarks).
func handleGenerate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("n"))
	seed, _ := strconv.ParseUint(q.Get("seed"), 10, 32)
	json.NewEncoder(w).Encode(packapi.GeneratePresetItems(q.Get("mode"), q.Get("kind"), n, uint32(seed)))
}

func handlePack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req packapi.PackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(packapi.PackResponse{Error: err.Error()})
		return
	}
	// r.Context() is cancelled if the client disconnects, aborting the solve.
	json.NewEncoder(w).Encode(packapi.PackCtx(r.Context(), req))
}

func handlePackStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering if present
	if cors(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	enc := json.NewEncoder(w)
	send := func(f packapi.StreamFrame) {
		_ = enc.Encode(f) // Encode writes a trailing newline → NDJSON
		flusher.Flush()
	}
	var req packapi.PackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		send(packapi.StreamFrame{Type: "error", Error: err.Error()})
		return
	}
	packapi.StreamPack(r.Context(), req, send)
}

func handleNestedPack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req packapi.NestedPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(packapi.NestedPackResponse{Error: err.Error()})
		return
	}
	resp, err := packapi.PackNestedCtx(r.Context(), req)
	if err != nil {
		json.NewEncoder(w).Encode(packapi.NestedPackResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(resp)
}

// handleVoids analyses an already-solved packing (bin + placements posted by the
// client) for internal voids — empty space sealed off from every container wall.
func handleVoids(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cors(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req packapi.VoidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(packapi.VoidResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(packapi.Voids(req))
}
