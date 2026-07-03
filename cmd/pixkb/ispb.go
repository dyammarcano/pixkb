package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"pixkb/internal/ispb"
)

func newISPBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ispb",
		Short: "Map BACEN ISPB codes to SPB/Pix participant institutions",
	}
	cmd.AddCommand(newISPBSTRCmd(), newISPBPixCmd(), newISPBSyncCmd(), newISPBLookupCmd())
	return cmd
}

func ispbLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func defaultISPBPath(cfg Config, name string) string {
	return filepath.Join(cfg.MirrorDir, "bacen-ispb", name)
}

func newISPBSTRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "str",
		Short: "STR participants source (canonical, all SPB participants)",
	}
	cmd.AddCommand(newISPBSTRFetchCmd(), newISPBSTRLoadCmd(), newISPBSTRSyncCmd())
	return cmd
}

func newISPBSTRFetchCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the STR participants CSV and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := out
			if path == "" {
				path = defaultISPBPath(cfg, "str-participants.csv")
			}
			data, err := ispb.DownloadSTR(cmd.Context(), ispb.DefaultSTRConfig(), ispbLogger())
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-ispb/str-participants.csv)")
	return cmd
}

func newISPBSTRLoadCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged STR participants CSV and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := file
			if path == "" {
				path = defaultISPBPath(cfg, "str-participants.csv")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			records, err := ispb.ParseSTR(data, time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertSTR(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d STR participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-ispb/str-participants.csv)")
	return cmd
}

func newISPBSTRSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download and load the STR participants CSV in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			ctx := cmd.Context()
			data, err := ispb.DownloadSTR(ctx, ispb.DefaultSTRConfig(), ispbLogger())
			if err != nil {
				return err
			}
			records, err := ispb.ParseSTR(data, time.Now())
			if err != nil {
				return err
			}
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertSTR(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d STR participants\n", len(records))
			return nil
		},
	}
}

func newISPBPixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pix",
		Short: "Pix participants source (Pix adherents, BCB-authorization flag)",
	}
	cmd.AddCommand(newISPBPixFetchCmd(), newISPBPixLoadCmd(), newISPBPixSyncCmd())
	return cmd
}

func newISPBPixFetchCmd() *cobra.Command {
	var out string
	var maxDaysBack int
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the Pix participants CSV and stage it to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := out
			if path == "" {
				path = defaultISPBPath(cfg, "pix-participants.csv")
			}
			pcfg := ispb.DefaultPixConfig()
			if maxDaysBack > 0 {
				pcfg.MaxDaysBack = maxDaysBack
			}
			data, _, err := ispb.DownloadPix(cmd.Context(), pcfg, ispbLogger())
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&out, "out", "", "output path (default: <mirror_dir>/bacen-ispb/pix-participants.csv)")
	cmd.Flags().IntVar(&maxDaysBack, "max-days-back", 0, "days to probe backward for a dated CSV (default: 60)")
	return cmd
}

func newISPBPixLoadCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Parse a staged Pix participants CSV and upsert it into the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			path := file
			if path == "" {
				path = defaultISPBPath(cfg, "pix-participants.csv")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			records, err := ispb.ParsePix(data, ispb.DefaultPixConfig(), time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertPix(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "loaded %d Pix participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "input path (default: <mirror_dir>/bacen-ispb/pix-participants.csv)")
	return cmd
}

func newISPBPixSyncCmd() *cobra.Command {
	var maxDaysBack int
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download and load the Pix participants CSV in one step",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			ctx := cmd.Context()
			pcfg := ispb.DefaultPixConfig()
			if maxDaysBack > 0 {
				pcfg.MaxDaysBack = maxDaysBack
			}
			data, _, err := ispb.DownloadPix(ctx, pcfg, ispbLogger())
			if err != nil {
				return err
			}
			records, err := ispb.ParsePix(data, pcfg, time.Now())
			if err != nil {
				return err
			}
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			for _, r := range records {
				if err := st.UpsertPix(ctx, r); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %d Pix participants\n", len(records))
			return nil
		},
	}
	cmd.Flags().IntVar(&maxDaysBack, "max-days-back", 0, "days to probe backward for a dated CSV (default: 60)")
	return cmd
}

func newISPBSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download and load both STR and Pix participant sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newISPBSTRSyncCmd().RunE(cmd, args); err != nil {
				return fmt.Errorf("str sync: %w", err)
			}
			if err := newISPBPixSyncCmd().RunE(cmd, args); err != nil {
				return fmt.Errorf("pix sync: %w", err)
			}
			return nil
		},
	}
}

func newISPBLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lookup <ispb-code>",
		Short: "Look up a participant by its 8-digit ISPB code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]
			if err := ispb.ValidateISPB(code); err != nil {
				return err
			}
			cfg := loadConfig()
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			p, err := st.GetISPB(ctx, code)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "ISPB:          %s\n", p.ISPB)
			_, _ = fmt.Fprintf(out, "Name:          %s\n", p.Name)
			if p.LegalName != "" {
				_, _ = fmt.Fprintf(out, "Legal name:    %s\n", p.LegalName)
			}
			if p.CompeCode != "" {
				_, _ = fmt.Fprintf(out, "COMPE code:    %s\n", p.CompeCode)
			}
			if !p.STRSyncedAt.IsZero() {
				_, _ = fmt.Fprintf(out, "Participates COMPE: %t\n", p.ParticipatesCompe)
				_, _ = fmt.Fprintf(out, "Access type:   %s\n", p.AccessType)
				_, _ = fmt.Fprintf(out, "STR synced:    %s\n", p.STRSyncedAt.Format(time.RFC3339))
			}
			if !p.PixSyncedAt.IsZero() {
				_, _ = fmt.Fprintf(out, "CNPJ:          %s\n", p.CNPJ)
				_, _ = fmt.Fprintf(out, "Pix authorized: %t\n", p.PixAuthorized)
				_, _ = fmt.Fprintf(out, "Pix synced:    %s\n", p.PixSyncedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}
