package handlers

import (
	"context"
	"net/http"
	"net/mail"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

type settingsOutput struct {
	Body store.Settings
}

type settingsInput struct {
	Body store.Settings
}

// validEmail accepts a bare address ("ops@example.com"), not a display-name
// form like "Ops <ops@example.com>": the value is used as a recipient as is.
func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

// registerSettings adds GET and PUT /api/admin/settings: the canonical
// "store something per shop" loop (form, API route, GORM upsert,
// Redis-cached read) the Settings page demonstrates.
func (h API) registerSettings(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getSettings",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "The shop's settings, or the defaults",
	}, func(ctx context.Context, _ *struct{}) (*settingsOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		settings, err := h.Store.GetSettings(ctx, session.Shop)
		if err != nil {
			h.Logger.Error("[Settings] Read failed", "shop", session.Shop, "error", err)
			return nil, huma.Error500InternalServerError("try again")
		}
		return &settingsOutput{Body: settings}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "saveSettings",
		Method:      http.MethodPut,
		Path:        "/settings",
		Summary:     "Save the shop's settings",
		Errors:      []int{http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *settingsInput) (*settingsOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		// Validation lives here, server-side; the form only mirrors it.
		settings := input.Body
		settings.NotificationEmail = strings.TrimSpace(settings.NotificationEmail)
		if settings.NotificationEmail != "" && !validEmail(settings.NotificationEmail) {
			return nil, huma.Error422UnprocessableEntity("notificationEmail must be an email address")
		}
		saved, err := h.Store.SaveSettings(ctx, session.Shop, settings)
		if err != nil {
			h.Logger.Error("[Settings] Save failed", "shop", session.Shop, "error", err)
			return nil, huma.Error500InternalServerError("try again")
		}
		// Dynamic subscriptions can depend on settings (see the registry's
		// When), so a save re-syncs them.
		if _, err := h.River.Insert(ctx, jobs.SyncWebhooksArgs{Shop: session.Shop, Host: h.Host}, nil); err != nil {
			// The save stands; the next app open re-syncs anyway.
			h.Logger.Warn("[Settings] Could not queue webhook sync", "shop", session.Shop, "error", err)
		}
		return &settingsOutput{Body: saved}, nil
	})
}
