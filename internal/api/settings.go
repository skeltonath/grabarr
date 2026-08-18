package api

import (
	"encoding/json"
	"net/http"

	"grabarr/internal/config"
)

// SettingsResponse is the payload for the settings page. The descriptors carry
// their own labels, bounds and kinds so the UI does not have to hard-code a
// form field per setting.
type SettingsResponse struct {
	Settings []config.Setting `json:"settings"`
	// PerJobBandwidthKiBps is what the bandwidth limit works out to for a
	// single transfer, so the UI can show the effect of splitting the total
	// across the concurrent slots.
	PerJobBandwidthKiBps int `json:"per_job_bandwidth_kib_s"`
}

// GetSettings returns every runtime-tunable setting with its current value and
// the value config.yaml specifies.
func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	h.writeSuccess(w, http.StatusOK, h.settingsPayload(), "")
}

// UpdateSettings applies a batch of setting changes. A null value clears the
// override, returning that setting to its config.yaml value.
func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	if len(updates) == 0 {
		h.writeError(w, http.StatusBadRequest, "no settings provided", nil)
		return
	}

	if err := h.config.UpdateSettings(updates); err != nil {
		// Every failure here is a rejected value or an unknown key, which is
		// the caller's problem rather than a server fault.
		h.writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, h.settingsPayload(), "Settings updated")
}

func (h *Handlers) settingsPayload() SettingsResponse {
	return SettingsResponse{
		Settings:             h.config.Settings(),
		PerJobBandwidthKiBps: h.config.PerJobBandwidthKiBps(),
	}
}
