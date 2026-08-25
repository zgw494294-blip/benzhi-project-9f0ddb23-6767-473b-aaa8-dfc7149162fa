package domain

import "fmt"

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func Invalid(field, reason string) error {
	return &Error{Code: "validation_error", Message: fmt.Sprintf("%s: %s", field, reason)}
}

func Conflict(message string) error {
	return &Error{Code: "state_conflict", Message: message}
}

func NotFound(entity string) error {
	return &Error{Code: "not_found", Message: entity + "不存在"}
}
