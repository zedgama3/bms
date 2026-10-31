#!/usr/bin/env bash
# Prints the facts a reviewer needs for the whole-repo architecture review
# (plan/design/boundaries.md, "make arch-report and the architecture review").
# Read-only, no network, no judgment: it reports, it never fails on what it finds.
#   (a) import edges among internal/core/*, internal/contracts/*, internal/api, internal/app
#   (b) production line counts per package and the ten largest production files
#   (c) every Options struct under internal/ with its field count
#   (d) the result of the seam test
set -euo pipefail
cd "$(dirname "$0")/.."
mod=$(go list -m)
# Production code only: no -test, so no _test.go imports. The data is the build's, not a guess.
listing=$(go list -f '{{.ImportPath}}|{{.Dir}}|{{join .Imports " "}}|{{join .GoFiles " "}}' ./internal/...)

echo "== (a) import edges (from -> to) among core, contracts, api, app =="
echo "$listing" | awk -F'|' -v mod="$mod/" '
  function tracked(p) { return p ~ /^internal\/(core|contracts)\// || p == "internal/api" || p == "internal/app" }
  {
    from = $1; sub("^" mod, "", from)
    if (!tracked(from)) next
    n = split($3, imps, " ")
    for (i = 1; i <= n; i++) {
      to = imps[i]
      if (index(to, mod) != 1) continue
      sub("^" mod, "", to)
      if (tracked(to)) print from " -> " to
    }
  }' | LC_ALL=C sort

echo
echo "== (b) production lines per package =="
files=$(echo "$listing" | while IFS='|' read -r pkg dir _ gofiles; do
  rel=${pkg#"$mod"/}
  for f in $gofiles; do
    printf '%s %s\n' "$rel" "$dir/$f"
  done
done)
echo "$files" | while read -r pkg path; do
  [ -n "$pkg" ] || continue
  printf '%s %s\n' "$(wc -l < "$path" | tr -d ' ')" "$pkg"
done | awk '{ total[$2] += $1 } END { for (p in total) printf "%7d  %s\n", total[p], p }' | LC_ALL=C sort -k2

echo
echo "== (b) ten largest production files =="
echo "$files" | while read -r pkg path; do
  [ -n "$pkg" ] || continue
  printf '%s %s\n' "$(wc -l < "$path" | tr -d ' ')" "${path#"$PWD"/}"
done | LC_ALL=C sort -rn | head -10 | awk '{ printf "%7d  %s\n", $1, $2 }'

echo
echo "== (c) Options structs and their field counts =="
# A field is a line at one tab of indent inside "type Options struct {", not a comment.
# A struct nested in Options would be counted as one field per line; the reviewer reads the file.
find internal -name '*.go' ! -name '*_test.go' | LC_ALL=C sort | while read -r f; do
  awk -v file="$f" '
    /^type Options struct \{/ { inside = 1; n = 0; start = FNR; next }
    inside && /^\}/ { printf "%3d fields  %s:%d\n", n, file, start; inside = 0; next }
    inside && /^\t[A-Za-z_]/ { n++ }
  ' "$f"
done

echo
echo "== (d) seam test (TestSeamTable_MatchesApp) =="
if go test -count=1 -run 'TestSeamTable_MatchesApp' ./internal/app/ 2>&1; then
  echo "seam test: PASS"
else
  echo "seam test: FAIL"
fi
