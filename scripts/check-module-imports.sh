#!/usr/bin/env bash
# Fails if any package under internal/modules/<a> imports internal/modules/<b> (a != b).
# Modules talk through core contracts and domain events (plan/07-abstractions.md).
set -euo pipefail
mod=$(go list -m)
prefix="$mod/internal/modules/"
listing=$(go list -test -tags integration,e2ehang -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./...)
fail=0
while read -r pkg imports; do
  [[ "$pkg" == "$prefix"* ]] || continue
  own=${pkg#"$prefix"}; own=${own%%/*}; own=${own%.test}; own=${own%_test}  # go list -test adds <module>.test and <module>_test packages
  for imp in $imports; do
    [[ "$imp" == "$prefix"* ]] || continue
    other=${imp#"$prefix"}; other=${other%%/*}; other=${other%.test}; other=${other%_test}
    if [[ "$other" != "$own" ]]; then
      echo "$pkg imports $imp: a module never imports another module"
      fail=1
    fi
  done
done <<< "$listing"
exit $fail
