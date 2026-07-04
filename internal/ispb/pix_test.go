package ispb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pixFixture uses Windows-1252 bytes: \xE3 = ã, \xC9 = É.
var pixFixture = []byte(
	"Lista de participantes em ades\xE3o ao Pix\n" +
		";ISPB;Nome Reduzido;CNPJ;Autorizada pelo BCB\n" +
		"1;00000000;BCO DO BRASIL S.A.;00000000000191;Sim\n" +
		"2;00204963;COOPERATIVA CR\xC9DITO;00204963000110;Sim\n" +
		"3;38166;BACEN;38166000105;Nao\n" +
		"4;;SEM ISPB;12345678000100;Sim\n")

func TestParsePix(t *testing.T) {
	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	records, err := ParsePix(pixFixture, DefaultPixConfig(), synced)
	require.NoError(t, err)
	require.Len(t, records, 3, "4 data rows minus 1 skipped empty-ISPB row")

	brasil := records[0]
	assert.Equal(t, "00000000", brasil.ISPB)
	assert.Equal(t, "BCO DO BRASIL S.A.", brasil.Name)
	assert.Equal(t, "00000000000191", brasil.CNPJ)
	assert.True(t, brasil.Authorized)
	assert.Equal(t, synced, brasil.SyncedAt)

	coop := records[1]
	assert.Equal(t, "COOPERATIVA CRÉDITO", coop.Name, "Windows-1252 0xC9 decodes to É")

	bacen := records[2]
	assert.Equal(t, "00038166", bacen.ISPB, "short code zero-padded to 8 digits")
	assert.False(t, bacen.Authorized, "Nao is not in AuthorizedValues")
}

func TestParsePix_NoDataRows(t *testing.T) {
	_, err := ParsePix([]byte(";ISPB;Nome Reduzido\n"), DefaultPixConfig(), time.Now())
	assert.Error(t, err)
}

// TestParsePix_StopsAtSecondEmbeddedTable covers BACEN's real live Pix CSV,
// which concatenates a second, differently-shaped table (pending/in-adhesion
// participants) after the active-participants table by re-stating the header
// row rather than starting a new file — discovered when a real sync errored
// on "invalid ISPB code \"0000ISPB\"" (the re-stated header's ISPB cell,
// zero-padded like a real code). Rows before the second header must still be
// parsed; the second header and anything after it must be dropped, not
// misparsed under the first table's column mapping.
func TestParsePix_StopsAtSecondEmbeddedTable(t *testing.T) {
	fixture := []byte("Lista de participantes ativos do Pix\n" +
		" ;Nome Reduzido;ISPB;CNPJ;Tipo de Instituição;Autorizada pelo BCB\n" +
		"1;99PAY IP S.A.;24313102;24.313.102/0001-25;Instituição de Pagamento;Sim\n" +
		" ;Nome Reduzido;ISPB;CNPJ;Tipo de Instituição;Status da adesão\n" +
		"1;PENDING BANK;12345678;12.345.678/0001-95;Banco;Em analise\n")

	records, err := ParsePix(fixture, DefaultPixConfig(), time.Now())
	require.NoError(t, err)
	require.Len(t, records, 1, "only the row before the second header table")
	assert.Equal(t, "24313102", records[0].ISPB)
}

// TestParsePix_InvalidCNPJStoredEmpty confirms a CNPJ that fails check-digit
// validation (e.g. corrupted source data) is dropped to "" rather than stored
// as garbage — the ISPB code + name are still the record's identity, CNPJ is
// supplementary metadata that must not silently persist a bad value.
func TestParsePix_InvalidCNPJStoredEmpty(t *testing.T) {
	fixture := []byte("Lista de participantes ativos do Pix\n" +
		" ;Nome Reduzido;ISPB;CNPJ;Tipo de Instituição;Autorizada pelo BCB\n" +
		"1;BAD CNPJ BANK;11111111;11.111.111/1111-11;Banco;Sim\n")

	records, err := ParsePix(fixture, DefaultPixConfig(), time.Now())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Empty(t, records[0].CNPJ, "invalid check digits must not be stored")
	assert.Equal(t, "11111111", records[0].ISPB, "the record itself is still kept")
}

// TestParsePix_NormalizesFormattedCNPJ covers BACEN's real live Pix CSV,
// which formats CNPJ with punctuation (e.g. "24.313.102/0001-25", 18 chars) —
// discovered when a real sync failed against the `cnpj VARCHAR(14)` column.
// The digits-only form is always exactly 14 characters (CNPJ's actual data
// type), so normalizing at parse time is correct, not lossy.
func TestParsePix_NormalizesFormattedCNPJ(t *testing.T) {
	fixture := []byte("Lista de participantes ativos do Pix\n" +
		" ;Nome Reduzido;ISPB;CNPJ;Tipo de Instituição;Autorizada pelo BCB\n" +
		"1;99PAY IP S.A.;24313102;24.313.102/0001-25;Instituição de Pagamento;Sim\n")

	records, err := ParsePix(fixture, DefaultPixConfig(), time.Now())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "24313102000125", records[0].CNPJ, "punctuation stripped, 14 digits")
}

func TestDownloadPix_ProbesDatesBackward(t *testing.T) {
	today := time.Now()
	validDate := today.AddDate(0, 0, -2).Format("20060102")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pix-"+validDate+".csv" {
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(pixFixture)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := PixConfig{
		BaseURL:          srv.URL + "/pix-%s.csv",
		HTTPTimeout:      5 * time.Second,
		MaxDaysBack:      10,
		CSVDelimiter:     ';',
		ColumnISPB:       "ISPB",
		ColumnName:       "Nome Reduzido",
		ColumnCNPJ:       "CNPJ",
		ColumnAuthorized: "Autorizada pelo BCB",
		AuthorizedValues: []string{"sim"},
	}
	data, url, err := DownloadPix(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, pixFixture, data)
	assert.Equal(t, fmt.Sprintf("%s/pix-%s.csv", srv.URL, validDate), url)
}

func TestDownloadPix_ExhaustsMaxDaysBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := DefaultPixConfig()
	cfg.BaseURL = srv.URL + "/pix-%s.csv"
	cfg.MaxDaysBack = 2
	cfg.HTTPTimeout = 5 * time.Second

	_, _, err := DownloadPix(context.Background(), cfg, nil)
	assert.Error(t, err)
}
