package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"pixkb/internal/okf"
)

type openAPISource struct {
	files  []string
	domain string
}

// NewOpenAPISource builds a Source that parses OpenAPI/Swagger specs and emits
// one ApiEndpoint concept per path+method operation. It is air-gap friendly:
// it reads local spec files (e.g. from staged repo mirrors), never the network.
func NewOpenAPISource(files []string) Source { return &openAPISource{files: files} }

// NewOpenAPISourceWithDomain is NewOpenAPISource plus a domain: every emitted
// ApiEndpoint concept gets a domain:<domain> tag. Empty domain == NewOpenAPISource.
func NewOpenAPISourceWithDomain(files []string, domain string) Source {
	return &openAPISource{files: files, domain: domain}
}

func (s *openAPISource) Name() string { return "openapi" }

// oaSpec captures only the fields pixkb indexes from an OpenAPI document. Path
// items are decoded as raw nodes so non-method keys ("parameters") are skipped.
type oaSpec struct {
	Info struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

type oaOp struct {
	Summary     string            `yaml:"summary"`
	Description string            `yaml:"description"`
	OperationID string            `yaml:"operationId"`
	Tags        []string          `yaml:"tags"`
	Parameters  []oaParam         `yaml:"parameters"`
	RequestBody oaBody            `yaml:"requestBody"`
	Responses   map[string]oaResp `yaml:"responses"`
}

type oaParam struct {
	Name        string      `yaml:"name"`
	In          string      `yaml:"in"`
	Required    bool        `yaml:"required"`
	Description string      `yaml:"description"`
	Schema      oaSchemaRef `yaml:"schema"`
}

type oaBody struct {
	Required bool               `yaml:"required"`
	Content  map[string]oaMedia `yaml:"content"`
}

type oaResp struct {
	Description string             `yaml:"description"`
	Content     map[string]oaMedia `yaml:"content"`
}

type oaMedia struct {
	Schema oaSchemaRef `yaml:"schema"`
}

type oaSchemaRef struct {
	Ref   string `yaml:"$ref"`
	Type  string `yaml:"type"`
	Items struct {
		Ref string `yaml:"$ref"`
	} `yaml:"items"`
}

// name resolves a schema reference to a readable name: the last segment of a
// $ref ("#/components/schemas/Cob" -> "Cob"), an array's item ref, or a type.
func (r oaSchemaRef) name() string {
	if r.Ref != "" {
		return refName(r.Ref)
	}
	if r.Items.Ref != "" {
		return "[]" + refName(r.Items.Ref)
	}
	return r.Type
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"patch": true, "options": true, "head": true, "trace": true,
}

func (s *openAPISource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, f := range s.files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("openapi read %s: %w", f, err)
		}
		var spec oaSpec
		if err := yaml.Unmarshal(data, &spec); err != nil {
			// A malformed spec must not abort the whole ingest run.
			continue
		}
		slug := slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		api := spec.Info.Title
		if api == "" {
			api = slug
		}

		// Deterministic order: sort paths, then methods.
		paths := make([]string, 0, len(spec.Paths))
		for p := range spec.Paths {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		for _, p := range paths {
			methods := make([]string, 0, len(spec.Paths[p]))
			for m := range spec.Paths[p] {
				if httpMethods[strings.ToLower(m)] {
					methods = append(methods, m)
				}
			}
			sort.Strings(methods)
			for _, m := range methods {
				node := spec.Paths[p][m]
				var op oaOp
				_ = node.Decode(&op) // best-effort; missing fields stay zero
				method := strings.ToUpper(m)
				title := method + " " + p
				body := buildEndpointBody(title, api, spec.Info.Version, op)
				tags := append([]string{"api", slug}, op.Tags...)
				if s.domain != "" {
					tags = append(tags, "domain:"+s.domain)
				}
				out = append(out, okf.Concept{
					ID:          "api/" + slug + "/" + slugify(method+" "+p) + ".md",
					Type:        "ApiEndpoint",
					Title:       title,
					Description: firstNonEmpty(op.Summary, op.OperationID, firstLine(op.Description)),
					Resource:    f,
					Tags:        tags,
					Language:    "pt",
					SourceURI:   fmt.Sprintf("openapi:%s#%s %s", filepath.Base(f), method, p),
					Body:        body,
					ContentSHA:  okf.ComputeSHA(body),
				})
			}
		}
	}
	return out, nil
}

// buildEndpointBody renders a rich, searchable markdown body for one operation:
// summary/description, parameters, request body schema, and response codes.
func buildEndpointBody(title, api, version string, op oaOp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nAPI: %s %s\n", title, api, version)
	if terms := intentPhrases(title); terms != "" {
		// Synthesized PT intent terms so queries like "consultar cobrança" or
		// "qr code dinâmico" surface the endpoint over generic manual fragments.
		fmt.Fprintf(&b, "Termos: %s\n", terms)
	}
	if op.OperationID != "" {
		fmt.Fprintf(&b, "operationId: %s\n", op.OperationID)
	}
	if len(op.Tags) > 0 {
		fmt.Fprintf(&b, "tags: %s\n", strings.Join(op.Tags, ", "))
	}
	if s := strings.TrimSpace(op.Summary); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	if d := strings.TrimSpace(op.Description); d != "" {
		fmt.Fprintf(&b, "\n%s\n", d)
	}

	if len(op.Parameters) > 0 {
		b.WriteString("\n## Parameters\n")
		for _, pm := range op.Parameters {
			req := ""
			if pm.Required {
				req = ", required"
			}
			sch := pm.Schema.name()
			if sch != "" {
				sch = " `" + sch + "`"
			}
			fmt.Fprintf(&b, "- `%s` (%s%s)%s%s\n", pm.Name, pm.In, req,
				sch, descSuffix(pm.Description))
		}
	}

	if len(op.RequestBody.Content) > 0 {
		b.WriteString("\n## Request body\n")
		if op.RequestBody.Required {
			b.WriteString("(required)\n")
		}
		for _, ct := range sortedKeys(op.RequestBody.Content) {
			fmt.Fprintf(&b, "- %s: `%s`\n", ct, op.RequestBody.Content[ct].Schema.name())
		}
	}

	if len(op.Responses) > 0 {
		b.WriteString("\n## Responses\n")
		for _, code := range sortedKeys(op.Responses) {
			r := op.Responses[code]
			schema := ""
			for _, ct := range sortedKeys(r.Content) {
				if n := r.Content[ct].Schema.name(); n != "" {
					schema = " `" + n + "`"
					break
				}
			}
			fmt.Fprintf(&b, "- %s%s%s\n", code, schema, descSuffix(r.Description))
		}
	}
	return b.String()
}

// verbsPT maps an HTTP method to Portuguese action verbs (Pix API intent).
var verbsPT = map[string][]string{
	"POST":   {"criar", "gerar", "cadastrar"},
	"GET":    {"consultar", "obter", "listar"},
	"PUT":    {"atualizar", "substituir", "configurar"},
	"PATCH":  {"revisar", "atualizar"},
	"DELETE": {"remover", "excluir", "cancelar"},
}

// resourcePT maps a path root to a Portuguese resource phrase so intent terms
// read naturally ("criar cobrança imediata", "consultar chave dict").
var resourcePT = map[string]string{
	"cob":       "cobrança imediata",
	"cobv":      "cobrança com vencimento",
	"cobr":      "cobrança recorrente",
	"lotecobv":  "lote de cobranças com vencimento",
	"loc":       "location qr code dinâmico",
	"locrec":    "location de recorrência",
	"webhook":   "webhook de notificação",
	"pix":       "pix recebido",
	"devolucao": "devolução",
	"entries":   "chave dict",
	"keys":      "chave dict",
	"cid":       "verificação de chave dict",
	"rec":       "recorrência",
	"reclote":   "lote de recorrências",
	"solicrec":  "solicitação de recorrência",
}

// intentPhrases synthesizes "verb resource" intent terms for an endpoint title
// ("POST /cob" -> "criar cobrança imediata, gerar cobrança imediata, ...").
func intentPhrases(title string) string {
	method, _, ok := strings.Cut(title, " ")
	if !ok {
		return ""
	}
	verbs := verbsPT[strings.ToUpper(method)]
	if len(verbs) == 0 {
		return ""
	}
	root := endpointPathRoot(title)
	noun := resourcePT[root]
	if noun == "" {
		noun = root
	}
	if noun == "" {
		return ""
	}
	phrases := make([]string, 0, len(verbs))
	for _, v := range verbs {
		phrases = append(phrases, v+" "+noun)
	}
	return strings.Join(phrases, ", ")
}

func descSuffix(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	return " — " + firstLine(d)
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
