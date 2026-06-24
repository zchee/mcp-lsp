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
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Definition is the goto-definition feature bound to a [Manager]. Its timeout
// bounds the whole request when the caller's context has no deadline.
type Definition struct {
	mgr     *Manager
	timeout time.Duration
}

// DefinitionRange is a flattened LSP range. Positions are zero-based here,
// matching the LSP wire format; the MCP layer converts them to one-based for the
// agent.
type DefinitionRange struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

// DefinitionLocation is a flattened goto-definition target. Positions are
// zero-based here, matching the LSP wire format; the MCP layer converts them to
// one-based for the agent.
type DefinitionLocation struct {
	TargetURI            string
	TargetRange          DefinitionRange
	TargetSelectionRange DefinitionRange
	OriginSelectionRange *DefinitionRange
}

// Definition returns the goto-definition feature for this manager.
func (m *Manager) Definition() *Definition {
	return &Definition{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the definition targets for pos. The input position and result
// positions are zero-based.
func (d *Definition) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]DefinitionLocation, error) {
	ctx, cancel := d.withTimeout(ctx)
	defer cancel()

	sess, err := d.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}

	cfg := d.mgr.cfg[lang]
	u := uri.File(absPath)
	if err := sess.server.DidOpen(ctx, didOpenParams(u, cfg.LanguageID, text)); err != nil {
		return nil, fmt.Errorf("open document: %w", err)
	}

	result, err := sess.server.Definition(ctx, definitionParams(u, pos))
	if err != nil {
		return nil, fmt.Errorf("definition request: %w", err)
	}
	return flattenDefinitionResult(result)
}

// withTimeout derives a context with the feature's timeout when the parent
// carries no deadline, so a wedged definition request cannot block indefinitely.
func (d *Definition) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d.timeout)
}

func definitionParams(u uri.URI, pos protocol.Position) *protocol.DefinitionParams {
	return &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	}
}

func flattenDefinitionResult(result protocol.DefinitionResult) ([]DefinitionLocation, error) {
	switch r := result.(type) {
	case nil:
		return []DefinitionLocation{}, nil
	case *protocol.Location:
		if r == nil {
			return []DefinitionLocation{}, nil
		}
		return []DefinitionLocation{definitionLocationFromLocation(*r)}, nil
	case protocol.LocationSlice:
		out := make([]DefinitionLocation, 0, len(r))
		for _, loc := range r {
			out = append(out, definitionLocationFromLocation(loc))
		}
		return out, nil
	case protocol.DefinitionLinkSlice:
		out := make([]DefinitionLocation, 0, len(r))
		for _, link := range r {
			out = append(out, definitionLocationFromLink(link))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported definition result %T", result)
	}
}

func definitionLocationFromLocation(loc protocol.Location) DefinitionLocation {
	rng := definitionRangeFromProtocol(loc.Range)
	return DefinitionLocation{
		TargetURI:            string(loc.URI),
		TargetRange:          rng,
		TargetSelectionRange: rng,
	}
}

func definitionLocationFromLink(link protocol.DefinitionLink) DefinitionLocation {
	out := DefinitionLocation{
		TargetURI:            string(link.TargetURI),
		TargetRange:          definitionRangeFromProtocol(link.TargetRange),
		TargetSelectionRange: definitionRangeFromProtocol(link.TargetSelectionRange),
	}
	if link.OriginSelectionRange != nil {
		rng := definitionRangeFromProtocol(*link.OriginSelectionRange)
		out.OriginSelectionRange = &rng
	}
	return out
}

func definitionRangeFromProtocol(rng protocol.Range) DefinitionRange {
	return DefinitionRange{
		StartLine:   int(rng.Start.Line),
		StartColumn: int(rng.Start.Character),
		EndLine:     int(rng.End.Line),
		EndColumn:   int(rng.End.Character),
	}
}
