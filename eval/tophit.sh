#!/usr/bin/env bash
# Deterministic top-hit harness — variance-free recall/precision measurement.
#
# The codex judge is too noisy at ~20 cases to detect a ranking change (a single
# query swinging ±2 on a 0–5 scale dwarfs the effect). This harness instead asks
# a deterministic question: for each (query, expected-concept-id) pair, at what
# RANK does the expected concept appear in `pixkb search`? It reports top@1,
# top@5, and MRR — same inputs always give the same numbers, so a before/after
# diff is real signal, not judge variance.
#
# Case file format:  query<TAB>id1[,id2,...]    (# comments / blank lines skipped)
# Multiple ids = any of them is an acceptable hit; rank is the best (lowest).
#
# Usage:  bash eval/tophit.sh eval/cases-precise-ids.tsv [--mode fts|vector|hybrid]
set -uo pipefail
cd "$(dirname "$0")/.."
set -a; [ -f .env ] && . ./.env; set +a   # PIXKB_DSN

CASES="${1:?usage: tophit.sh <cases-ids.tsv> [--mode MODE]}"
MODE=""
[ "${2:-}" = "--mode" ] && MODE="--mode ${3:?mode value}"
BIN=bin/pixkb.exe
[ -f "$BIN" ] || { echo "build $BIN first: go build -o $BIN ./cmd/pixkb"; exit 1; }

n=0; t1=0; t5=0; mrr=0
printf '%-22s %5s  %s\n' "case" "rank" "query"
while IFS=$'\t' read -r query ids; do
  [ -z "${query:-}" ] && continue
  case "$query" in \#*) continue ;; esac
  [ -z "${ids:-}" ] && continue
  out=$($BIN search "$query" $MODE 2>/dev/null)
  best=0
  IFS=',' read -ra want <<< "$ids"
  for id in "${want[@]}"; do
    # rank = first column of the line whose 2nd column equals the id
    r=$(awk -v id="$id" '$2==id{print $1; exit}' <<< "$out")
    [ -n "$r" ] && { [ "$best" -eq 0 ] && best=$r || [ "$r" -lt "$best" ] && best=$r; }
  done
  n=$((n+1))
  if [ "$best" -gt 0 ]; then
    [ "$best" -le 1 ] && t1=$((t1+1))
    [ "$best" -le 5 ] && t5=$((t5+1))
    mrr=$(awk -v m="$mrr" -v r="$best" 'BEGIN{printf "%.4f", m + 1.0/r}')
    printf '%-22s %5s  %.50s\n' "${ids%%,*}" "$best" "$query"
  else
    printf '%-22s %5s  %.50s\n' "${ids%%,*}" "—" "$query"
  fi
done < "$CASES"

echo "----"
awk -v n="$n" -v t1="$t1" -v t5="$t5" -v mrr="$mrr" 'BEGIN{
  if(n==0){print "no cases"; exit}
  printf "cases=%d  top@1=%d (%.0f%%)  top@5=%d (%.0f%%)  MRR=%.3f\n", n, t1, 100*t1/n, t5, 100*t5/n, mrr/n
}'
