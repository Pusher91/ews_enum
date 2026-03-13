package ews

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKind describes a classified EWS failure type.
type ErrorKind int

const (
	ErrorKindUnknown ErrorKind = iota
	ErrorKindAuth
	ErrorKindResolveDenied
	ErrorKindServer
	ErrorKindParse
	ErrorKindTransport
)

// ResponseError is a typed EWS error that callers can inspect instead of
// string-matching free-form error messages.
type ResponseError struct {
	Op         string
	Kind       ErrorKind
	Code       string
	StatusCode int
	Detail     string
	Cause      error
}

func (e *ResponseError) Error() string {
	parts := []string{}
	if e.Op != "" {
		parts = append(parts, e.Op)
	}

	switch {
	case e.Code != "":
		parts = append(parts, fmt.Sprintf("EWS error: %s", e.Code))
	case e.StatusCode != 0 && e.Detail != "":
		parts = append(parts, fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Detail))
	case e.StatusCode != 0:
		parts = append(parts, fmt.Sprintf("HTTP %d", e.StatusCode))
	case e.Detail != "":
		parts = append(parts, e.Detail)
	case e.Cause != nil:
		parts = append(parts, e.Cause.Error())
	default:
		parts = append(parts, "unexpected error")
	}

	if e.Cause != nil && e.Code == "" && e.StatusCode == 0 && e.Detail != "" {
		parts = append(parts, e.Cause.Error())
	}

	return strings.Join(parts, ": ")
}

func (e *ResponseError) Unwrap() error {
	return e.Cause
}

func ErrorKindOf(err error) ErrorKind {
	var respErr *ResponseError
	if errors.As(err, &respErr) {
		return respErr.Kind
	}
	return ErrorKindUnknown
}

func classifyResponseCode(code string) ErrorKind {
	switch {
	case isAuthFailureCode(code):
		return ErrorKindAuth
	case isResolveDeniedCode(code):
		return ErrorKindResolveDenied
	case code != "":
		return ErrorKindServer
	default:
		return ErrorKindUnknown
	}
}

func isAuthFailureCode(code string) bool {
	switch code {
	case "ErrorAccountDisabled", "ErrorLogonFailure":
		return true
	default:
		return false
	}
}

func isResolveDeniedCode(code string) bool {
	switch code {
	case "ErrorAccessDenied", "ErrorNonExistentMailbox", "ErrorMailboxMoveInProgress":
		return true
	default:
		return false
	}
}
