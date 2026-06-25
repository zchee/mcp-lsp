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
	"context"
	"fmt"
	"log/slog"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type openedDocument struct {
	version int32
	text    string
}

// syncTextDocument tracks open documents for this session and applies
// text-document synchronization updates.
//
// LSP sync contract is:
//   - send didOpen once per document URI
//   - send didChange with a monotonically increasing version for updates
//   - close open documents during session shutdown
//
// For now this implementation uses full-document replace events, which is a
// practical, protocol-correct baseline for this project.
func (s *serverSession) syncTextDocument(ctx context.Context, u uri.URI, lang protocol.LanguageKind, text string) error {
	s.docsMu.Lock()
	defer s.docsMu.Unlock()

	if s.openDocs == nil {
		s.openDocs = make(map[uri.URI]*openedDocument)
	}
	doc, ok := s.openDocs[u]
	if !ok {
		if err := s.server.DidOpen(ctx, didOpenParams(u, lang, text)); err != nil {
			return fmt.Errorf("open document: %w", err)
		}
		s.openDocs[u] = &openedDocument{version: 1, text: text}
		return nil
	}

	if doc.text == text {
		return nil
	}

	nextVersion := doc.version + 1
	if err := s.server.DidChange(ctx, didChangeParams(u, nextVersion, text)); err != nil {
		return fmt.Errorf("change document: %w", err)
	}
	doc.version = nextVersion
	doc.text = text
	return nil
}

func (s *serverSession) closeOpenDocuments(ctx context.Context) {
	s.docsMu.Lock()
	docs := make([]uri.URI, 0, len(s.openDocs))
	for u := range s.openDocs {
		docs = append(docs, u)
	}
	s.openDocs = nil
	s.docsMu.Unlock()

	for _, u := range docs {
		if err := s.server.DidClose(ctx, didCloseParams(u)); err != nil {
			s.logger.Debug("language server didClose request failed", slog.Any("error", err))
		}
	}
}

func didChangeParams(u uri.URI, version int32, text string) *protocol.DidChangeTextDocumentParams {
	return &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: u},
			Version:                version,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: text},
		},
	}
}

func didCloseParams(u uri.URI) *protocol.DidCloseTextDocumentParams {
	return &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	}
}
