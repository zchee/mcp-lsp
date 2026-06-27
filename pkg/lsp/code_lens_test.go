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
	"path/filepath"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCodeLensesRejectUnsupportedCapabilityBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &fakeManagerServer{}
	mgr := newFeatureManager(t, srv, root)

	_, err := mgr.CodeLenses().Lookup(t.Context(), "go", path, "package main\n", false)
	requireErrorContains(t, err, "code lens request is not supported")
	requireNoFeatureSync(t, srv)
}

func TestCodeLensesResolveWhenSupported(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	lensRange := protocol.Range{Start: protocol.Position{Line: 4, Character: 0}, End: protocol.Position{Line: 4, Character: 0}}
	srv := &fakeManagerServer{
		capabilities: protocol.ServerCapabilities{
			CodeLensProvider: &protocol.CodeLensOptions{ResolveProvider: new(true)},
		},
		codeLenses: []protocol.CodeLens{
			{Range: lensRange, Data: protocol.LSPAny(`{"lens":1}`)},
		},
		codeLensResolveResult: &protocol.CodeLens{
			Range:   lensRange,
			Command: protocol.Command{Title: "Test", Command: "go.test"},
		},
	}
	mgr := newFeatureManager(t, srv, root)

	gotLenses, err := mgr.CodeLenses().Lookup(t.Context(), "go", path, "package main\n", true)
	if err != nil {
		t.Fatalf("CodeLenses.Lookup: %v", err)
	}
	wantLenses := []CodeLens{
		{
			Range:   NavigationRange{StartLine: 4, StartColumn: 0, EndLine: 4, EndColumn: 0},
			Command: &Command{Title: "Test", Command: "go.test"},
		},
	}
	if diff := gocmp.Diff(wantLenses, gotLenses); diff != "" {
		t.Fatalf("code lenses mismatch (-want +got):\n%s", diff)
	}
	lensCalls := srv.codeLensCalls()
	if len(lensCalls) != 1 {
		t.Fatalf("code lens calls = %d, want 1", len(lensCalls))
	}
	if lensCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("code lens URI = %q, want %q", lensCalls[0].TextDocument.URI, fileURI)
	}
	if got := len(srv.codeLensResolveCalls()); got != 1 {
		t.Fatalf("codeLens/resolve calls = %d, want 1", got)
	}
}
