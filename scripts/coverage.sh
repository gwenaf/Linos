#!/bin/sh
# Runs the Go tests and fails unless every function is fully covered. Excluded:
# - main() of the commands, which only wires things;
# - generated files, marked by Go's standard first line "// Code generated <tool>. DO NOT EDIT.";
# - a function whose doc comment ends with the directive "//coverage:ignore <reason>": an exception
#   decided and explained, reviewed like any code. Without a reason the directive does nothing.
# A package without tests counts as uncovered.
set -eu

module=$(go list -m)
profile=$(mktemp)
trap 'rm -f "$profile" "$profile.skip" "$profile.ignored"' EXIT

go test -coverprofile="$profile" ./...

# --untracked: a new file counts before it is added.
generated=$(git grep --untracked -lE '^// Code generated .* DO NOT EDIT\.$' -- '*.go' || true)
if [ -n "$generated" ]; then
	echo "$generated" | sed "s|^|$module/|; s|\$|:|" >"$profile.skip"
	grep -vFf "$profile.skip" "$profile" >"$profile.kept" || true
	mv "$profile.kept" "$profile"
fi

# The directive sits on the line before "func", where go tool cover locates the function.
git grep --untracked -nE '^//coverage:ignore +[^ ]' -- '*.go' |
	awk -F: -v m="$module" '{ print m "/" $1 ":" $2 + 1 ":" }' >"$profile.ignored" || true

missing=$(go tool cover -func="$profile" | awk -v cmd="^$module/cmd/[^/]+/main[.]go:" -v ignored="$profile.ignored" '
	BEGIN { while ((getline line < ignored) > 0) skip[line] = 1 }
	$1 == "total:" || ($1 ~ cmd && $2 == "main") || ($1 in skip) { next }
	$NF != "100.0%"')
if [ -n "$missing" ]; then
	echo "Couverture inférieure à 100 % :"
	echo "$missing"
	exit 1
fi
echo "Couverture : 100 %"
