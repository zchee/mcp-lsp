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

package rustintegration

import (
	"testing"

	"go.lsp.dev/uri"
)

func TestIntegrationRustAnalyzerDefinitionResolvesAcrossFiles(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_crossfile.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("src/main.rs")
	text := ws.Source(t, "src/main.rs")
	query := ws.MarkerPosition(t, "src/main.rs", "query", "Greeting")
	target := ws.MarkerPosition(t, "src/lib.rs", "target", "Greeting")

	defs := lookupDefinition(t, mgr, mainFile, text, query)
	assertResolvesTo(t, defs, string(uri.File(ws.Path("src/lib.rs"))), target)
}
