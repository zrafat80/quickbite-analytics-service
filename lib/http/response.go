// Package http (lib/http) implements the response envelope:
//
//	success: {"success": true, "data": ...}
//	error:   {"success": false, "error": {"code": "...", "message": "..."}}
//
// Every handler funnels here. Controllers must build Response DTOs and pass
// them to SendSuccess so DB columns never leak.
package http

import (
	"encoding/json"
	"net/http"
)

type SuccessEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
	Meta    any  `json:"meta,omitempty"`
}

type ErrorEnvelope struct {
	Success bool      `json:"success"`
	Error   ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func SendSuccess(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(SuccessEnvelope{Success: true, Data: data})
}

func SendPaginated(w http.ResponseWriter, status int, data, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(SuccessEnvelope{Success: true, Data: data, Meta: meta})
}

func SendError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Success: false,
		Error:   ErrorBody{Code: code, Message: message},
	})
}
