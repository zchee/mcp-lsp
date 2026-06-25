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
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestClientApplyEditDefaultPolicyRejects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := newClient(newStore(), slog.New(slog.DiscardHandler))

	got, err := client.ApplyEdit(t.Context(), &protocol.ApplyWorkspaceEditParams{
		Edit: protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{
				uri.File(filepath.Join(root, "main.go")): {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: 0, Character: 0},
							End:   protocol.Position{Line: 0, Character: 1},
						},
						NewText: "x",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}
	if got.Applied {
		t.Fatalf("applied = true, want false")
	}
	if got.FailureReason == nil {
		t.Fatal("expected failure reason, got nil")
	}
	if !strings.Contains(*got.FailureReason, "disabled") {
		t.Fatalf("failure reason = %q, want contains disabled", *got.FailureReason)
	}
	if got.FailedChange != nil {
		t.Errorf("failed change = %v, want nil", *got.FailedChange)
	}
}

func TestClientApplyEditPolicyAppliesWhenEnabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client := newClient(newStore(), slog.New(slog.DiscardHandler))
	params := &protocol.ApplyWorkspaceEditParams{
		Edit: protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{
				uri.File(file): {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: 0, Character: 0},
							End:   protocol.Position{Line: 0, Character: 0},
						},
						NewText: "package ",
					},
				},
			},
		},
	}

	var got *protocol.ApplyWorkspaceEditResult
	var applyErr error
	err := client.withApplyEditPolicy(WorkspaceEditApplyOptions{
		WorkspaceRoot:   root,
		CurrentVersions: nil,
	}, func() error {
		got, applyErr = client.ApplyEdit(t.Context(), params)
		return applyErr
	})
	if err != nil {
		t.Fatalf("withApplyEditPolicy: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil")
	}
	if !got.Applied {
		t.Fatalf("applied = false, want true; reason=%v", got.FailureReason)
	}

	gotContent, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if diff := cmp.Diff("package hello\n", string(gotContent)); diff != "" {
		t.Errorf("file content mismatch (-want +got):\n%s", diff)
	}
}
