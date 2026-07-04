package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/econindex"
)

func newEconIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "econindex",
		Short: "Fetch and look up BACEN economic-indicator series (SELIC, USD/BRL PTAX)",
	}
	cmd.AddCommand(newEconIndexFetchCmd(), newEconIndexLoadCmd(), newEconIndexSyncCmd(), newEconIndexLookupCmd())
	return cmd
}

func econIndexLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func defaultEconIndexPath(cfg Config, seriesName string) string {
	return filepath.Join(cfg.MirrorDir, "bacen-econindex", seriesName+".json")
}

// parseEconIndexDate parses a CLI-facing YYYY-MM-DD date flag into UTC midnight.
func parseEconIndexDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

func newEconIndexFetchCmd() *cobra.Command {
	var series, from, to, out string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download a BACEN SGS series (latest point, or a date range) and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if series == "" {
				return fmt.Errorf("--series is required")
			}
			cfg, err := econindex.FindSeriesConfig(series)
			if err != nil {
				return err
			}

			var data []byte
			switch {
			case from == "" && to == "":
				data, err = econindex.DownloadLatest(cmd.Context(), cfg, econIndexLogger())
			case from != "" && to != "":
				fromT, err2 := parseEconIndexDate(from)
				if err2 != nil {
					return err2
				}
				toT, err2 := parseEconIndexDate(to)
				if err2 != nil {
					return err2
				}
				data, err = econindex.DownloadRange(cmd.Context(), cfg, fromT, toT, econIndexLogger())
			default:
				return fmt.Errorf("--from and --to must be given together")
			}
			if err != nil {
				return err
			}

			cliCfg := loadConfig()
			path := out
			if path == "" {
				path = defaultEconIndexPath(cliCfg, cfg.Name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("create staging dir: %w", err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "staged %d bytes to %s\n", len(data), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&from, "from", "", "range start, YYYY-MM-DD (requires --to; default: latest point only)")
	cmd.Flags().StringVar(&to, "to", "", "range end, YYYY-MM-DD (requires --from)")
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-econindex/<series>.json)")
	return cmd
}

func newEconIndexLoadCmd() *cobra.Command {
	var series, file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged BACEN SGS JSON file and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if series == "" {
				return fmt.Errorf("--series is required")
			}
			cfg, err := econindex.FindSeriesConfig(series)
			if err != nil {
				return err
			}
			cliCfg := loadConfig()
			path := file
			if path == "" {
				path = defaultEconIndexPath(cliCfg, cfg.Name)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			points, err := econindex.ParseSeries(data, cfg.Code, time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.UpsertSeriesPoints(ctx, points); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d %s points\n", len(points), cfg.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-econindex/<series>.json)")
	return cmd
}

func newEconIndexSyncCmd() *cobra.Command {
	var series, from, to string
	var all bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download and load one (or all) BACEN SGS series in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliCfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()

			var targets []econindex.SeriesConfig
			if all {
				for _, cfg := range econindex.DefaultSeriesConfigs() {
					targets = append(targets, cfg)
				}
			} else {
				if series == "" {
					return fmt.Errorf("--series is required unless --all is given")
				}
				cfg, err := econindex.FindSeriesConfig(series)
				if err != nil {
					return err
				}
				targets = []econindex.SeriesConfig{cfg}
			}

			var fromT, toT time.Time
			ranged := from != "" || to != ""
			if ranged {
				if from == "" || to == "" {
					return fmt.Errorf("--from and --to must be given together")
				}
				if fromT, err = parseEconIndexDate(from); err != nil {
					return err
				}
				if toT, err = parseEconIndexDate(to); err != nil {
					return err
				}
			}

			for _, cfg := range targets {
				var data []byte
				if ranged {
					data, err = econindex.DownloadRange(ctx, cfg, fromT, toT, econIndexLogger())
				} else {
					data, err = econindex.DownloadLatest(ctx, cfg, econIndexLogger())
				}
				if err != nil {
					return err
				}
				points, err := econindex.ParseSeries(data, cfg.Code, time.Now())
				if err != nil {
					return err
				}
				if err := st.UpsertSeriesPoints(ctx, points); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d %s points\n", len(points), cfg.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&series, "series", "", "series key or SGS code (e.g. selic-diaria, 11)")
	cmd.Flags().StringVar(&from, "from", "", "range start, YYYY-MM-DD (requires --to; default: latest point only)")
	cmd.Flags().StringVar(&to, "to", "", "range end, YYYY-MM-DD (requires --from)")
	cmd.Flags().BoolVar(&all, "all", false, "sync every known series instead of a single --series")
	return cmd
}

func newEconIndexLookupCmd() *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "lookup <series>",
		Short: "Look up a stored BACEN SGS series point (latest, or a specific date)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := econindex.FindSeriesConfig(args[0])
			if err != nil {
				return err
			}
			cliCfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cliCfg)
			if err != nil {
				return err
			}
			defer st.Close()

			out := cmd.OutOrStdout()
			if date == "" {
				p, err := st.GetLatestSeriesPoint(ctx, cfg.Code)
				if err != nil {
					return err
				}
				return printEconIndexPoint(out, cfg, p)
			}

			d, err := parseEconIndexDate(date)
			if err != nil {
				return err
			}
			points, err := st.GetSeriesRange(ctx, cfg.Code, d, d)
			if err != nil {
				return err
			}
			if len(points) == 0 {
				return fmt.Errorf("no stored point for series %s on %s", cfg.Name, date)
			}
			return printEconIndexPoint(out, cfg, points[0])
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "exact date to look up, YYYY-MM-DD (default: latest stored point)")
	return cmd
}

func printEconIndexPoint(out io.Writer, cfg econindex.SeriesConfig, p econindex.SeriesPoint) error {
	_, err := fmt.Fprintf(out, "Series:   %s (%s, code %s)\nDate:     %s\nValue:    %s\nSynced:   %s\n",
		cfg.Name, cfg.Description, cfg.Code, p.Date.Format("2006-01-02"), p.Value, p.SyncedAt.Format(time.RFC3339))
	return err
}
