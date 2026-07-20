package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inovacc/corral"
	"github.com/spf13/cobra"

	"pixkb/internal/embed"
	"pixkb/internal/ingest"
	"pixkb/internal/output"
	"pixkb/internal/query"
	"pixkb/internal/rag"
	"pixkb/internal/store/postgres"
	"pixkb/internal/watch"
)

// askPage is the self-contained ask UI served at GET / when `serve --ask` is set.
//
//go:embed web/ask.html
var askPage []byte

// attachOps wires the operational subcommands (watch/serve/doctor/export).
func attachOps(root *cobra.Command) {
	root.AddCommand(newWatchCmd(), newExportBundleCmd(), newServeCmd(), newDoctorCmd())
}

func newWatchCmd() *cobra.Command {
	var dsn string
	var debounceMS int
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch the ingest drop-dir and re-ingest on new artifacts (offline daemon)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			if err := os.MkdirAll(cfg.IngestDir, 0o755); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "watching %s (debounce %dms); Ctrl-C to stop\n", cfg.IngestDir, debounceMS)
			return watch.Watch(cmd.Context(), cfg.IngestDir, time.Duration(debounceMS)*time.Millisecond,
				func(ctx context.Context, files []string) error {
					r, st, err := newRunner(ctx, cfg)
					if err != nil {
						// Daemon survives a bad drop; emit to slog so an offline
						// (non-stdout) consumer still gets an audit trail.
						slog.Warn("watch ingest setup failed", "err", err)
						_, _ = fmt.Fprintln(out, "ingest:", err)
						return nil
					}
					defer st.Close()
					concepts, err := ingest.GatherAll(ctx, buildSources(cfg))
					if err != nil {
						slog.Warn("watch gather failed", "err", err)
						_, _ = fmt.Fprintln(out, "gather:", err)
						return nil
					}
					res, err := r.Run(ctx, concepts, "watch")
					if err != nil {
						slog.Warn("watch run failed", "err", err)
						_, _ = fmt.Fprintln(out, "run:", err)
						return nil
					}
					_, _ = fmt.Fprintf(out, "epoch %d: +%d ~%d -%d (triggered by %d file(s))\n",
						res.Epoch, res.Added, res.Changed, res.Removed, len(files))
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().IntVar(&debounceMS, "debounce-ms", 800, "debounce window in milliseconds")
	return cmd
}

func newExportBundleCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export-bundle",
		Short: "Package the OKF bundle as a portable tar.gz",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if out == "" {
				out = "pixkb-bundle.tar.gz"
			}
			if err := tarDir(cfg.BundleDir, out); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "exported %s -> %s\n", cfg.BundleDir, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output tar.gz path (default pixkb-bundle.tar.gz)")
	return cmd
}

func tarDir(dir, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	absOut, _ := filepath.Abs(out)
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(path)
		if info.IsDir() || abs == absOut || strings.Contains(filepath.ToSlash(path), "/.git/") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fh, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = fh.Close() }()
		// Size the header from the OPEN fd, so the declared size matches the bytes
		// we are about to read (tightens the stat-vs-copy race window).
		fi, err := fh.Stat()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		// LimitReader caps the copy at the header size so a file that GREW after
		// stat can never produce "archive/tar: write too long". There is no
		// artificial size limit — large artifacts are archived in full.
		n, err := io.Copy(tw, io.LimitReader(fh, hdr.Size))
		if err != nil {
			return err
		}
		// If the file SHRANK after stat, fewer bytes were available than the
		// header declares; pad with zeros to the declared size so the tar entry
		// stays valid instead of failing with "write too short".
		if n < hdr.Size {
			if _, err := io.CopyN(tw, zeroReader{}, hdr.Size-n); err != nil {
				return err
			}
		}
		return nil
	})
}

// zeroReader is an infinite source of zero bytes, used to pad a tar entry whose
// file shrank between stat and copy.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func newServeCmd() *cobra.Command {
	var dsn, addr, provider string
	var withAsk bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Read-only HTTP JSON search API over the KB (GET /search?q=...&type=...&format=...&explain=true)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/search", newSearchHandler(st, emb, cfg.BundleDir))

			// --ask adds the RAG ask UI + endpoint. It invokes the agent fleet
			// (generation), so it is not part of the default read-only contract
			// and is opt-in. The agency is created once here and closed on
			// shutdown — never per request.
			if withAsk {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				ag, err := corral.NewAgency(provider, dir)
				if err != nil {
					return err
				}
				defer func() { _ = ag.Close() }()

				stats, err := st.Stats(ctx)
				if err != nil {
					return err
				}
				cache := rag.NewLRUCache(128)
				ask := func(ctx context.Context, q, typ string) (rag.Answer, rag.Grounding, error) {
					return rag.Ask(ctx,
						rag.HybridRetriever{Store: st, Emb: emb, Filter: postgres.Filter{Type: typ}, BundleDir: cfg.BundleDir},
						rag.BundleSource{Dir: cfg.BundleDir},
						rag.AgentGenerator{Agency: ag},
						q,
						rag.Options{Cache: cache, Epoch: stats.LatestEpoch},
					)
				}
				mux.HandleFunc("/ask", newAskHandler(ask))

				// Dump/Ingest section: stage dropped files + fetched URLs under
				// <ingest_dir>/inbox, then cut a new epoch on explicit request.
				inbox := &inboxServer{cfg: cfg}
				mux.HandleFunc("/inbox/upload", inbox.handleUpload)
				mux.HandleFunc("/inbox/url", inbox.handleURL)
				mux.HandleFunc("/inbox/ingest", inbox.handleIngest)
				mux.HandleFunc("/inbox", inbox.handleList)

				mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/" {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write(askPage)
				})
			}

			srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			go func() { <-ctx.Done(); _ = srv.Close() }()
			if withAsk {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "serving search + ask UI on %s (open http://localhost%s/)\n", addr, addr)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "serving read-only search on %s (GET /search?q=...)\n", addr)
			}
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().BoolVar(&withAsk, "ask", false, "also serve the RAG ask UI (GET /) and POST /ask endpoint (invokes the agent fleet)")
	cmd.Flags().StringVar(&provider, "provider", "claude", "ask answerer backend: claude|codex|agy (only with --ask)")
	return cmd
}

// askRequest is the POST /ask JSON body: the question and an optional concept-type filter.
type askRequest struct {
	Q    string `json:"q"`
	Type string `json:"type"`
}

// newAskHandler is `serve --ask`'s POST /ask handler, taking the ask closure as a
// parameter so it can be exercised with httptest and a stub — no live DB or agent
// fleet. It mirrors `pixkb ask --json`'s response shape (askJSON) exactly.
func newAskHandler(ask func(ctx context.Context, q, typ string) (rag.Answer, rag.Grounding, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body askRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Q) == "" {
			http.Error(w, "missing q", http.StatusBadRequest)
			return
		}
		ans, g, err := ask(req.Context(), body.Q, body.Type)
		if err != nil {
			if errors.Is(err, rag.ErrRateLimited) {
				http.Error(w, "the agent fleet is rate-limited; try again later", http.StatusTooManyRequests)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cites := make([]askCitation, 0, len(ans.Citations))
		for _, id := range ans.Citations {
			cites = append(cites, askCitation{ID: id, Source: g.SourceFor(id)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(askJSON{Answer: ans.Text, Refused: ans.Refused, Citations: cites})
	}
}

// newSearchHandler is `pixkb serve`'s /search handler, extracted as its own
// function so it can be exercised directly with httptest (see ops_test.go)
// instead of only through a live-listening server. GET /search?q=...
// supports the same type/format/explain surface as `pixkb search` and MCP's
// search tool: type= filters by concept type, format= (json|text|md|yaml,
// default json — preserving this endpoint's original JSON-only contract
// exactly when unset) mirrors `pixkb search --format` (internal/output),
// and explain=true always returns JSON (matching the CLI/MCP explain
// surfaces, which are also JSON-only) via the same printExplain helper
// `pixkb search --explain` uses — http.ResponseWriter satisfies io.Writer.
func newSearchHandler(st *postgres.Store, emb embed.Embedder, bundleDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query().Get("q")
		if q == "" {
			http.Error(w, "missing q", http.StatusBadRequest)
			return
		}
		f := postgres.Filter{Type: req.URL.Query().Get("type")}

		if req.URL.Query().Get("explain") == "true" {
			hits, explains, err := query.HybridExplain(req.Context(), st, emb, q, f)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := printExplain(w, hits, explains, q, bundleDir); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		hits, err := query.Hybrid(req.Context(), st, emb, q, f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		format := req.URL.Query().Get("format")
		if format == "" {
			format = "json"
		}
		if format == "json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hits)
			return
		}
		rendered, err := output.Render(format, hits)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch format {
		case "md":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		case "yaml":
			w.Header().Set("Content-Type", "application/yaml")
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, _ = fmt.Fprint(w, rendered)
	}
}

func newDoctorCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Air-gap readiness checks (db, pgvector, embedder, bundle)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			ok := true
			check := func(name string, pass bool, detail string) {
				mark := "ok"
				if !pass {
					mark = "FAIL"
					ok = false
				}
				_, _ = fmt.Fprintf(out, "[%-4s] %s %s\n", mark, name, detail)
			}

			check("dsn configured", cfg.DSN != "", "")
			if cfg.DSN != "" {
				// Open pings and registers pgvector types, so success implies both
				// Postgres reachability and the vector extension being installed.
				st, err := openStore(ctx, cfg)
				check("postgres + pgvector", err == nil, errStr(err))
				if err == nil {
					st.Close()
				}
			}
			emb, eerr := newEmbedder(cfg)
			check("embedder", eerr == nil, embName(emb, eerr, cfg.Embedder))
			werr := os.MkdirAll(cfg.BundleDir, 0o755)
			check("bundle dir writable", werr == nil, cfg.BundleDir)

			if !ok {
				return fmt.Errorf("doctor: one or more checks failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	return cmd
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func embName(emb interface{ Name() string }, err error, configured string) string {
	if err != nil {
		return configured + " (" + err.Error() + ")"
	}
	return emb.Name()
}
