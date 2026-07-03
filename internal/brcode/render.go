package brcode

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// RenderPNG encodes a BR Code string as a scannable QR image (PNG bytes) at the
// given pixel size. Medium error correction is a good default for printed Pix QR
// codes. Pure Go — no native runtime, air-gap clean.
func RenderPNG(code string, size int) ([]byte, error) {
	if size <= 0 {
		size = 512
	}
	png, err := qrcode.Encode(code, qrcode.Medium, size)
	if err != nil {
		return nil, fmt.Errorf("brcode: render QR: %w", err)
	}
	return png, nil
}

// EncodePNG is the one-shot convenience: build the BR Code from a Payload and
// render it as a PNG, returning both the "Copia e Cola" string and the image.
func (p Payload) EncodePNG(size int) (code string, png []byte, err error) {
	code, err = p.Encode()
	if err != nil {
		return "", nil, err
	}
	png, err = RenderPNG(code, size)
	if err != nil {
		return "", nil, err
	}
	return code, png, nil
}
