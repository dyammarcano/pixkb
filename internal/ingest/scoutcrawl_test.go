package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCrawlPage(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

const scoutCrawlNavPreamble = `Banco Central do Brasil
![](https://www.bcb.gov.br/assets/img/logo_bacen_preto.png)

- [Ir para o conteúdo 1](https://www.bcb.gov.br/#inicioConteudo)

1. [Home](https://www.bcb.gov.br/)
2. Some Page
`

const scoutCrawlFooterChrome = `
Siga o BC
[Instagram](http://www.instagram.com/bancocentraldobrasil/)

© Banco Central do Brasil - [Todos os direitos reservados](https://www.bcb.gov.br/acessoinformacao/direitosautorais)
`

func TestScoutCrawlSource_NormalPage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Pagamento em moeda local

Sistemas de pagamentos internacionais são infraestruturas que possibilitam a transferência de recursos financeiros entre usuários de diferentes países.

### Sistema de Pagamentos em Moeda Local

O Sistema de Pagamentos em Moeda Local (SML) é administrado pelo Banco Central do Brasil em parceria com os bancos centrais da Argentina, Uruguai e Paraguai.
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "estabilidadefinanceira/sml.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)

	c := got[0]
	assert.Equal(t, "WebPage", c.Type)
	assert.Equal(t, "Pagamento em moeda local", c.Title)
	assert.Equal(t, "web/estabilidadefinanceira-sml.md", c.ID)
	assert.Equal(t, "https://www.bcb.gov.br/estabilidadefinanceira/sml", c.SourceURI)
	assert.Contains(t, c.Body, "Sistema de Pagamentos em Moeda Local")
	assert.Contains(t, c.Tags, "estabilidadefinanceira")
	assert.NotContains(t, c.Body, "Siga o BC")
	assert.NotEmpty(t, c.ContentSHA)
}

func TestScoutCrawlSource_RootIndexUsesBareBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Banco Central do Brasil

Garantir a estabilidade de preços, zelar por um sistema financeiro sólido e eficiente, e fomentar o bem-estar econômico da sociedade e do país inteiro.
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "index.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "https://www.bcb.gov.br/", got[0].SourceURI)
}

func TestScoutCrawlSource_SkipsPageWithNoH1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCrawlPage(t, dir, "en.md", scoutCrawlNavPreamble+"\nNo heading anywhere in this failed capture.\n")

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestScoutCrawlSource_SkipsStubPageWithNoRealContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Atas das reuniões do CMN
` + scoutCrawlFooterChrome
	writeCrawlPage(t, dir, "acessoinformacao/cmnatasreun.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestScoutCrawlSource_NoFooterMarkerFallsBackToEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := scoutCrawlNavPreamble + `
# Octavio Gouvêa de Bulhões

Octavio Gouvêa de Bulhões nasceu no Rio de Janeiro e formou-se na Faculdade de Direito do Rio de Janeiro, mesma escola onde obteve seu grau de doutor em ciências jurídicas e sociais.
`
	writeCrawlPage(t, dir, "historiacontada/index.html.md", page)

	got, err := NewScoutCrawlSource(dir, "https://www.bcb.gov.br").Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Body, "Faculdade de Direito")
	assert.Equal(t, "https://www.bcb.gov.br/historiacontada/index.html", got[0].SourceURI)
}
