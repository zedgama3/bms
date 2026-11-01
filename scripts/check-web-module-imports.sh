#!/usr/bin/env bash
# A module's or core area's screens import only @/api, @/i18n, @/ui, @/usage, @/registry/types and
# themselves; never another set, core/*, shell/* or the lists; and call only their own
# /api/<name>/ and the core's routes (web-shell.md decision 4). grep only, no Node needed,
# so `make imports` can run it next to the Go layer check.
#
# Usage: check-web-module-imports.sh [path to web/src]
set -euo pipefail

src="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/web/src}"
# API prefixes any set may call: the shell's routes and the core areas' (module-contract: core contracts).
core_api=" modules admin client-errors openapi.json accounts access people departments events roles settings audit regional "
fail=0

report() {
  echo "$1:$2: $3" >&2
  fail=1
}

for kind in modules core; do
  [ -d "$src/$kind" ] || continue
  for dir in "$src/$kind"/*/; do
    [ -d "$dir" ] || continue
    dir="${dir%/}"
    name="$(basename "$dir")"
    while IFS= read -r -d '' file; do
      # Forbidden aliases: the frame, other sets, the lists.
      while IFS=: read -r line text; do
        [ -n "$line" ] && report "$file" "$line" "imports $(echo "$text" | grep -oE "@/(shell|core|modules)[^'\"]*" | head -1); a set imports only @/api, @/i18n, @/ui, @/usage, @/registry/types and itself"
      done < <(grep -nE "['\"]@/(shell|core|modules)(/[^'\"]*)?['\"]" "$file" || true)
      # @/api, @/i18n, @/ui and @/usage only as their index: subpaths such as @/api/internal are frame-private.
      while IFS=: read -r line text; do
        [ -n "$line" ] && report "$file" "$line" "imports $(echo "$text" | grep -oE "@/(api|i18n|ui|usage)/[^'\"]*" | head -1); the subpaths are frame-private"
      done < <(grep -nE "['\"]@/(api|i18n|ui|usage)/[^'\"]*['\"]" "$file" || true)
      # @/registry is allowed only as @/registry/types.
      while IFS=: read -r line text; do
        [ -n "$line" ] && report "$file" "$line" "imports $(echo "$text" | grep -oE "@/registry[^'\"]*" | head -1); only @/registry/types is allowed"
      done < <(grep -nE "['\"]@/registry(/[^'\"]*)?['\"]" "$file" | grep -vE "['\"]@/registry/types['\"]" || true)
      # Relative imports must stay inside the set's own folder.
      while IFS=: read -r line spec; do
        [ -n "$line" ] || continue
        spec="${spec//[\'\"]/}"
        resolved="$(realpath -m "$(dirname "$file")/$spec")"
        case "$resolved/" in
          "$dir"/*) ;;
          *) report "$file" "$line" "imports $spec, which leaves the set's folder" ;;
        esac
      done < <(grep -noE "['\"]\.{1,2}/[^'\"]*['\"]" "$file" || true)
      # Literal /api/<other>/ calls.
      while IFS=: read -r line hit; do
        [ -n "$line" ] || continue
        api="${hit#/api/}"
        if [ "$api" != "$name" ] && [[ "$core_api" != *" $api "* ]]; then
          report "$file" "$line" "names \"/api/$api\"; calling another module's API is importing it by another name"
        fi
      done < <(grep -noE "/api/[a-z0-9][a-z0-9.-]*" "$file" || true)
    done < <(find "$dir" -type f \( -name '*.ts' -o -name '*.tsx' \) ! -name '*.test.ts' ! -name '*.test.tsx' ! -name '*.d.ts' -print0)
  done
done

if [ "$fail" -ne 0 ]; then
  echo "web module isolation check failed (web-shell.md decision 4)" >&2
  exit 1
fi
echo "web module isolation: ok"
