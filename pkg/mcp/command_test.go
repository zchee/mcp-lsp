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
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

type fakeCommandExecutor struct {
	result        protocol.LSPAny
	gotLang       string
	gotCommand    string
	gotArgs       []protocol.LSPAny
	gotApplyEdits bool
	calls         int
}

func (f *fakeCommandExecutor) Execute(_ context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error) {
	f.calls++
	f.gotLang = lang
	f.gotCommand = command
	f.gotArgs = append([]protocol.LSPAny(nil), args...)
	f.gotApplyEdits = applyEdits
	return f.result, nil
}

func TestExecuteCommandHandlerRequiresCommandBeforeExecute(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{}
	handler := executeCommandHandler(executor)

	_, _, err := handler(t.Context(), nil, ExecuteCommandInput{})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("empty command error = %v, want required error", err)
	}
	if executor.calls != 0 {
		t.Fatalf("Execute calls = %d, want 0 for invalid input", executor.calls)
	}
}

func TestExecuteCommandHandlerDefaultsLanguageAndKeepsRawArguments(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{result: protocol.LSPAny(`{"ok":true}`)}
	handler := executeCommandHandler(executor)
	args := []protocol.LSPAny{protocol.LSPAny(`"arg"`), protocol.LSPAny(`1`)}

	_, out, err := handler(t.Context(), nil, ExecuteCommandInput{Command: "server.test", Arguments: args, ApplyEdits: true})
	if err != nil {
		t.Fatalf("execute command handler: %v", err)
	}
	if executor.gotLang != "go" {
		t.Fatalf("Execute language = %q, want go", executor.gotLang)
	}
	if executor.gotCommand != "server.test" || !executor.gotApplyEdits {
		t.Fatalf("Execute command/applyEdits = %q/%v, want server.test/true", executor.gotCommand, executor.gotApplyEdits)
	}
	if len(executor.gotArgs) != 2 || string(executor.gotArgs[0]) != `"arg"` || string(executor.gotArgs[1]) != `1` {
		t.Fatalf("Execute arguments = %v, want raw JSON args", executor.gotArgs)
	}
	if string(out.Result) != `{"ok":true}` {
		t.Fatalf("Execute result = %s, want {\"ok\":true}", out.Result)
	}
}
