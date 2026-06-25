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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// WorkspaceEditApplyOptions controls how a workspace edit is applied.
//
// The root path is required and used as a security boundary: any non-file URI or
// path outside this root is rejected.
//
// Set AllowCreateFile, AllowRenameFile, and AllowDeleteFile to true only for
// explicit mutating operations. When false, resource operations are rejected.
type WorkspaceEditApplyOptions struct {
	// WorkspaceRoot is the trusted workspace root for path validation.
	WorkspaceRoot string

	// AllowCreateFile controls create-file resource operations.
	AllowCreateFile bool
	// AllowRenameFile controls rename-file resource operations.
	AllowRenameFile bool
	// AllowDeleteFile controls delete-file resource operations.
	AllowDeleteFile bool

	// CurrentVersions maps file URI to the expected current document version.
	// When non-empty, version checks are applied for each text document edit that
	// carries a version.
	CurrentVersions map[string]uint32
}

// ApplyWorkspaceEdit applies a workspace edit to the filesystem following
// LSP position semantics.
//
// The function returns an LSP-compatible result and only returns a transport error
// on unexpected execution failures. Invalid ranges, policy rejections, version
// mismatches, overlap violations, and out-of-root writes are reported as a
// failed [protocol.ApplyWorkspaceEditResult] when possible.
func ApplyWorkspaceEdit(edit WorkspaceEdit, opts WorkspaceEditApplyOptions) (protocol.ApplyWorkspaceEditResult, error) {
	root, err := rootPath(opts.WorkspaceRoot)
	if err != nil {
		return protocol.ApplyWorkspaceEditResult{
			Applied:       false,
			FailureReason: strptr(err.Error()),
		}, nil
	}

	changeIndex := uint32(0)
	result := protocol.ApplyWorkspaceEditResult{Applied: true}

	uriSet := make([]string, 0, len(edit.Changes))
	for rawURI := range edit.Changes {
		uriSet = append(uriSet, rawURI)
	}
	sort.Strings(uriSet)
	for _, rawURI := range uriSet {
		textEdits := edit.Changes[rawURI]
		if err := applyTextDocumentChanges(root, rawURI, textEdits, opts.CurrentVersions, nil); err != nil {
			return failEdit(changeIndex, err)
		}
		changeIndex++
	}

	for _, rawChange := range edit.DocumentChanges {
		if err := applyDocumentChange(root, rawChange, opts); err != nil {
			return failEdit(changeIndex, err)
		}
		changeIndex++
	}

	return result, nil
}

func applyDocumentChange(root string, change WorkspaceDocumentChange, opts WorkspaceEditApplyOptions) error {
	switch {
	case change.TextDocumentEdit != nil:
		targetURI := change.TextDocumentEdit.TextDocument.URI
		return applyTextDocumentChanges(
			root,
			targetURI,
			change.TextDocumentEdit.Edits,
			opts.CurrentVersions,
			change.TextDocumentEdit.TextDocument.Version,
		)
	case change.CreateFile != nil:
		if !opts.AllowCreateFile {
			return fmt.Errorf("create operation for %q is disabled by policy", change.CreateFile.URI)
		}
		return applyResourceCreate(root, change.CreateFile)
	case change.RenameFile != nil:
		if !opts.AllowRenameFile {
			return fmt.Errorf("rename operation for %q is disabled by policy", change.RenameFile.OldURI)
		}
		return applyResourceRename(root, change.RenameFile)
	case change.DeleteFile != nil:
		if !opts.AllowDeleteFile {
			return fmt.Errorf("delete operation for %q is disabled by policy", change.DeleteFile.URI)
		}
		return applyResourceDelete(root, change.DeleteFile)
	default:
		return fmt.Errorf("unsupported document change payload")
	}
}

// failEdit builds an applied-false protocol result with index and reason.
func failEdit(index uint32, err error) (protocol.ApplyWorkspaceEditResult, error) {
	if err == nil {
		return protocol.ApplyWorkspaceEditResult{Applied: true}, nil
	}
	r := err.Error()
	return protocol.ApplyWorkspaceEditResult{
		Applied:       false,
		FailureReason: &r,
		FailedChange:  &index,
	}, nil
}

func rootPath(rawRoot string) (string, error) {
	if strings.TrimSpace(rawRoot) == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	root, err := filepath.Abs(rawRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %q: %w", rawRoot, err)
	}
	return filepath.Clean(root), nil
}

func applyTextDocumentChanges(root, rawURI string, edits []WorkspaceTextEdit, versions map[string]uint32, expectedVersion *uint32) error {
	if len(edits) == 0 {
		return nil
	}

	parsed, err := uri.Parse(rawURI)
	if err != nil {
		return fmt.Errorf("invalid uri %q: %w", rawURI, err)
	}
	if !parsed.IsFile() {
		return fmt.Errorf("non-file uri %q is not supported", rawURI)
	}
	path := parsed.FsPath()
	if err := ensureUnderRoot(root, path); err != nil {
		return fmt.Errorf("uri %q: %w", rawURI, err)
	}
	if versions != nil && expectedVersion != nil {
		current, ok := versions[string(parsed)]
		if !ok {
			return fmt.Errorf("uri %q: missing current version for versioned edit", rawURI)
		}
		if current != *expectedVersion {
			return fmt.Errorf("uri %q: version mismatch, expected %d got %d", rawURI, *expectedVersion, current)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %q: %w", path, err)
	}
	text := string(content)
	patched, err := applyTextEdits(text, edits)
	if err != nil {
		return fmt.Errorf("uri %q: %w", rawURI, err)
	}
	if err := writeTextFileAtomic(path, patched, contentMode(path)); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

// applyTextEdits applies a slice of zero-based LSP text edits into text.
func applyTextEdits(text string, edits []WorkspaceTextEdit) (string, error) {
	targetRanges, err := textEditRanges(text, edits)
	if err != nil {
		return "", err
	}

	// Apply in reverse start-offset order so later offsets do not shift earlier ones.
	sort.SliceStable(targetRanges, func(i, j int) bool {
		if targetRanges[i].start == targetRanges[j].start {
			return targetRanges[i].end > targetRanges[j].end
		}
		return targetRanges[i].start > targetRanges[j].start
	})

	patched := text
	for _, tgt := range targetRanges {
		patched = patched[:tgt.start] + tgt.replaceWith + patched[tgt.end:]
	}
	return patched, nil
}

type textRange struct {
	start       int
	end         int
	replaceWith string
}

func textEditRanges(text string, edits []WorkspaceTextEdit) ([]textRange, error) {
	ranges := make([]textRange, 0, len(edits))
	for _, edit := range edits {
		start, err := lspPositionToOffset(text, edit.Range.StartLine, edit.Range.StartColumn)
		if err != nil {
			return nil, fmt.Errorf("start position (%d,%d) invalid: %w", edit.Range.StartLine, edit.Range.StartColumn, err)
		}
		end, err := lspPositionToOffset(text, edit.Range.EndLine, edit.Range.EndColumn)
		if err != nil {
			return nil, fmt.Errorf("end position (%d,%d) invalid: %w", edit.Range.EndLine, edit.Range.EndColumn, err)
		}
		if start > end {
			return nil, fmt.Errorf("start position is after end position")
		}
		ranges = append(ranges, textRange{start: start, end: end, replaceWith: edit.NewText})
	}

	if err := verifyNonOverlappingRanges(ranges); err != nil {
		return nil, err
	}
	return ranges, nil
}

func verifyNonOverlappingRanges(ranges []textRange) error {
	if len(ranges) < 2 {
		return nil
	}

	sorted := make([]textRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].start == sorted[j].start {
			return sorted[i].end < sorted[j].end
		}
		return sorted[i].start < sorted[j].start
	})

	for i := 1; i < len(sorted); i++ {
		prev, curr := sorted[i-1], sorted[i]
		if prev.end > curr.start {
			return fmt.Errorf("overlapping text edits at offsets %d:%d and %d:%d", prev.start, prev.end, curr.start, curr.end)
		}
	}
	return nil
}

func lspPositionToOffset(text string, line, column int) (int, error) {
	if line < 0 || column < 0 {
		return 0, fmt.Errorf("negative line/column")
	}

	lineStart := 0
	currentLine := 0
	for currentLine < line {
		nl := strings.IndexByte(text[lineStart:], '\n')
		if nl < 0 {
			return 0, fmt.Errorf("line %d is out of range", line)
		}
		lineStart += nl + 1
		currentLine++
	}

	nl := strings.IndexByte(text[lineStart:], '\n')
	lineEnd := len(text)
	if nl >= 0 {
		lineEnd = lineStart + nl
	}

	if lineEnd < lineStart {
		return 0, fmt.Errorf("invalid line start")
	}
	lineText := text[lineStart:lineEnd]
	if strings.HasSuffix(lineText, "\r") {
		lineText = strings.TrimSuffix(lineText, "\r")
	}

	offset, err := utf16Offset(lineText, column)
	if err != nil {
		return 0, err
	}
	return lineStart + offset, nil
}

func utf16Offset(line string, column int) (int, error) {
	if column == 0 {
		return 0, nil
	}
	if column < 0 {
		return 0, fmt.Errorf("negative UTF-16 column")
	}

	units := 0
	byteOffset := 0
	for idx := 0; idx < len(line); {
		r, size := utf8.DecodeRuneInString(line[idx:])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		if units > column {
			return 0, fmt.Errorf("column %d is inside multi-unit character", column)
		}
		byteOffset = idx + size
		idx += size
		if units == column {
			return byteOffset, nil
		}
	}
	if units != column {
		return 0, fmt.Errorf("column %d is out of range for line", column)
	}
	return byteOffset, nil
}

func ensureUnderRoot(root, target string) error {
	if target == "" {
		return fmt.Errorf("empty path")
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target path %q: %w", target, err)
	}

	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return fmt.Errorf("compute workspace-relative path: %w", err)
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside workspace", cleanTarget)
	}
	return nil
}

func contentMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode() & os.ModePerm
	}
	return 0o666
}

func writeTextFileAtomic(path, text string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".mcp-lsp-edit-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	_, err = tmp.WriteString(text)
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func applyResourceCreate(root string, op *WorkspaceCreateFile) error {
	parsed, err := uri.Parse(op.URI)
	if err != nil {
		return fmt.Errorf("invalid create uri %q: %w", op.URI, err)
	}
	if !parsed.IsFile() {
		return fmt.Errorf("create uri %q is not a file URI", op.URI)
	}
	path := parsed.FsPath()
	if err := ensureUnderRoot(root, path); err != nil {
		return fmt.Errorf("create uri %q: %w", op.URI, err)
	}
	_, statErr := os.Stat(path)
	if statErr == nil {
		if op.Overwrite != nil && *op.Overwrite {
			return writeTextFileAtomic(path, "", contentMode(path))
		}
		if op.IgnoreIfExists != nil && *op.IgnoreIfExists {
			return nil
		}
		return fmt.Errorf("create file %q already exists", path)
	}
	if !os.IsNotExist(statErr) {
		return fmt.Errorf("create file %q: %w", path, statErr)
	}
	if err := writeTextFileAtomic(path, "", 0o666); err != nil {
		return fmt.Errorf("create file %q: %w", path, err)
	}
	return nil
}

func applyResourceRename(root string, op *WorkspaceRenameFile) error {
	parsedOld, err := uri.Parse(op.OldURI)
	if err != nil {
		return fmt.Errorf("invalid rename uri %q: %w", op.OldURI, err)
	}
	if !parsedOld.IsFile() {
		return fmt.Errorf("rename source uri %q is not a file URI", op.OldURI)
	}
	oldPath := parsedOld.FsPath()
	if err := ensureUnderRoot(root, oldPath); err != nil {
		return fmt.Errorf("rename source uri %q: %w", op.OldURI, err)
	}

	if _, err := os.Stat(oldPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("rename source file %q does not exist", oldPath)
		}
		return fmt.Errorf("rename source file %q: %w", oldPath, err)
	}

	newPath, err := fileURIPathUnderRoot(root, op.NewURI, "rename destination")
	if err != nil {
		return err
	}
	skip, err := prepareRenameDestination(newPath, op)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return fmt.Errorf("rename destination dir %q: %w", filepath.Dir(newPath), err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename %q to %q: %w", oldPath, newPath, err)
	}
	return nil
}

func fileURIPathUnderRoot(root, rawURI, label string) (string, error) {
	parsed, err := uri.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("invalid %s uri %q: %w", label, rawURI, err)
	}
	if !parsed.IsFile() {
		return "", fmt.Errorf("%s uri %q is not a file URI", label, rawURI)
	}
	path := parsed.FsPath()
	if err := ensureUnderRoot(root, path); err != nil {
		return "", fmt.Errorf("%s uri %q: %w", label, rawURI, err)
	}
	return path, nil
}

func prepareRenameDestination(newPath string, op *WorkspaceRenameFile) (bool, error) {
	_, err := os.Stat(newPath)
	switch {
	case err == nil && op.Overwrite != nil && *op.Overwrite:
		if err := os.RemoveAll(newPath); err != nil {
			return false, fmt.Errorf("remove rename destination file %q: %w", newPath, err)
		}
		return false, nil
	case err == nil && op.IgnoreIfExists != nil && *op.IgnoreIfExists:
		return true, nil
	case err == nil:
		return false, fmt.Errorf("rename destination file %q already exists", newPath)
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("rename destination file %q: %w", newPath, err)
	}
}

func applyResourceDelete(root string, op *WorkspaceDeleteFile) error {
	parsed, err := uri.Parse(op.URI)
	if err != nil {
		return fmt.Errorf("invalid delete uri %q: %w", op.URI, err)
	}
	if !parsed.IsFile() {
		return fmt.Errorf("delete uri %q is not a file URI", op.URI)
	}
	path := parsed.FsPath()
	if err := ensureUnderRoot(root, path); err != nil {
		return fmt.Errorf("delete uri %q: %w", op.URI, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if op.IgnoreIfNotExists != nil && *op.IgnoreIfNotExists {
				return nil
			}
			return fmt.Errorf("delete file %q does not exist", path)
		}
		return fmt.Errorf("delete file %q: %w", path, err)
	}

	if info.IsDir() && op.Recursive != nil && *op.Recursive {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("delete file %q: %w", path, err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete file %q: %w", path, err)
	}
	return nil
}

func strptr(v string) *string { return &v }
