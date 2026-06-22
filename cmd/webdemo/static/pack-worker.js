// pack-worker.js — runs the Go packing solver (WebAssembly) off the main thread.
//
// It loads wasm_exec.js + app.wasm once, then handles request messages from the
// page. For "pack" it streams: goPackStream invokes a callback per StreamFrame,
// and each frame is posted straight back, so the main thread renders the pack
// progressively as it is solved (the solve blocks THIS worker thread, not the
// UI). For "nested" it posts a single result. Every reply carries the request's
// reqId so the page can discard frames from a superseded request.
//
// Message protocol:
//   page → worker: { kind: 'pack'|'nested'|'packOnce'|'voids', reqId, body }
//   worker → page: { kind: 'ready' }                       once, after wasm loads
//                  { reqId, frame: <StreamFrame> }          per pack frame
//                  { reqId, result: <NestedPackResponse> }  once per nested
//                  { reqId, result: <VoidResponse> }        once per voids
//
// This file is only used by the static WASM bundle; the Go server's page talks
// to /api/* directly and never spawns the worker.

importScripts('wasm_exec.js');

let ready = false;
const pending = []; // requests received before the wasm finished instantiating

const go = new Go();

// Prefer instantiateStreaming, but fall back to a buffered instantiate when the
// host does not serve app.wasm as Content-Type: application/wasm (streaming
// instantiation rejects in that case).
async function loadWasm() {
  const resp = await fetch('app.wasm');
  try {
    return await WebAssembly.instantiateStreaming(resp.clone(), go.importObject);
  } catch (_) {
    const bytes = await resp.arrayBuffer();
    return await WebAssembly.instantiate(bytes, go.importObject);
  }
}

loadWasm()
  .then((res) => {
    go.run(res.instance); // sets self.goPack* globals, then parks on select{}
    ready = true;
    postMessage({ kind: 'ready' });
    for (const m of pending) handle(m);
    pending.length = 0;
  })
  .catch((err) => {
    console.error('pack-worker: wasm load failed:', err);
  });

self.onmessage = (e) => {
  if (!ready) { pending.push(e.data); return; }
  handle(e.data);
};

function handle(m) {
  try {
    if (m.kind === 'pack') {
      // goPackStream calls back synchronously per frame; forward each immediately.
      self.goPackStream(JSON.stringify(m.body), (frameJson) => {
        postMessage({ reqId: m.reqId, frame: JSON.parse(frameJson) });
      });
    } else if (m.kind === 'nested') {
      const result = JSON.parse(self.goPackNested(JSON.stringify(m.body)));
      postMessage({ reqId: m.reqId, result });
    } else if (m.kind === 'voids') {
      // Internal-void inspection of an already-solved packing (no streaming).
      const result = JSON.parse(self.goVoids(JSON.stringify(m.body)));
      postMessage({ reqId: m.reqId, result });
    } else if (m.kind === 'packOnce') {
      // Non-streaming single solve (used by container-catalog mode, which runs a
      // full solve per container type and has no honest partial state to stream).
      const result = JSON.parse(self.goPack(JSON.stringify(m.body)));
      postMessage({ reqId: m.reqId, result });
    }
  } catch (err) {
    postMessage({ reqId: m.reqId, frame: { type: 'error', error: String(err && err.message || err) } });
  }
}
