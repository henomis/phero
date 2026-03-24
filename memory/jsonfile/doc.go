// Package jsonfile provides a file-backed message store for agents.
//
// It stores llm.Message values in an unbounded slice that is persisted as
// JSON to a file on disk. The file path acts as the session identifier:
// different files represent different sessions.
package jsonfile
