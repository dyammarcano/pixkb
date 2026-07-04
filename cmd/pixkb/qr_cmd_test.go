package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCmd executes the root command with args and returns stdout + any error.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestQRCmd_WriteThenRead(t *testing.T) {
	out, err := runCmd(t, "qr", "write", "--key", "loja@pix.com",
		"--name", "ACME LTDA", "--city", "SAO PAULO", "--amount", "10.00", "--txid", "PED1")
	require.NoError(t, err)
	code := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	require.True(t, strings.HasPrefix(code, "0002"), "expected BR Code, got %q", code)

	read, err := runCmd(t, "qr", "read", code)
	require.NoError(t, err)
	assert.Contains(t, read, "loja@pix.com")
	assert.Contains(t, read, "10.00")
	assert.Contains(t, read, "valid=true")
}

func TestQRCmd_WritePNGThenReadImage(t *testing.T) {
	png := filepath.Join(t.TempDir(), "q.png")
	_, err := runCmd(t, "qr", "write", "--key", "k@e.com", "--name", "ACME", "--city", "SP",
		"--txid", "ORD7", "--png", png)
	require.NoError(t, err)
	fi, err := os.Stat(png)
	require.NoError(t, err)
	require.Positive(t, fi.Size())

	read, err := runCmd(t, "qr", "read", "--image", png)
	require.NoError(t, err)
	assert.Contains(t, read, "k@e.com")
	assert.Contains(t, read, "ORD7")
}

func TestQRCmd_ReadTamperedFails(t *testing.T) {
	out, _ := runCmd(t, "qr", "write", "--key", "k@e.com", "--name", "ACME", "--city", "SP")
	code := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	bad := code[:len(code)-1] + map[bool]string{true: "0", false: "1"}[code[len(code)-1] != '0']
	_, err := runCmd(t, "qr", "read", bad)
	require.Error(t, err, "tampered CRC should make qr read fail")
}

func TestQRCmd_WriteFromJSONFile(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "payload.json")
	payload := `{"key":"loja@pix.com","merchant_name":"ACME LTDA","city":"SAO PAULO","amount":"10.00","txid":"PED1"}`
	require.NoError(t, os.WriteFile(jsonPath, []byte(payload), 0o644))

	out, err := runCmd(t, "qr", "write", "--json", jsonPath)
	require.NoError(t, err)
	code := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])

	read, err := runCmd(t, "qr", "read", code)
	require.NoError(t, err)
	assert.Contains(t, read, "loja@pix.com")
	assert.Contains(t, read, "10.00")
	assert.Contains(t, read, "valid=true")
}

func TestQRCmd_WriteFromJSONStdin(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(`{"key":"k@e.com","merchant_name":"ACME","city":"SP"}`))
	cmd.SetArgs([]string{"qr", "write", "--json", "-"})
	require.NoError(t, cmd.Execute())
	code := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])
	require.True(t, strings.HasPrefix(code, "0002"))
}

func TestQRCmd_WriteJSONOverridesFlags(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "payload.json")
	require.NoError(t, os.WriteFile(jsonPath, []byte(`{"city":"RECIFE"}`), 0o644))

	out, err := runCmd(t, "qr", "write", "--key", "k@e.com", "--name", "ACME",
		"--city", "SAO PAULO", "--json", jsonPath)
	require.NoError(t, err)
	code := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])

	read, err := runCmd(t, "qr", "read", code)
	require.NoError(t, err)
	assert.Contains(t, read, "RECIFE", "JSON field must override the --city flag")
	assert.Contains(t, read, "k@e.com", "flag not present in JSON must survive")
}

func TestQRCmd_ValidateValidAndTampered(t *testing.T) {
	out, err := runCmd(t, "qr", "write", "--key", "k@e.com", "--name", "ACME", "--city", "SP")
	require.NoError(t, err)
	code := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])

	valid, err := runCmd(t, "qr", "validate", code)
	require.NoError(t, err)
	assert.Contains(t, valid, "valid")

	bad := code[:len(code)-1] + map[bool]string{true: "0", false: "1"}[code[len(code)-1] != '0']
	invalid, err := runCmd(t, "qr", "validate", bad)
	require.Error(t, err)
	assert.Contains(t, invalid, "invalid")
}

func TestQRCmd_ValidateRequiresOneInput(t *testing.T) {
	_, err := runCmd(t, "qr", "validate")
	require.Error(t, err)
	_, err = runCmd(t, "qr", "validate", "0002", "--image", "x.png")
	require.Error(t, err)
}

func TestQRCmd_ReadRequiresOneInput(t *testing.T) {
	// neither arg nor --image
	_, err := runCmd(t, "qr", "read")
	require.Error(t, err)
	// both arg and --image
	_, err = runCmd(t, "qr", "read", "0002", "--image", "x.png")
	require.Error(t, err)
}
