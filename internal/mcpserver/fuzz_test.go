package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzServeLine(f *testing.F) {
	f.Add(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	f.Add(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"网络"}}}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"jsonrpc":"2.0"`)
	f.Add("\xEF\xBB\xBF{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}")
	f.Add(`{"jsonrpc":"1.0","id":7,"method":"ping"}`)
	// One server for all iterations: read tools are stateless, and
	// rebuilding an index per iteration would starve the fuzzer.
	srv := newTestServer(f)
	f.Fuzz(func(t *testing.T, line string) {
		var out bytes.Buffer
		_ = srv.Serve(strings.NewReader(line+"\n"), &out)
		for _, l := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			if len(l) == 0 {
				continue
			}
			if !json.Valid(l) {
				t.Fatalf("non-JSON response line: %q", l)
			}
		}
	})
}
