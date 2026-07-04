package query

import "testing"

func TestExpandQuery_OriginalAlwaysFirstAndUnmodified(t *testing.T) {
	t.Parallel()
	q := "consultar cobrança por txid"
	out := ExpandQuery(q)
	if len(out) == 0 || out[0] != q {
		t.Fatalf("expected original query first and unmodified, got %v", out)
	}
}

func TestExpandQuery_NoEntityMatch_OnlyOriginalAndRewrite(t *testing.T) {
	t.Parallel()
	// No recognized domain entity here -> at most [original, folded rewrite].
	out := ExpandQuery("prazos de implementação")
	if len(out) > 2 {
		t.Fatalf("expected at most 2 subqueries (original + rewrite), got %v", out)
	}
}

func TestExpandQuery_MatchesRefundEntityOnRealEvalCase(t *testing.T) {
	t.Parallel()
	// Exact case from eval/cases-fuzzy-ids.tsv — this is the query the spec's
	// worked example (Pix refund) names explicitly.
	out := ExpandQuery("como estornar um pix que recebi por engano")
	found := false
	for _, sq := range out {
		if sq == "devolução pix refund" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the refund entity subquery, got %v", out)
	}
}

func TestExpandQuery_MatchesWebhookEntity(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("notificar via webhook pix")
	if len(out) != 2 {
		t.Fatalf("expected exactly [original, webhook subquery], got %v", out)
	}
	if out[1] != "webhook notificação pix" {
		t.Fatalf("expected the webhook entity subquery second, got %v", out)
	}
}

func TestExpandQuery_CapsAtMaxSubqueries(t *testing.T) {
	t.Parallel()
	// Hits many entity stems at once (estorno, webhook, chave, api, endpoint,
	// certificado, qr, liquidacao) -> must still cap at maxSubqueries.
	out := ExpandQuery("estorno webhook chave api endpoint certificado qr liquidacao")
	if len(out) > maxSubqueries {
		t.Fatalf("expected at most %d subqueries, got %d: %v", maxSubqueries, len(out), out)
	}
}

func TestExpandQuery_Deterministic(t *testing.T) {
	t.Parallel()
	q := "como estornar um pix que recebi por engano"
	a := ExpandQuery(q)
	b := ExpandQuery(q)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at index %d: %v vs %v", i, a, b)
		}
	}
}

func TestExpandQuery_NoDuplicateSubqueries(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("estorno de devolução via refund")
	seen := map[string]bool{}
	for _, sq := range out {
		key := sq
		if seen[key] {
			t.Fatalf("duplicate subquery %q in %v", sq, out)
		}
		seen[key] = true
	}
}
