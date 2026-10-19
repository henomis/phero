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

package trace

import (
	"context"
	"errors"
	"sync/atomic"
)

// Provider creates spans and delivers them to processors.
//
// A nil *Provider is valid: spans are still recorded for totals and run
// summaries, but nothing is exported.
type Provider struct {
	processors []Processor
	ids        IDSource
	attrs      []Attr
	stopped    atomic.Bool
}

// Option configures a Provider.
type Option func(*Provider)

// WithProcessor adds a processor. Processors are called in the order they are
// added. Several processors fan spans out to several backends.
func WithProcessor(p Processor) Option {
	return func(pr *Provider) {
		if p != nil {
			pr.processors = append(pr.processors, p)
		}
	}
}

// WithResourceAttributes adds attributes to every span of the provider, such
// as the service name or the environment.
func WithResourceAttributes(attrs ...Attr) Option {
	return func(pr *Provider) { pr.attrs = append(pr.attrs, attrs...) }
}

// WithIDSource sets how span identifiers are assigned. trace/otel uses it so
// that phero and OpenTelemetry spans share identifiers.
func WithIDSource(src IDSource) Option {
	return func(pr *Provider) { pr.ids = src }
}

// NewProvider returns a Provider configured with opts.
func NewProvider(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Shutdown flushes and stops all processors. Spans ending afterwards are not
// delivered.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.stopped.Swap(true) {
		return nil
	}

	errs := make([]error, 0, len(p.processors))
	for _, proc := range p.processors {
		errs = append(errs, proc.Shutdown(ctx))
	}

	return errors.Join(errs...)
}

func (p *Provider) recording() bool {
	return p != nil && len(p.processors) > 0 && !p.stopped.Load()
}

func (p *Provider) onStart(ctx context.Context, d *SpanData) {
	for _, proc := range p.processors {
		proc.OnStart(ctx, d)
	}
}

func (p *Provider) onEnd(d *SpanData) {
	for _, proc := range p.processors {
		proc.OnEnd(d)
	}
}

// WithProvider returns a context carrying p. Spans started from it, and agents
// run with it that have no provider of their own, use p.
func WithProvider(ctx context.Context, p *Provider) context.Context {
	return context.WithValue(ctx, providerKey, p)
}

// ProviderFrom returns the provider carried by ctx, or nil.
func ProviderFrom(ctx context.Context) *Provider {
	p, _ := ctx.Value(providerKey).(*Provider)
	return p
}
