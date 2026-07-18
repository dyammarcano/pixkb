package ingest

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func writeTestXlsx(t *testing.T) string {
	t.Helper()
	fx := excelize.NewFile()
	// Sheet1 -> "Participantes" with a header + 2 data rows.
	require.NoError(t, fx.SetSheetName("Sheet1", "Participantes"))
	for cell, val := range map[string]string{
		"A1": "ISPB", "B1": "Nome",
		"A2": "00000000", "B2": "Banco A | LTDA",
		"A3": "11111111", "B3": "Banco B",
	} {
		require.NoError(t, fx.SetCellValue("Participantes", cell, val))
	}
	// An empty sheet (must be skipped).
	_, err := fx.NewSheet("Vazia")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "part.xlsx")
	require.NoError(t, fx.SaveAs(path))
	require.NoError(t, fx.Close())
	return path
}

func TestXlsxSource_SheetsToTables(t *testing.T) {
	path := writeTestXlsx(t)
	cs, err := NewXlsxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1, "empty sheet skipped, one concept for Participantes")

	c := cs[0]
	require.Equal(t, "Reference", c.Type)
	require.Contains(t, c.Title, "Participantes")
	require.Subset(t, c.Tags, []string{"xlsx", "part", "participantes"})
	require.Contains(t, c.Body, "| ISPB |")        // header row
	require.Contains(t, c.Body, "| --- |")         // separator
	require.Contains(t, c.Body, "00000000")        // data
	require.Contains(t, c.Body, `Banco A \| LTDA`) // pipe escaped
	require.NotEmpty(t, c.ContentSHA)
	require.Contains(t, c.ID, "reference/part/")
}

func TestXlsxSource_RowCap(t *testing.T) {
	fx := excelize.NewFile()
	require.NoError(t, fx.SetCellValue("Sheet1", "A1", "H"))
	for i := 2; i < maxXlsxRows+50; i++ {
		require.NoError(t, fx.SetCellValue("Sheet1", "A"+strconv.Itoa(i), "v"))
	}
	path := filepath.Join(t.TempDir(), "big.xlsx")
	require.NoError(t, fx.SaveAs(path))
	require.NoError(t, fx.Close())

	cs, err := NewXlsxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Contains(t, cs[0].Body, "more rows omitted")
	require.LessOrEqual(t, strings.Count(cs[0].Body, "\n| v |"), maxXlsxRows)
}

// TestXlsxSource_SlugCollisionDistinctIDs confirms two sheets whose names
// slugify to the same value get distinct concept IDs (index-prefixed), so
// neither silently overwrites the other on upsert (item 1 / review fix 302a950).
func TestXlsxSource_SlugCollisionDistinctIDs(t *testing.T) {
	fx := excelize.NewFile()
	// "Data 1" and "Data-1" both slugify to "data-1"; both non-empty.
	require.NoError(t, fx.SetSheetName("Sheet1", "Data 1"))
	require.NoError(t, fx.SetCellValue("Data 1", "A1", "x"))
	_, err := fx.NewSheet("Data-1")
	require.NoError(t, err)
	require.NoError(t, fx.SetCellValue("Data-1", "A1", "y"))

	path := filepath.Join(t.TempDir(), "collide.xlsx")
	require.NoError(t, fx.SaveAs(path))
	require.NoError(t, fx.Close())

	cs, err := NewXlsxSource([]string{path}).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, cs, 2)
	require.NotEqual(t, cs[0].ID, cs[1].ID, "colliding sheet slugs must not share an ID")
}

func TestXlsxSource_Name(t *testing.T) {
	require.Equal(t, "xlsx", NewXlsxSource(nil).Name())
}
