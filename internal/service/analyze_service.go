// internal/service/analyze_service.go

package service

import (
	"iossa/internal/github"
	"iossa/internal/models"
)

func AnalyzeRepo(req models.AnalyzeRequest) models.AnalyzeResponse {

	owner, repo, err := github.ParseRepoURL(req.RepoURL)
	if err != nil {
		return models.AnalyzeResponse{
			Message: "Error: " + err.Error(),
		}
	}

	// returns owner and repo for now. 
	return models.AnalyzeResponse{
		Message: "Parsed repo - owner: " + owner + ", repo: " + repo,
	}

}