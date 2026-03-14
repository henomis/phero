// Package simple provides a small in-memory message store for agents.
//
// It stores recent llm.Message values in a fixed-size FIFO ring buffer.
// Older messages are overwritten once the maximum capacity is reached.
package simple
