package qdrant

import (
	"context"
	"strconv"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/vectorstore"
)

var _ vectorstore.Store = (*Store)(nil)

// Distance is the Qdrant distance metric type used when creating collections.
//
// It is re-exported from the official Qdrant Go client for convenience.
type Distance = qdrantapi.Distance

const (
	// DistanceCosine computes cosine similarity.
	DistanceCosine = qdrantapi.Distance_Cosine
	// DistanceEuclid computes Euclidean distance.
	DistanceEuclid = qdrantapi.Distance_Euclid
	// DistanceDot computes dot-product similarity.
	DistanceDot = qdrantapi.Distance_Dot
	// DistanceManhattan computes Manhattan (L1) distance.
	DistanceManhattan = qdrantapi.Distance_Manhattan
)

const (
	defaultDistance  = DistanceCosine
	defaultBatchSize = 100
)

// Store is a Qdrant-backed implementation of vectorstore.Store.
//
// Users are expected to construct a Qdrant client themselves, then inject it:
//
//	qc, _ := qdrantapi.NewClient(&qdrantapi.Config{Host: "localhost", Port: 6334})
//	vs, _ := qdrant.New(qc, "my_collection", WithVectorSize(1536), WithBatchSize(128))
//	_ = vs.EnsureCollection(ctx)
type Store struct {
	client     *qdrantapi.Client
	collection string
	vectorSize uint64
	distance   Distance
	batchSize  int

	// wait controls whether write operations block until applied.
	wait *bool
}

// Option configures a Store created by New.
type Option func(*Store)

// WithBatchSize sets the maximum number of points sent per upsert request.
//
// Default is 100. A non-positive value disables batching.
func WithBatchSize(batchSize int) Option {
	return func(s *Store) {
		s.batchSize = batchSize
	}
}

// WithVectorSize sets the vector size used when creating a new collection and
// validating points/queries.
//
// This is required.
func WithVectorSize(vectorSize uint64) Option {
	return func(s *Store) {
		s.vectorSize = vectorSize
	}
}

// WithDistance configures the distance metric used when creating a new collection.
//
// Default is DistanceCosine.
func WithDistance(distance Distance) Option {
	return func(s *Store) {
		s.distance = distance
	}
}

// WithWait configures whether write operations should wait until applied.
func WithWait(wait bool) Option {
	return func(s *Store) {
		s.wait = qdrantapi.PtrOf(wait)
	}
}

// New constructs a Qdrant-backed vectorstore bound to a single collection.
//
// The provided client is treated as an injected dependency and is not owned by
// the Store (i.e. Store does not Close it).
func New(client *qdrantapi.Client, collection string, opts ...Option) (*Store, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if collection == "" {
		return nil, ErrEmptyCollection
	}

	s := &Store{
		client:     client,
		collection: collection,
		distance:   defaultDistance,
		batchSize:  defaultBatchSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.vectorSize == 0 {
		return nil, ErrInvalidVectorSize
	}
	return s, nil
}

// EnsureCollection ensures that the configured collection exists.
//
// If the collection already exists, EnsureCollection is a no-op.
// If it does not exist, it is created using the Store's configured vector size
// and distance metric.
func (s *Store) EnsureCollection(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, s.collection)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	req := &qdrantapi.CreateCollection{
		CollectionName: s.collection,
		VectorsConfig: qdrantapi.NewVectorsConfig(&qdrantapi.VectorParams{
			Size:     s.vectorSize,
			Distance: s.distance,
		}),
	}

	return s.client.CreateCollection(ctx, req)
}

// Upsert inserts or replaces points in the configured collection.
//
// Upsert validates that points have a non-empty ID, a non-empty vector, and
// that the vector length matches the Store's configured vector size.
//
// If WithBatchSize was configured with a positive size smaller than the number
// of points, Upsert will split the input into multiple upsert requests.
func (s *Store) Upsert(ctx context.Context, points []vectorstore.Point) error {
	if len(points) == 0 {
		return vectorstore.ErrEmptyPoints
	}

	if s.batchSize <= 0 || s.batchSize >= len(points) {
		return s.upsertOnce(ctx, points)
	}

	for start := 0; start < len(points); start += s.batchSize {
		end := start + s.batchSize
		if end > len(points) {
			end = len(points)
		}
		if err := s.upsertOnce(ctx, points[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) upsertOnce(ctx context.Context, points []vectorstore.Point) error {
	if len(points) == 0 {
		return vectorstore.ErrEmptyPoints
	}

	qPoints := make([]*qdrantapi.PointStruct, 0, len(points))
	for _, p := range points {
		if p.ID == "" {
			return ErrPointIDRequired
		}
		if len(p.Vector) == 0 {
			return &EmptyVectorError{PointID: p.ID}
		}
		if uint64(len(p.Vector)) != s.vectorSize {
			return &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(p.Vector)}
		}

		var payload map[string]*qdrantapi.Value
		if p.Payload != nil {
			m, err := qdrantapi.TryValueMap(p.Payload)
			if err != nil {
				return &InvalidPayloadError{PointID: p.ID, Err: err}
			}
			payload = m
		}

		qPoints = append(qPoints, &qdrantapi.PointStruct{
			Id:      idToPointID(p.ID),
			Vectors: qdrantapi.NewVectors(p.Vector...),
			Payload: payload,
		})
	}

	_, err := s.client.Upsert(ctx, &qdrantapi.UpsertPoints{
		CollectionName: s.collection,
		Points:         qPoints,
		Wait:           s.wait,
	})
	return err
}

// Query runs a nearest-neighbors search for the provided query vector.
//
// The query vector must be non-empty and match the Store's configured vector
// size. If limit is 0, Query returns an empty slice and a nil error.
//
// Returned points include the decoded payload if present.
func (s *Store) Query(ctx context.Context, query vectorstore.Vector, limit uint64) ([]vectorstore.ScoredPoint, error) {
	if len(query) == 0 {
		return nil, vectorstore.ErrEmptyQuery
	}
	if limit == 0 {
		return []vectorstore.ScoredPoint{}, nil
	}
	if uint64(len(query)) != s.vectorSize {
		return nil, &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(query)}
	}

	res, err := s.client.Query(ctx, &qdrantapi.QueryPoints{
		CollectionName: s.collection,
		Query:          qdrantapi.NewQuery(query...),
		Limit:          qdrantapi.PtrOf(limit),
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}

	out := make([]vectorstore.ScoredPoint, 0, len(res))
	for _, sp := range res {
		if sp == nil || sp.Id == nil {
			continue
		}

		payload := make(map[string]any, len(sp.Payload))
		for k, v := range sp.Payload {
			payload[k] = decodeValue(v)
		}

		out = append(out, vectorstore.ScoredPoint{
			ID:      pointIDToString(sp.Id),
			Score:   sp.Score,
			Payload: payload,
		})
	}

	return out, nil
}

// Delete removes the points with the given IDs from the configured collection.
//
// Empty IDs are ignored. If all provided IDs are empty, Delete returns
// vectorstore.ErrEmptyIDs.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return vectorstore.ErrEmptyIDs
	}

	qIDs := make([]*qdrantapi.PointId, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		qIDs = append(qIDs, idToPointID(id))
	}
	if len(qIDs) == 0 {
		return vectorstore.ErrEmptyIDs
	}

	_, err := s.client.Delete(ctx, &qdrantapi.DeletePoints{
		CollectionName: s.collection,
		Points:         qdrantapi.NewPointsSelectorIDs(qIDs),
		Wait:           s.wait,
	})
	return err
}

// Clear removes all points from the collection while preserving the collection
// structure (vector config, distance metric, indexes).
//
// This matches the semantics of the vectorstore.Store interface, which requires
// only the points to be removed, not the collection itself.
func (s *Store) Clear(ctx context.Context) error {
	_, err := s.client.Delete(ctx, &qdrantapi.DeletePoints{
		CollectionName: s.collection,
		Points:         qdrantapi.NewPointsSelectorFilter(&qdrantapi.Filter{}),
		Wait:           s.wait,
	})
	return err
}

func idToPointID(id string) *qdrantapi.PointId {
	if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		return qdrantapi.NewIDNum(n)
	}
	return qdrantapi.NewIDUUID(id)
}

func pointIDToString(id *qdrantapi.PointId) string {
	if id == nil {
		return ""
	}
	if uuid := id.GetUuid(); uuid != "" {
		return uuid
	}
	return strconv.FormatUint(id.GetNum(), 10)
}

func decodeValue(v *qdrantapi.Value) any {
	if v == nil {
		return nil
	}

	switch k := v.GetKind().(type) {
	case *qdrantapi.Value_NullValue:
		return nil
	case *qdrantapi.Value_DoubleValue:
		return k.DoubleValue
	case *qdrantapi.Value_IntegerValue:
		return k.IntegerValue
	case *qdrantapi.Value_StringValue:
		return k.StringValue
	case *qdrantapi.Value_BoolValue:
		return k.BoolValue
	case *qdrantapi.Value_StructValue:
		m := make(map[string]any, len(k.StructValue.GetFields()))
		for fk, fv := range k.StructValue.GetFields() {
			m[fk] = decodeValue(fv)
		}
		return m
	case *qdrantapi.Value_ListValue:
		vals := k.ListValue.GetValues()
		out := make([]any, 0, len(vals))
		for _, item := range vals {
			out = append(out, decodeValue(item))
		}
		return out
	default:
		return nil
	}
}
