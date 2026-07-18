package main

import (
	"log/slog"
	"path/filepath"

	"pixkb/internal/ingest"
)

// knownDomains are the valid KB domain values. A configured domain outside this
// set (e.g. the typo "taxx") silently produces a domain:<typo> tag that is
// invisible to both the pix and tax domain filters, so buildSources warns.
var knownDomains = map[string]bool{"pix": true, "tax": true}

// unknownConfiguredDomains returns a human-readable label for every configured
// source whose domain is neither empty nor a known KB domain. An empty domain is
// allowed — it is backfilled to domain:pix by ingest.tagDomain — so only an
// explicit, non-empty, unrecognized domain is reported.
func unknownConfiguredDomains(cfg Config) []string {
	var bad []string
	check := func(domain, label string) {
		if domain != "" && !knownDomains[domain] {
			bad = append(bad, label+" (domain: "+domain+")")
		}
	}
	for _, spec := range cfg.OpenAPISpecs {
		check(spec.Domain, "openapi_specs:"+spec.File)
	}
	for _, l := range cfg.Legislation {
		check(l.Domain, "legislation:"+l.File)
	}
	return bad
}

// buildSources assembles the ingest sources from config. The ISO-20022 message
// set is always present; PDF and git-mirror sources are added when configured.
func buildSources(cfg Config) []ingest.Source {
	for _, b := range unknownConfiguredDomains(cfg) {
		slog.Warn("configured domain is not a known KB domain (pix|tax); its concepts will be invisible to domain filters", "source", b)
	}
	srcs := []ingest.Source{ingest.NewISOSpecSource(ingest.DefaultMsgDefs())}
	if len(cfg.PDFs) > 0 {
		srcs = append(srcs, ingest.NewPDFSource(cfg.PDFs))
	}
	if len(cfg.Markdown) > 0 {
		srcs = append(srcs, ingest.NewMarkdownSource(cfg.Markdown))
	}
	if len(cfg.Docx) > 0 {
		srcs = append(srcs, ingest.NewDocxSource(cfg.Docx))
	}
	if len(cfg.Xlsx) > 0 {
		srcs = append(srcs, ingest.NewXlsxSource(cfg.Xlsx))
	}
	if len(cfg.Repos) > 0 {
		specs := make([]ingest.RepoSpec, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			specs = append(specs, ingest.RepoSpec{Owner: r.Owner, Name: r.Name})
		}
		srcs = append(srcs, ingest.NewGitSource(specs, cfg.MirrorDir))
		// OpenAPI specs bundled inside the staged mirrors yield endpoint concepts.
		if oa := discoverOpenAPISpecs(cfg); len(oa) > 0 {
			srcs = append(srcs, ingest.NewOpenAPISource(oa))
		}
	}
	if len(cfg.APIDocs) > 0 {
		srcs = append(srcs, ingest.NewAPIDocSource(cfg.APIDocs))
	}
	if cfg.ScoutCrawlDir != "" {
		baseURL := cfg.ScoutCrawlBaseURL
		if baseURL == "" {
			baseURL = defaultScoutCrawlBaseURL
		}
		srcs = append(srcs, ingest.NewScoutCrawlSource(cfg.ScoutCrawlDir, baseURL))
	}
	for _, spec := range cfg.OpenAPISpecs {
		srcs = append(srcs, ingest.NewOpenAPISourceWithDomain([]string{spec.File}, spec.Domain))
	}
	for _, l := range cfg.Legislation {
		srcs = append(srcs, ingest.NewLegislationSource([]string{l.File}, l.Lei, l.Domain))
	}
	return srcs
}

// discoverOpenAPISpecs finds OpenAPI/Swagger YAML files inside the staged repo
// mirrors (common layouts: <repo>/openapi.yaml and <repo>/openapi/*.yaml).
func discoverOpenAPISpecs(cfg Config) []string {
	var files []string
	seen := map[string]bool{}
	for _, r := range cfg.Repos {
		base := filepath.Join(cfg.MirrorDir, r.Name)
		patterns := []string{
			filepath.Join(base, "openapi.yaml"),
			filepath.Join(base, "openapi.yml"),
			filepath.Join(base, "openapi", "*.yaml"),
			filepath.Join(base, "openapi", "*.yml"),
			filepath.Join(base, "openapi", "*", "*.yaml"),
		}
		for _, p := range patterns {
			matches, _ := filepath.Glob(p)
			for _, f := range matches {
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
		}
	}
	return files
}
