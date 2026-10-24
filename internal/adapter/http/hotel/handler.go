// Package hotel exposes hotels & contracting over HTTP.
package hotel

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/hotel"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/hotel"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

func seesCost(r *http.Request) bool {
	claims, ok := middleware.ClaimsFrom(r.Context())
	return ok && platformauth.HasPermission(claims.Role, platformauth.PermHotelsPricing)
}

type Handler struct {
	Svc *appsvc.Service
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return false
	}
	return true
}

// day parses an optional YYYY-MM-DD field; blank stays zero for the domain to report.
func day(field, v string) (time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return time.Time{}, nil
	}
	t, err := domain.ParseDay(v)
	if err != nil {
		e := shared.NewValidation(field + " must be YYYY-MM-DD")
		e.Details = map[string]any{field: "YYYY-MM-DD"}
		return time.Time{}, e
	}
	return t, nil
}

func dayRange(start, end string) (time.Time, time.Time, error) {
	s, err := day("start_date", start)
	if err != nil {
		return s, s, err
	}
	e, err := day("end_date", end)
	return s, e, err
}

type monthlyWire struct {
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	RoomType string `json:"room_type"`
	Net      int64  `json:"net"`
	Sell     int64  `json:"sell"`
}

func (w monthlyWire) rate() domain.MonthlyRate {
	return domain.MonthlyRate{Year: w.Year, Month: w.Month, RoomType: w.RoomType, Net: w.Net, Sell: w.Sell}
}

func asMonthly(rows []monthlyWire) []domain.MonthlyRate {
	out := make([]domain.MonthlyRate, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.rate())
	}
	return out
}

type hotelRequest struct {
	Name         string                     `json:"name"`
	NameAr       string                     `json:"name_ar"`
	Stars        int                        `json:"stars"`
	Location     domain.Location            `json:"location"`
	Contact      domain.Contact             `json:"contact"`
	RoomTypes    []string                   `json:"room_types"`
	MealPlans    []string                   `json:"meal_plans"`
	Currency     string                     `json:"currency"`
	Markup       domain.Markup              `json:"markup"`
	ChildPolicy  *domain.ChildPolicy        `json:"child_policy"`
	Cancellation *domain.CancellationPolicy `json:"cancellation"`
	Notes        string                     `json:"notes"`
	IsActive     *bool                      `json:"is_active"`
	Monthly      []monthlyWire              `json:"monthly_rates"`
}

func (req hotelRequest) input() appsvc.HotelInput {
	return appsvc.HotelInput{
		Name: req.Name, NameAr: req.NameAr, Stars: req.Stars, Location: req.Location, Contact: req.Contact,
		RoomTypes: req.RoomTypes, MealPlans: req.MealPlans, Currency: req.Currency, Markup: req.Markup,
		ChildPolicy: req.ChildPolicy, Cancellation: req.Cancellation, Notes: req.Notes, IsActive: req.IsActive,
		Monthly: asMonthly(req.Monthly),
	}
}

func mapHotel(h *domain.Hotel, cost bool) map[string]any {
	row := map[string]any{
		"id": h.ID, "branch_id": h.BranchID, "name": h.Name, "name_ar": h.NameAr, "stars": h.Stars,
		"location": h.Location, "contact": h.Contact, "room_types": nonNil(h.RoomTypes), "meal_plans": nonNil(h.MealPlans),
		"currency": h.Currency, "child_policy": h.ChildPolicy, "cancellation": h.Cancellation,
		"notes": h.Notes, "is_active": h.IsActive,
		"created_at": h.CreatedAt.UTC().Format(time.RFC3339), "updated_at": h.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if cost {
		row["markup"] = h.Markup
	}
	return row
}

func mapMonthly(m domain.MonthlyRate, cost bool) map[string]any {
	row := map[string]any{
		"year": m.Year, "month": m.Month, "room_type": m.RoomType, "sell": m.Sell,
	}
	if cost {
		row["net"] = m.Net
	}
	return row
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func mapSeason(s *domain.Season, h *domain.Hotel, cost bool) map[string]any {
	rates := s.Rates
	if rates == nil {
		rates = []domain.Rate{}
	}
	if !cost {
		m := s.MarkupFor(h)
		out := make([]domain.Rate, len(rates))
		for i, r := range rates {
			out[i] = domain.Rate{
				RoomType: r.RoomType, MealPlan: r.MealPlan,
				Single: m.Apply(r.Single, 1), Double: m.Apply(r.Double, 1),
				Triple: m.Apply(r.Triple, 1), Quad: m.Apply(r.Quad, 1),
			}
		}
		rates = out
	}
	row := map[string]any{
		"id": s.ID, "hotel_id": s.HotelID, "name": s.Name, "kind": s.Kind,
		"start_date": domain.FormatDay(s.Start), "end_date": domain.FormatDay(s.End),
		"rates": rates,
	}
	if cost {
		row["markup"] = s.Markup
	}
	return row
}

func mapAllotment(a *domain.Allotment, today time.Time) map[string]any {
	return map[string]any{
		"id": a.ID, "hotel_id": a.HotelID, "room_type": a.RoomType, "kind": a.Kind,
		"start_date": domain.FormatDay(a.Start), "end_date": domain.FormatDay(a.End),
		"rooms": a.Rooms, "sold": a.Sold, "release_days": a.ReleaseDays, "notes": a.Notes,
		"release_date": domain.FormatDay(a.ReleaseDate()), "status": a.Status(today), "available": a.Available(today),
	}
}

func mapStopSale(s *domain.StopSale) map[string]any {
	return map[string]any{
		"id": s.ID, "hotel_id": s.HotelID, "start_date": domain.FormatDay(s.Start), "end_date": domain.FormatDay(s.End),
		"room_type": s.RoomType, "reason": s.Reason, "created_at": s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h Handler) mapDetail(d *domain.Detail, cost bool) map[string]any {
	today := h.Svc.Today()
	seasons := make([]map[string]any, 0, len(d.Seasons))
	for i := range d.Seasons {
		seasons = append(seasons, mapSeason(&d.Seasons[i], &d.Hotel, cost))
	}
	if !cost {
		domain.RedactCost(&d.Hotel)
		for i := range d.Monthly {
			domain.RedactMonthly(&d.Monthly[i])
		}
	}
	monthly := make([]map[string]any, 0, len(d.Monthly))
	for _, m := range d.Monthly {
		monthly = append(monthly, mapMonthly(m, cost))
	}
	allotments := make([]map[string]any, 0, len(d.Allotments))
	for i := range d.Allotments {
		allotments = append(allotments, mapAllotment(&d.Allotments[i], today))
	}
	stops := make([]map[string]any, 0, len(d.StopSales))
	for i := range d.StopSales {
		stops = append(stops, mapStopSale(&d.StopSales[i]))
	}
	return map[string]any{
		"hotel": mapHotel(&d.Hotel, cost), "seasons": seasons, "monthly_rates": monthly,
		"allotments": allotments, "stop_sales": stops, "today": domain.FormatDay(today),
	}
}

// List: GET /hotels?q=&city=&active=false.
func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Svc.List(r.Context(), domain.ListFilter{
		BranchID: branchID, Query: request.FilterString(r, "q"), City: request.FilterString(r, "city"),
		ActiveOnly: request.FilterString(r, "active") == "true",
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	cost := seesCost(r)
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		s := &items[i]
		if !cost {
			domain.RedactSummary(s)
		}
		row := mapHotel(&s.Hotel, cost)
		sum := map[string]any{
			"season_name": s.SeasonName, "season_kind": s.SeasonKind, "from_sell": s.FromSell,
			"rooms_total": s.RoomsTotal, "rooms_sold": s.RoomsSold, "next_release": dayPtr(s.NextRelease),
			"stop_sale_today": s.StopSaleToday, "contract_files": s.ContractFiles,
			"seasons_count": s.SeasonsCount, "allotment_count": s.AllotmentCount,
		}
		if cost {
			sum["from_net"] = s.FromNet
		}
		row["summary"] = sum
		out = append(out, row)
	}
	response.JSON(w, http.StatusOK, out)
}

func dayPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return domain.FormatDay(*t)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var req hotelRequest
	if !decode(w, r, &req) {
		return
	}
	if !seesCost(r) {
		req.Monthly = nil
	}
	d, err := h.Svc.Create(r.Context(), branchID, req.input())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, h.mapDetail(d, true))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, h.mapDetail(d, seesCost(r)))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req hotelRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := h.Svc.Update(r.Context(), id, req.input())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, h.mapDetail(d, true))
}

func (h Handler) ReplaceMonthlyRates(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Year  int           `json:"year"`
		Rates []monthlyWire `json:"rates"`
	}
	if !decode(w, r, &req) {
		return
	}
	d, err := h.Svc.ReplaceMonthlyRates(r.Context(), id, req.Year, asMonthly(req.Rates))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, h.mapDetail(d, true))
}

type seasonRequest struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	StartDate string         `json:"start_date"`
	EndDate   string         `json:"end_date"`
	Markup    *domain.Markup `json:"markup"`
	Rates     []domain.Rate  `json:"rates"`
}

func (h Handler) saveSeason(w http.ResponseWriter, r *http.Request, seasonID *uuid.UUID) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req seasonRequest
	if !decode(w, r, &req) {
		return
	}
	start, end, err := dayRange(req.StartDate, req.EndDate)
	if err != nil {
		response.Error(w, err)
		return
	}
	s, err := h.Svc.SaveSeason(r.Context(), id, seasonID, appsvc.SeasonInput{
		Name: req.Name, Kind: req.Kind, Start: start, End: end, Markup: req.Markup, Rates: req.Rates,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	status := http.StatusOK
	if seasonID == nil {
		status = http.StatusCreated
	}
	response.JSON(w, status, mapSeason(s, nil, true))
}

func (h Handler) CreateSeason(w http.ResponseWriter, r *http.Request) { h.saveSeason(w, r, nil) }

func (h Handler) UpdateSeason(w http.ResponseWriter, r *http.Request) {
	sid, ok := pathID(w, r, "seasonId")
	if !ok {
		return
	}
	h.saveSeason(w, r, &sid)
}

func (h Handler) DeleteSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	sid, ok := pathID(w, r, "seasonId")
	if !ok {
		return
	}
	if err := h.Svc.DeleteSeason(r.Context(), id, sid); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type allotmentRequest struct {
	RoomType    string `json:"room_type"`
	Kind        string `json:"kind"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Rooms       int    `json:"rooms"`
	ReleaseDays int    `json:"release_days"`
	Notes       string `json:"notes"`
}

func (h Handler) saveAllotment(w http.ResponseWriter, r *http.Request, allotmentID *uuid.UUID) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req allotmentRequest
	if !decode(w, r, &req) {
		return
	}
	start, end, err := dayRange(req.StartDate, req.EndDate)
	if err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Svc.SaveAllotment(r.Context(), id, allotmentID, appsvc.AllotmentInput{
		RoomType: req.RoomType, Kind: req.Kind, Start: start, End: end,
		Rooms: req.Rooms, ReleaseDays: req.ReleaseDays, Notes: req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	status := http.StatusOK
	if allotmentID == nil {
		status = http.StatusCreated
	}
	response.JSON(w, status, mapAllotment(a, h.Svc.Today()))
}

func (h Handler) CreateAllotment(w http.ResponseWriter, r *http.Request) { h.saveAllotment(w, r, nil) }

func (h Handler) UpdateAllotment(w http.ResponseWriter, r *http.Request) {
	aid, ok := pathID(w, r, "allotmentId")
	if !ok {
		return
	}
	h.saveAllotment(w, r, &aid)
}

func (h Handler) AdjustAllotment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	aid, ok := pathID(w, r, "allotmentId")
	if !ok {
		return
	}
	var req struct {
		Delta int `json:"delta"`
	}
	if !decode(w, r, &req) {
		return
	}
	a, err := h.Svc.AdjustAllotment(r.Context(), id, aid, req.Delta)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAllotment(a, h.Svc.Today()))
}

func (h Handler) DeleteAllotment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	aid, ok := pathID(w, r, "allotmentId")
	if !ok {
		return
	}
	if err := h.Svc.DeleteAllotment(r.Context(), id, aid); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) CreateStopSale(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		RoomType  string `json:"room_type"`
		Reason    string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	start, end, err := dayRange(req.StartDate, req.EndDate)
	if err != nil {
		response.Error(w, err)
		return
	}
	s, err := h.Svc.CreateStopSale(r.Context(), id, claims.UserID, appsvc.StopSaleInput{
		Start: start, End: end, RoomType: req.RoomType, Reason: req.Reason,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapStopSale(s))
}

func (h Handler) DeleteStopSale(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	sid, ok := pathID(w, r, "stopSaleId")
	if !ok {
		return
	}
	if err := h.Svc.DeleteStopSale(r.Context(), id, sid); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type quoteRequest struct {
	CheckIn  string         `json:"check_in"`
	CheckOut string         `json:"check_out"`
	RoomType string         `json:"room_type"`
	MealPlan string         `json:"meal_plan"`
	Rooms    int            `json:"rooms"`
	Adults   int            `json:"adults"`
	Children []domain.Child `json:"children"`
	ExtraBed bool           `json:"extra_bed"`
}

// Quote: POST /hotels/{id}/quote prices a stay from the contract.
func (h Handler) Quote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req quoteRequest
	if !decode(w, r, &req) {
		return
	}
	in, err := day("check_in", req.CheckIn)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := day("check_out", req.CheckOut)
	if err != nil {
		response.Error(w, err)
		return
	}
	q, err := h.Svc.Quote(r.Context(), id, domain.QuoteRequest{
		CheckIn: in, CheckOut: out, RoomType: strings.ToLower(strings.TrimSpace(req.RoomType)),
		MealPlan: strings.ToLower(strings.TrimSpace(req.MealPlan)), Rooms: req.Rooms, Adults: req.Adults,
		Children: req.Children, ExtraBed: req.ExtraBed,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	if !seesCost(r) {
		domain.RedactQuote(q)
	}
	response.JSON(w, http.StatusOK, q)
}
