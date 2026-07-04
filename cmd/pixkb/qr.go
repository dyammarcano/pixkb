package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"pixkb/internal/brcode"
)

// newQRCmd is the Pix BR Code (EMV MPM, "Pix Copia e Cola") tool: parse a code
// into fields (read) or build one — optionally as a scannable PNG — from fields
// (write). Pure Go, no DB, no network.
func newQRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qr",
		Short: "Read and write Pix BR Codes (EMV MPM / 'Copia e Cola')",
	}
	cmd.AddCommand(newQRReadCmd(), newQRWriteCmd(), newQRValidateCmd())
	return cmd
}

func newQRReadCmd() *cobra.Command {
	var asJSON bool
	var imagePath string
	cmd := &cobra.Command{
		Use:   "read [brcode]",
		Short: "Parse a Pix BR Code into its fields and verify the CRC (string arg or --image)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (imagePath == "") == (len(args) == 0) {
				return fmt.Errorf("provide exactly one of: a BR Code string argument or --image <file>")
			}
			var p brcode.Payload
			var err error
			if imagePath != "" {
				p, err = brcode.ParseImageFile(imagePath)
			} else {
				p, err = brcode.Parse(args[0])
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(p)
			}
			kind := "static"
			if p.Dynamic {
				kind = "dynamic"
			}
			_, _ = fmt.Fprintf(w, "kind:          %s\n", kind)
			if p.Key != "" {
				_, _ = fmt.Fprintf(w, "key:           %s\n", p.Key)
			}
			if p.URL != "" {
				_, _ = fmt.Fprintf(w, "url:           %s\n", p.URL)
			}
			_, _ = fmt.Fprintf(w, "merchant:      %s\ncity:          %s\n", p.MerchantName, p.City)
			if p.Amount != "" {
				_, _ = fmt.Fprintf(w, "amount:        %s\n", p.Amount)
			}
			_, _ = fmt.Fprintf(w, "txid:          %s\ncrc:           %s (valid=%v)\n", p.TxID, p.CRC, p.CRCValid)
			if !p.CRCValid {
				return fmt.Errorf("CRC check failed — the code may be corrupted or tampered")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit parsed fields as JSON")
	cmd.Flags().StringVar(&imagePath, "image", "", "decode the BR Code from a PNG/JPEG image file")
	return cmd
}

func newQRWriteCmd() *cobra.Command {
	var p brcode.Payload
	var pngPath string
	var pngSize int
	var jsonPath string
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Build a Pix BR Code (and optionally a PNG) from fields or a JSON payload",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonPath != "" {
				data, err := readJSONInput(cmd, jsonPath)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(data, &p); err != nil {
					return fmt.Errorf("parse --json payload: %w", err)
				}
			}
			code, err := p.Encode()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(w, code)
			if pngPath != "" {
				png, err := brcode.RenderPNG(code, pngSize)
				if err != nil {
					return err
				}
				if err := os.WriteFile(pngPath, png, 0o644); err != nil {
					return fmt.Errorf("write png %q: %w", pngPath, err)
				}
				_, _ = fmt.Fprintf(w, "wrote %s (%d bytes)\n", pngPath, len(png))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&p.Key, "key", "", "Pix key (static code)")
	f.StringVar(&p.URL, "url", "", "payload-location URL (dynamic code)")
	f.StringVar(&p.MerchantName, "name", "", "merchant name (max 25, required)")
	f.StringVar(&p.City, "city", "", "merchant city (max 15, required)")
	f.StringVar(&p.Amount, "amount", "", "amount as decimal string, e.g. 10.00 (omit to let payer choose)")
	f.StringVar(&p.TxID, "txid", "", "transaction id / reference label (default ***)")
	f.StringVar(&p.Description, "description", "", "optional free description (static)")
	f.StringVar(&pngPath, "png", "", "also render a scannable PNG to this path")
	f.IntVar(&pngSize, "png-size", 512, "PNG size in pixels")
	f.StringVar(&jsonPath, "json", "", `path to a JSON file with payload fields ("-" for stdin); fields present in the JSON override the flags above`)
	return cmd
}

// readJSONInput reads --json's argument: "-" means the command's stdin,
// anything else is a file path.
func readJSONInput(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read --json from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --json %q: %w", path, err)
	}
	return data, nil
}

// newQRValidateCmd checks a serialized BR Code (string arg or --image) for
// strict TLV framing and a matching CRC16 — "is this Copia e Cola correct".
// Unlike `qr read`, it does not dump the fields, only the verdict.
func newQRValidateCmd() *cobra.Command {
	var imagePath string
	cmd := &cobra.Command{
		Use:   "validate [brcode]",
		Short: "Check whether a Pix BR Code is well-formed and its CRC16 checks out (string arg or --image)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (imagePath == "") == (len(args) == 0) {
				return fmt.Errorf("provide exactly one of: a BR Code string argument or --image <file>")
			}
			code := ""
			if imagePath != "" {
				f, err := os.Open(imagePath)
				if err != nil {
					return fmt.Errorf("open %q: %w", imagePath, err)
				}
				defer func() { _ = f.Close() }()
				text, err := brcode.DecodeImage(f)
				if err != nil {
					return err
				}
				code = text
			} else {
				code = args[0]
			}
			w := cmd.OutOrStdout()
			if err := brcode.ValidateCode(code); err != nil {
				_, _ = fmt.Fprintf(w, "invalid: %v\n", err)
				return err
			}
			_, _ = fmt.Fprintln(w, "valid")
			return nil
		},
	}
	cmd.Flags().StringVar(&imagePath, "image", "", "decode the BR Code from a PNG/JPEG image file")
	return cmd
}
