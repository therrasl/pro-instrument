package appconfig

import (
	"encoding/json"
	"net/http"
)

const DemoMessage = "SMS в демо-режиме не отправляется. Используйте согласованный номер телефона и код:"

type Handler struct {
	demoEnabled   bool
	demoOTPCode   string
	pickupAddress string
}

func NewHandler(demoEnabled bool, demoOTPCode string, pickupAddress string) *Handler {
	return &Handler{
		demoEnabled:   demoEnabled,
		demoOTPCode:   demoOTPCode,
		pickupAddress: pickupAddress,
	}
}

func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/app-config", handler.get)
}

func (handler *Handler) get(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(struct {
		DemoMode      bool   `json:"demo_mode"`
		Message       string `json:"message,omitempty"`
		DemoOTPCode   string `json:"demo_otp_code,omitempty"`
		PickupAddress string `json:"pickup_address"`
	}{
		DemoMode:      handler.demoEnabled,
		Message:       demoMessage(handler.demoEnabled),
		DemoOTPCode:   demoCode(handler.demoEnabled, handler.demoOTPCode),
		PickupAddress: handler.pickupAddress,
	})
}

func demoMessage(enabled bool) string {
	if enabled {
		return DemoMessage
	}
	return ""
}

func demoCode(enabled bool, code string) string {
	if enabled {
		return code
	}
	return ""
}
