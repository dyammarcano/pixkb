package econindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/series-11/dados/ultimos/1", r.URL.Path)
		assert.Equal(t, "formato=json", r.URL.RawQuery)
		_, _ = w.Write([]byte(`[{"data":"03/07/2026","valor":"0.052531"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	data, err := DownloadLatest(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), "0.052531")
}

func TestDownloadRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/series-1/dados", r.URL.Path)
		assert.Equal(t, "01/01/2024", r.URL.Query().Get("dataInicial"))
		assert.Equal(t, "31/01/2024", r.URL.Query().Get("dataFinal"))
		_, _ = w.Write([]byte(`[{"data":"02/01/2024","valor":"5.10"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "1", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	data, err := DownloadRange(context.Background(), cfg, from, to, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), "5.10")
}

func TestDownloadRange_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error":"janela de consulta"}`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second}
	_, err := DownloadRange(context.Background(), cfg, time.Now(), time.Now(), nil)
	assert.Error(t, err)
}

func TestFetchHistory_PagesOverTenYears(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("dataInicial")
		requests = append(requests, start+".."+r.URL.Query().Get("dataFinal"))
		if start == "01/01/2010" {
			_, _ = w.Write([]byte(`[{"data":"01/01/2010","valor":"1.0"},{"data":"31/12/2019","valor":"2.0"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"data":"01/01/2020","valor":"3.0"},{"data":"01/01/2025","valor":"4.0"}]`))
	}))
	defer srv.Close()

	cfg := SeriesConfig{Code: "11", BaseURL: srv.URL + "/series-%s", HTTPTimeout: 5 * time.Second, MaxRangeYears: 10}
	from := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	points, err := FetchHistory(context.Background(), cfg, from, to, nil)
	require.NoError(t, err)
	require.Len(t, requests, 2, "a >10y range must page into exactly 2 windows")
	require.Len(t, points, 4)
	assert.True(t, sort.SliceIsSorted(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) }))
	assert.Equal(t, "4.0", points[3].Value)
}

func TestFetchHistory_InvalidRange(t *testing.T) {
	cfg := SeriesConfig{Code: "11", BaseURL: "http://unused/%s", HTTPTimeout: time.Second}
	_, err := FetchHistory(context.Background(), cfg, time.Now(), time.Now().AddDate(0, 0, -1), nil)
	assert.Error(t, err)
}
