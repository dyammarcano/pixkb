#!/usr/bin/env bash
# Codex-as-judge evaluation of the pixkb RAG layer (pixkb ask / kb_ask).
#
# For each case in eval/cases-rag.tsv: run `pixkb ask --json` to produce a
# grounded answer + citations, then have Codex judge it against the RAG rubric
# (eval/rag-judge-schema.json): relevance, FAITHFULNESS (every claim supported by
# the cited concept), citation accuracy, and correct-refusal on out-of-domain
# cases. Faithfulness is the whole game for a normative KB.
#
# Usage: ANSWERER=claude bash eval/run-rag-judge.sh [single-case-id]
#   ANSWERER selects the answerer backend (claude|codex|agy); default claude.
set -uo pipefail

cd "$(dirname "$0")/.."          # repo root
set -a; [ -f .env ] && . ./.env; set +a   # PIXKB_DSN -> remote KB

ONLY="${1:-}"
ANSWERER="${ANSWERER:-claude}"
CASES="${CASES:-eval/cases-rag.tsv}"
mkdir -p eval/out

winpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }
WSCHEMA=$(winpath eval/rag-judge-schema.json)

REPORT=eval/report-rag.md
: > "$REPORT"
echo "# Codex RAG judge report" >> "$REPORT"
echo >> "$REPORT"

while IFS=$'\t' read -r id question expect <&9; do
  [ -z "${id:-}" ] && continue
  case "$id" in \#*) continue ;; esac
  [ -n "$ONLY" ] && [ "$ONLY" != "$id" ] && continue

  echo ">> asking $id: $question"
  # Produce the grounded answer + citations as JSON.
  answer_json=$(bin/pixkb.exe ask "$question" --provider "$ANSWERER" --json 2>/dev/null)

  prompt="You are a STRICT evaluator of a RAG answer from the pixkb Pix/SPB knowledge base.
Question: $question
Expectation: $expect   (answer:<id> = should answer grounded on that concept; refuse = should refuse as out-of-domain / not in the KB)

The system returned this JSON (answer, refused, citations[]):
$answer_json

You MAY verify by running, in the repo working directory:
  bin/pixkb.exe concept_get <id>   (read a cited concept's full text)
  bin/pixkb.exe search \"$question\"

Judge against the rubric and emit ONLY the JSON verdict:
- answered: did it answer (true) or refuse (false)?
- relevance 0-5: does the answer address the question?
- faithfulness 0-5: is EVERY claim supported by the cited concept(s)? Fabrication = 0.
- citation_accuracy 0-5: do the cited ids actually support the claims?
- correct_refusal: for a 'refuse' expectation, true iff it refused; for an 'answer' expectation, true iff it did NOT wrongly refuse.
- verdict pass|weak|fail, and a short critique.
For a 'refuse' case, the CORRECT outcome is refused=true: score relevance/faithfulness 5 and correct_refusal=true if it refused, else fail."

  codex exec --output-schema "$WSCHEMA" "$prompt" 2>/dev/null \
    | tee -a "$REPORT" >/dev/null
  echo >> "$REPORT"
done 9< "$CASES"

echo "done -> $REPORT"
