// Command webdemo serves the packing UI and a small JSON API. All packing logic
// lives in the transport-independent packapi package, so these handlers only do
// HTTP plumbing (decode → packapi → encode). The same packapi powers the
// WebAssembly build in cmd/wasm.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/W-Floyd/go-pack-bins/packapi"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/api/pack", handlePack)
	http.HandleFunc("/api/pack/stream", handlePackStream)
	http.HandleFunc("/api/pack/nested", handleNestedPack)
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
	json.NewEncoder(w).Encode(packapi.Pack(req))
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
	packapi.StreamPack(req, send)
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
	resp, err := packapi.PackNested(req)
	if err != nil {
		json.NewEncoder(w).Encode(packapi.NestedPackResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(resp)
}
