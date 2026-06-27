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

// NavigationRange is a flattened range for definition-like LSP methods whose
// result shape is [protocol.DefinitionResult]. Positions are zero-based here,
// matching the LSP wire format; the MCP layer converts them to one-based for the
// agent. If a future navigation feature needs different target/range semantics,
// add a feature-specific DTO instead of widening this shared one.
type NavigationRange struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

// NavigationLocation is a flattened target for definition-like LSP methods
// whose result shape is [protocol.DefinitionResult]. Positions are zero-based
// here, matching the LSP wire format; the MCP layer converts them to one-based
// for the agent. If a future navigation feature needs different target/range
// semantics, add a feature-specific DTO instead of widening this shared one.
type NavigationLocation struct {
	TargetURI            string
	TargetRange          NavigationRange
	TargetSelectionRange NavigationRange
	OriginSelectionRange *NavigationRange
}

func featureTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func flattenNavigationResult(kind string, result protocol.DefinitionResult) ([]NavigationLocation, error) {
	switch r := result.(type) {
	case nil:
		return []NavigationLocation{}, nil
	case *protocol.Location:
		if r == nil {
			return []NavigationLocation{}, nil
		}
		return []NavigationLocation{navigationLocationFromLocation(*r)}, nil
	case protocol.LocationSlice:
		out := make([]NavigationLocation, 0, len(r))
		for _, loc := range r {
			out = append(out, navigationLocationFromLocation(loc))
		}
		return out, nil
	case protocol.DefinitionLinkSlice:
		out := make([]NavigationLocation, 0, len(r))
		for _, link := range r {
			out = append(out, navigationLocationFromLink(link))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported %s result %T", kind, result)
	}
}

func navigationLocationFromLocation(loc protocol.Location) NavigationLocation {
	rng := navigationRangeFromProtocol(loc.Range)
	return NavigationLocation{
		TargetURI:            string(loc.URI),
		TargetRange:          rng,
		TargetSelectionRange: rng,
	}
}

func navigationLocationFromLink(link protocol.DefinitionLink) NavigationLocation {
	out := NavigationLocation{
		TargetURI:            string(link.TargetURI),
		TargetRange:          navigationRangeFromProtocol(link.TargetRange),
		TargetSelectionRange: navigationRangeFromProtocol(link.TargetSelectionRange),
	}
	if link.OriginSelectionRange != nil {
		rng := navigationRangeFromProtocol(*link.OriginSelectionRange)
		out.OriginSelectionRange = &rng
	}
	return out
}

func navigationRangeFromProtocol(rng protocol.Range) NavigationRange {
	return NavigationRange{
		StartLine:   int(rng.Start.Line),
		StartColumn: int(rng.Start.Character),
		EndLine:     int(rng.End.Line),
		EndColumn:   int(rng.End.Character),
	}
}
