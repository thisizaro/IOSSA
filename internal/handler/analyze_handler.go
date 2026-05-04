package handler

import (
	"encoding/json"
	"net/http"

	"iossa/internal/models"
	"iossa/internal/service"
)

func AnalyzeHandler(w http.ResponseWriter, r *http.Request) {
	var req models.AnalyzeRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.RepoURL == "" {
		http.Error(w, "repo_url is required", http.StatusBadRequest)
		return
	}

	result := service.AnalyzeRepo(req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}