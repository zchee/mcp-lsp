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
	"fmt"
	"os"

	"go.lsp.dev/protocol"
)

func readInputFile(workspaceRoot, file, lang string) (absPath, text, resolvedLang string, err error) {
	if file == "" {
		return "", "", "", fmt.Errorf("file is required")
	}
	absPath, err = resolveFilePath(workspaceRoot, file)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve file path %q: %w", file, err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", "", "", fmt.Errorf("read file %q: %w", absPath, err)
	}
	return absPath, string(content), defaultedLanguage(lang), nil
}

func defaultedLanguage(lang string) string {
	if lang == "" {
		return defaultLanguage
	}
	return lang
}

func inputRange(startLine, startColumn, endLine, endColumn int) (protocol.Range, error) {
	start, err := navigationInputPosition(startLine, startColumn)
	if err != nil {
		return protocol.Range{}, err
	}
	end, err := navigationInputPosition(endLine, endColumn)
	if err != nil {
		return protocol.Range{}, err
	}
	return protocol.Range{Start: start, End: end}, nil
}
