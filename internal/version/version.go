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
// Injected at build time via -ldflags "-X github.com/zchee/mcp-lsp/internal/version.versionStamp=...".
var versionStamp = "dev"

// Version is the complete build version advertised by the command.
var Version string

// embeddedInfo is the VCS metadata recovered from the binary's build info.
type embeddedInfo struct {
	valid      bool
	commit     string
	commitDate string
}

// commitAbbrev returns the first nine characters of the commit hash, or the
// whole hash when it is shorter.
func commitAbbrev(commit string) string {
	if len(commit) >= 9 {
		return commit[:9]
	}
	return commit
}

// parseBuildInfo extracts the commit and normalized commit date from build
// info. It reports an invalid result when build info is absent or carries no
// usable VCS data, in which case the caller falls back to the injected stamp.
func parseBuildInfo(bi *debug.BuildInfo, ok bool) embeddedInfo {
	if !ok {
		return embeddedInfo{}
	}

	ret := embeddedInfo{valid: true}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			ret.commit = s.Value
		case "vcs.time":
			if len(s.Value) >= len("yyyy-mm-dd") {
				ret.commitDate = strings.ReplaceAll(s.Value[:len("yyyy-mm-dd")], "-", "")
			}
		}
	}
	if ret.commit == "" || ret.commitDate == "" {
		// Build info is present in the binary but has no useful data; act as if
		// it is missing.
		return embeddedInfo{}
	}
	return ret
}

// getEmbeddedInfo reads and parses the binary's build info exactly once.
var getEmbeddedInfo = sync.OnceValue(func() embeddedInfo {
	return parseBuildInfo(debug.ReadBuildInfo())
})

// buildVersion assembles the advertised version from the injected stamp and the
// embedded VCS metadata. When the metadata is invalid it returns the bare
// stamp; otherwise it appends the commit, date, and abbreviated commit.
func buildVersion(stamp string, info embeddedInfo) string {
	stamp = strings.TrimSpace(stamp)
	if !info.valid {
		return stamp
	}

	gitCommit := fmt.Sprintf("%s-%s-t%s", info.commit, info.commitDate, commitAbbrev(info.commit))
	return stamp + "-" + gitCommit
}

func init() {
	if Version != "" {
		return
	}

	Version = buildVersion(versionStamp, getEmbeddedInfo())
}
