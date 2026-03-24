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

// Package qdrant implements the vectorstore.Store interface using Qdrant.
//
// This package is a thin adapter around the official Qdrant Go client.
// Callers are expected to create and configure the Qdrant client and then
// inject it into New.
//
// A Store is bound to a single collection. EnsureCollection can be used to
// create the collection if it does not already exist.
//
// Basic usage:
//
//	qc, _ := qdrantapi.NewClient(&qdrantapi.Config{Host: "localhost", Port: 6334})
//	vs, _ := qdrant.New(qc, "my_collection", qdrant.WithVectorSize(1536))
//	_ = vs.EnsureCollection(ctx)
//	_ = vs.Upsert(ctx, []vectorstore.Point{{ID: "1", Vector: make([]float32, 1536)}})
//	res, _ := vs.Query(ctx, make([]float32, 1536), 5)
package qdrant
