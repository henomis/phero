package psql

import "errors"

var (
	ErrNilDB            = errors.New("nil db")
	ErrEmptySessionID   = errors.New("empty session id")
	ErrEmptyTableName   = errors.New("empty table name")
	ErrInvalidTableName = errors.New("invalid table name")
)
