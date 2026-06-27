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

package version

import (
	"runtime/debug"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
)

func TestCommitAbbrev(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		commit string
		want   string
	}{
		"success: long hash truncates to nine": {
			commit: "0123456789abcdef",
			want:   "012345678",
		},
		"success: exactly nine is unchanged": {
			commit: "012345678",
			want:   "012345678",
		},
		"success: shorter hash is returned whole": {
			commit: "abcd",
			want:   "abcd",
		},
		"success: empty hash yields empty": {
			commit: "",
			want:   "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := commitAbbrev(tt.commit); got != tt.want {
				t.Errorf("commitAbbrev(%q) = %q, want %q", tt.commit, got, tt.want)
			}
		})
	}
}

// settings builds a [debug.BuildInfo] carrying the given VCS settings so
// parseBuildInfo can be driven without a real binary's embedded build info.
func settings(kv map[string]string) *debug.BuildInfo {
	bi := &debug.BuildInfo{}
	for k, v := range kv {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
	}
	return bi
}

func TestParseBuildInfo(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bi   *debug.BuildInfo
		ok   bool
		want embeddedInfo
	}{
		"success: full revision and time": {
			bi: settings(map[string]string{
				"vcs.revision": "0123456789abcdef",
				"vcs.time":     "2026-06-24T07:07:47Z",
			}),
			ok: true,
			want: embeddedInfo{
				valid:      true,
				commit:     "0123456789abcdef",
				commitDate: "20260624",
			},
		},
		"invalid: build info absent": {
			bi:   nil,
			ok:   false,
			want: embeddedInfo{},
		},
		"invalid: no vcs settings at all": {
			bi:   settings(nil),
			ok:   true,
			want: embeddedInfo{},
		},
		"invalid: revision without time": {
			bi: settings(map[string]string{
				"vcs.revision": "0123456789abcdef",
			}),
			ok:   true,
			want: embeddedInfo{},
		},
		"invalid: time without revision": {
			bi: settings(map[string]string{
				"vcs.time": "2026-06-24T07:07:47Z",
			}),
			ok:   true,
			want: embeddedInfo{},
		},
		"invalid: time too short to parse a date": {
			bi: settings(map[string]string{
				"vcs.revision": "0123456789abcdef",
				"vcs.time":     "2026-06",
			}),
			ok:   true,
			want: embeddedInfo{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := parseBuildInfo(tt.bi, tt.ok)
			if diff := gocmp.Diff(tt.want, got, gocmp.AllowUnexported(embeddedInfo{})); diff != "" {
				t.Errorf("parseBuildInfo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBuildVersion locks the exact [Version] string buildVersion assembles from
// the injected stamp and parsed build info, the contract init commits to for
// both the embedded and fallback cases. It exercises the production helper
// directly so a change to the assembly format is caught here.
func TestBuildVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stamp string
		info  embeddedInfo
		want  string
	}{
		"success: embedded info appends commit detail": {
			stamp: "v1.2.3",
			info: embeddedInfo{
				valid:      true,
				commit:     "0123456789abcdef",
				commitDate: "20260624",
			},
			want: "v1.2.3-0123456789abcdef-20260624-t012345678",
		},
		"success: short commit is not truncated in the suffix": {
			stamp: "v1.2.3",
			info: embeddedInfo{
				valid:      true,
				commit:     "abcd",
				commitDate: "20260624",
			},
			want: "v1.2.3-abcd-20260624-tabcd",
		},
		"success: missing info falls back to the bare stamp": {
			stamp: "v1.2.3",
			info:  embeddedInfo{},
			want:  "v1.2.3",
		},
		"success: stamp is trimmed before use": {
			stamp: "  dev\n",
			info:  embeddedInfo{},
			want:  "dev",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := buildVersion(tt.stamp, tt.info); got != tt.want {
				t.Errorf("buildVersion(%q, %+v) = %q, want %q", tt.stamp, tt.info, got, tt.want)
			}
		})
	}
}
