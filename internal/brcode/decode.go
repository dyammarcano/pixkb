package brcode

import (
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"io"
	"os"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// DecodeImage reads a QR code from an image stream (PNG or JPEG) and returns the
// embedded text — for a Pix QR, the BR Code string. Pure Go (gozxing, no cgo),
// so it runs air-gapped. It does NOT validate that the text is a Pix BR Code;
// pass the result to Parse for that.
func DecodeImage(r io.Reader) (string, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return "", fmt.Errorf("brcode: decode image: %w", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("brcode: bitmap: %w", err)
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("brcode: no QR code found in image: %w", err)
	}
	return res.GetText(), nil
}

// ParseImage decodes a QR image and parses the result as a Pix BR Code, returning
// the structured Payload (with CRC verification). The one-shot read-from-image
// path: image file -> QR text -> Pix fields.
func ParseImage(r io.Reader) (Payload, error) {
	text, err := DecodeImage(r)
	if err != nil {
		return Payload{}, err
	}
	return Parse(text)
}

// ParseImageFile is ParseImage over a file path.
func ParseImageFile(path string) (Payload, error) {
	f, err := os.Open(path)
	if err != nil {
		return Payload{}, fmt.Errorf("brcode: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return ParseImage(f)
}
