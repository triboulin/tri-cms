package storage

import "errors"

var (
	ErrNotFound      = errors.New("storage: resource not found")
	ErrAlreadyExists = errors.New("storage: resource already exists")
	ErrConflict      = errors.New("storage: conflicting state")
)
