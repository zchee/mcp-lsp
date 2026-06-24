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
)

// NewServer assembles an [mcp.Server] that exposes language server capabilities
// backed by mgr as read-only tools.
func NewServer(mgr *lsp.Manager, logger *slog.Logger) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-lsp",
		Version: version.Version,
	}, &mcp.ServerOptions{
		Logger: logger,
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_diagnostics",
		Description: "Report LSP diagnostics (errors and warnings) for a file via its language server.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, diagnosticsHandler(mgr.Diagnostics(), mgr.WorkspaceRoot()))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lsp_definition",
		Description: "Find definition locations for a symbol at a file position via its language server.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, definitionHandler(mgr.Definition(), mgr.WorkspaceRoot()))

	return s
}
