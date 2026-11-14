#!/bin/sh
# Mutation testing with gremlins: small changes are made to the code (< becomes <=, a condition is
# negated…) and each one should make a test fail. A mutant that "LIVED" points at behaviour no test
# checks, even at 100 % coverage. Slow (minutes per package), so run on demand, not on each commit:
#   sh scripts/mutation.sh                     # every package
#   sh scripts/mutation.sh ./internal/game     # one package
#   sh scripts/mutation.sh -D main ./...       # only the code changed since main
# Only LIVED and TIMED OUT mutants are listed. "NOT COVERED" on a switch case is a gremlins blind spot
# (scripts/coverage.sh is the reference for coverage). Run by go run, so no dependency enters go.mod.
set -eu

# A generous timeout: tests run in parallel, and a slow machine would otherwise report false TIMED OUT.
# The timeout scales with a first test run: -count=1 keeps the test cache from making it look instant.
export GOFLAGS=-count=1
exec go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash \
	--timeout-coefficient 10 --output-statuses lt "${@:-./...}"
