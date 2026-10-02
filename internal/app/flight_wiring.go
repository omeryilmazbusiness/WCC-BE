package app

import (
	"log/slog"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/travelpayouts"
	appflight "github.com/wodi-crm/wodi-crm-be/internal/app/flight"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

func newFlightService(cfg config.FlightsConfig, log *slog.Logger) *appflight.Service {
	tp := travelpayouts.Config{
		APIURL: cfg.APIURL, AutocompleteURL: cfg.AutocompleteURL,
		Token: cfg.TravelpayoutsToken, Market: cfg.Market, Timeout: cfg.Timeout,
	}
	if cfg.TravelpayoutsToken == "" {
		log.Info("flight search disabled: TRAVELPAYOUTS_TOKEN not set")
	}
	return appflight.NewService(
		travelpayouts.NewFares(tp),
		travelpayouts.NewPlaces(tp),
		travelpayouts.NewAirlines(tp, log),
		domain.AviasalesLinker{BaseURL: cfg.AviasalesURL, Marker: cfg.TravelpayoutsMarker},
		appflight.Options{Enabled: cfg.TravelpayoutsToken != "", Log: log},
	)
}
