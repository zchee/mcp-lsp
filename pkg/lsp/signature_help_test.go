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
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
)

func (f *fakeServer) SignatureHelp(_ context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.signatureHelpRequests = append(f.signatureHelpRequests, *params)
	if f.signatureHelpErr != nil {
		return nil, f.signatureHelpErr
	}
	return f.signatureHelpResult, nil
}

func (f *fakeServer) signatureHelpCalls() []protocol.SignatureHelpParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.SignatureHelpParams(nil), f.signatureHelpRequests...)
}

func fakeSignatureHelp(sess *serverSession) *SignatureHelp {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &SignatureHelp{mgr: mgr, timeout: 2 * time.Second}
}

func TestSignatureHelpLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.signatureHelp {
		t.Fatal("session detected signature-help support that the fake did not advertise")
	}

	_, err := fakeSignatureHelp(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "signature help request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.signatureHelpCalls()); got != 0 {
		t.Errorf("Lookup issued %d requests despite unsupported provider, want 0", got)
	}
}

func TestSignatureHelpLookupResult(t *testing.T) {
	t.Parallel()

	active := uint32(1)
	fake := &fakeServer{
		signatureHelpSupported: true,
		signatureHelpResult: &protocol.SignatureHelp{
			ActiveSignature: &active,
			Signatures: []protocol.SignatureInformation{
				{
					Label: "Greeting(name string) string",
					Parameters: []protocol.ParameterInformation{
						{Label: protocol.String("name string")},
					},
				},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.signatureHelp {
		t.Fatal("session did not detect signature-help support advertised by the fake")
	}

	got, err := fakeSignatureHelp(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 5, Character: 8})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := &SignatureHelpResult{
		ActiveSignature: 1,
		Signatures: []SignatureInfo{
			{Label: "Greeting(name string) string", Parameters: []string{"name string"}},
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup signature help mismatch (-want +got):\n%s", diff)
	}
}

func TestSignatureHelpLookupNilResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{signatureHelpSupported: true})
	got, err := fakeSignatureHelp(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != nil {
		t.Errorf("Lookup off a call site = %+v, want nil", got)
	}
}

func TestSignatureHelpLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{signatureHelpSupported: true, signatureHelpErr: sentinel})

	_, err := fakeSignatureHelp(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	if !strings.Contains(err.Error(), "signature help request") || !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want signature help request context and server error %q", err, sentinel)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v must not match ErrUnsupported for a server failure", err)
	}
}
