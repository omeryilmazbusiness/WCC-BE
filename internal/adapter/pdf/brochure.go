package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/go-pdf/fpdf"
	_ "golang.org/x/image/webp"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

//go:embed fonts/NotoNaskhArabic-Regular.ttf
var naskhTTF []byte

const (
	pageW    = 210.0
	pageH    = 297.0
	margin   = 14.0
	contentW = pageW - 2*margin
)

var (
	ivory    = rgb{252, 247, 238}
	paper    = rgb{255, 252, 247}
	gold     = rgb{196, 154, 88}
	goldDeep = rgb{168, 122, 58}
	goldWash = rgb{244, 230, 204}
	ink      = rgb{36, 30, 24}
	mute     = rgb{120, 108, 92}
)

type rgb struct{ r, g, b int }

// Renderer paints a Brochure as a portrait A4 visual leaflet.
type Renderer struct{}

func NewRenderer() Renderer { return Renderer{} }

func (Renderer) Render(doc domain.Brochure) ([]byte, error) {
	p := newPainter(doc)
	p.chrome()
	p.hero()
	p.facts()
	p.journey()
	p.features()
	p.itinerary()
	p.prices()
	p.excluded()
	p.colophon()
	var buf bytes.Buffer
	if err := p.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("brochure pdf: %w", err)
	}
	return buf.Bytes(), nil
}

type painter struct {
	pdf      *fpdf.Fpdf
	doc      domain.Brochure
	y        float64
	rtl      bool
	page     int
	latin    func(string) string
	fontSize float64
	fontBold bool
}

func newPainter(doc domain.Brochure) *painter {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)
	pdf.AddPage()
	if len(naskhTTF) > 0 {
		pdf.AddUTF8FontFromBytes("Naskh", "", naskhTTF)
	}
	return &painter{
		pdf: pdf, doc: doc, y: 0, rtl: doc.RTL, page: 1, fontSize: 10,
		latin: pdf.UnicodeTranslatorFromDescriptor(""),
	}
}

func (p *painter) chrome() {
	p.fill(ivory)
	p.pdf.Rect(0, 0, pageW, pageH, "F")
	p.fill(goldWash)
	p.pdf.Circle(-16, -10, 48, "F")
	p.pdf.Circle(pageW+22, 70, 56, "F")
	p.pdf.Circle(18, pageH+20, 44, "F")
	x := margin
	if p.embedLogo(x, 12, 11) {
		x += 14
	}
	p.ink(ink)
	p.setFont(13, true)
	p.cell(x, 11, contentW-36, 6.5, p.doc.BrandName, "L")
	p.ink(mute)
	p.setFont(8.5, false)
	p.cell(x, 17.2, contentW-36, 5, p.doc.Labels.Official, "L")
	p.fill(gold)
	p.pdf.Circle(pageW-margin-8, 18, 8, "F")
	p.ink(paper)
	p.setFont(9, true)
	p.cell(pageW-margin-16, 14.6, 16, 7, fmt.Sprintf("%02d", p.page), "C")
	p.y = 28
}

func (p *painter) hero() {
	pills := strings.TrimSpace(strings.Trim(p.doc.Kind+"  ·  "+p.doc.Category, " ·"))
	if pills != "" {
		p.pill(margin, p.y, p.measurePill(pills, 8.5)+12, 8.5, goldWash)
		p.ink(goldDeep)
		p.setFont(8.5, true)
		p.cell(margin, p.y+1.4, p.measurePill(pills, 8.5)+12, 6, pills, "C")
	}
	if p.doc.FromPrice != "" {
		p.pill(pageW-margin-54, p.y, 54, 20, gold)
		p.ink(paper)
		p.setFont(7, false)
		p.cell(pageW-margin-54, p.y+2, 54, 4.5, p.doc.Labels.From, "C")
		p.setFont(11, true)
		p.cell(pageW-margin-54, p.y+6.4, 54, 6.5, p.doc.FromPrice, "C")
		p.setFont(6.5, false)
		p.cell(pageW-margin-54, p.y+13.2, 54, 4.5, p.doc.Labels.PerPerson, "C")
	}
	p.y += 12
	p.ink(ink)
	p.setFont(26, true)
	p.y += p.textBlock(margin, p.y, contentW-58, 11.5, p.doc.Title, "L")
	if p.doc.Subtitle != "" {
		p.ink(mute)
		p.setFont(12, false)
		p.y += p.textBlock(margin, p.y, contentW-58, 6, p.doc.Subtitle, "L")
	}
	if p.doc.Tagline != "" {
		p.ink(goldDeep)
		p.setFont(12, true)
		p.y += p.textBlock(margin, p.y, contentW, 6.2, p.doc.Tagline, "L")
	}
	if p.doc.Code != "" {
		p.ink(mute)
		p.setFont(9, false)
		p.y += 2
		p.cell(margin, p.y, contentW, 5, p.doc.Code, "L")
		p.y += 6
	} else {
		p.y += 5
	}
}

func (p *painter) facts() {
	facts := p.doc.Facts
	if len(facts) == 0 {
		return
	}
	if len(facts) > 4 {
		facts = facts[:4]
	}
	gap := 4.0
	w := (contentW - gap*float64(len(facts)-1)) / float64(len(facts))
	p.need(36)
	for i, f := range facts {
		x := margin + float64(i)*(w+gap)
		p.pill(x, p.y, w, 34, paper)
		p.fill(goldWash)
		p.pdf.Circle(x+w/2, p.y+10, 7.4, "F")
		p.mark(x+w/2-6, p.y+4, 12, f.Icon)
		p.ink(mute)
		p.setFont(7.4, false)
		p.cell(x+2, p.y+18.2, w-4, 5, f.Label, "C")
		p.ink(ink)
		p.setFont(9.5, true)
		p.cell(x+2, p.y+23.4, w-4, 6.5, f.Value, "C")
	}
	p.y += 40
}

func (p *painter) journey() {
	if len(p.doc.Chapters) == 0 {
		return
	}
	p.heading(p.doc.Labels.Journey)
	for _, ch := range p.doc.Chapters {
		p.need(28)
		p.pill(margin, p.y, contentW, 26, paper)
		p.fill(gold)
		p.pdf.Circle(margin+15, p.y+13, 9, "F")
		p.ink(paper)
		p.setFont(13, true)
		p.cell(margin+6, p.y+8.4, 18, 9, fmt.Sprintf("%d", ch.Number), "C")
		p.fill(goldWash)
		p.pdf.Circle(pageW-margin-15, p.y+13, 9, "F")
		p.mark(pageW-margin-21, p.y+7, 12, ch.Icon)
		p.ink(ink)
		p.setFont(13, true)
		p.cell(margin+28, p.y+4.2, contentW-56, 8, ch.Title, "L")
		p.ink(mute)
		p.setFont(10, false)
		p.cell(margin+28, p.y+13, contentW-56, 8, ch.Body, "L")
		p.y += 30
	}
}

func (p *painter) features() {
	items := p.doc.Features
	if len(items) == 0 {
		return
	}
	p.heading(p.doc.Labels.Features)
	cols := 2
	gap := 5.0
	w := (contentW - gap) / 2
	h := 28.0
	for i, it := range items {
		if i%cols == 0 {
			p.need(h + 4)
		}
		x := margin + float64(i%cols)*(w+gap)
		p.pill(x, p.y, w, h, paper)
		p.fill(goldWash)
		p.pdf.Circle(x+16, p.y+14, 9, "F")
		p.mark(x+9.5, p.y+7.5, 13, it.Icon)
		p.ink(ink)
		p.setFont(11, true)
		p.cell(x+28, p.y+4.4, w-34, 9, it.Title, "L")
		p.ink(mute)
		p.setFont(9, false)
		p.cell(x+28, p.y+14, w-34, 9, it.Detail, "L")
		if i%cols == cols-1 {
			p.y += h + 4
		}
	}
	if len(items)%cols != 0 {
		p.y += h + 4
	}
}

func (p *painter) itinerary() {
	days := p.doc.Itinerary
	if len(days) == 0 {
		return
	}
	p.heading(p.doc.Labels.Itinerary)
	for _, d := range days {
		title := d.Title
		if d.City != "" {
			title = d.City + "  ·  " + d.Title
		}
		p.need(16)
		p.pill(margin, p.y, contentW, 14, paper)
		p.fill(gold)
		p.pdf.Circle(margin+10, p.y+7, 5.6, "F")
		p.ink(paper)
		p.setFont(9, true)
		p.cell(margin+4.4, p.y+3.6, 11.2, 7, fmt.Sprintf("%d", d.Day), "C")
		p.ink(ink)
		p.setFont(11, true)
		p.cell(margin+20, p.y+1.6, contentW-24, 6, title, "L")
		if d.Details != "" {
			p.ink(mute)
			p.setFont(9, false)
			p.cell(margin+20, p.y+7.2, contentW-24, 5, d.Details, "L")
		}
		p.y += 17
	}
	p.y += 2
}

func (p *painter) prices() {
	if len(p.doc.Prices) == 0 {
		return
	}
	p.heading(p.doc.Labels.Prices)
	for _, row := range p.doc.Prices {
		p.need(16)
		p.pill(margin, p.y, contentW, 14, goldWash)
		p.ink(ink)
		p.setFont(12, true)
		p.cell(margin+8, p.y+3.2, contentW-70, 8, row.Label, "L")
		p.ink(goldDeep)
		p.setFont(13, true)
		p.cell(pageW-margin-62, p.y+3, 54, 8, row.Amount, "R")
		p.y += 17
	}
}

func (p *painter) excluded() {
	if len(p.doc.Excluded) == 0 {
		return
	}
	p.heading(p.doc.Labels.Excluded)
	p.ink(mute)
	p.setFont(10, false)
	p.need(10)
	p.y += p.textBlock(margin, p.y, contentW, 5.2, strings.Join(p.doc.Excluded, "   ·   "), "L") + 3
}

func (p *painter) colophon() {
	p.need(18)
	if p.y < pageH-28 {
		p.y = pageH - 28
	}
	p.pill(margin, p.y, contentW, 14, gold)
	p.ink(paper)
	p.setFont(10, true)
	p.cell(margin, p.y+3.4, contentW, 8, p.doc.Footer, "C")
}

func (p *painter) heading(title string) {
	if title == "" {
		return
	}
	p.need(16)
	p.ink(ink)
	p.setFont(16, true)
	p.cell(margin, p.y, contentW, 8, title, "L")
	p.y += 11
}

func (p *painter) pill(x, y, w, h float64, c rgb) {
	p.fill(c)
	r := h / 2
	if r > w/2 {
		r = w / 2
	}
	p.pdf.RoundedRect(x, y, w, h, r, "1234", "F")
}

func (p *painter) measurePill(text string, size float64) float64 {
	p.setFont(size, true)
	return p.measure(text)
}

func (p *painter) mark(x, y, s float64, kind string) {
	if p.lucideIconColor(x, y, s, kind, "#A87A3A") {
		return
	}
	p.stroke(goldDeep, 0.45)
	p.drawMark(x, y, s, kind)
}

func (p *painter) need(h float64) {
	if p.y+h < pageH-22 {
		return
	}
	p.pdf.AddPage()
	p.page++
	p.chrome()
}

func (p *painter) setFont(size float64, bold bool) {
	p.fontSize = size
	p.fontBold = bold
	p.applyFont("")
}

func (p *painter) applyFont(text string) {
	if p.fontSize == 0 {
		p.fontSize = 10
	}
	if (p.rtl || containsArabic(text)) && len(naskhTTF) > 0 {
		p.pdf.SetFont("Naskh", "", p.fontSize)
		return
	}
	style := ""
	if p.fontBold {
		style = "B"
	}
	p.pdf.SetFont("Helvetica", style, p.fontSize)
}

func (p *painter) display(s string) string {
	if p.rtl || containsArabic(s) {
		return shapeArabic(s)
	}
	if p.latin != nil {
		return p.latin(s)
	}
	return s
}

func (p *painter) measure(s string) float64 {
	p.applyFont(s)
	return p.pdf.GetStringWidth(p.display(s))
}

func (p *painter) wrap(text string, width float64) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	var lines []string
	var cur string
	for _, w := range words {
		trial := w
		if cur != "" {
			trial = cur + " " + w
		}
		if p.measure(trial) <= width || cur == "" {
			cur = trial
			continue
		}
		lines = append(lines, cur)
		cur = w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (p *painter) textBlock(x, y, w, lineH float64, text, align string) float64 {
	lines := p.wrap(text, w)
	for i, line := range lines {
		p.cell(x, y+float64(i)*lineH, w, lineH, line, align)
	}
	return float64(len(lines)) * lineH
}

func (p *painter) cell(x, y, w, h float64, text, align string) {
	if text == "" {
		return
	}
	p.applyFont(text)
	text = p.display(text)
	if p.rtl {
		if align == "L" {
			align = "R"
		} else if align == "R" {
			align = "L"
		}
	}
	p.pdf.SetXY(x, y)
	p.pdf.CellFormat(w, h, text, "", 0, align, false, 0, "")
}

func (p *painter) embedLogo(x, y, size float64) bool {
	if len(p.doc.Logo) == 0 {
		return false
	}
	pngBytes, err := rasterPNG(p.doc.Logo)
	if err != nil || len(pngBytes) == 0 {
		return false
	}
	opt := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}
	if p.pdf.GetImageInfo("brand-logo") == nil {
		p.pdf.RegisterImageOptionsReader("brand-logo", opt, bytes.NewReader(pngBytes))
	}
	p.pdf.ImageOptions("brand-logo", x, y, size, size, false, opt, 0, "")
	return true
}

func rasterPNG(raw []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *painter) fill(c rgb) { p.pdf.SetFillColor(c.r, c.g, c.b) }
func (p *painter) ink(c rgb)  { p.pdf.SetTextColor(c.r, c.g, c.b) }
func (p *painter) stroke(c rgb, width float64) {
	p.pdf.SetDrawColor(c.r, c.g, c.b)
	p.pdf.SetLineWidth(width)
	p.pdf.SetLineCapStyle("round")
	p.pdf.SetLineJoinStyle("round")
}
