#!/usr/bin/env bash

set -euo pipefail

# Allowlist of called vulnerabilities with no upstream fix available.
readonly DEFAULT_ALLOWLIST=(
  "GO-2026-5932"
  "GO-2026-5622"
  "GO-2026-5338"
  "GO-2026-5064"
  "GO-2026-5026"
  "GO-2026-5972"
  "GO-2026-6088"
  "GO-2026-6089"
  "GO-2026-6090"
  "GO-2026-6218"
)

allowlist=("${DEFAULT_ALLOWLIST[@]}")

govulncheck_bin=""
if command -v govulncheck >/dev/null 2>&1; then
  govulncheck_bin="$(command -v govulncheck)"
elif command -v go >/dev/null 2>&1 && [[ -x "$(go env GOPATH)/bin/govulncheck" ]]; then
  govulncheck_bin="$(go env GOPATH)/bin/govulncheck"
else
  echo "govulncheck not found in PATH or \$(go env GOPATH)/bin" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found in PATH" >&2
  exit 1
fi

echo "Allowlisted OSV IDs: ${allowlist[*]}"

echo
echo "== govulncheck text report (informational; non-blocking) =="
set +e
"${govulncheck_bin}" ./...
text_rc=$?
set -e
echo "(informational govulncheck exit code: ${text_rc}; gate verdict uses JSON filter below)"

allowlist_json="$(printf '%s\n' "${allowlist[@]}" | jq -R . | jq -s .)"
json_file="$(mktemp)"
trap 'rm -f "${json_file}"' EXIT

set +e
"${govulncheck_bin}" -format json ./... >"${json_file}"
json_rc=$?
set -e

if [[ ${json_rc} -ne 0 && ${json_rc} -ne 3 ]]; then
  echo "govulncheck -format json exited with unexpected code ${json_rc}" >&2
  exit "${json_rc}"
fi
echo "(json govulncheck exit code: ${json_rc}; proceeding to jq policy evaluation)"

offenders="$(jq -r --argjson allowlist "${allowlist_json}" '
  select(has("finding"))
  | .finding
  | select(any(.trace[]?; .function != null))
  | . as $f
  | ($allowlist | index($f.osv)) as $is_allowlisted
  | if $is_allowlisted != null then
      empty
    elif $f.fixed_version == null then
      "\($f.osv)\tcalled_no_fix_not_allowlisted"
    else
      "\($f.osv)\tcalled_fix_available_not_allowlisted\t\($f.fixed_version)"
    end
' "${json_file}" | sort -u)"

if [[ -n "${offenders}" ]]; then
  echo
  echo "govulncheck gate FAILED"
  echo "Called vulnerabilities not covered by policy:"
  while IFS= read -r line; do
    echo "  - ${line}"
  done <<<"${offenders}"
  exit 1
fi

echo
echo "govulncheck gate PASSED"
echo "All called vulnerabilities are either allowlisted N/A findings or absent."
