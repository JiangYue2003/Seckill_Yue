package model

import "errors"

var (
	ErrNotFound      = errors.New("record not found")
	ErrAlreadyExists = errors.New("record already exists")
	ErrInvalidParams = errors.New("invalid params")
)
