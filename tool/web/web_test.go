// Copyright 2026 Simone Vellei
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

package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv
}

func page(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}
}

func TestFetch_HTMLToText(t *testing.T) {
	srv := serve(t, page("text/html; charset=utf-8", `<html><head><title>Docs</title><style>p{}</style></head>
<body><h2>Install</h2><p>Run   <code>go get</code> first.</p><script>alert(1)</script>
<ul><li>See <a href="/guide">the guide</a></li><li><a href="#top">top</a></li></ul>
<pre>  indented
  code</pre></body></html>`))

	out, err := New(WithPrivateNetworks(true)).fetch(context.Background(), &Input{URL: srv.URL + "/docs"})
	if err != nil {
		t.Fatal(err)
	}

	want := "# Docs\n\n## Install\n\nRun go get first.\n\n- See the guide [" + srv.URL + "/guide]\n- top\n\n  indented\n  code"
	if out.Content != want || out.Status != http.StatusOK {
		t.Fatalf("content =\n%s\nwant\n%s", out.Content, want)
	}
}

func TestFetch_TextTypesAndLimits(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			page("application/json", `{"ok":true}`)(w, r)
		case "/image":
			page("image/png", "\x89PNG")(w, r)
		default:
			page("text/plain", strings.Repeat("x", 100))(w, r)
		}
	})

	f := New(WithPrivateNetworks(true), WithMaxOutputChars(10))
	ctx := context.Background()

	if out, err := f.fetch(ctx, &Input{URL: srv.URL + "/json"}); err != nil || out.Content != `{"ok":true}`[:10] || !out.Truncated {
		t.Fatalf("json = %+v, %v", out, err)
	}

	var unsupported *UnsupportedContentTypeError
	if _, err := f.fetch(ctx, &Input{URL: srv.URL + "/image"}); !errors.As(err, &unsupported) {
		t.Fatalf("image: err = %v, want UnsupportedContentTypeError", err)
	}

	if out, err := New(WithPrivateNetworks(true), WithMaxBytes(20)).fetch(ctx, &Input{URL: srv.URL}); err != nil ||
		len(out.Content) != 20 || !out.Truncated {
		t.Fatalf("text = %+v, %v; want 20 bytes, truncated", out, err)
	}
}

func TestFetch_RefusesPrivateNetworksByDefault(t *testing.T) {
	srv := serve(t, page("text/plain", "internal secret"))

	if _, err := New().fetch(context.Background(), &Input{URL: srv.URL}); !errors.Is(err, ErrPrivateNetwork) {
		t.Fatalf("err = %v, want ErrPrivateNetwork for %s", err, srv.URL)
	}
}

func TestFetch_SchemeAndHostRules(t *testing.T) {
	ctx := context.Background()

	if _, err := New().fetch(ctx, &Input{URL: "file:///etc/passwd"}); !errors.Is(err, ErrSchemeNotAllowed) {
		t.Fatalf("file url: err = %v", err)
	}

	// The first server is allowed, and redirects to a host that is not.
	target := serve(t, page("text/plain", "elsewhere"))
	redirect := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(target.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	})

	f := New(WithPrivateNetworks(true), WithAllowedHosts("127.0.0.1"))
	if _, err := f.fetch(ctx, &Input{URL: redirect.URL}); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("redirect to another host: err = %v, want ErrHostNotAllowed", err)
	}

	if _, err := f.fetch(ctx, &Input{URL: target.URL}); err != nil {
		t.Fatalf("allowed host: %v", err)
	}
}

func TestIsPrivate(t *testing.T) {
	cases := []struct {
		ip      string
		private bool
	}{
		{"8.8.8.8", false},
		{"2606:4700::1111", false},
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"169.254.169.254", true},
		{"::ffff:10.0.0.1", true}, // IPv4-mapped
		{"0.1.2.3", true},         // "this network": reaches the local host on Linux
		{"100.64.0.1", true},      // CGNAT
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"64:ff9b::a00:1", true},             // NAT64 of 10.0.0.1
		{"64:ff9b::808:808", false},          // NAT64 of 8.8.8.8
		{"64:ff9b:1::1", true},               // local-use NAT64 prefix
		{"2002:a00:1::1", true},              // 6to4 of 10.0.0.1
		{"2002:808:808::1", false},           // 6to4 of 8.8.8.8
		{"2001:0:808:808::f5ff:fffe", true},  // Teredo, client 10.0.0.1 (obfuscated)
		{"2001:0:a00:1::f7f7:f7f7", true},    // Teredo, server 10.0.0.1
		{"2001:0:808:808::f7f7:f7f7", false}, // Teredo, server and client 8.8.8.8
	}

	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tc.ip)
		}

		if got := isPrivate(ip); got != tc.private {
			t.Errorf("isPrivate(%s) = %v, want %v", tc.ip, got, tc.private)
		}
	}
}
