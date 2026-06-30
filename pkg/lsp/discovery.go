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
	"slices"

	"go.lsp.dev/protocol"
)

// LookPathFunc resolves a command name to an executable path.
type LookPathFunc func(file string) (string, error)

// DiscoverServerConfigs builds active server configs only for candidates that
// are actually resolvable by lookPath. It selects the first discovered candidate
// per language in catalog order.
func DiscoverServerConfigs(specs []LanguageSpec, lookPath LookPathFunc) map[string]ServerConfig {
	if lookPath == nil {
		return nil
	}
	discovered := make(map[string]ServerConfig)
	for i := range specs {
		spec := &specs[i]
		canonical := normalizeLanguage(spec.Language)
		if canonical == "" {
			continue
		}
		for _, candidate := range spec.Candidates {
			if candidate.Command == "" {
				continue
			}
			command, err := lookPath(candidate.Command)
			if err != nil {
				continue
			}
			languageID := spec.LanguageID
			if languageID == "" {
				languageID = protocol.LanguageKind(canonical)
			}
			discovered[canonical] = ServerConfig{
				Command:    command,
				Args:       slices.Clone(candidate.Args),
				LanguageID: languageID,
			}
			break
		}
	}
	return discovered
}
