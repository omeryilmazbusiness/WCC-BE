package pdf

func (p *painter) drawMark(x, y, s float64, kind string) {
	switch kind {
	case "plane":
		p.iconPlane(x, y, s)
	case "mosque":
		p.iconMosque(x, y, s)
	case "kaaba":
		p.iconKaaba(x, y, s)
	case "bus":
		p.iconBus(x, y, s)
	case "hotel":
		p.iconHotel(x, y, s)
	case "return", "luggage":
		p.iconLuggage(x, y, s)
	case "calendar":
		p.iconCalendar(x, y, s)
	case "moon":
		p.iconMoon(x, y, s)
	case "pin":
		p.iconPin(x, y, s)
	case "guide", "users":
		p.iconPerson(x, y, s)
	case "stamp":
		p.iconStamp(x, y, s)
	case "check":
		p.iconCheck(x, y, s)
	case "star":
		p.iconStar(x, y, s)
	default:
		p.pdf.Circle(x+s/2, y+s/2, s*0.22, "D")
	}
}

func (p *painter) iconPlane(x, y, s float64) {
	// Side-view airliner: fuselage, wings, tail, plus a small terminal mark.
	p.pdf.Line(x+s*0.08, y+s*0.58, x+s*0.72, y+s*0.40)
	p.pdf.Line(x+s*0.72, y+s*0.40, x+s*0.86, y+s*0.44)
	p.pdf.Line(x+s*0.86, y+s*0.44, x+s*0.78, y+s*0.50)
	p.pdf.Line(x+s*0.42, y+s*0.48, x+s*0.34, y+s*0.20)
	p.pdf.Line(x+s*0.34, y+s*0.20, x+s*0.40, y+s*0.22)
	p.pdf.Line(x+s*0.44, y+s*0.50, x+s*0.30, y+s*0.78)
	p.pdf.Line(x+s*0.30, y+s*0.78, x+s*0.38, y+s*0.72)
	p.pdf.Line(x+s*0.68, y+s*0.42, x+s*0.80, y+s*0.18)
	p.pdf.Line(x+s*0.80, y+s*0.18, x+s*0.84, y+s*0.22)
	p.pdf.Line(x+s*0.14, y+s*0.22, x+s*0.14, y+s*0.40)
	p.pdf.Line(x+s*0.08, y+s*0.40, x+s*0.22, y+s*0.40)
}

func (p *painter) iconMosque(x, y, s float64) {
	// Green-dome silhouette: central dome, two minarets, arcade.
	p.pdf.Arc(x+s*0.50, y+s*0.44, s*0.20, s*0.18, 0, 180, 360, "D")
	p.pdf.Line(x+s*0.50, y+s*0.24, x+s*0.50, y+s*0.18)
	p.pdf.Circle(x+s*0.50, y+s*0.16, s*0.025, "D")
	p.pdf.Line(x+s*0.30, y+s*0.44, x+s*0.30, y+s*0.78)
	p.pdf.Line(x+s*0.70, y+s*0.44, x+s*0.70, y+s*0.78)
	p.pdf.Line(x+s*0.30, y+s*0.78, x+s*0.70, y+s*0.78)
	p.pdf.Line(x+s*0.38, y+s*0.62, x+s*0.38, y+s*0.78)
	p.pdf.Line(x+s*0.50, y+s*0.62, x+s*0.50, y+s*0.78)
	p.pdf.Line(x+s*0.62, y+s*0.62, x+s*0.62, y+s*0.78)
	p.pdf.Line(x+s*0.16, y+s*0.28, x+s*0.16, y+s*0.78)
	p.pdf.Line(x+s*0.12, y+s*0.28, x+s*0.20, y+s*0.28)
	p.pdf.Line(x+s*0.84, y+s*0.28, x+s*0.84, y+s*0.78)
	p.pdf.Line(x+s*0.80, y+s*0.28, x+s*0.88, y+s*0.28)
}

func (p *painter) iconKaaba(x, y, s float64) {
	// Cube with kiswa band — the Sacred House, seen slightly in elevation.
	p.pdf.Rect(x+s*0.26, y+s*0.28, s*0.48, s*0.50, "D")
	p.pdf.Line(x+s*0.26, y+s*0.28, x+s*0.40, y+s*0.16)
	p.pdf.Line(x+s*0.40, y+s*0.16, x+s*0.88, y+s*0.16)
	p.pdf.Line(x+s*0.88, y+s*0.16, x+s*0.74, y+s*0.28)
	p.pdf.Line(x+s*0.88, y+s*0.16, x+s*0.88, y+s*0.66)
	p.pdf.Line(x+s*0.88, y+s*0.66, x+s*0.74, y+s*0.78)
	p.pdf.Line(x+s*0.26, y+s*0.46, x+s*0.74, y+s*0.46)
	p.pdf.Line(x+s*0.74, y+s*0.46, x+s*0.88, y+s*0.34)
}

func (p *painter) iconBus(x, y, s float64) {
	p.pdf.RoundedRect(x+s*0.08, y+s*0.28, s*0.84, s*0.40, 1.15, "1234", "D")
	p.pdf.Line(x+s*0.08, y+s*0.46, x+s*0.92, y+s*0.46)
	p.pdf.Line(x+s*0.28, y+s*0.28, x+s*0.28, y+s*0.46)
	p.pdf.Line(x+s*0.50, y+s*0.28, x+s*0.50, y+s*0.46)
	p.pdf.Line(x+s*0.72, y+s*0.28, x+s*0.72, y+s*0.46)
	p.pdf.Circle(x+s*0.28, y+s*0.74, s*0.07, "D")
	p.pdf.Circle(x+s*0.70, y+s*0.74, s*0.07, "D")
	p.pdf.Line(x+s*0.78, y+s*0.18, x+s*0.88, y+s*0.28)
}

func (p *painter) iconHotel(x, y, s float64) {
	p.pdf.Rect(x+s*0.22, y+s*0.18, s*0.56, s*0.64, "D")
	p.pdf.Line(x+s*0.22, y+s*0.18, x+s*0.50, y+s*0.06)
	p.pdf.Line(x+s*0.50, y+s*0.06, x+s*0.78, y+s*0.18)
	p.pdf.Rect(x+s*0.34, y+s*0.56, s*0.14, s*0.26, "D")
	p.pdf.Rect(x+s*0.54, y+s*0.32, s*0.12, s*0.12, "D")
	p.pdf.Rect(x+s*0.34, y+s*0.32, s*0.12, s*0.12, "D")
}

func (p *painter) iconLuggage(x, y, s float64) {
	p.pdf.RoundedRect(x+s*0.20, y+s*0.38, s*0.58, s*0.44, 0.95, "1234", "D")
	p.pdf.Line(x+s*0.34, y+s*0.38, x+s*0.34, y+s*0.24)
	p.pdf.Line(x+s*0.64, y+s*0.38, x+s*0.64, y+s*0.24)
	p.pdf.Line(x+s*0.34, y+s*0.24, x+s*0.64, y+s*0.24)
	p.pdf.Line(x+s*0.49, y+s*0.46, x+s*0.49, y+s*0.74)
	p.pdf.Circle(x+s*0.84, y+s*0.62, s*0.07, "D")
	p.pdf.Line(x+s*0.84, y+s*0.70, x+s*0.84, y+s*0.84)
}

func (p *painter) iconCalendar(x, y, s float64) {
	p.pdf.RoundedRect(x+s*0.16, y+s*0.26, s*0.68, s*0.54, 0.7, "1234", "D")
	p.pdf.Line(x+s*0.16, y+s*0.40, x+s*0.84, y+s*0.40)
	p.pdf.Line(x+s*0.32, y+s*0.18, x+s*0.32, y+s*0.32)
	p.pdf.Line(x+s*0.68, y+s*0.18, x+s*0.68, y+s*0.32)
	p.pdf.Rect(x+s*0.30, y+s*0.50, s*0.12, s*0.12, "D")
	p.pdf.Rect(x+s*0.50, y+s*0.50, s*0.12, s*0.12, "D")
}

func (p *painter) iconMoon(x, y, s float64) {
	p.pdf.Arc(x+s*0.46, y+s*0.50, s*0.26, s*0.26, 0, 50, 310, "D")
	p.pdf.Arc(x+s*0.58, y+s*0.44, s*0.20, s*0.20, 0, 110, 300, "D")
}

func (p *painter) iconPin(x, y, s float64) {
	p.pdf.Circle(x+s*0.50, y+s*0.34, s*0.16, "D")
	p.pdf.Circle(x+s*0.50, y+s*0.34, s*0.06, "D")
	p.pdf.Line(x+s*0.36, y+s*0.44, x+s*0.50, y+s*0.82)
	p.pdf.Line(x+s*0.64, y+s*0.44, x+s*0.50, y+s*0.82)
}

func (p *painter) iconPerson(x, y, s float64) {
	p.pdf.Circle(x+s*0.50, y+s*0.28, s*0.13, "D")
	p.pdf.Arc(x+s*0.50, y+s*0.82, s*0.24, s*0.24, 0, 200, 340, "D")
}

func (p *painter) iconStamp(x, y, s float64) {
	p.pdf.RoundedRect(x+s*0.20, y+s*0.20, s*0.60, s*0.60, 0.8, "1234", "D")
	p.pdf.Line(x+s*0.32, y+s*0.40, x+s*0.68, y+s*0.40)
	p.pdf.Line(x+s*0.32, y+s*0.54, x+s*0.56, y+s*0.54)
}

func (p *painter) iconCheck(x, y, s float64) {
	p.pdf.Line(x+s*0.16, y+s*0.52, x+s*0.40, y+s*0.76)
	p.pdf.Line(x+s*0.40, y+s*0.76, x+s*0.86, y+s*0.24)
}

func (p *painter) iconStar(x, y, s float64) {
	cx, cy, r := x+s/2, y+s/2, s*0.28
	p.pdf.Line(cx, cy-r, cx+r*0.28, cy-r*0.18)
	p.pdf.Line(cx+r*0.28, cy-r*0.18, cx+r, cy-r*0.12)
	p.pdf.Line(cx+r, cy-r*0.12, cx+r*0.38, cy+r*0.18)
	p.pdf.Line(cx+r*0.38, cy+r*0.18, cx+r*0.60, cy+r)
	p.pdf.Line(cx+r*0.60, cy+r, cx, cy+r*0.42)
	p.pdf.Line(cx, cy+r*0.42, cx-r*0.60, cy+r)
	p.pdf.Line(cx-r*0.60, cy+r, cx-r*0.38, cy+r*0.18)
	p.pdf.Line(cx-r*0.38, cy+r*0.18, cx-r, cy-r*0.12)
	p.pdf.Line(cx-r, cy-r*0.12, cx-r*0.28, cy-r*0.18)
	p.pdf.Line(cx-r*0.28, cy-r*0.18, cx, cy-r)
}
