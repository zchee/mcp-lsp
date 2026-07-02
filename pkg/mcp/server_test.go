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
	"context"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// TestServerExposesReadOnlyTools drives the assembled server over an in-memory
// transport with an [mcp.Client] and asserts the language-server tools are
// listed with read-only annotations and non-nil input schemas.
func TestServerExposesReadOnlyTools(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	cfg := map[string]lsp.ServerConfig{
		"go": {Command: "gopls"},
	}
	registry, err := lsp.NewRegistry(lsp.DefaultCatalog(), cfg)
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	mgr := lsp.NewManager(registry.ServerConfigs(), t.TempDir(), logger)
	t.Cleanup(func() { _ = mgr.Close(context.WithoutCancel(t.Context())) })

	srv := NewServer(mgr, logger, NewLanguageResolver(registry))

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

	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for i := range res.Tools {
		tool := res.Tools[i]
		tools[tool.Name] = tool
	}
	for _, name := range []string{
		"lsp_diagnostics",
		"lsp_definition",
		"lsp_implementation",
		"lsp_find_references",
		"lsp_hover",
		"lsp_workspace_symbol",
		"lsp_formatting",
		"lsp_range_formatting",
		"lsp_rename",
		"lsp_code_action",
		"lsp_code_lens",
	} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tool %q was not listed; got tools %+v", name, res.Tools)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q ReadOnlyHint not set; annotations = %+v", name, tool.Annotations)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q input schema is nil", name)
		}
		if tool.OutputSchema == nil {
			t.Errorf("tool %q output schema is nil", name)
		}
	}

	executeCommand := tools["lsp_execute_command"]
	if executeCommand == nil {
		t.Fatalf("tool %q was not listed; got tools %+v", "lsp_execute_command", res.Tools)
	}
	if executeCommand.Annotations == nil {
		t.Fatalf("tool %q annotations are nil", "lsp_execute_command")
	}
	if executeCommand.Annotations.ReadOnlyHint {
		t.Errorf("tool %q should not be marked read-only", "lsp_execute_command")
	}
	if executeCommand.Annotations.DestructiveHint == nil || !*executeCommand.Annotations.DestructiveHint {
		t.Errorf("tool %q DestructiveHint not set; annotations = %+v", "lsp_execute_command", executeCommand.Annotations)
	}
	if executeCommand.Annotations.OpenWorldHint == nil || *executeCommand.Annotations.OpenWorldHint {
		t.Errorf("tool %q OpenWorldHint should be false; annotations = %+v", "lsp_execute_command", executeCommand.Annotations)
	}
	if executeCommand.InputSchema == nil {
		t.Errorf("tool %q input schema is nil", "lsp_execute_command")
	}
	if executeCommand.OutputSchema == nil {
		t.Errorf("tool %q output schema is nil", "lsp_execute_command")
	}

	applyEdit := tools["lsp_apply_workspace_edit"]
	if applyEdit == nil {
		t.Fatalf("tool %q was not listed; got tools %+v", "lsp_apply_workspace_edit", res.Tools)
	}
	if applyEdit.Annotations == nil {
		t.Fatalf("tool %q annotations are nil", "lsp_apply_workspace_edit")
	}
	if applyEdit.Annotations.ReadOnlyHint {
		t.Errorf("tool %q should not be marked read-only", "lsp_apply_workspace_edit")
	}
	if applyEdit.Annotations.DestructiveHint == nil || !*applyEdit.Annotations.DestructiveHint {
		t.Errorf("tool %q DestructiveHint not set; annotations = %+v", "lsp_apply_workspace_edit", applyEdit.Annotations)
	}
	if applyEdit.Annotations.OpenWorldHint == nil || *applyEdit.Annotations.OpenWorldHint {
		t.Errorf("tool %q OpenWorldHint should be false; annotations = %+v", "lsp_apply_workspace_edit", applyEdit.Annotations)
	}
	if applyEdit.InputSchema == nil {
		t.Errorf("tool %q input schema is nil", "lsp_apply_workspace_edit")
	}
	if applyEdit.OutputSchema == nil {
		t.Errorf("tool %q output schema is nil", "lsp_apply_workspace_edit")
	}
}
