package importexport

import "strings"

// FieldCatalog returns canonical fields for an entity type (import mapping + export schemas).
func FieldCatalog(entity EntityType) []FieldDef {
	switch entity {
	case EntityCustomers:
		return []FieldDef{
			{Key: "full_name", Label: "Full name", Required: true, Type: "string"},
			{Key: "full_name_ar", Label: "Full name (AR)", Required: false, Type: "string"},
			{Key: "phone", Label: "Phone", Required: true, Type: "phone"},
			{Key: "email", Label: "Email", Required: false, Type: "string"},
			{Key: "nationality", Label: "Nationality", Required: false, Type: "string"},
			{Key: "passport_no", Label: "Passport", Required: false, Type: "string"},
			{Key: "date_of_birth", Label: "Date of birth", Required: false, Type: "date"},
			{Key: "notes", Label: "Notes", Required: false, Type: "string"},
			{Key: "special_requirements", Label: "Special requirements", Required: false, Type: "string"},
		}
	case EntityBookings:
		return []FieldDef{
			{Key: "customer_phone", Label: "Customer phone", Required: true, Type: "phone"},
			{Key: "departure_code", Label: "Departure code", Required: true, Type: "string"},
			{Key: "status", Label: "Status", Required: false, Type: "status"},
			{Key: "pax_count", Label: "Pax count", Required: false, Type: "int"},
			{Key: "total_amount", Label: "Total amount", Required: false, Type: "money"},
			{Key: "currency", Label: "Currency", Required: false, Type: "currency"},
			{Key: "notes", Label: "Notes", Required: false, Type: "string"},
		}
	case EntityPayments:
		return []FieldDef{
			{Key: "booking_id", Label: "Booking ID", Required: true, Type: "string"},
			{Key: "amount", Label: "Amount", Required: true, Type: "money"},
			{Key: "currency", Label: "Currency", Required: false, Type: "currency"},
			{Key: "method", Label: "Method", Required: false, Type: "string"},
			{Key: "reference", Label: "Reference", Required: false, Type: "string"},
			{Key: "status", Label: "Status", Required: false, Type: "status"},
			{Key: "note", Label: "Note", Required: false, Type: "string"},
		}
	case EntityDepartures:
		return []FieldDef{
			{Key: "package_code", Label: "Package code", Required: true, Type: "string"},
			{Key: "code", Label: "Departure code", Required: true, Type: "string"},
			{Key: "depart_date", Label: "Depart date", Required: true, Type: "date"},
			{Key: "return_date", Label: "Return date", Required: false, Type: "date"},
			{Key: "capacity_total", Label: "Capacity", Required: false, Type: "int"},
			{Key: "base_price", Label: "Base price", Required: false, Type: "money"},
			{Key: "currency", Label: "Currency", Required: false, Type: "currency"},
		}
	default:
		return nil
	}
}

// AliasMap maps normalized header tokens → canonical field keys.
var fieldAliases = map[string]string{
	"phone": "phone", "mobile": "phone", "tel": "phone", "telephone": "phone",
	"cell": "phone", "cellphone": "phone", "customer_phone": "customer_phone",
	"name": "full_name", "full_name": "full_name", "fullname": "full_name",
	"customer_name": "full_name", "full name": "full_name",
	"full_name_ar": "full_name_ar", "name_ar": "full_name_ar", "arabic_name": "full_name_ar",
	"email": "email", "e_mail": "email", "mail": "email",
	"nationality": "nationality", "nation": "nationality", "country": "nationality",
	"passport": "passport_no", "passport_no": "passport_no", "passport_number": "passport_no",
	"date_of_birth": "date_of_birth", "dob": "date_of_birth", "birth_date": "date_of_birth",
	"birthday": "date_of_birth", "birthdate": "date_of_birth",
	"notes": "notes", "note": "note", "special_requirements": "special_requirements",
	"status": "status", "state": "status",
	"currency": "currency", "curr": "currency",
	"departure_code": "departure_code", "depart_code": "departure_code",
	"pax": "pax_count", "pax_count": "pax_count", "passengers": "pax_count",
	"total": "total_amount", "total_amount": "total_amount", "amount": "amount",
	"booking_id": "booking_id", "booking": "booking_id",
	"method": "method", "payment_method": "method",
	"reference": "reference", "ref": "reference",
	"package_code": "package_code", "package": "package_code",
	"code": "code", "departure": "code",
	"depart_date": "depart_date", "departure_date": "depart_date", "start_date": "depart_date",
	"return_date": "return_date", "end_date": "return_date",
	"capacity": "capacity_total", "capacity_total": "capacity_total",
	"base_price": "base_price", "price": "base_price",
}

// SuggestMapping auto-maps source headers to canonical fields using aliases.
func SuggestMapping(headers []string) map[string]string {
	out := make(map[string]string)
	used := make(map[string]bool)
	for _, h := range headers {
		canon := MatchAlias(h)
		if canon == "" || used[canon] {
			continue
		}
		out[canon] = h
		used[canon] = true
	}
	return out
}

// MatchAlias returns the canonical field for a header, or empty if unknown.
func MatchAlias(header string) string {
	key := normalizeHeaderKey(header)
	if key == "" {
		return ""
	}
	if v, ok := fieldAliases[key]; ok {
		return v
	}
	// try without spaces already handled; also try underscored form of spaces
	return ""
}

func normalizeHeaderKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return s
}

// MapRow applies mapping (canonical→header) to a header-indexed row, returning canonical→value.
func MapRow(headers []string, row []string, mapping map[string]string) map[string]string {
	idx := make(map[string]int, len(headers))
	for i, h := range headers {
		idx[h] = i
	}
	out := make(map[string]string, len(mapping))
	for canon, header := range mapping {
		i, ok := idx[header]
		if !ok || i >= len(row) {
			out[canon] = ""
			continue
		}
		out[canon] = strings.TrimSpace(row[i])
	}
	return out
}
