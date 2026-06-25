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
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// ApplyWorkspaceEditInput is the input schema for lsp_apply_workspace_edit.
type ApplyWorkspaceEditInput struct {
	// Edit is the LSP workspace edit to apply under the configured workspace root.
	Edit protocol.WorkspaceEdit `json:"edit" jsonschema:"LSP workspace edit to apply"`

	// CurrentVersions maps file URI to its expected current document version.
	CurrentVersions map[string]uint32 `json:"currentVersions,omitempty" jsonschema:"optional current document versions keyed by file URI"`

	// AllowCreateFile controls whether create-file resource operations are allowed.
	AllowCreateFile bool `json:"allowCreateFile"`
	// AllowRenameFile controls whether rename-file resource operations are allowed.
	AllowRenameFile bool `json:"allowRenameFile"`
	// AllowDeleteFile controls whether delete-file resource operations are allowed.
	AllowDeleteFile bool `json:"allowDeleteFile"`
}

// ApplyWorkspaceEditOutput is the output schema for lsp_apply_workspace_edit.
type ApplyWorkspaceEditOutput struct {
	// Applied reports whether the workspace edit was applied.
	Applied bool `json:"applied"`
	// FailureReason describes why the edit did not apply.
	FailureReason *string `json:"failureReason,omitempty"`
	// FailedChange is the zero-based index of the change that failed.
	FailedChange *uint32 `json:"failedChange,omitempty"`
}

// applyWorkspaceEditHandler returns the mutating workspace edit handler.
func applyWorkspaceEditHandler(workspaceRoot string) mcp.ToolHandlerFor[ApplyWorkspaceEditInput, ApplyWorkspaceEditOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in ApplyWorkspaceEditInput) (*mcp.CallToolResult, ApplyWorkspaceEditOutput, error) {
		edit, err := lsp.WorkspaceEditFromProtocol(in.Edit)
		if err != nil {
			return nil, ApplyWorkspaceEditOutput{}, fmt.Errorf("invalid workspace edit: %w", err)
		}

		result, err := lsp.ApplyWorkspaceEdit(edit, lsp.WorkspaceEditApplyOptions{
			WorkspaceRoot:   workspaceRoot,
			CurrentVersions: in.CurrentVersions,
			AllowCreateFile: in.AllowCreateFile,
			AllowRenameFile: in.AllowRenameFile,
			AllowDeleteFile: in.AllowDeleteFile,
		})
		if err != nil {
			return nil, ApplyWorkspaceEditOutput{}, fmt.Errorf("apply workspace edit: %w", err)
		}
		return nil, ApplyWorkspaceEditOutput(result), nil
	}
}
