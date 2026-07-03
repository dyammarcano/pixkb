#!/usr/bin/env bash
# Codex-as-judge evaluation of the pixkb knowledge base.
#
# Claude authors eval/cases.tsv. For each case Codex runs the pixkb search(es)
# itself, judges relevance/precision against the intent, and returns a JSON
# verdict (eval/judge-schema.json) with concrete enhancement suggestions.
#
# Usage: bash eval/run-judge.sh [single-case-id]
#   CASES=eval/cases-fuzzy.tsv bash eval/run-judge.sh   # judge a different case set
set -uo pipefail

cd "$(dirname "$0")/.."          # repo root
set -a; [ -f .env ] && . ./.env; set +a   # PIXKB_DSN for pixkb -> remote KB

ONLY="${1:-}"
CASES="${CASES:-eval/cases.tsv}"          # case set to judge (override for the fuzzy set)
mkdir -p eval/out

# winpath converts a repo-relative path to the form codex's native process
# resolves. On Windows (git-bash) codex.exe cannot resolve forward-slash bash
# paths passed as --output-schema/-o args, so it fails with "os error 2" and
# every case is reported FAILED; cygpath -w fixes it. On Linux/macOS it is a
# no-op passthrough.
winpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }
WDIR=$(winpath "$PWD")
WSCHEMA=$(winpath eval/judge-schema.json)

REPORT=eval/report-$(basename "$CASES" .tsv).md
: > "$REPORT"
echo "# Codex KB judge report" >> "$REPORT"
echo >> "$REPORT"

while IFS=$'\t' read -r id query intent expect <&9; do
  [ -z "${id:-}" ] && continue
  case "$id" in \#*) continue ;; esac
  [ -n "$ONLY" ] && [ "$ONLY" != "$id" ] && continue

  echo ">> judging $id: $query"
  prompt="You are a STRICT evaluator of the pixkb knowledge base for Brazil Pix/SPB.
The repo root is the working directory and contains the binary bin/pixkb.exe.
Perform the search yourself: run  bin/pixkb.exe search \"$query\"
You MAY also run:  bin/pixkb.exe search \"$query\" --type ApiEndpoint|ManualSection|PacsMessage ,
  --mode fts|vector , or  bin/pixkb.exe related <concept-id>  to inspect the graph.
User intent: $intent
A strong result should surface: $expect
Judge whether the TOP results actually satisfy the intent. Score relevance (0-5)
and precision (0-5, penalise noisy/irrelevant top hits). Identify the top hit id.
Write a concise critique and concrete, actionable KB enhancements (better titles,
richer concept bodies, cross-links, ranking/embedder tweaks).
Use case_id=\"$id\" and query=\"$query\". Return ONLY the JSON for the output schema."

  # </dev/null: codex exec reads stdin and would otherwise drain the loop's
  # case file, stopping the loop after the first case.
  if codex exec --dangerously-bypass-approvals-and-sandbox -C "$WDIR" \
       --output-schema "$WSCHEMA" \
       -o "$(winpath "eval/out/$id.json")" "$prompt" </dev/null >"eval/out/$id.log" 2>&1; then
    echo "## $id — \`$query\`" >> "$REPORT"
    echo '```json' >> "$REPORT"
    cat "eval/out/$id.json" >> "$REPORT" 2>/dev/null
    echo '```' >> "$REPORT"
    echo >> "$REPORT"
  else
    echo "## $id — FAILED (see eval/out/$id.log)" >> "$REPORT"
    echo >> "$REPORT"
  fi
done 9< "$CASES"

echo "done -> $REPORT"
