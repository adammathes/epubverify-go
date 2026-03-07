#!/bin/bash
# Validate toolchain-generated EPUBs with both epubverify and epubcheck.
# Compares verdicts and error codes to find discrepancies.
#
# Usage: bash stress-test/toolchain-epubs/validate-epubs.sh
#
# Environment variables:
#   EPUBVERIFY    Path to epubverify binary (default: ./epubverify)
#   EPUBCHECK_JAR Path to epubcheck JAR (default: $HOME/tools/epubcheck-5.3.0/epubcheck.jar)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EPUB_DIR="${SCRIPT_DIR}/epubs"
RESULTS_DIR="${SCRIPT_DIR}/results"
EPUBVERIFY="${EPUBVERIFY:-${SCRIPT_DIR}/../../epubverify}"
EPUBCHECK_JAR="${EPUBCHECK_JAR:-${HOME}/tools/epubcheck-5.3.0/epubcheck.jar}"

# Verify tools exist
if [ ! -x "${EPUBVERIFY}" ]; then
  echo "ERROR: epubverify not found at ${EPUBVERIFY}"
  echo "  Run 'make build' first, or set EPUBVERIFY=/path/to/binary"
  exit 2
fi
if [ ! -f "${EPUBCHECK_JAR}" ]; then
  echo "ERROR: epubcheck JAR not found at ${EPUBCHECK_JAR}"
  echo "  Run 'bash scripts/install-epubcheck.sh' first"
  exit 2
fi

mkdir -p "${RESULTS_DIR}/epubverify" "${RESULTS_DIR}/epubcheck"

total=0
match=0
mismatch=0
ev_errors=0
ec_errors=0

echo "=== Validating toolchain-generated EPUBs ==="
echo ""
echo "epubverify: ${EPUBVERIFY}"
echo "epubcheck:  ${EPUBCHECK_JAR}"
echo ""

for epub in "${EPUB_DIR}"/*.epub; do
  [ -f "$epub" ] || continue
  name=$(basename "$epub" .epub)
  total=$((total + 1))

  # Run epubverify
  ev_json="${RESULTS_DIR}/epubverify/${name}.json"
  ev_stderr="${RESULTS_DIR}/epubverify/${name}.stderr"
  timeout 60 "${EPUBVERIFY}" "${epub}" --json "${ev_json}" 2>"${ev_stderr}" || true

  # Run epubcheck
  ec_json="${RESULTS_DIR}/epubcheck/${name}.json"
  ec_stderr="${RESULTS_DIR}/epubcheck/${name}.stderr"
  timeout 120 java -jar "${EPUBCHECK_JAR}" "${epub}" \
    --json "${ec_json}" 2>"${ec_stderr}" >/dev/null || true

  # Extract verdicts
  ev_valid="unknown"
  ec_valid="unknown"
  if [ -f "${ev_json}" ]; then
    ev_fatal=$(python3 -c "
import json, sys
try:
    d = json.load(open('${ev_json}'))
    msgs = d.get('messages', [])
    fatals = [m for m in msgs if m.get('severity') in ('fatal', 'error', 'FATAL', 'ERROR')]
    print(len(fatals))
except: print(-1)
" 2>/dev/null)
    if [ "${ev_fatal}" = "0" ]; then
      ev_valid="VALID"
    elif [ "${ev_fatal}" = "-1" ]; then
      ev_valid="CRASH"
    else
      ev_valid="INVALID(${ev_fatal})"
      ev_errors=$((ev_errors + ev_fatal))
    fi
  fi

  if [ -f "${ec_json}" ]; then
    ec_fatal=$(python3 -c "
import json, sys
try:
    d = json.load(open('${ec_json}'))
    msgs = d.get('messages', [])
    fatals = [m for m in msgs if m.get('severity') in ('fatal', 'error', 'FATAL', 'ERROR')]
    print(len(fatals))
except: print(-1)
" 2>/dev/null)
    if [ "${ec_fatal}" = "0" ]; then
      ec_valid="VALID"
    elif [ "${ec_fatal}" = "-1" ]; then
      ec_valid="CRASH"
    else
      ec_valid="INVALID(${ec_fatal})"
      ec_errors=$((ec_errors + ec_fatal))
    fi
  fi

  # Compare verdicts (VALID vs INVALID, ignoring error counts)
  ev_verdict=$(echo "${ev_valid}" | sed 's/(.*//')
  ec_verdict=$(echo "${ec_valid}" | sed 's/(.*//')

  if [ "${ev_verdict}" = "${ec_verdict}" ]; then
    status="MATCH"
    match=$((match + 1))
  else
    status="MISMATCH"
    mismatch=$((mismatch + 1))
  fi

  printf "  [%2d] %-40s  ev=%-15s ec=%-15s %s\n" \
    "${total}" "${name}" "${ev_valid}" "${ec_valid}" "${status}"
done

echo ""
echo "=== Validation Summary ==="
echo "  Total EPUBs: ${total}"
echo "  Verdict match: ${match}"
echo "  Verdict mismatch: ${mismatch}"
echo "  Agreement rate: $(( match * 100 / total ))%"
echo ""
echo "  Results saved to: ${RESULTS_DIR}/"
echo ""

# Generate detailed diff report
echo "=== Detailed Error Code Analysis ==="
echo ""

for epub in "${EPUB_DIR}"/*.epub; do
  [ -f "$epub" ] || continue
  name=$(basename "$epub" .epub)
  ev_json="${RESULTS_DIR}/epubverify/${name}.json"
  ec_json="${RESULTS_DIR}/epubcheck/${name}.json"

  if [ ! -f "${ev_json}" ] || [ ! -f "${ec_json}" ]; then
    continue
  fi

  python3 -c "
import json, sys

ev = json.load(open('${ev_json}'))
ec = json.load(open('${ec_json}'))

ev_msgs = ev.get('messages', [])
ec_msgs = ec.get('messages', [])

# Extract error/fatal check IDs
ev_checks = set()
for m in ev_msgs:
    sev = m.get('severity', '').upper()
    if sev in ('ERROR', 'FATAL'):
        cid = m.get('ID', m.get('id', ''))
        if cid:
            ev_checks.add(cid)

ec_checks = set()
for m in ec_msgs:
    sev = m.get('severity', '').upper()
    if sev in ('ERROR', 'FATAL'):
        cid = m.get('ID', m.get('id', ''))
        if cid:
            ec_checks.add(cid)

ev_only = ev_checks - ec_checks
ec_only = ec_checks - ev_checks
both = ev_checks & ec_checks

if ev_only or ec_only:
    print(f'  {\"${name}\"}:')
    if both:
        print(f'    Both: {sorted(both)}')
    if ev_only:
        print(f'    epubverify-only: {sorted(ev_only)}')
    if ec_only:
        print(f'    epubcheck-only: {sorted(ec_only)}')
    print()
" 2>/dev/null || true
done

echo "=== Done ==="
