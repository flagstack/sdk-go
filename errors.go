package switchonyourcode

import "fmt"

type AuthenticationError struct{ Message string }

func (e *AuthenticationError) Error() string { return e.Message }

type HTTPError struct {
	Message    string
	StatusCode int
}

func (e *HTTPError) Error() string { return e.Message }

type ConfigurationError struct{ Message string }

func (e *ConfigurationError) Error() string { return e.Message }

type evaluationFailure struct {
	code EvaluationErrorCode
	err  error
}

func (e *evaluationFailure) Error() string { return e.err.Error() }
func (e *evaluationFailure) Unwrap() error { return e.err }

func evaluationError(code EvaluationErrorCode, format string, args ...any) error {
	return &evaluationFailure{code: code, err: fmt.Errorf(format, args...)}
}
