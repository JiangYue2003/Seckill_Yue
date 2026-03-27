package model

import "errors"

// 预定义的错误
var (
	ErrNotFound      = errors.New("record not found")
	ErrAlreadyExists = errors.New("record already exists")
	ErrInvalidParams = errors.New("invalid params")
)
