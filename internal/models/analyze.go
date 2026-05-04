package models

type AnalyzeRequest struct {
	RepoURL string `json:"repo_url"`
	Since   string `json:"since,omitempty"`
}

type AnalyzeResponse struct {
	Message string `json:"message"`
}