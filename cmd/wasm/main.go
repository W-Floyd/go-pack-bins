//go:build js && wasm

// Command wasm is the WebAssembly bridge for the packing UI. Built with
// GOOS=js GOARCH=wasm, it exposes the same packapi solve entry points to
// JavaScript as global functions, so the web frontend can run fully in the
// browser with no server:
//
//	goPack(jsonRequest)            -> jsonResponse  (mirrors POST /api/pack)
//	goPackNested(jsonRequest)      -> jsonResponse  (mirrors POST /api/pack/nested)
//	goPackStream(jsonRequest, cb)  -> (frames via cb) (mirrors POST /api/pack/stream)
//
// goPack/goPackNested take a JSON string and return a JSON string, matching the
// wire shapes the server uses. goPackStream takes the request plus a JS callback
// and invokes it once per StreamFrame (as a JSON string), in solve order — the
// same NDJSON protocol the server flushes. Because the solve runs synchronously,
// goPackStream must be called from a Web Worker (pack-worker.js): there the
// per-frame callbacks postMessage to the main thread, which renders
// progressively. Calling it on the main thread would block rendering until done.
package main

import (
	"context"
	"encoding/json"
	"syscall/js"

	"github.com/W-Floyd/go-pack-bins/packapi"
)

func main() {
	js.Global().Set("goPack", js.FuncOf(packFn))
	js.Global().Set("goPackNested", js.FuncOf(packNestedFn))
	js.Global().Set("goPackStream", js.FuncOf(packStreamFn))
	js.Global().Set("goVoids", js.FuncOf(voidsFn))
	// goPackReady lets the frontend feature-detect the bridge without poking at
	// each function. Set last so it is only true once everything is registered.
	js.Global().Set("goPackReady", true)
	select {} // keep the Go runtime alive so the exported funcs stay callable
}

// packFn implements goPack(jsonRequest) -> jsonResponse.
func packFn(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return errResponse("goPack: missing request argument")
	}
	var req packapi.PackRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return errResponse("goPack: " + err.Error())
	}
	return marshal(packapi.Pack(req))
}

// voidsFn implements goVoids(jsonRequest) -> jsonResponse (mirrors POST /api/voids).
func voidsFn(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return errResponse("goVoids: missing request argument")
	}
	var req packapi.VoidRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return errResponse("goVoids: " + err.Error())
	}
	return marshal(packapi.Voids(req))
}

// packNestedFn implements goPackNested(jsonRequest) -> jsonResponse.
func packNestedFn(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return errResponse("goPackNested: missing request argument")
	}
	var req packapi.NestedPackRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return errResponse("goPackNested: " + err.Error())
	}
	resp, err := packapi.PackNested(req)
	if err != nil {
		resp.Error = err.Error()
	}
	return marshal(resp)
}

// packStreamFn implements goPackStream(jsonRequest, callback). It invokes the JS
// callback once per StreamFrame (as a JSON string), synchronously, in solve
// order, then returns. Intended to run in a Web Worker so the callbacks can
// postMessage frames to the main thread for progressive rendering.
func packStreamFn(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return errResponse("goPackStream: need (request, callback)")
	}
	cb := args[1]
	emit := func(f packapi.StreamFrame) {
		b, err := json.Marshal(f)
		if err != nil {
			b = []byte(errResponse(err.Error()))
		}
		cb.Invoke(string(b))
	}
	var req packapi.PackRequest
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		emit(packapi.StreamFrame{Type: "error", Error: "goPackStream: " + err.Error()})
		return nil
	}
	// No per-call cancellation from JS yet; the worker tears down on supersede.
	packapi.StreamPack(context.Background(), req, emit)
	return nil
}

// marshal serialises v to a JSON string for return to JavaScript. Encoding the
// fixed packapi response types cannot fail; if it somehow does, return an error
// envelope the frontend already knows how to surface.
func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return errResponse(err.Error())
	}
	return string(b)
}

// errResponse builds a minimal {"error": msg} JSON string — the same shape the
// server returns on failure, so the frontend's existing error path handles it.
func errResponse(msg string) string {
	b, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: msg})
	return string(b)
}
