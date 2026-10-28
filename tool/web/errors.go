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
	"errors"
	"fmt"
)

// ErrURLRequired is returned when the url argument is empty.
var ErrURLRequired = errors.New("web: url is required")

// ErrSchemeNotAllowed is returned for a URL that is not http or https.
var ErrSchemeNotAllowed = errors.New("web: only http and https URLs can be fetched")

// ErrHostNotAllowed is returned for a host outside WithAllowedHosts.
var ErrHostNotAllowed = errors.New("web: host not allowed")

// ErrPrivateNetwork is returned for an address on a loopback, private or
// link-local network, unless WithPrivateNetworks(true).
var ErrPrivateNetwork = errors.New("web: address on a private network")

// ErrTooManyRedirects is returned when a fetch is redirected too many times.
var ErrTooManyRedirects = errors.New("web: too many redirects")

// UnsupportedContentTypeError is returned for a response that is not text.
type UnsupportedContentTypeError struct {
	ContentType string
}

func (e *UnsupportedContentTypeError) Error() string {
	return fmt.Sprintf("web: cannot read content of type %q", e.ContentType)
}
