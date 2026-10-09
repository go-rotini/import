#!/usr/bin/env bash
# Builds a go.work that tests the importers against the root module of the same commit,
# whatever root version their go.mod requires (released or not).
set -euo pipefail

root=github.com/go-rotini/import
go work init . ./cobra
for mod in cobra; do
	version=$(awk -v m="$root" '$1 == m { print $2 }' "$mod/go.mod")
	if [ -n "$version" ]; then
		go work edit -replace="$root@$version=."
	fi
done
