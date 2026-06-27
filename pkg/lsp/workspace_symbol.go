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
)

// WorkspaceSymbols is the workspace/symbol feature bound to a [Manager].
type WorkspaceSymbols struct {
	mgr     *Manager
	timeout time.Duration
}

// WorkspaceSymbols returns the workspace/symbol feature for this manager.
func (m *Manager) WorkspaceSymbols() *WorkspaceSymbols {
	return &WorkspaceSymbols{mgr: m, timeout: defaultTimeout}
}

// WorkspaceSymbol is a compact workspace symbol response with zero-based ranges.
type WorkspaceSymbol struct {
	Name          string
	Kind          string
	ContainerName string
	URI           string
	Range         *NavigationRange
}

// Lookup returns workspace symbols for query.
func (w *WorkspaceSymbols) Lookup(ctx context.Context, lang, query string) ([]WorkspaceSymbol, error) {
	ctx, cancel := withRequestTimeout(ctx, w.timeout)
	defer cancel()

	sess, err := w.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.workspaceSymbol {
		return nil, fmt.Errorf("workspace/symbol request is not supported by language server")
	}
	result, err := sess.server.Symbols(ctx, &protocol.WorkspaceSymbolParams{Query: query})
	if err != nil {
		return nil, fmt.Errorf("workspace/symbol request: %w", err)
	}
	return flattenWorkspaceSymbols(result), nil
}

func flattenWorkspaceSymbols(result protocol.WorkspaceSymbolResult) []WorkspaceSymbol {
	switch v := result.(type) {
	case nil:
		return nil
	case protocol.SymbolInformationSlice:
		out := make([]WorkspaceSymbol, 0, len(v))
		for _, sym := range v {
			out = append(out, WorkspaceSymbol{Name: sym.Name, Kind: fmt.Sprint(sym.Kind), ContainerName: stringValue(sym.ContainerName), URI: string(sym.Location.URI), Range: ptrNavigationRange(sym.Location.Range)})
		}
		return out
	case protocol.WorkspaceSymbolSlice:
		out := make([]WorkspaceSymbol, 0, len(v))
		for _, sym := range v {
			item := WorkspaceSymbol{Name: sym.Name, Kind: fmt.Sprint(sym.Kind), ContainerName: stringValue(sym.ContainerName)}
			switch loc := sym.Location.(type) {
			case *protocol.Location:
				if loc != nil {
					item.URI = string(loc.URI)
					item.Range = ptrNavigationRange(loc.Range)
				}
			case *protocol.LocationUriOnly:
				if loc != nil {
					item.URI = string(loc.URI)
				}
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func ptrNavigationRange(rng protocol.Range) *NavigationRange {
	out := navigationRangeFromProtocol(rng)
	return &out
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
