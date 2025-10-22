package api

import (
	"net/http"
	"time"
)

var startTime = time.Now()

func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(startTime).String(),
		"version":   "1.0.0", // TODO: Get from build info
	}

	// Check resource status
	if h.gatekeeper != nil {
		resourceStatus := h.gatekeeper.GetResourceStatus()
		health["resources"] = resourceStatus
	}

	h.writeSuccess(w, http.StatusOK, health, "Service is healthy")
}

func (h *Handlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := make(map[string]interface{})

	// Add resource status
	if h.gatekeeper != nil {
		metrics["resources"] = h.gatekeeper.GetResourceStatus()
	}

	// Add job queue metrics
	summary, err := h.queue.GetSummary()
	if err == nil {
		metrics["jobs"] = summary
	}

	h.writeSuccess(w, http.StatusOK, metrics, "")
}

func (h *Handlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"service":   "grabarr",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(startTime).String(),
	}

	// Get job summary
	if summary, err := h.queue.GetSummary(); err == nil {
		status["jobs"] = summary
	}

	// Get resource status
	if h.gatekeeper != nil {
		status["resources"] = h.gatekeeper.GetResourceStatus()
	}

	h.writeSuccess(w, http.StatusOK, status, "")
}
