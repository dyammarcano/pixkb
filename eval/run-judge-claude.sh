#!/usr/bin/env bash
# Claude (sonnet)-as-judge variant of run-judge.sh. Claude Code has no
# --output-schema flag, so the schema is embedded in the prompt and the JSON
# object is extracted from stdout. Claude runs the pixkb searches itself via its
# Bash tool (headless, permissions bypassed).
#
# Usage: bash eval/run-judge-claude.sh [single-case-id]
set -uo pipefail

cd "$(dirname "$0")/.."
set -a; [ -f .env ] && . ./.env; set +a   # PIXKB_DSN -> remote KB

ONLY="${1:-}"
MODEL="${JUDGE_MODEL:-claude-sonnet-4-6}"
SCHEMA="$(cat eval/judge-schema.json)"
mkdir -p eval/out
REPORT=eval/report-claude.md
: > "$REPORT"
echo "# Claude ($MODEL) KB judge report" >> "$REPORT"
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
Write a concise critique and concrete, actionable KB enhancements.
Use case_id=\"$id\" and query=\"$query\".
Return ONLY a single JSON object (no prose, no code fences) validating this JSON Schema:
$SCHEMA"

  claude -p --model "$MODEL" --dangerously-skip-permissions "$prompt" </dev/null \
      >"eval/out/$id.raw" 2>"eval/out/$id.log"

  # Extract the first complete JSON object from the output.
  python - "eval/out/$id.raw" "eval/out/$id.json" <<'PY'
import sys, json, re
raw = open(sys.argv[1], encoding="utf-8", errors="replace").read()
m = re.search(r"\{.*\}", raw, re.DOTALL)
if m:
    try:
        obj = json.loads(m.group(0))
        json.dump(obj, open(sys.argv[2], "w", encoding="utf-8"))
        sys.exit(0)
    except Exception:
        pass
sys.exit(1)
PY
  if [ -s "eval/out/$id.json" ]; then
    echo "## $id — \`$query\`" >> "$REPORT"
    echo '```json' >> "$REPORT"; cat "eval/out/$id.json" >> "$REPORT"; echo >> "$REPORT"; echo '```' >> "$REPORT"; echo >> "$REPORT"
  else
    echo "## $id — FAILED (see eval/out/$id.log / .raw)" >> "$REPORT"; echo >> "$REPORT"
  fi
done 9< eval/cases.tsv

echo "done -> $REPORT"
