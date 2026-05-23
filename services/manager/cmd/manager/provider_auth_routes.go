package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

func registerProviderAuthRoutes(mux *http.ServeMux, stateDir, codexBridgeURL string) {
	mux.HandleFunc("/providers/copilot/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, sageagents.CopilotAuthState(stateDir))
	})

	mux.HandleFunc("/providers/copilot/login/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		clientID := ""
		var body struct {
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(body.ClientID) != "" {
			clientID = strings.TrimSpace(body.ClientID)
		}
		result, err := sageagents.StartGitHubDeviceLogin(clientID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/providers/copilot/login/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		clientID := ""
		var body struct {
			ClientID   string `json:"clientId"`
			DeviceCode string `json:"deviceCode"`
			Token      string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(body.ClientID) != "" {
			clientID = strings.TrimSpace(body.ClientID)
		}
		var err error
		if strings.TrimSpace(body.Token) != "" {
			err = sageagents.SaveGitHubOAuthToken(stateDir, body.Token)
		} else {
			err = sageagents.CompleteGitHubDeviceLogin(stateDir, clientID, body.DeviceCode)
		}
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sageagents.CopilotAuthState(stateDir))
	})

	mux.HandleFunc("/providers/copilot/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := sageagents.DeleteGitHubOAuthToken(stateDir); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sageagents.CopilotAuthState(stateDir))
	})

	mux.HandleFunc("/providers/copilot/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if _, err := sageagents.GetCopilotToken(stateDir); err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sageagents.CopilotAuthState(stateDir))
	})

	mux.HandleFunc("/providers/codex/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		status := sageagents.NewCodexBridgeClient(codexBridgeURL).Status(r.Context(), sageagents.DefaultCodexModel)
		writeJSON(w, http.StatusOK, status)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("HTTP JSON response write failed: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
