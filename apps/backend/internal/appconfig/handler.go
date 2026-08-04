package appconfig

import (
	"encoding/json"
	"net/http"
)

const DemoMessage = "SMS в демо-режиме не отправляется. Используйте согласованный номер телефона и код:"

type Handler struct {
	demoEnabled bool
	demoOTPCode string
}

func NewHandler(demoEnabled bool, demoOTPCode string) *Handler {
	return &Handler{demoEnabled: demoEnabled, demoOTPCode: demoOTPCode}
}

func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/app-config", handler.get)
}

func (handler *Handler) get(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	if !handler.demoEnabled {
		_ = json.NewEncoder(response).Encode(map[string]bool{"demo_mode": false})
		return
	}
	_ = json.NewEncoder(response).Encode(struct {
		DemoMode    bool   `json:"demo_mode"`
		Message     string `json:"message"`
		DemoOTPCode string `json:"demo_otp_code"`
	}{
		DemoMode:    true,
		Message:     DemoMessage,
		DemoOTPCode: handler.demoOTPCode,
	})
}
