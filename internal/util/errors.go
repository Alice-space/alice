package util

import "fmt"

type ConflictError struct {
	Message string
}

func (e ConflictError) Error() string {
	return e.Message
}

func NewConflict(format string, args ...any) error {
	return ConflictError{Message: fmt.Sprintf(format, args...)}
}

func IsConflict(err error) bool {
	_, ok := err.(ConflictError)
	return ok
}
