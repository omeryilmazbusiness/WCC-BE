package pdf

import (
	"strings"
	"unicode"
)

// shapeArabic turns logical Arabic into presentation forms and reverses each
// Arabic run so gofpdf (LTR) paints it in reading order.
func shapeArabic(s string) string {
	if !strings.ContainsFunc(s, isArabic) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, run := range splitRuns(s) {
		if run.ar {
			b.WriteString(reverseRunes(applyForms([]rune(run.s))))
			continue
		}
		b.WriteString(run.s)
	}
	return b.String()
}

type run struct {
	s  string
	ar bool
}

func splitRuns(s string) []run {
	var out []run
	var cur []rune
	ar := false
	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, run{s: string(cur), ar: ar})
		cur = cur[:0]
	}
	for _, r := range s {
		next := isArabic(r) || (ar && (unicode.IsMark(r) || r == ' '))
		if len(cur) > 0 && next != ar && !(r == ' ' && ar) {
			flush()
		}
		if len(cur) == 0 {
			ar = isArabic(r)
		}
		cur = append(cur, r)
	}
	flush()
	return out
}

func isArabic(r rune) bool {
	return unicode.In(r, unicode.Arabic)
}

func containsArabic(s string) bool {
	return strings.ContainsFunc(s, isArabic)
}

func reverseRunes(in []rune) string {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return string(in)
}

// Isolated / final / initial / medial for dual-joining letters.
var arabicForms = map[rune][4]rune{
	0x0626: {0xFE89, 0xFE8A, 0xFE8B, 0xFE8C},
	0x0628: {0xFE8F, 0xFE90, 0xFE91, 0xFE92},
	0x062A: {0xFE95, 0xFE96, 0xFE97, 0xFE98},
	0x062B: {0xFE99, 0xFE9A, 0xFE9B, 0xFE9C},
	0x062C: {0xFE9D, 0xFE9E, 0xFE9F, 0xFEA0},
	0x062D: {0xFEA1, 0xFEA2, 0xFEA3, 0xFEA4},
	0x062E: {0xFEA5, 0xFEA6, 0xFEA7, 0xFEA8},
	0x0633: {0xFEB1, 0xFEB2, 0xFEB3, 0xFEB4},
	0x0634: {0xFEB5, 0xFEB6, 0xFEB7, 0xFEB8},
	0x0635: {0xFEB9, 0xFEBA, 0xFEBB, 0xFEBC},
	0x0636: {0xFEBD, 0xFEBE, 0xFEBF, 0xFEC0},
	0x0637: {0xFEC1, 0xFEC2, 0xFEC3, 0xFEC4},
	0x0638: {0xFEC5, 0xFEC6, 0xFEC7, 0xFEC8},
	0x0639: {0xFEC9, 0xFECA, 0xFECB, 0xFECC},
	0x063A: {0xFECD, 0xFECE, 0xFECF, 0xFED0},
	0x0641: {0xFED1, 0xFED2, 0xFED3, 0xFED4},
	0x0642: {0xFED5, 0xFED6, 0xFED7, 0xFED8},
	0x0643: {0xFED9, 0xFEDA, 0xFEDB, 0xFEDC},
	0x0644: {0xFEDD, 0xFEDE, 0xFEDF, 0xFEE0},
	0x0645: {0xFEE1, 0xFEE2, 0xFEE3, 0xFEE4},
	0x0646: {0xFEE5, 0xFEE6, 0xFEE7, 0xFEE8},
	0x0647: {0xFEE9, 0xFEEA, 0xFEEB, 0xFEEC},
	0x064A: {0xFEF1, 0xFEF2, 0xFEF3, 0xFEF4},
}

// Right-joining only (no initial/medial).
var arabicRight = map[rune][2]rune{
	0x0622: {0xFE81, 0xFE82},
	0x0623: {0xFE83, 0xFE84},
	0x0624: {0xFE85, 0xFE86},
	0x0625: {0xFE87, 0xFE88},
	0x0627: {0xFE8D, 0xFE8E},
	0x0629: {0xFE93, 0xFE94},
	0x062F: {0xFEA9, 0xFEAA},
	0x0630: {0xFEAB, 0xFEAC},
	0x0631: {0xFEAD, 0xFEAE},
	0x0632: {0xFEAF, 0xFEB0},
	0x0648: {0xFEED, 0xFEEE},
	0x0649: {0xFEEF, 0xFEF0},
}

func joinsDual(r rune) bool { _, ok := arabicForms[r]; return ok }
func joinsRight(r rune) bool {
	_, ok := arabicRight[r]
	return ok || joinsDual(r)
}

func applyForms(in []rune) []rune {
	out := make([]rune, len(in))
	copy(out, in)
	for i, r := range in {
		prev := i > 0 && joinsDual(in[i-1])
		next := i+1 < len(in) && joinsRight(in[i+1])
		if forms, ok := arabicForms[r]; ok {
			switch {
			case prev && next:
				out[i] = forms[3]
			case !prev && next:
				out[i] = forms[2]
			case prev && !next:
				out[i] = forms[1]
			default:
				out[i] = forms[0]
			}
			continue
		}
		if forms, ok := arabicRight[r]; ok {
			if prev {
				out[i] = forms[1]
			} else {
				out[i] = forms[0]
			}
		}
	}
	return lamAlef(out)
}

func lamAlef(in []rune) []rune {
	out := in[:0]
	for i := 0; i < len(in); i++ {
		if i+1 < len(in) && (in[i] == 0xFEDF || in[i] == 0xFEE0 || in[i] == 0x0644) {
			switch in[i+1] {
			case 0xFE8E, 0x0627:
				out = append(out, 0xFEFC)
				i++
				continue
			case 0xFE82, 0x0622:
				out = append(out, 0xFEF6)
				i++
				continue
			case 0xFE84, 0x0623:
				out = append(out, 0xFEF8)
				i++
				continue
			case 0xFE88, 0x0625:
				out = append(out, 0xFEFA)
				i++
				continue
			}
		}
		out = append(out, in[i])
	}
	return out
}
