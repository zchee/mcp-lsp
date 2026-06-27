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

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestInitializeParamsAdvertisesFeatureSuiteCapabilities(t *testing.T) {
	t.Parallel()

	params := initializeParams(uri.File("/workspace"))
	workspace := params.Capabilities.Workspace
	if workspace == nil {
		t.Fatal("Workspace capabilities are nil")
	}
	if workspace.ApplyEdit == nil || !*workspace.ApplyEdit {
		t.Fatalf("Workspace ApplyEdit = %v, want true", workspace.ApplyEdit)
	}
	if workspace.WorkspaceEdit == nil {
		t.Fatal("WorkspaceEdit capabilities are nil")
	}
	if workspace.WorkspaceEdit.DocumentChanges == nil || !*workspace.WorkspaceEdit.DocumentChanges {
		t.Fatalf("WorkspaceEdit DocumentChanges = %v, want true", workspace.WorkspaceEdit.DocumentChanges)
	}
	if workspace.WorkspaceEdit.SnippetEditSupport != nil {
		t.Fatalf("WorkspaceEdit SnippetEditSupport = %v, want not advertised", *workspace.WorkspaceEdit.SnippetEditSupport)
	}
	if len(workspace.WorkspaceEdit.ResourceOperations) != 0 {
		t.Fatalf("WorkspaceEdit ResourceOperations = %v, want not advertised for server-initiated applyEdit", workspace.WorkspaceEdit.ResourceOperations)
	}
	if workspace.Symbol == nil {
		t.Fatal("Workspace Symbol capabilities are nil")
	}
	if workspace.Symbol.SymbolKind == nil {
		t.Fatal("Workspace SymbolKind capabilities are nil")
	}
	if diff := gocmp.Diff(supportedSymbolKinds(), workspace.Symbol.SymbolKind.ValueSet); diff != "" {
		t.Fatalf("Workspace SymbolKind valueSet mismatch (-want +got):\n%s", diff)
	}
	if workspace.Symbol.ResolveSupport.Properties != nil {
		t.Fatalf("Workspace Symbol ResolveSupport = %v, want not advertised", workspace.Symbol.ResolveSupport.Properties)
	}
	if workspace.ExecuteCommand == nil {
		t.Fatal("Workspace ExecuteCommand capabilities are nil")
	}

	textDocument := params.Capabilities.TextDocument
	if textDocument == nil {
		t.Fatal("TextDocument capabilities are nil")
	}
	if textDocument.Hover == nil {
		t.Fatal("Hover capabilities are nil")
	}
	wantMarkup := []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText}
	if diff := gocmp.Diff(wantMarkup, textDocument.Hover.ContentFormat); diff != "" {
		t.Fatalf("Hover contentFormat mismatch (-want +got):\n%s", diff)
	}
	if textDocument.CodeAction == nil {
		t.Fatal("CodeAction capabilities are nil")
	}
	if diff := gocmp.Diff(supportedCodeActionKinds(), textDocument.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet); diff != "" {
		t.Fatalf("CodeAction kind valueSet mismatch (-want +got):\n%s", diff)
	}
	if textDocument.CodeAction.IsPreferredSupport == nil || !*textDocument.CodeAction.IsPreferredSupport {
		t.Fatalf("CodeAction IsPreferredSupport = %v, want true", textDocument.CodeAction.IsPreferredSupport)
	}
	if textDocument.CodeAction.DisabledSupport == nil || !*textDocument.CodeAction.DisabledSupport {
		t.Fatalf("CodeAction DisabledSupport = %v, want true", textDocument.CodeAction.DisabledSupport)
	}
	if textDocument.CodeAction.DataSupport == nil || !*textDocument.CodeAction.DataSupport {
		t.Fatalf("CodeAction DataSupport = %v, want true", textDocument.CodeAction.DataSupport)
	}
	if diff := gocmp.Diff([]string{"edit", "command"}, textDocument.CodeAction.ResolveSupport.Properties); diff != "" {
		t.Fatalf("CodeAction resolve properties mismatch (-want +got):\n%s", diff)
	}
	if textDocument.CodeAction.HonorsChangeAnnotations != nil {
		t.Fatalf("CodeAction HonorsChangeAnnotations = %v, want not advertised", *textDocument.CodeAction.HonorsChangeAnnotations)
	}
	if textDocument.CodeLens == nil {
		t.Fatal("CodeLens capabilities are nil")
	}
	if diff := gocmp.Diff([]string{"command"}, textDocument.CodeLens.ResolveSupport.Properties); diff != "" {
		t.Fatalf("CodeLens resolve properties mismatch (-want +got):\n%s", diff)
	}
	if textDocument.Formatting == nil {
		t.Fatal("Formatting capabilities are nil")
	}
	if textDocument.RangeFormatting == nil {
		t.Fatal("RangeFormatting capabilities are nil")
	}
	if textDocument.RangeFormatting.RangesSupport != nil {
		t.Fatalf("RangeFormatting RangesSupport = %v, want not advertised", *textDocument.RangeFormatting.RangesSupport)
	}
	if textDocument.Rename == nil {
		t.Fatal("Rename capabilities are nil")
	}
	if textDocument.Rename.PrepareSupport != nil {
		t.Fatalf("Rename PrepareSupport = %v, want not advertised", *textDocument.Rename.PrepareSupport)
	}
	if textDocument.Rename.HonorsChangeAnnotations != nil {
		t.Fatalf("Rename HonorsChangeAnnotations = %v, want not advertised", *textDocument.Rename.HonorsChangeAnnotations)
	}
}

func TestSnapshotCapabilitiesCapturesFeatureSuiteProviders(t *testing.T) {
	t.Parallel()

	resolve := true
	got := snapshotCapabilities(&protocol.ServerCapabilities{
		DiagnosticProvider:              &protocol.DiagnosticOptions{},
		ImplementationProvider:          protocol.Boolean(true),
		HoverProvider:                   protocol.Boolean(true),
		CodeActionProvider:              &protocol.CodeActionOptions{ResolveProvider: &resolve},
		CodeLensProvider:                &protocol.CodeLensOptions{ResolveProvider: &resolve},
		WorkspaceSymbolProvider:         &protocol.WorkspaceSymbolOptions{},
		DocumentFormattingProvider:      protocol.Boolean(true),
		DocumentRangeFormattingProvider: &protocol.DocumentRangeFormattingOptions{},
		RenameProvider:                  &protocol.RenameOptions{},
		ExecuteCommandProvider:          protocol.ExecuteCommandOptions{Commands: []string{"gopls.test"}},
	})

	if !got.pullDiagnostics || !got.implementation || !got.hover || !got.codeAction || !got.codeActionResolve ||
		!got.codeLens || !got.codeLensResolve || !got.workspaceSymbol || !got.formatting || !got.rangeFormatting ||
		!got.rename {
		t.Fatalf("snapshotCapabilities did not capture feature providers: %+v", got)
	}
	if diff := gocmp.Diff([]string{"gopls.test"}, got.executeCommands); diff != "" {
		t.Fatalf("executeCommands mismatch (-want +got):\n%s", diff)
	}
}

func TestSnapshotCapabilitiesClonesExecuteCommands(t *testing.T) {
	t.Parallel()

	capabilities := &protocol.ServerCapabilities{
		ExecuteCommandProvider: protocol.ExecuteCommandOptions{Commands: []string{"one", "two"}},
	}
	got := snapshotCapabilities(capabilities)
	capabilities.ExecuteCommandProvider.Commands[0] = "mutated"
	if diff := gocmp.Diff([]string{"one", "two"}, got.executeCommands); diff != "" {
		t.Fatalf("executeCommands were not cloned (-want +got):\n%s", diff)
	}
}
