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

package weaviate

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	weaviateclient "github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"

	"github.com/henomis/phero/vectorstore"
)

var _ vectorstore.Store = (*Store)(nil)

// idNamespace is the fixed UUID namespace used to derive deterministic UUID v5
// values from arbitrary string point IDs.
var idNamespace = uuid.MustParse("1b671a64-40d5-491e-99b0-da01ff1f3341")

const (
	propID      = "pheroId"
	propPayload = "pheroPayload"
)

// Distance is the Weaviate vector-index distance metric.
type Distance string

const (
	// DistanceCosine computes cosine distance (default).
	DistanceCosine Distance = "cosine"
	// DistanceDot computes dot-product distance.
	DistanceDot Distance = "dot"
	// DistanceL2 computes L2-squared (Euclidean) distance.
	DistanceL2 Distance = "l2-squared"
)

const (
	defaultDistance  Distance = DistanceCosine
	defaultBatchSize          = 100
)

// Store is a Weaviate-backed implementation of vectorstore.Store.
//
// Construct it by injecting an already-configured *weaviate.Client:
//
//	c := weaviateclient.New(weaviateclient.Config{Host: "localhost:8080", Scheme: "http"})
//	vs, _ := weaviate.New(c, "myCollection", weaviate.WithDistance(weaviate.DistanceCosine))
//	_ = vs.EnsureCollection(ctx)
type Store struct {
	client     *weaviateclient.Client
	class      string
	vectorSize uint64
	distance   Distance
	batchSize  int
}

// Option configures a Store created by New.
type Option func(*Store)

// WithVectorSize sets an optional vector dimensionality used for client-side
// validation. When zero (the default), no dimension check is performed.
func WithVectorSize(vectorSize uint64) Option {
	return func(s *Store) {
		s.vectorSize = vectorSize
	}
}

// WithBatchSize sets the maximum number of objects sent per upsert request.
//
// Default is 100. A non-positive value disables batching.
func WithBatchSize(batchSize int) Option {
	return func(s *Store) {
		s.batchSize = batchSize
	}
}

// WithDistance configures the distance metric used when creating the backing
// Weaviate class. Default is DistanceCosine.
func WithDistance(distance Distance) Option {
	return func(s *Store) {
		s.distance = distance
	}
}

// New constructs a Weaviate-backed vectorstore bound to a single class.
//
// The first rune of class is uppercased automatically to satisfy Weaviate's
// class-name convention. The provided client is treated as an injected
// dependency and is not owned by the Store.
func New(client *weaviateclient.Client, class string, opts ...Option) (*Store, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if class == "" {
		return nil, ErrEmptyCollection
	}

	s := &Store{
		client:    client,
		class:     capitalizeFirst(class),
		distance:  defaultDistance,
		batchSize: defaultBatchSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s, nil
}

// EnsureCollection ensures the backing Weaviate class exists.
//
// If the class already exists, EnsureCollection is a no-op. Otherwise the
// class is created with the Store's configured distance metric and the
// two text properties used internally for ID and payload storage.
func (s *Store) EnsureCollection(ctx context.Context) error {
	exists, err := s.client.Schema().ClassExistenceChecker().WithClassName(s.class).Do(ctx)
	if err != nil {
		return fmt.Errorf("checking class existence: %w", err)
	}
	if exists {
		return nil
	}

	class := &models.Class{
		Class: s.class,
		VectorIndexConfig: map[string]interface{}{
			"distance": string(s.distance),
		},
		Properties: []*models.Property{
			{Name: propID, DataType: []string{"text"}},
			{Name: propPayload, DataType: []string{"text"}},
		},
	}
	if err := s.client.Schema().ClassCreator().WithClass(class).Do(ctx); err != nil {
		return fmt.Errorf("creating class: %w", err)
	}
	return nil
}

// Upsert inserts or replaces points in the configured Weaviate class.
//
// Each Point.ID is converted to a deterministic UUID v5 for storage; the
// original string ID is kept in the pheroId property so Query can return it.
// Payload is JSON-encoded into the pheroPayload property.
//
// If WithBatchSize was set to a positive value smaller than the number of
// points, Upsert splits the input into multiple requests.
func (s *Store) Upsert(ctx context.Context, points []vectorstore.Point) error {
	if len(points) == 0 {
		return vectorstore.ErrEmptyPoints
	}

	if s.batchSize <= 0 || s.batchSize >= len(points) {
		return s.upsertBatch(ctx, points)
	}
	for start := 0; start < len(points); start += s.batchSize {
		end := start + s.batchSize
		if end > len(points) {
			end = len(points)
		}
		if err := s.upsertBatch(ctx, points[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) upsertBatch(ctx context.Context, points []vectorstore.Point) error {
	objs := make([]*models.Object, 0, len(points))
	for _, p := range points {
		if p.ID == "" {
			return ErrPointIDRequired
		}
		if len(p.Vector) == 0 {
			return &EmptyVectorError{PointID: p.ID}
		}
		if s.vectorSize > 0 && uint64(len(p.Vector)) != s.vectorSize {
			return &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(p.Vector)}
		}

		payloadJSON, err := json.Marshal(p.Payload)
		if err != nil {
			return fmt.Errorf("marshaling payload for point %q: %w", p.ID, err)
		}

		objs = append(objs, &models.Object{
			Class:  s.class,
			ID:     idToUUID(p.ID),
			Vector: models.C11yVector(p.Vector),
			Properties: map[string]interface{}{
				propID:      p.ID,
				propPayload: string(payloadJSON),
			},
		})
	}

	if _, err := s.client.Batch().ObjectsBatcher().WithObjects(objs...).Do(ctx); err != nil {
		return fmt.Errorf("batch upsert: %w", err)
	}
	return nil
}

// Query returns the top-k nearest points to query using nearVector search.
//
// Score convention: higher values indicate greater similarity. For cosine
// distance the score is 1−distance; for all other metrics the score is
// −distance.
func (s *Store) Query(ctx context.Context, query vectorstore.Vector, limit uint64) ([]vectorstore.ScoredPoint, error) {
	if len(query) == 0 {
		return nil, vectorstore.ErrEmptyQuery
	}
	if limit == 0 {
		return nil, fmt.Errorf("limit must be greater than zero")
	}
	if s.vectorSize > 0 && uint64(len(query)) != s.vectorSize {
		return nil, &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(query)}
	}

	nearVec := s.client.GraphQL().NearVectorArgBuilder().WithVector(query)

	result, err := s.client.GraphQL().Get().
		WithClassName(s.class).
		WithNearVector(nearVec).
		WithLimit(int(limit)).
		WithFields(
			graphql.Field{Name: "_additional { id distance }"},
			graphql.Field{Name: propID},
			graphql.Field{Name: propPayload},
		).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("graphql query: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %v", result.Errors)
	}

	return s.parseQueryResult(result)
}

func (s *Store) parseQueryResult(result *models.GraphQLResponse) ([]vectorstore.ScoredPoint, error) {
	getRaw, ok := result.Data["Get"]
	if !ok {
		return nil, nil
	}
	getMap, ok := getRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected type for Get response: %T", getRaw)
	}
	raw, ok := getMap[s.class]
	if !ok {
		return nil, nil
	}
	objs, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response shape for class %q", s.class)
	}

	scored := make([]vectorstore.ScoredPoint, 0, len(objs))
	for _, obj := range objs {
		m, ok := obj.(map[string]interface{})
		if !ok {
			continue
		}

		addl, _ := m["_additional"].(map[string]interface{})
		dist, _ := addl["distance"].(float64)
		score := s.distanceToScore(float32(dist))

		origID, _ := m[propID].(string)

		payloadStr, _ := m[propPayload].(string)
		var payload map[string]any
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
				return nil, fmt.Errorf("unmarshaling payload: %w", err)
			}
		}

		scored = append(scored, vectorstore.ScoredPoint{
			ID:      origID,
			Score:   score,
			Payload: payload,
		})
	}
	return scored, nil
}

// Delete deletes points by their original string IDs.
//
// Each ID is converted to its deterministic UUID v5 before the deletion
// request is sent to Weaviate. Deletion proceeds one object at a time.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return vectorstore.ErrEmptyIDs
	}
	for _, id := range ids {
		if err := s.client.Data().Deleter().
			WithClassName(s.class).
			WithID(string(idToUUID(id))).
			Do(ctx); err != nil {
			return fmt.Errorf("deleting object %q: %w", id, err)
		}
	}
	return nil
}

// Clear removes all objects in the collection by deleting and recreating the
// Weaviate class.
func (s *Store) Clear(ctx context.Context) error {
	if err := s.client.Schema().ClassDeleter().WithClassName(s.class).Do(ctx); err != nil {
		return fmt.Errorf("deleting class: %w", err)
	}
	return s.EnsureCollection(ctx)
}

// idToUUID converts an arbitrary string ID to a deterministic UUID v5 using
// the package-level idNamespace.
func idToUUID(id string) strfmt.UUID {
	return strfmt.UUID(uuid.NewSHA1(idNamespace, []byte(id)).String())
}

// capitalizeFirst returns s with its first Unicode code point uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// distanceToScore converts a Weaviate distance value into a similarity score
// where higher means more similar. For cosine distance: 1−dist; for all
// others: −dist.
func (s *Store) distanceToScore(dist float32) float32 {
	if s.distance == DistanceCosine {
		return 1 - dist
	}
	return -dist
}
