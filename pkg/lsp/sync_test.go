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
	"slices"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestSyncTextDocumentOpensAndChangesTextWithVersions(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	sess.openDocs = nil
	u := uri.File("/workspace/main.go")

	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "package main\n"); err != nil {
		t.Fatalf("syncTextDocument: %v", err)
	}

	opened := waitForOpenedDocs(t, fake, 1)
	if len(opened) != 1 {
		t.Fatalf("expected first sync to send exactly one didOpen, got %d", len(opened))
	}
	if opened[0].TextDocument.Text != "package main\n" {
		t.Fatalf("didOpen text = %q, want %q", opened[0].TextDocument.Text, "package main\n")
	}
	if got := len(fake.changedDocs()); got != 0 {
		t.Fatalf("didChange was sent before first edit; got %d", got)
	}

	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "package main\n"); err != nil {
		t.Fatalf("syncTextDocument same text: %v", err)
	}
	if got := len(fake.changedDocs()); got != 0 {
		t.Fatalf("expected no didChange for unchanged text, got %d", got)
	}

	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "package main\n\n"); err != nil {
		t.Fatalf("syncTextDocument updated text: %v", err)
	}

	changes := waitForChangedDocs(t, fake, 1)
	if len(changes) != 1 {
		t.Fatalf("expected one didChange after text update, got %d", len(changes))
	}
	if changes[0].TextDocument.Version != 2 {
		t.Fatalf("didChange version = %d, want 2", changes[0].TextDocument.Version)
	}
	if len(changes[0].ContentChanges) != 1 {
		t.Fatalf("expected one content change event, got %d", len(changes[0].ContentChanges))
	}
	whole, ok := changes[0].ContentChanges[0].(*protocol.TextDocumentContentChangeWholeDocument)
	if !ok {
		t.Fatalf("expected whole-document content change, got %T", changes[0].ContentChanges[0])
	}
	if whole.Text != "package main\n\n" {
		t.Fatalf("didChange text = %q, want %q", whole.Text, "package main\n\n")
	}
}

func TestSyncTextDocumentChangeVersionIncrementsAcrossUpdates(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	sess.openDocs = nil
	u := uri.File("/workspace/main.go")

	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "first\n"); err != nil {
		t.Fatalf("syncTextDocument: %v", err)
	}
	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "second\n"); err != nil {
		t.Fatalf("syncTextDocument second: %v", err)
	}
	if err := sess.syncTextDocument(t.Context(), u, protocol.LanguageKindGo, "third\n"); err != nil {
		t.Fatalf("syncTextDocument third: %v", err)
	}

	changes := waitForChangedDocs(t, fake, 2)
	if len(changes) != 2 {
		t.Fatalf("expected two didChange events, got %d", len(changes))
	}
	versions := []int32{changes[0].TextDocument.Version, changes[1].TextDocument.Version}
	slices.Sort(versions)
	if want := []int32{2, 3}; !slices.Equal(versions, want) {
		t.Fatalf("didChange versions = %v, want %v", versions, want)
	}
}

func waitForChangedDocs(t *testing.T, fake *fakeServer, want int) []protocol.DidChangeTextDocumentParams {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		changes := fake.changedDocs()
		if len(changes) >= want {
			return changes
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d didChange events (got %d)", want, len(fake.changedDocs()))
	return nil
}

func waitForOpenedDocs(t *testing.T, fake *fakeServer, want int) []protocol.DidOpenTextDocumentParams {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		opened := fake.openedDocs()
		if len(opened) >= want {
			return opened
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d didOpen events (got %d)", want, len(fake.openedDocs()))
	return nil
}
