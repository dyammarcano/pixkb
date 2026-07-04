package econindex

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

const sgsDateFormat = "02/01/2006"

// DownloadLatest fetches the single most recent point of a BACEN SGS series.
// log may be nil.
func DownloadLatest(ctx context.Context, cfg SeriesConfig, log *slog.Logger) ([]byte, error) {
	url := fmt.Sprintf(cfg.BaseURL, cfg.Code) + "/dados/ultimos/1?formato=json"
	return fetchURL(ctx, cfg, url, log)
}

// DownloadRange fetches every point of a BACEN SGS series between from and
// to (inclusive, both dates). Callers with a span over cfg.MaxRangeYears
// must page via FetchHistory instead — BCB rejects longer single requests
// with HTTP 406 for daily series (confirmed live during design).
func DownloadRange(ctx context.Context, cfg SeriesConfig, from, to time.Time, log *slog.Logger) ([]byte, error) {
	url := fmt.Sprintf(cfg.BaseURL, cfg.Code) +
		fmt.Sprintf("/dados?formato=json&dataInicial=%s&dataFinal=%s",
			from.Format(sgsDateFormat), to.Format(sgsDateFormat))
	return fetchURL(ctx, cfg, url, log)
}

func fetchURL(ctx context.Context, cfg SeriesConfig, url string, log *slog.Logger) ([]byte, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d: %s", url, resp.StatusCode, body)
	}
	log.Info("econindex: downloaded series", "series", cfg.Code, "url", url, "bytes", len(body))
	return body, nil
}

// FetchHistory downloads and parses every point of a BACEN SGS series
// between from and to, paging the request in cfg.MaxRangeYears-sized windows
// to stay under BCB's per-request range cap (confirmed live: HTTP 406 over
// 10 years on a daily series). Results are merged and returned sorted
// ascending by date; a date present in more than one window (window
// boundaries are inclusive on both ends) keeps its last-fetched value.
func FetchHistory(ctx context.Context, cfg SeriesConfig, from, to time.Time, log *slog.Logger) ([]SeriesPoint, error) {
	if to.Before(from) {
		return nil, fmt.Errorf("invalid range: to (%s) before from (%s)", to.Format(sgsDateFormat), from.Format(sgsDateFormat))
	}
	maxYears := cfg.MaxRangeYears
	if maxYears <= 0 {
		maxYears = 10
	}

	byDate := map[time.Time]SeriesPoint{}
	windowStart := from
	for !windowStart.After(to) {
		windowEnd := windowStart.AddDate(maxYears, 0, -1)
		if windowEnd.After(to) {
			windowEnd = to
		}
		data, err := DownloadRange(ctx, cfg, windowStart, windowEnd, log)
		if err != nil {
			return nil, err
		}
		points, err := ParseSeries(data, cfg.Code, time.Now())
		if err != nil {
			return nil, err
		}
		for _, p := range points {
			byDate[p.Date] = p
		}
		windowStart = windowEnd.AddDate(0, 0, 1)
	}

	out := make([]SeriesPoint, 0, len(byDate))
	for _, p := range byDate {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}
