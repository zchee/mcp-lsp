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
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/uri"
)

func TestApplyWorkspaceEditTextEditsApplyInReverseOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("abcde"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	edit := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			uri.File(path).String(): {
				{
					Range:   NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 0},
					NewText: "X",
				},
				{
					Range:   NavigationRange{StartLine: 0, StartColumn: 1, EndLine: 0, EndColumn: 2},
					NewText: "Y",
				},
			},
		},
	}

	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if !got.Applied {
		t.Fatalf("applied=false, failure=%v", got.FailureReason)
	}
	gotContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if diff := gocmp.Diff("XaYcde", string(gotContent)); diff != "" {
		t.Errorf("file content mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyWorkspaceEditRejectsOverlappingTextEdits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	edit := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			uri.File(path).String(): {
				{
					Range:   NavigationRange{0, 0, 0, 2},
					NewText: "x",
				},
				{
					Range:   NavigationRange{0, 1, 0, 3},
					NewText: "y",
				},
			},
		},
	}

	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if got.Applied {
		t.Fatal("applied=true, want false")
	}
	if got.FailureReason == nil || !strings.Contains(*got.FailureReason, "overlapping") {
		t.Fatalf("failure reason = %v, want overlap error", got.FailureReason)
	}
}

func TestApplyWorkspaceEditRejectsUtf16MismatchedOffsets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("a😀b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	edit := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			uri.File(path).String(): {
				{
					Range:   NavigationRange{0, 1, 0, 3},
					NewText: "X",
				},
			},
		},
	}

	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if !got.Applied {
		t.Fatalf("applied=false, failure=%v", got.FailureReason)
	}
	gotContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if diff := gocmp.Diff("aXb", string(gotContent)); diff != "" {
		t.Errorf("file content mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyWorkspaceEditRejectsNonFileURIs(t *testing.T) {
	t.Parallel()

	edit := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			"http://example.com/main.go": {
				{
					Range:   NavigationRange{0, 0, 0, 0},
					NewText: "x",
				},
			},
		},
	}

	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: "/tmp"})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if got.Applied {
		t.Fatal("applied=true, want false")
	}
	if got.FailureReason == nil || !strings.Contains(*got.FailureReason, "file") {
		t.Fatalf("failure reason = %v, want non-file rejection", got.FailureReason)
	}
}

func TestApplyWorkspaceEditRejectsOutOfRootEdits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	other := t.TempDir()
	path := filepath.Join(other, "main.go")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	edit := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			uri.File(path).String(): {
				{
					Range:   NavigationRange{0, 0, 0, 0},
					NewText: "x",
				},
			},
		},
	}

	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if got.Applied {
		t.Fatal("applied=true, want false")
	}
	if got.FailureReason == nil || !strings.Contains(*got.FailureReason, "outside workspace") {
		t.Fatalf("failure reason = %v, want outside-workspace rejection", got.FailureReason)
	}
}

func TestApplyWorkspaceEditRenameRejectsExistingDestinationByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	edit := WorkspaceEdit{DocumentChanges: []WorkspaceDocumentChange{
		{RenameFile: &WorkspaceRenameFile{OldURI: uri.File(source).String(), NewURI: uri.File(destination).String()}},
	}}
	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root, AllowRenameFile: true})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if got.Applied {
		t.Fatal("rename applied despite existing destination and nil overwrite")
	}
	if got.FailureReason == nil || !strings.Contains(*got.FailureReason, "already exists") {
		t.Fatalf("failure reason = %v, want already exists", got.FailureReason)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "source" {
		t.Fatalf("source content after rejected rename = %q, err=%v", string(content), err)
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != "destination" {
		t.Fatalf("destination content after rejected rename = %q, err=%v", string(content), err)
	}
}

func TestApplyWorkspaceEditDeleteRejectsNonRecursiveNonEmptyDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	child := filepath.Join(dir, "child.txt")
	if err := os.WriteFile(child, []byte("child"), 0o600); err != nil {
		t.Fatalf("write child: %v", err)
	}

	edit := WorkspaceEdit{DocumentChanges: []WorkspaceDocumentChange{
		{DeleteFile: &WorkspaceDeleteFile{URI: uri.File(dir).String()}},
	}}
	got, err := ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root, AllowDeleteFile: true})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if got.Applied {
		t.Fatal("delete applied recursively despite nil recursive option")
	}
	if got.FailureReason == nil {
		t.Fatal("failure reason = nil, want non-empty directory error")
	}
	if _, err := os.Stat(child); err != nil {
		t.Fatalf("child should remain after rejected delete: %v", err)
	}

	recursive := true
	edit.DocumentChanges[0].DeleteFile.Recursive = &recursive
	got, err = ApplyWorkspaceEdit(edit, WorkspaceEditApplyOptions{WorkspaceRoot: root, AllowDeleteFile: true})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit recursive: %v", err)
	}
	if !got.Applied {
		t.Fatalf("recursive delete failed: %v", got.FailureReason)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should be deleted recursively, stat err=%v", err)
	}
}

func TestApplyWorkspaceEditRequiresResourceOperationsPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	created := filepath.Join(root, "created.txt")
	renamed := filepath.Join(root, "renamed.txt")
	removed := filepath.Join(root, "removed.txt")
	if err := os.WriteFile(removed, []byte("remove-me"), 0o600); err != nil {
		t.Fatalf("write removed file: %v", err)
	}

	editCreate := WorkspaceEdit{
		DocumentChanges: []WorkspaceDocumentChange{
			{
				CreateFile: &WorkspaceCreateFile{
					URI: string(uri.File(created)),
				},
			},
		},
	}

	blocked, err := ApplyWorkspaceEdit(editCreate, WorkspaceEditApplyOptions{
		WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if blocked.Applied {
		t.Fatal("create applied despite disabled policy")
	}
	if got := blocked.FailureReason; got == nil || !strings.Contains(*got, "create operation") {
		t.Fatalf("failure reason = %v, want create policy rejection", got)
	}

	allowedCreate, err := ApplyWorkspaceEdit(editCreate, WorkspaceEditApplyOptions{
		WorkspaceRoot:   root,
		AllowCreateFile: true,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if !allowedCreate.Applied {
		t.Fatalf("create apply failed: %v", allowedCreate.FailureReason)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("created file should exist: %v", err)
	}

	editRename := WorkspaceEdit{
		DocumentChanges: []WorkspaceDocumentChange{
			{
				RenameFile: &WorkspaceRenameFile{
					OldURI: uri.File(removed).String(),
					NewURI: uri.File(renamed).String(),
				},
			},
		},
	}
	blockedRename, err := ApplyWorkspaceEdit(editRename, WorkspaceEditApplyOptions{
		WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if blockedRename.Applied {
		t.Fatalf("rename applied despite disabled policy")
	}
	if blockedRename.FailureReason == nil || !strings.Contains(*blockedRename.FailureReason, "rename operation") {
		t.Fatalf("failure reason = %v, want rename policy rejection", blockedRename.FailureReason)
	}

	allowedRename, err := ApplyWorkspaceEdit(editRename, WorkspaceEditApplyOptions{
		WorkspaceRoot:   root,
		AllowRenameFile: true,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if !allowedRename.Applied {
		t.Fatalf("rename should be applied: %v", allowedRename.FailureReason)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file should exist: %v", err)
	}

	editDelete := WorkspaceEdit{
		DocumentChanges: []WorkspaceDocumentChange{
			{
				DeleteFile: &WorkspaceDeleteFile{
					URI: uri.File(renamed).String(),
				},
			},
		},
	}
	blockedDelete, err := ApplyWorkspaceEdit(editDelete, WorkspaceEditApplyOptions{
		WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if blockedDelete.Applied {
		t.Fatalf("delete applied despite disabled policy")
	}
	if blockedDelete.FailureReason == nil || !strings.Contains(*blockedDelete.FailureReason, "delete operation") {
		t.Fatalf("failure reason = %v, want delete policy rejection", blockedDelete.FailureReason)
	}

	allowedDelete, err := ApplyWorkspaceEdit(editDelete, WorkspaceEditApplyOptions{
		WorkspaceRoot:   root,
		AllowDeleteFile: true,
	})
	if err != nil {
		t.Fatalf("ApplyWorkspaceEdit: %v", err)
	}
	if !allowedDelete.Applied {
		t.Fatalf("delete should be applied: %v", allowedDelete.FailureReason)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatalf("renamed file should be deleted, stat err=%v", err)
	}
}
