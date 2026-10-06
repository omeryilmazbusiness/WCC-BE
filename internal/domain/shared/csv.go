package shared

// NeutralizeCSVCell stops spreadsheet apps from evaluating a cell as a
// formula (CSV injection) by prefixing risky leading characters with '.
func NeutralizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// RestoreCSVCell reverses NeutralizeCSVCell so a file exported by us can be
// re-imported without a stray quote (e.g. on "+9665…" phone numbers).
func RestoreCSVCell(s string) string {
	if len(s) < 2 || s[0] != '\'' {
		return s
	}
	switch s[1] {
	case '=', '+', '-', '@', '\t', '\r':
		return s[1:]
	}
	return s
}

// CSVBOM is the UTF-8 byte order mark Excel needs to detect UTF-8 (Arabic).
const CSVBOM = "\ufeff"
