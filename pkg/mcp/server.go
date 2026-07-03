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

package mcp

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/mcp-lsp/internal/version"
	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
)

// NewServer assembles an [mcp.Server] that exposes language server capabilities
// backed by mgr as read-only tools.
func NewServer(mgr *lsp.Manager, logger *slog.Logger, resolver languageResolver) *mcp.Server {
	if mgr == nil {
		panic("manager is required")
	}
	if resolver == nil {
		panic("language resolver is required")
	}
	serverOpts := &mcp.ServerOptions{
		Logger: logger,
	}
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-lsp",
		Version: version.Version,
	}, serverOpts)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_diagnostics",
		Description: "Report LSP diagnostics (errors and warnings) for a file via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, diagnosticsHandler(mgr.Diagnostics(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_definition",
		Description: "Find definition locations for a symbol at a file position via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, definitionHandler(mgr.Definition(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_implementation",
		Description: "Find implementation locations for an interface, trait, or method at a file position via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, implementationHandler(mgr.Implementation(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_find_references",
		Description: "Find every reference to a symbol at a file position via its language server, gated by a readiness check so cold-index results are never reported as an authoritative zero.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, referencesHandler(mgr.References(), mgr.WorkspaceRoot(), resolver))

	registerCompositeTools(s, mgr, resolver)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_hover",
		Description: "Return hover information for a file position via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, hoverHandler(mgr.Hover(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_workspace_symbol",
		Description: "Search workspace symbols via the language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, workspaceSymbolHandler(mgr.WorkspaceSymbols(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_formatting",
		Description: "Preview full-document formatting edits without applying them.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, formattingHandler(mgr.Formatting(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_range_formatting",
		Description: "Preview range formatting edits without applying them.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, rangeFormattingHandler(mgr.Formatting(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_rename",
		Description: "Preview rename workspace edits without applying them.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, renameHandler(mgr.Rename(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_code_action",
		Description: "Preview code actions for a file range via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, codeActionHandler(mgr.CodeActions(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_code_lens",
		Description: "Return code lenses for a file via its language server.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, codeLensHandler(mgr.CodeLenses(), mgr.WorkspaceRoot(), resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_apply_workspace_edit",
		Description: "Apply an LSP workspace edit to files under the workspace root with an explicit mutation policy.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: new(true),
			IdempotentHint:  false,
			OpenWorldHint:   new(false),
		},
	}, applyWorkspaceEditHandler(mgr.WorkspaceRoot()))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_execute_command",
		Description: "Execute a server-advertised workspace command; may mutate files when applyEdits is true.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: new(true),
			IdempotentHint:  false,
			OpenWorldHint:   new(false),
		},
	}, executeCommandHandler(mgr.Commands(), resolver))

	return s
}

// registerCompositeTools registers the flagship composite tools, which fuse
// several language-server requests into one agent-facing call. They share a
// single composite engine built over the manager's exported helper surface.
func registerCompositeTools(s *mcp.Server, mgr *lsp.Manager, resolver languageResolver) {
	engine := composite.NewEngine(mgr)
	root := mgr.WorkspaceRoot()
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_impact_analysis",
		Description: "Compute the blast radius of changing a symbol: references, call graph, type graph, implementations, and diagnostics, gathered in one call with a readiness gate so cold-index results are never reported as an authoritative zero.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, impactAnalysisHandler(composite.NewImpactAnalysis(engine), root, resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_symbol_context",
		Description: "Assemble a dense symbol card (hover, enclosing outline, signature, navigation targets, same-file occurrences, inlay context) in one call, degrading each leg independently when a capability is unavailable.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, symbolContextHandler(composite.NewSymbolContext(engine), root, resolver))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_change_guard",
		Description: "Read the post-edit, on-disk diagnostics of a changed file and report an advisory verdict (clean/attention/broken); the verdict is settle-gated so cold or unsettled diagnostics yield notReady, never a false clean.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, changeGuardHandler(composite.NewChangeGuard(engine), root, resolver))
}
