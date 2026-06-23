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

// Package version derives the build version exposed by mcp-lsp.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
)

// versionStamp is the CLI version number.
//
// Injected at build time via -ldflags "-X github.com/zchee/mcp-lsp/internal/versionStamp.versionStamp=...".
var versionStamp = "dev"

var (
	gitCommitStamp string

	// Version is the complete build version advertised by the command.
	Version string
)

type embeddedInfo struct {
	valid      bool
	commit     string
	commitDate string
	commitTime string
	dirty      bool
}

func (i embeddedInfo) commitAbbrev() string {
	if len(i.commit) >= 9 {
		return i.commit[:9]
	}
	return i.commit
}

var getEmbeddedInfo = sync.OnceValue(func() embeddedInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return embeddedInfo{}
	}
	ret := embeddedInfo{valid: true}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			ret.commit = s.Value
		case "vcs.time":
			ret.commitTime = s.Value
			if len(s.Value) >= len("yyyy-mm-dd") {
				ret.commitDate = s.Value[:len("yyyy-mm-dd")]
				ret.commitDate = strings.ReplaceAll(ret.commitDate, "-", "")
			}
		case "vcs.modified":
			ret.dirty = s.Value == "true"
		}
	}
	if ret.commit == "" || ret.commitDate == "" {
		// Build info is present in the binary, but has no useful data. Act as
		// if it's missing.
		return embeddedInfo{}
	}
	return ret
})

func init() {
	if Version != "" {
		return
	}
	bi := getEmbeddedInfo()
	if !bi.valid {
		gitCommitStamp = "dev"
		Version = strings.TrimSpace(versionStamp)
		return
	}
	gitCommitStamp = fmt.Sprintf("%s-%s-t%s", bi.commit, bi.commitDate, bi.commitAbbrev())
	Version = strings.TrimSpace(versionStamp) + "-" + gitCommitStamp
}
