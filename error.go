package main

type ExitError struct {
	code int
}

func (e *ExitError) Error() string {
	return ""
}

func (e *ExitError) ExitCode() int {
	return e.code
}
