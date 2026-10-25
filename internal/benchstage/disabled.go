//go:build !benchstage

// Package benchstage marks opt-in measurement regions on the monotonic clock.
package benchstage

func Span(string) func()      { return func() {} }
func Values(string, int, int) {}

func Output(int, []byte) {}
