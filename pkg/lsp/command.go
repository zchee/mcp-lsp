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
	"slices"
	"time"

	"go.lsp.dev/protocol"
)

// Command is a compact command DTO.
type Command struct {
	Title   string
	Tooltip string
	Command string
}

// Commands is the workspace/executeCommand feature bound to a [Manager].
type Commands struct {
	mgr     *Manager
	timeout time.Duration
}

// Commands returns the execute-command feature for this manager.
func (m *Manager) Commands() *Commands { return &Commands{mgr: m, timeout: defaultTimeout} }

// Execute runs an advertised workspace command.
func (c *Commands) Execute(ctx context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error) {
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, err := c.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(sess.capabilities.executeCommands, command) {
		return nil, fmt.Errorf("execute command %q is not advertised by language server", command)
	}
	var result protocol.LSPAny
	exec := func() error {
		var err error
		result, err = sess.server.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{Command: command, Arguments: args})
		if err != nil {
			return fmt.Errorf("execute command request: %w", err)
		}
		return nil
	}
	if applyEdits {
		if sess.client == nil {
			return nil, fmt.Errorf("execute command apply mode requires initialized client")
		}
		if err := sess.client.withApplyEditPolicy(WorkspaceEditApplyOptions{WorkspaceRoot: c.mgr.rootDir}, exec); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := exec(); err != nil {
		return nil, err
	}
	return result, nil
}

func flattenCommand(command protocol.Command) *Command {
	tooltip := ""
	if command.Tooltip != nil {
		tooltip = *command.Tooltip
	}
	return &Command{Title: command.Title, Tooltip: tooltip, Command: command.Command}
}
