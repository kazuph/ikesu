// Package perf holds the performance-measurement tests required by
// SPEC.md "P1. 性能要件 / 計測用のテスト". They are behind the "perf" build
// tag so a plain "go test ./..." never runs them. They are meant to run on
// the bench Mac mini or Linux bench host (ubuntu-3090 while mini is unavailable):
//
//	go test -tags perf ./tests/perf -run . -timeout 30m
//
// IKESU_PERF_BIN picks the ikesu under test, IKESU_PERF_BASELINE_BIN the version
// compared against for the relative check, and IKESU_PERF_OUT an optional
// file that also receives the printed result tables.
package perf
