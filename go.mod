module github.com/zchee/mcp-lsp

go 1.27

replace (
	github.com/phuslu/log => github.com/zchee/phuslu-log v1.0.114-0.20260624070747-a403bb07450a
	github.com/yosida95/uritemplate/v3 => github.com/zchee/uritemplate/v4 v4.0.0-20260624002930-bae857730b2b
)

require (
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68
	github.com/google/go-cmp v0.7.0
	github.com/modelcontextprotocol/go-sdk v1.6.1-0.20260624100256-7f4aa4a0cec8 // main
	github.com/phuslu/log v1.0.127
	go.lsp.dev/jsonrpc2 v1.0.1
	go.lsp.dev/protocol v1.0.1-0.20260627153620-175c67bd3c33
	go.lsp.dev/uri v1.0.1
	golang.org/x/tools v0.46.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)
