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

package composite

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// The engine depends on the language server through one narrow interface per
// capability group rather than on *lsp.Manager directly, so composites and
// their tests can substitute fakes without a subprocess. Each concrete
// pkg/lsp helper already satisfies its interface; the compile-time assertions
// below pin that.

// referencesLooker finds all references to a symbol, optionally including its
// declaration.
type referencesLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position, includeDeclaration bool) ([]lsp.NavigationLocation, error)
}

// navigationLooker resolves a single navigation family request (definition,
// declaration, type definition, or implementation) to target locations.
type navigationLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]lsp.NavigationLocation, error)
}

// documentSymbolLooker returns a file's symbol outline.
type documentSymbolLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string) ([]lsp.DocumentSymbolEntry, error)
}

// callHierarchyLooker prepares call-hierarchy items and walks their callers
// and callees.
type callHierarchyLooker interface {
	Prepare(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]protocol.CallHierarchyItem, error)
	IncomingCalls(ctx context.Context, lang string, item *protocol.CallHierarchyItem) ([]protocol.CallHierarchyIncomingCall, error)
	OutgoingCalls(ctx context.Context, lang string, item *protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error)
}

// typeHierarchyLooker prepares type-hierarchy items and walks their supertypes
// and subtypes.
type typeHierarchyLooker interface {
	Prepare(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]protocol.TypeHierarchyItem, error)
	Supertypes(ctx context.Context, lang string, item *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error)
	Subtypes(ctx context.Context, lang string, item *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error)
}

// capabilityProbe reports which capabilities a language server advertised.
type capabilityProbe interface {
	CapabilitySnapshot(ctx context.Context, lang string) (lsp.CapabilitySnapshot, error)
}

// Compile-time proof that the concrete pkg/lsp helpers satisfy the engine's
// narrow interfaces. A signature drift in pkg/lsp breaks the build here.
var (
	_ referencesLooker     = (*lsp.References)(nil)
	_ navigationLooker     = (*lsp.Definition)(nil)
	_ navigationLooker     = (*lsp.Declaration)(nil)
	_ navigationLooker     = (*lsp.TypeDefinition)(nil)
	_ navigationLooker     = (*lsp.Implementation)(nil)
	_ documentSymbolLooker = (*lsp.DocumentSymbols)(nil)
	_ callHierarchyLooker  = (*lsp.CallHierarchy)(nil)
	_ typeHierarchyLooker  = (*lsp.TypeHierarchy)(nil)
	_ capabilityProbe      = (*lsp.Manager)(nil)
)

// Engine is the shared substrate the flagship composites build on. It holds
// one looker per capability group plus the capability probe, all backed by the
// exported *lsp.Manager helper surface, and never reaches into unexported
// session state.
type Engine struct {
	references     referencesLooker
	definition     navigationLooker
	declaration    navigationLooker
	typeDefinition navigationLooker
	implementation navigationLooker
	documentSymbol documentSymbolLooker
	callHierarchy  callHierarchyLooker
	typeHierarchy  typeHierarchyLooker
	capabilities   capabilityProbe
}

// NewEngine wires an engine from a manager's exported helpers.
func NewEngine(mgr *lsp.Manager) *Engine {
	return &Engine{
		references:     mgr.References(),
		definition:     mgr.Definition(),
		declaration:    mgr.Declaration(),
		typeDefinition: mgr.TypeDefinition(),
		implementation: mgr.Implementation(),
		documentSymbol: mgr.DocumentSymbols(),
		callHierarchy:  mgr.CallHierarchy(),
		typeHierarchy:  mgr.TypeHierarchy(),
		capabilities:   mgr,
	}
}
