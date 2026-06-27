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

package lsp

import (
	"testing"

	"go.lsp.dev/protocol"
)

func TestCommandsRejectUnadvertisedCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srv := &featureServer{}
	mgr := newFeatureManager(t, srv, root)

	_, err := mgr.Commands().Execute(t.Context(), "go", "missing", nil, false)
	requireErrorContains(t, err, `execute command "missing" is not advertised`)
	requireNoFeatureSync(t, srv)
}

func TestCommandsExecuteUsesAdvertisedCommandAndRawJSONArguments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srv := &featureServer{
		capabilities: protocol.ServerCapabilities{
			ExecuteCommandProvider: protocol.ExecuteCommandOptions{Commands: []string{"server.test"}},
		},
		executeResult: protocol.LSPAny(`{"ok":true}`),
	}
	mgr := newFeatureManager(t, srv, root)
	args := []protocol.LSPAny{protocol.LSPAny(`"arg"`), protocol.LSPAny(`1`)}

	got, err := mgr.Commands().Execute(t.Context(), "go", "server.test", args, false)
	if err != nil {
		t.Fatalf("Commands.Execute: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("execute result = %s, want {\"ok\":true}", got)
	}

	calls := srv.executeCalls()
	if len(calls) != 1 {
		t.Fatalf("execute calls = %d, want 1", len(calls))
	}
	if calls[0].Command != "server.test" {
		t.Fatalf("execute command = %q, want server.test", calls[0].Command)
	}
	if len(calls[0].Arguments) != 2 {
		t.Fatalf("execute argument count = %d, want 2", len(calls[0].Arguments))
	}
	if string(calls[0].Arguments[0]) != `"arg"` || string(calls[0].Arguments[1]) != `1` {
		t.Fatalf("execute arguments = [%s %s], want [\"arg\" 1]", calls[0].Arguments[0], calls[0].Arguments[1])
	}
}
