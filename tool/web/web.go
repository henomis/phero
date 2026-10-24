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
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/henomis/phero/v2/llm"
)

const (
	// DefaultTimeout bounds one fetch, redirects included.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxBytes is the most of a response body that is read.
	DefaultMaxBytes = 5 << 20
	// DefaultMaxChars is the most text the tool returns.
	DefaultMaxChars = 50000

	maxRedirects = 5

	mimeXHTML = "application/xhtml+xml"
)

// Input is the input schema of the web_fetch tool.
type Input struct {
	URL string `json:"url" jsonschema:"description=The http or https URL to fetch"`
}

// Output is the output schema of the web_fetch tool.
type Output struct {
	// URL is the final URL, after redirects.
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	// Truncated reports that the body or the text was cut.
	Truncated bool `json:"truncated,omitempty"`
}

// Fetcher fetches web pages for an agent.
type Fetcher struct {
	timeout         time.Duration
	maxBytes        int64
	maxChars        int
	allowedHosts    []*regexp.Regexp
	privateNetworks bool
	transport       http.RoundTripper
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithTimeout bounds one fetch; the default is DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(f *Fetcher) { f.timeout = d }
}

// WithMaxBytes caps how much of a response body is read; the default is
// DefaultMaxBytes.
func WithMaxBytes(n int64) Option {
	return func(f *Fetcher) { f.maxBytes = n }
}

// WithMaxChars caps the text returned; the default is DefaultMaxChars.
func WithMaxChars(n int) Option {
	return func(f *Fetcher) { f.maxChars = n }
}

// WithAllowedHosts limits fetches, redirects included, to hosts matching one
// of patterns, where * matches any run of characters ("*.go.dev"). Without it
// every host can be fetched.
func WithAllowedHosts(patterns ...string) Option {
	return func(f *Fetcher) {
		for _, p := range patterns {
			parts := strings.Split(strings.ToLower(p), "*")
			for i := range parts {
				parts[i] = regexp.QuoteMeta(parts[i])
			}

			f.allowedHosts = append(f.allowedHosts, regexp.MustCompile("^"+strings.Join(parts, ".*")+"$"))
		}
	}
}

// WithPrivateNetworks chooses whether addresses on loopback, private and
// link-local networks can be fetched; they are refused by default.
func WithPrivateNetworks(allowed bool) Option {
	return func(f *Fetcher) { f.privateNetworks = allowed }
}

// New returns a Fetcher.
func New(opts ...Option) *Fetcher {
	f := &Fetcher{timeout: DefaultTimeout, maxBytes: DefaultMaxBytes, maxChars: DefaultMaxChars}
	for _, opt := range opts {
		opt(f)
	}

	if f.transport == nil {
		f.transport = f.newTransport()
	}

	return f
}

// Tool returns the web_fetch tool.
func (f *Fetcher) Tool() (*llm.Tool, error) {
	return llm.NewTool("web_fetch",
		"Fetch a web page by URL and return its content as text (HTML is converted, links kept as [url]).",
		f.fetch)
}

// newTransport returns a transport whose connections are checked against the
// private network rule after DNS resolution, so neither a redirect nor a DNS
// answer can reach a refused address.
func (f *Fetcher) newTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: f.timeout}
	if !f.privateNetworks {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}

			if ip := net.ParseIP(host); ip == nil || isPrivate(ip) {
				return fmt.Errorf("%w: %s", ErrPrivateNetwork, host)
			}

			return nil
		}
	}

	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // a proxy would connect for us, out of our check

	return transport
}

// isPrivate reports whether ip is on a network the tool refuses by default.
func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

// checkURL applies the scheme and host rules to u.
func (f *Fetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrSchemeNotAllowed
	}

	if len(f.allowedHosts) == 0 {
		return nil
	}

	host := strings.ToLower(u.Hostname())
	for _, re := range f.allowedHosts {
		if re.MatchString(host) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
}

func (f *Fetcher) fetch(ctx context.Context, in *Input) (*Output, error) {
	if in == nil || strings.TrimSpace(in.URL) == "" {
		return nil, ErrURLRequired
	}

	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil {
		return nil, fmt.Errorf("web: invalid url: %w", err)
	}

	if err = f.checkURL(u); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	client := &http.Client{
		Transport: f.transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return ErrTooManyRedirects
			}

			return f.checkURL(req.URL)
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "phero-web-fetch/1")
	req.Header.Set("Accept", "text/html, text/plain, application/json, */*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return nil, unwrapURLError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	return f.readResponse(resp)
}

// unwrapURLError returns the rule error inside a *url.Error, so the model
// reads "host not allowed" rather than the whole request line.
func unwrapURLError(err error) error {
	for _, rule := range []error{ErrPrivateNetwork, ErrHostNotAllowed, ErrSchemeNotAllowed, ErrTooManyRedirects} {
		if errors.Is(err, rule) {
			return fmt.Errorf("%w (%v)", rule, err)
		}
	}

	return err
}

// readResponse reads up to maxBytes of resp's body and converts it to text.
func (f *Fetcher) readResponse(resp *http.Response) (*Output, error) {
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	if !isText(mediaType) {
		return nil, &UnsupportedContentTypeError{ContentType: contentType}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, err
	}

	truncated := int64(len(body)) > f.maxBytes
	if truncated {
		body = body[:f.maxBytes]
	}

	content := string(body)
	if mediaType == "text/html" || mediaType == mimeXHTML {
		content = htmlToText(content, resp.Request.URL)
	}

	if runes := []rune(content); f.maxChars > 0 && len(runes) > f.maxChars {
		content, truncated = string(runes[:f.maxChars]), true
	}

	return &Output{
		URL:         resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: contentType,
		Content:     content,
		Truncated:   truncated,
	}, nil
}

// isText reports whether a media type can be returned as text. An empty one
// is sniffed as text by the caller's luck: servers that omit it mostly send
// text.
func isText(mediaType string) bool {
	switch {
	case mediaType == "", strings.HasPrefix(mediaType, "text/"):
		return true
	case strings.HasSuffix(mediaType, "+json"), strings.HasSuffix(mediaType, "+xml"):
		return true
	}

	switch mediaType {
	case "application/json", "application/xml", "application/javascript", mimeXHTML,
		"application/x-yaml", "application/yaml", "application/toml":
		return true
	}

	return false
}
