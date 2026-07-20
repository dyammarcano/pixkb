package main

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/spf13/cobra"

	"pixkb/internal/store/postgres"
)

func newDBCmd() *cobra.Command {
	var dsn string

	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Manage the derived pgvector schema",
	}
	dbCmd.PersistentFlags().StringVar(&dsn, "dsn", "", "Postgres DSN (postgres://...)")

	dbCmd.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMigrate(dsn, true)
		},
	})
	dbCmd.AddCommand(&cobra.Command{
		Use:   "down",
		Short: "Roll back all migrations",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMigrate(dsn, false)
		},
	})
	return dbCmd
}

func runMigrate(dsn string, up bool) error {
	var err error
	dsn, err = resolveDSN(dsn)
	if err != nil {
		return err
	}

	src, err := iofs.New(postgres.SchemaFS, "schema")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, normalizeDSN(dsn))
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		serr, derr := m.Close()
		_ = serr
		_ = derr
	}()

	if up {
		err := m.Up()
		if err == nil || errorsIsNoChange(err) {
			return nil
		}
		// Recover from a dirty database left by an interrupted prior run: force
		// the recorded version clean and retry once.
		var dirty migrate.ErrDirty
		if errors.As(err, &dirty) {
			if ferr := m.Force(dirty.Version); ferr != nil {
				return fmt.Errorf("migrate force %d: %w", dirty.Version, ferr)
			}
			if err := m.Up(); err != nil && !errorsIsNoChange(err) {
				return fmt.Errorf("migrate up after force: %w", err)
			}
			return nil
		}
		return fmt.Errorf("migrate up: %w", err)
	}
	if err := m.Down(); err != nil && !errorsIsNoChange(err) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// normalizeDSN ensures the golang-migrate pgx/v5 driver scheme is used.
func normalizeDSN(dsn string) string {
	if len(dsn) >= 11 && dsn[:11] == "postgres://" {
		return "pgx5://" + dsn[11:]
	}
	if len(dsn) >= 13 && dsn[:13] == "postgresql://" {
		return "pgx5://" + dsn[13:]
	}
	return dsn
}

// resolveDSN honors the project's config-file-as-source-of-truth design: the
// --dsn flag wins if set; otherwise the loaded config supplies the DSN (global
// config file -> pixkb.yaml -> PIXKB_DSN env, via loadConfig). db commands
// previously read only the flag/env and ignored the config file entirely — a
// deviation from that design, fixed here so `pixkb db up` resolves the DSN the
// same way every other command already does.
func resolveDSN(flagVal string) (string, error) {
	if flagVal == "" {
		flagVal = loadConfig().DSN
	}
	if flagVal == "" {
		return "", fmt.Errorf("no database DSN: set `dsn` in the config file (%s), pixkb.yaml, PIXKB_DSN, or --dsn", globalConfigPath())
	}
	return flagVal, nil
}

func errorsIsNoChange(err error) bool {
	return errors.Is(err, migrate.ErrNoChange)
}
