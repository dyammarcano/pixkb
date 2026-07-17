package ingest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pixkb/internal/okf"
)

type isoSpecSource struct {
	defs []MsgDef
}

// NewISOSpecSource builds a Source that emits one OKF concept per ISO-20022
// message definition. Pass DefaultMsgDefs() for the Pix-relevant set.
func NewISOSpecSource(defs []MsgDef) Source {
	return &isoSpecSource{defs: defs}
}

func (s *isoSpecSource) Name() string { return "iso-spec" }

func (s *isoSpecSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	out := make([]okf.Concept, 0, len(s.defs))
	for _, d := range s.defs {
		if d.ID == "" || d.Family == "" {
			return nil, fmt.Errorf("iso-spec: invalid msgdef %q", d.ID)
		}
		body := renderMsgBody(d)
		out = append(out, okf.Concept{
			ID:          "messages/" + d.ID + ".md",
			Type:        msgType(d.Family),
			Title:       d.Title,
			Description: d.Summary,
			Resource:    "https://www.iso20022.org/iso-20022-message-definitions?search=" + d.ID,
			Tags:        []string{"iso20022", d.Family, strings.ReplaceAll(d.ID, ".", "")},
			Language:    "en",
			SourceURI:   "iso20022:" + d.ID,
			Body:        body,
			ContentSHA:  okf.ComputeSHA(body),
			Links:       siblingLinkIDs(d.Links),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// siblingLinkIDs resolves related message ids (e.g. "pacs.002") to
// bundle-relative concept ids under messages/ (e.g. "messages/pacs.002.md").
func siblingLinkIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, "messages/"+id+".md")
	}
	return out
}

func msgType(family string) string {
	switch family {
	case "pacs":
		return "PacsMessage"
	case "camt":
		return "CamtMessage"
	default:
		return "IsoMessage"
	}
}

func renderMsgBody(d MsgDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n%s\n\n", d.ID, d.Title, d.Summary)
	if d.Intent != "" {
		// Bilingual intent phrases so pt and en natural-language queries surface
		// the right ISO message (e.g. "credit transfer interbank settlement").
		fmt.Fprintf(&b, "Termos / Terms: %s\n\n", d.Intent)
	}
	if len(d.Fields) > 0 {
		b.WriteString("## Key fields\n\n| Field | Description |\n|-------|-------------|\n")
		for _, f := range d.Fields {
			name, desc := f, ""
			if n, dsc, ok := strings.Cut(f, " — "); ok {
				name, desc = n, dsc
			}
			fmt.Fprintf(&b, "| `%s` | %s |\n", name, desc)
		}
		b.WriteString("\n")
	}
	if len(d.Links) > 0 {
		b.WriteString("## Related messages\n\n")
		for _, l := range d.Links {
			// Link target must be the bundle-relative concept id (messages/<id>.md)
			// so hygiene resolves it against the messages/-prefixed ids; the link
			// text stays the bare message name for readability.
			fmt.Fprintf(&b, "- [%s](messages/%s.md)\n", l, l)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// DefaultMsgDefs returns the curated Pix/SPB-relevant ISO-20022 message set
// (the messages a Pix participant encounters: send, status, return, request).
func DefaultMsgDefs() []MsgDef {
	return []MsgDef{
		{ID: "pacs.008", Family: "pacs", Title: "FI to FI Customer Credit Transfer",
			Summary: "Credit transfer between financial institutions on behalf of a customer — the Pix payment order itself.",
			Intent:  "credit transfer interbank settlement, customer credit transfer, FI to FI, transferência de crédito interbancária, ordem de pagamento Pix, liquidação interbancária",
			Fields:  []string{"InstgAgt — instructing agent (ISPB)", "InstdAgt — instructed agent (ISPB)", "IntrBkSttlmAmt — settlement amount", "EndToEndId — end-to-end identifier", "ChrgBr — charge bearer"},
			Links:   []string{"pacs.002", "pacs.004"}},
		{ID: "pacs.002", Family: "pacs", Title: "FI to FI Payment Status Report",
			Summary: "Status of a payment instruction: accepted, rejected, pending, or settled. Pix confirmation/rejection.",
			Intent:  "payment status report, transaction status, accepted rejected pending settled, relatório de status de pagamento, confirmação rejeição Pix",
			Fields:  []string{"OrgnlEndToEndId — original e2e id", "TxSts — transaction status (ACSC/RJCT/...)", "StsRsnInf — status reason"},
			Links:   []string{"pacs.008"}},
		{ID: "pacs.004", Family: "pacs", Title: "Payment Return",
			Summary: "Return of funds after settlement — used for Pix devolução (refund).",
			Intent:  "payment return, reversal of funds, refund after settlement, devolução de Pix, retorno de pagamento, estorno",
			Fields:  []string{"OrgnlEndToEndId — original e2e id", "RtrdIntrBkSttlmAmt — returned amount", "RtrRsnInf — return reason"},
			Links:   []string{"pacs.008", "camt.056"}},
		{ID: "pacs.007", Family: "pacs", Title: "FI to FI Payment Reversal",
			Summary: "Request to reverse/cancel a previously settled payment.",
			Intent:  "payment reversal, cancel settled payment, reversão de pagamento, cancelamento de liquidação",
			Fields:  []string{"OrgnlEndToEndId — original e2e id", "RvslRsnInf — reversal reason"},
			Links:   []string{"pacs.008"}},
		{ID: "pacs.009", Family: "pacs", Title: "FI Credit Transfer",
			Summary: "Credit transfer between financial institutions for liquidity/settlement (RTGS/STR), not customer Pix.",
			Intent:  "FI credit transfer, liquidity settlement RTGS STR, transferência entre instituições, liquidação no SPI reservas",
			Fields:  []string{"InstgAgt — instructing agent", "InstdAgt — instructed agent", "IntrBkSttlmAmt — settlement amount"},
			Links:   []string{"pacs.002"}},
		{ID: "pacs.003", Family: "pacs", Title: "FI to FI Customer Direct Debit",
			Summary: "Direct debit collection between financial institutions on behalf of a customer.",
			Intent:  "direct debit collection, débito direto, cobrança por débito",
			Fields:  []string{"InstdAgt — instructed agent", "IntrBkSttlmAmt — settlement amount", "MndtRltdInf — mandate info"},
			Links:   []string{"pacs.002"}},
		{ID: "pacs.010", Family: "pacs", Title: "FI Direct Debit",
			Summary: "Direct debit between financial institutions (no customer).",
			Intent:  "FI direct debit, débito direto interbancário",
			Fields:  []string{"InstgAgt — instructing agent", "IntrBkSttlmAmt — settlement amount"},
			Links:   []string{"pacs.002"}},
		{ID: "camt.056", Family: "camt", Title: "FI to FI Payment Cancellation Request",
			Summary: "Request to cancel/return a payment — drives Pix devolução requests.",
			Intent:  "payment cancellation request, solicitação de cancelamento, pedido de devolução Pix",
			Fields:  []string{"OrgnlEndToEndId — original e2e id", "CxlRsnInf — cancellation reason"},
			Links:   []string{"camt.029", "pacs.004"}},
		{ID: "camt.029", Family: "camt", Title: "Resolution of Investigation",
			Summary: "Response to a cancellation/return request (camt.056): accepted or rejected.",
			Intent:  "resolution of investigation, cancellation response, resposta a solicitação de cancelamento",
			Fields:  []string{"OrgnlEndToEndId — original e2e id", "CxlStsRsnInf — cancellation status reason"},
			Links:   []string{"camt.056"}},
		{ID: "camt.052", Family: "camt", Title: "Bank to Customer Account Report",
			Summary: "Intraday account report (balances and entries) sent by a bank to its customer — account movement reporting.",
			Intent:  "account statement notification, account report, intraday balance and entries, extrato de conta intradia, relatório de conta, notificação de movimentação",
			Fields:  []string{"Acct — account", "Bal — balance", "Ntry — statement entry", "CdtDbtInd — credit/debit indicator"},
			Links:   []string{"camt.053", "camt.054"}},
		{ID: "camt.053", Family: "camt", Title: "Bank to Customer Statement",
			Summary: "End-of-day account statement (booked balances and entries) sent by a bank to its customer.",
			Intent:  "account statement, end of day statement, bank to customer statement, extrato bancário de fim de dia, demonstrativo de conta, statement camt",
			Fields:  []string{"Acct — account", "Bal — booked balance", "Ntry — statement entry", "Amt — amount"},
			Links:   []string{"camt.052", "camt.054"}},
		{ID: "camt.054", Family: "camt", Title: "Bank to Customer Debit/Credit Notification",
			Summary: "Notification of individual debit or credit entries on a customer account — credit/debit advice.",
			Intent:  "debit credit notification, credit advice, account notification, aviso de crédito e débito, notificação de lançamento, statement notification",
			Fields:  []string{"Acct — account", "Ntry — entry", "CdtDbtInd — credit/debit indicator", "Amt — amount"},
			Links:   []string{"camt.052", "camt.053"}},
	}
}
