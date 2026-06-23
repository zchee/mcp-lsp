// Copyright 2026 The mcp-lsp Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp

import (
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// TestServerExposesDiagnosticsTool drives the assembled server over an in-memory
// transport with an SDK client and asserts the lsp_diagnostics tool is listed
// with a read-only annotation and a non-nil input schema.
func TestServerExposesDiagnosticsTool(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	mgr := lsp.NewManager(lsp.DefaultConfig(), t.TempDir(), logger)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })

	srv := NewServer(mgr, logger)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	if len(res.Tools) != 1 {
		t.Fatalf("server listed %d tools, want exactly 1", len(res.Tools))
	}

	tool := res.Tools[0]
	if tool.Name != "lsp_diagnostics" {
		t.Errorf("tool name = %q, want %q", tool.Name, "lsp_diagnostics")
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Errorf("tool ReadOnlyHint not set; annotations = %+v", tool.Annotations)
	}
	if tool.InputSchema == nil {
		t.Error("tool input schema is nil")
	}
}
