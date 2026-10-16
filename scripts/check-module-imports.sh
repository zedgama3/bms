#!/usr/bin/env bash
# Fails if any package under internal/modules/<a> imports internal/modules/<b> (a != b).
# Modules talk through core contracts and domain events (plan/07-abstractions.md).
set -euo pipefail
mod=$(go list -m)
prefix="$mod/internal/modules/"
listing=$(go list -test -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./...)
fail=0
while read -r pkg imports; do
  [[ "$pkg" == "$prefix"* ]] || continue
  own=${pkg#"$prefix"}; own=${own%%/*}
  for imp in $imports; do
    [[ "$imp" == "$prefix"* ]] || continue
    other=${imp#"$prefix"}; other=${other%%/*}
    if [[ "$other" != "$own" ]]; then
      echo "$pkg imports $imp: a module never imports another module"
      fail=1
    fi
  done
done <<< "$listing"
exit $fail
