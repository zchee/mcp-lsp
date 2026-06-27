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

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (m *Manager) sessionForFile(ctx context.Context, lang, absPath string) (*serverSession, protocol.LanguageKind, uri.URI, error) {
	sess, err := m.session(ctx, lang)
	if err != nil {
		return nil, "", "", err
	}
	cfg := m.cfg[lang]
	return sess, cfg.LanguageID, uri.File(absPath), nil
}
