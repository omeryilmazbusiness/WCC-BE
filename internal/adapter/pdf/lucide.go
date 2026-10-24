package pdf

import (
	"bytes"
	"embed"
	"image"
	"image/png"

	"github.com/go-pdf/fpdf"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

//go:embed icons/*.svg
var lucideFS embed.FS

// Lucide Static (ISC) stroke names → file. Mosque and Kaaba stay hand-drawn.
var lucideFile = map[string]string{
	"plane":    "plane.svg",
	"bus":      "bus.svg",
	"hotel":    "building-2.svg",
	"luggage":  "luggage.svg",
	"return":   "luggage.svg",
	"calendar": "calendar.svg",
	"moon":     "moon.svg",
	"pin":      "map-pin.svg",
	"guide":    "users.svg",
	"users":    "users.svg",
	"stamp":    "stamp.svg",
	"check":    "check.svg",
	"star":     "star.svg",
}

func (p *painter) lucideIcon(x, y, s float64, kind string) bool {
	return p.lucideIconColor(x, y, s, kind, "#B8956A")
}

func (p *painter) lucideIconColor(x, y, s float64, kind, hex string) bool {
	file, ok := lucideFile[kind]
	if !ok {
		return false
	}
	raw, err := lucideFS.ReadFile("icons/" + file)
	if err != nil {
		return false
	}
	key := "lucide-" + file + "-" + hex
	if p.pdf.GetImageInfo(key) == nil {
		pngBytes, err := rasterLucide(raw, 160, hex)
		if err != nil || len(pngBytes) == 0 {
			return false
		}
		opt := fpdf.ImageOptions{ImageType: "PNG"}
		p.pdf.RegisterImageOptionsReader(key, opt, bytes.NewReader(pngBytes))
	}
	p.pdf.ImageOptions(key, x, y, s, s, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	return true
}

func rasterLucide(svg []byte, px int, hex string) ([]byte, error) {
	svg = bytes.ReplaceAll(svg, []byte("currentColor"), []byte(hex))
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(px), float64(px))
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	scanner := rasterx.NewScannerGV(px, px, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(px, px, scanner), 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
