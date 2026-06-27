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
	"math"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// WorkspaceEdit is a domain DTO for LSP workspace edits. Ranges are
// zero-based to match protocol wire semantics.
type WorkspaceEdit struct {
	Changes           map[string][]WorkspaceTextEdit
	DocumentChanges   []WorkspaceDocumentChange
	ChangeAnnotations map[string]ChangeAnnotation
}

// WorkspaceTextEdit is a domain edit for one contiguous range.
type WorkspaceTextEdit struct {
	Range        NavigationRange
	NewText      string
	AnnotationID *string
}

// WorkspaceDocumentChange models one item in the documentChanges union.
type WorkspaceDocumentChange struct {
	TextDocumentEdit *WorkspaceTextDocumentEdit
	CreateFile       *WorkspaceCreateFile
	RenameFile       *WorkspaceRenameFile
	DeleteFile       *WorkspaceDeleteFile
}

// WorkspaceTextDocumentEdit represents text edits for one document.
type WorkspaceTextDocumentEdit struct {
	TextDocument WorkspaceVersionedTextDocumentIdentifier
	Edits        []WorkspaceTextEdit
}

// WorkspaceVersionedTextDocumentIdentifier identifies a document version for
// edits that require version checks.
type WorkspaceVersionedTextDocumentIdentifier struct {
	URI     string
	Version *uint32
}

// WorkspaceCreateFile represents a create-file resource operation.
type WorkspaceCreateFile struct {
	URI            string
	Overwrite      *bool
	IgnoreIfExists *bool
}

// WorkspaceRenameFile represents a rename-file resource operation.
type WorkspaceRenameFile struct {
	OldURI         string
	NewURI         string
	Overwrite      *bool
	IgnoreIfExists *bool
}

// WorkspaceDeleteFile represents a delete-file resource operation.
type WorkspaceDeleteFile struct {
	URI               string
	Recursive         *bool
	IgnoreIfNotExists *bool
}

// ChangeAnnotation captures shared metadata for annotated text/document edits.
type ChangeAnnotation struct {
	Label             string
	NeedsConfirmation *bool
	Description       *string
}

// WorkspaceEditFromProtocol converts a protocol workspace edit into the domain
// [WorkspaceEdit] representation. Union arms are preserved as typed variants.
// Unsupported text-edit element variants return an explicit error.
func WorkspaceEditFromProtocol(edit protocol.WorkspaceEdit) (WorkspaceEdit, error) {
	out := WorkspaceEdit{
		Changes:         make(map[string][]WorkspaceTextEdit, len(edit.Changes)),
		DocumentChanges: make([]WorkspaceDocumentChange, 0, len(edit.DocumentChanges)),
	}

	for rawURI, rawChanges := range edit.Changes {
		if !rawURI.IsFile() {
			return WorkspaceEdit{}, fmt.Errorf("changes URI must be file: URI %q", rawURI)
		}
		converted := make([]WorkspaceTextEdit, 0, len(rawChanges))
		for _, rawChange := range rawChanges {
			te, err := workspaceTextEditFromProtocol(rawChange)
			if err != nil {
				return WorkspaceEdit{}, err
			}
			converted = append(converted, te)
		}
		out.Changes[string(rawURI)] = converted
	}

	for i, rawChange := range edit.DocumentChanges {
		converted, err := workspaceDocumentChangeFromProtocol(rawChange)
		if err != nil {
			return WorkspaceEdit{}, fmt.Errorf("document change at %d: %w", i, err)
		}
		out.DocumentChanges = append(out.DocumentChanges, converted)
	}

	if len(edit.ChangeAnnotations) > 0 {
		out.ChangeAnnotations = make(map[string]ChangeAnnotation, len(edit.ChangeAnnotations))
		for key, ann := range edit.ChangeAnnotations {
			out.ChangeAnnotations[string(key)] = ChangeAnnotation{
				Label:             ann.Label,
				NeedsConfirmation: ann.NeedsConfirmation,
				Description:       ann.Description,
			}
		}
	}

	return out, nil
}

func workspaceTextEditFromProtocol(edit protocol.TextEdit) (WorkspaceTextEdit, error) {
	return WorkspaceTextEdit{
		Range:   navigationRangeFromProtocol(edit.Range),
		NewText: edit.NewText,
	}, nil
}

// workspaceTextEditFromProtocolUnion converts a union member from protocol text
// edit-like unions. Snippet text edits are rejected explicitly because the
// current applier path intentionally does not execute snippets.
func workspaceTextEditFromProtocolUnion(change protocol.TextDocumentEditElement) (WorkspaceTextEdit, error) {
	switch c := change.(type) {
	case *protocol.TextEdit:
		if c == nil {
			return WorkspaceTextEdit{}, fmt.Errorf("nil text edit element")
		}
		return workspaceTextEditFromProtocol(*c)

	case *protocol.AnnotatedTextEdit:
		if c == nil {
			return WorkspaceTextEdit{}, fmt.Errorf("nil annotated text edit element")
		}
		id := string(c.AnnotationID)
		return WorkspaceTextEdit{
			Range:        navigationRangeFromProtocol(c.Range),
			NewText:      c.NewText,
			AnnotationID: &id,
		}, nil

	case *protocol.SnippetTextEdit:
		return WorkspaceTextEdit{}, fmt.Errorf("unsupported snippet text edit")
	default:
		return WorkspaceTextEdit{}, fmt.Errorf("unsupported text edit element %T", change)
	}
}

func workspaceDocumentChangeFromProtocol(change protocol.DocumentChange) (WorkspaceDocumentChange, error) {
	switch c := change.(type) {
	case *protocol.TextDocumentEdit:
		return textDocumentEditFromProtocol(c)
	case *protocol.CreateFile:
		return createFileFromProtocol(c)
	case *protocol.RenameFile:
		return renameFileFromProtocol(c)
	case *protocol.DeleteFile:
		return deleteFileFromProtocol(c)
	default:
		return WorkspaceDocumentChange{}, fmt.Errorf("unsupported document change %T", change)
	}
}

func textDocumentEditFromProtocol(change *protocol.TextDocumentEdit) (WorkspaceDocumentChange, error) {
	if change == nil {
		return WorkspaceDocumentChange{}, fmt.Errorf("nil text document edit")
	}
	edits := make([]WorkspaceTextEdit, 0, len(change.Edits))
	for i, rawEdit := range change.Edits {
		edit, err := workspaceTextEditFromProtocolUnion(rawEdit)
		if err != nil {
			return WorkspaceDocumentChange{}, fmt.Errorf("textDocument edit element %d: %w", i, err)
		}
		edits = append(edits, edit)
	}

	if !change.TextDocument.URI.IsFile() {
		return WorkspaceDocumentChange{}, fmt.Errorf("text document edit URI must be file: URI %q", change.TextDocument.URI)
	}

	var version *uint32
	if change.TextDocument.Version != nil {
		if *change.TextDocument.Version < 0 {
			return WorkspaceDocumentChange{}, fmt.Errorf("negative text document version %d", *change.TextDocument.Version)
		}
		v := uint32(*change.TextDocument.Version)
		version = &v
	}
	return WorkspaceDocumentChange{
		TextDocumentEdit: &WorkspaceTextDocumentEdit{
			TextDocument: WorkspaceVersionedTextDocumentIdentifier{
				URI:     string(change.TextDocument.URI),
				Version: version,
			},
			Edits: edits,
		},
	}, nil
}

func createFileFromProtocol(change *protocol.CreateFile) (WorkspaceDocumentChange, error) {
	if change == nil {
		return WorkspaceDocumentChange{}, fmt.Errorf("nil create file operation")
	}
	if _, err := parseRequiredFileURI(change.URI.String(), "create operation"); err != nil {
		return WorkspaceDocumentChange{}, err
	}
	var overwrite, ignoreIfExists *bool
	if change.Options != nil {
		overwrite = change.Options.Overwrite
		ignoreIfExists = change.Options.IgnoreIfExists
	}
	return WorkspaceDocumentChange{
		CreateFile: &WorkspaceCreateFile{
			URI:            string(change.URI),
			Overwrite:      overwrite,
			IgnoreIfExists: ignoreIfExists,
		},
	}, nil
}

func renameFileFromProtocol(change *protocol.RenameFile) (WorkspaceDocumentChange, error) {
	if change == nil {
		return WorkspaceDocumentChange{}, fmt.Errorf("nil rename file operation")
	}
	if _, err := parseRequiredFileURI(change.OldURI.String(), "rename operation old"); err != nil {
		return WorkspaceDocumentChange{}, err
	}
	if _, err := parseRequiredFileURI(change.NewURI.String(), "rename operation new"); err != nil {
		return WorkspaceDocumentChange{}, err
	}
	var overwrite, ignoreIfExists *bool
	if change.Options != nil {
		overwrite = change.Options.Overwrite
		ignoreIfExists = change.Options.IgnoreIfExists
	}
	return WorkspaceDocumentChange{
		RenameFile: &WorkspaceRenameFile{
			OldURI:         string(change.OldURI),
			NewURI:         string(change.NewURI),
			Overwrite:      overwrite,
			IgnoreIfExists: ignoreIfExists,
		},
	}, nil
}

func deleteFileFromProtocol(change *protocol.DeleteFile) (WorkspaceDocumentChange, error) {
	if change == nil {
		return WorkspaceDocumentChange{}, fmt.Errorf("nil delete file operation")
	}
	if _, err := parseRequiredFileURI(change.URI.String(), "delete operation"); err != nil {
		return WorkspaceDocumentChange{}, err
	}
	var recursive, ignoreIfNotExists *bool
	if change.Options != nil {
		recursive = change.Options.Recursive
		ignoreIfNotExists = change.Options.IgnoreIfNotExists
	}
	return WorkspaceDocumentChange{
		DeleteFile: &WorkspaceDeleteFile{
			URI:               string(change.URI),
			Recursive:         recursive,
			IgnoreIfNotExists: ignoreIfNotExists,
		},
	}, nil
}

// WorkspaceEditToProtocol converts the domain [WorkspaceEdit] to protocol form.
// The returned value intentionally omits unsupported fields rather than guessing.
func WorkspaceEditToProtocol(edit WorkspaceEdit) (protocol.WorkspaceEdit, error) {
	out := protocol.WorkspaceEdit{}
	if len(edit.Changes) > 0 {
		out.Changes = make(map[uri.URI][]protocol.TextEdit, len(edit.Changes))
		for rawURI, rawEdits := range edit.Changes {
			u, err := parseRequiredFileURI(rawURI, "changes")
			if err != nil {
				return protocol.WorkspaceEdit{}, err
			}
			converted := make([]protocol.TextEdit, 0, len(rawEdits))
			for _, raw := range rawEdits {
				te, err := workspaceTextEditToProtocol(raw)
				if err != nil {
					return protocol.WorkspaceEdit{}, err
				}
				converted = append(converted, te)
			}
			out.Changes[u] = converted
		}
	}

	if len(edit.DocumentChanges) > 0 {
		out.DocumentChanges = make([]protocol.DocumentChange, 0, len(edit.DocumentChanges))
		for _, rawChange := range edit.DocumentChanges {
			change, err := workspaceDocumentChangeToProtocol(rawChange)
			if err != nil {
				return protocol.WorkspaceEdit{}, err
			}
			out.DocumentChanges = append(out.DocumentChanges, change)
		}
	}

	if len(edit.ChangeAnnotations) > 0 {
		out.ChangeAnnotations = make(map[protocol.ChangeAnnotationIdentifier]protocol.ChangeAnnotation, len(edit.ChangeAnnotations))
		for key, ann := range edit.ChangeAnnotations {
			out.ChangeAnnotations[protocol.ChangeAnnotationIdentifier(key)] = protocol.ChangeAnnotation{
				Label:             ann.Label,
				NeedsConfirmation: ann.NeedsConfirmation,
				Description:       ann.Description,
			}
		}
	}

	return out, nil
}

func workspaceTextEditToProtocol(edit WorkspaceTextEdit) (protocol.TextEdit, error) {
	rng, err := protocolRangeFromNavigationRange(edit.Range)
	if err != nil {
		return protocol.TextEdit{}, err
	}
	return protocol.TextEdit{Range: rng, NewText: edit.NewText}, nil
}

func protocolRangeFromNavigationRange(rng NavigationRange) (protocol.Range, error) {
	start, err := protocolPositionFromInts(rng.StartLine, rng.StartColumn)
	if err != nil {
		return protocol.Range{}, fmt.Errorf("start position: %w", err)
	}
	end, err := protocolPositionFromInts(rng.EndLine, rng.EndColumn)
	if err != nil {
		return protocol.Range{}, fmt.Errorf("end position: %w", err)
	}
	return protocol.Range{Start: start, End: end}, nil
}

func protocolPositionFromInts(line, character int) (protocol.Position, error) {
	if line < 0 || character < 0 {
		return protocol.Position{}, fmt.Errorf("line and character must be non-negative")
	}
	if int64(line) > math.MaxUint32 || int64(character) > math.MaxUint32 {
		return protocol.Position{}, fmt.Errorf("line and character must fit uint32")
	}
	return protocol.Position{
		Line:      uint32(line),
		Character: uint32(character),
	}, nil
}

func workspaceDocumentChangeToProtocol(change WorkspaceDocumentChange) (protocol.DocumentChange, error) {
	switch {
	case change.TextDocumentEdit != nil:
		return textDocumentChangeToProtocol(change.TextDocumentEdit)
	case change.CreateFile != nil:
		return createFileChangeToProtocol(change.CreateFile)
	case change.RenameFile != nil:
		return renameFileChangeToProtocol(change.RenameFile)
	case change.DeleteFile != nil:
		return deleteFileChangeToProtocol(change.DeleteFile)
	default:
		return nil, fmt.Errorf("unsupported document change payload")
	}
}

func textDocumentChangeToProtocol(change *WorkspaceTextDocumentEdit) (protocol.DocumentChange, error) {
	if change.TextDocument.URI == "" {
		return nil, fmt.Errorf("text document edit is missing URI")
	}
	u, err := parseRequiredFileURI(change.TextDocument.URI, "text document edit")
	if err != nil {
		return nil, err
	}

	edits := make([]protocol.TextDocumentEditElement, 0, len(change.Edits))
	for _, edit := range change.Edits {
		converted, err := textDocumentEditElementToProtocol(edit)
		if err != nil {
			return nil, err
		}
		edits = append(edits, converted)
	}

	textDoc := protocol.OptionalVersionedTextDocumentIdentifier{URI: u}
	if change.TextDocument.Version != nil {
		if *change.TextDocument.Version > math.MaxInt32 {
			return nil, fmt.Errorf("text document version exceeds int32 range")
		}
		v := int32(*change.TextDocument.Version)
		textDoc.Version = &v
	}
	return &protocol.TextDocumentEdit{TextDocument: textDoc, Edits: edits}, nil
}

func textDocumentEditElementToProtocol(edit WorkspaceTextEdit) (protocol.TextDocumentEditElement, error) {
	pe, err := workspaceTextEditToProtocol(edit)
	if err != nil {
		return nil, err
	}
	if edit.AnnotationID == nil {
		return &pe, nil
	}
	return &protocol.AnnotatedTextEdit{
		Range:        pe.Range,
		NewText:      pe.NewText,
		AnnotationID: protocol.ChangeAnnotationIdentifier(*edit.AnnotationID),
	}, nil
}

func createFileChangeToProtocol(change *WorkspaceCreateFile) (protocol.DocumentChange, error) {
	if change.URI == "" {
		return nil, fmt.Errorf("create operation is missing URI")
	}
	u, err := parseRequiredFileURI(change.URI, "create operation")
	if err != nil {
		return nil, err
	}
	return &protocol.CreateFile{
		ResourceOperation: protocol.ResourceOperation{Kind: "create"},
		Kind:              "create",
		URI:               u,
		Options:           createFileOptions(change),
	}, nil
}

func createFileOptions(change *WorkspaceCreateFile) *protocol.CreateFileOptions {
	if change.Overwrite == nil && change.IgnoreIfExists == nil {
		return nil
	}
	return &protocol.CreateFileOptions{
		Overwrite:      change.Overwrite,
		IgnoreIfExists: change.IgnoreIfExists,
	}
}

func renameFileChangeToProtocol(change *WorkspaceRenameFile) (protocol.DocumentChange, error) {
	if change.OldURI == "" || change.NewURI == "" {
		return nil, fmt.Errorf("rename operation requires old and new URI")
	}
	oldURI, err := parseRequiredFileURI(change.OldURI, "rename operation old")
	if err != nil {
		return nil, err
	}
	newURI, err := parseRequiredFileURI(change.NewURI, "rename operation new")
	if err != nil {
		return nil, err
	}
	return &protocol.RenameFile{
		ResourceOperation: protocol.ResourceOperation{Kind: "rename"},
		Kind:              "rename",
		OldURI:            oldURI,
		NewURI:            newURI,
		Options:           renameFileOptions(change),
	}, nil
}

func renameFileOptions(change *WorkspaceRenameFile) *protocol.RenameFileOptions {
	if change.Overwrite == nil && change.IgnoreIfExists == nil {
		return nil
	}
	return &protocol.RenameFileOptions{
		Overwrite:      change.Overwrite,
		IgnoreIfExists: change.IgnoreIfExists,
	}
}

func deleteFileChangeToProtocol(change *WorkspaceDeleteFile) (protocol.DocumentChange, error) {
	if change.URI == "" {
		return nil, fmt.Errorf("delete operation is missing URI")
	}
	u, err := parseRequiredFileURI(change.URI, "delete operation")
	if err != nil {
		return nil, err
	}
	return &protocol.DeleteFile{
		ResourceOperation: protocol.ResourceOperation{Kind: "delete"},
		Kind:              "delete",
		URI:               u,
		Options:           deleteFileOptions(change),
	}, nil
}

func deleteFileOptions(change *WorkspaceDeleteFile) *protocol.DeleteFileOptions {
	if change.Recursive == nil && change.IgnoreIfNotExists == nil {
		return nil
	}
	return &protocol.DeleteFileOptions{
		Recursive:         change.Recursive,
		IgnoreIfNotExists: change.IgnoreIfNotExists,
	}
}

func parseRequiredFileURI(rawURI, context string) (uri.URI, error) {
	u, err := uri.ParseStrict(rawURI)
	if err != nil {
		return "", fmt.Errorf("invalid %s URI %q: %w", context, rawURI, err)
	}
	if !u.IsFile() {
		return "", fmt.Errorf("%s URI must be file: URI %q", context, rawURI)
	}
	return u, nil
}
