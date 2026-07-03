// internal/ispb/str_test.go
package ispb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const strFixture = "\xEF\xBB\xBFISPB,Nome_Reduzido,Número_Código,Participa_da_Compe,Acesso_Principal,Nome_Extenso,Início_da_Operação\n" +
	"00000000,BCO DO BRASIL S.A.,001,Sim,RSFN,Banco do Brasil S.A.,22/04/2002\n" +
	"00038121,Selic,n/a,Não,RSFN,Banco Central do Brasil - Selic,22/04/2002\n" +
	"00122327,SANTINVEST S.A. - CFI,539,Não,RSFN,\"SANTINVEST S.A. - CREDITO, FINANCIAMENTO E INVESTIMENTOS\",17/04/2023\n" +
	"3456,SHORT CODE BANK,999,Sim,Internet,Short Code Bank S.A.,01/01/2020\n" +
	",NO ISPB AT ALL,100,Sim,RSFN,Should Be Skipped,01/01/2020\n"

func TestParseSTR(t *testing.T) {
	synced := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	records, err := ParseSTR([]byte(strFixture), synced)
	require.NoError(t, err)
	require.Len(t, records, 4, "5 data rows minus 1 skipped empty-ISPB row")

	brasil := records[0]
	assert.Equal(t, "00000000", brasil.ISPB)
	assert.Equal(t, "BCO DO BRASIL S.A.", brasil.Name)
	assert.Equal(t, "001", brasil.CompeCode)
	assert.True(t, brasil.ParticipatesCompe)
	assert.Equal(t, "RSFN", brasil.AccessType)
	assert.Equal(t, "Banco do Brasil S.A.", brasil.LegalName)
	assert.Equal(t, time.Date(2002, 4, 22, 0, 0, 0, 0, time.UTC), brasil.OperationStart)
	assert.Equal(t, synced, brasil.SyncedAt)

	selic := records[1]
	assert.Equal(t, "", selic.CompeCode, "n/a compe code normalizes to empty")
	assert.False(t, selic.ParticipatesCompe)

	santinvest := records[2]
	assert.Equal(t, "SANTINVEST S.A. - CREDITO, FINANCIAMENTO E INVESTIMENTOS", santinvest.LegalName,
		"quoted comma survives RFC4180 parsing")

	short := records[3]
	assert.Equal(t, "00003456", short.ISPB, "short code zero-padded to 8 digits")
}

func TestParseSTR_NoDataRows(t *testing.T) {
	_, err := ParseSTR([]byte("ISPB,Nome_Reduzido\n"), time.Now())
	assert.Error(t, err)
}

func TestDownloadSTR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strFixture))
	}))
	defer srv.Close()

	data, err := DownloadSTR(context.Background(), STRConfig{URL: srv.URL, HTTPTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)
	assert.Equal(t, strFixture, string(data))
}

func TestDownloadSTR_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := DownloadSTR(context.Background(), STRConfig{URL: srv.URL, HTTPTimeout: 5 * time.Second}, nil)
	assert.Error(t, err)
}
