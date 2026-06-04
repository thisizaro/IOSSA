package github

import "time"

// GraphQLResponse wraps a raw GitHub GraphQL response.
type GraphQLResponse struct {
	Data   []byte         `json:"data"`
	Errors []GraphQLError `json:"errors"`
}

// GraphQLError represents a single GraphQL error.
type GraphQLError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// RateLimitInfo holds rate limit data returned by every GraphQL response.
type RateLimitInfo struct {
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
	Cost      int       `json:"cost"`
}

// GHRepository is the repository data fetched via GraphQL.
type GHRepository struct {
	NameWithOwner    string     `json:"nameWithOwner"`
	Description      string     `json:"description"`
	StargazerCount   int        `json:"stargazerCount"`
	ForkCount        int        `json:"forkCount"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	HasIssuesEnabled bool       `json:"hasIssuesEnabled"`
	CodeOfConduct    *struct {
		Name string `json:"name"`
	} `json:"codeOfConduct"`
	BeginnerIssues struct {
		TotalCount int `json:"totalCount"`
	} `json:"beginnerIssues"`
	Issues struct {
		PageInfo PageInfo  `json:"pageInfo"`
		Nodes    []GHIssue `json:"nodes"`
	} `json:"issues"`
	PullRequests struct {
		PageInfo PageInfo `json:"pageInfo"`
		Nodes    []GHPR   `json:"nodes"`
	} `json:"pullRequests"`
}

// PageInfo holds GraphQL pagination cursors.
type PageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// GHIssue represents a GitHub issue from GraphQL.
type GHIssue struct {
	Number    int        `json:"number"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
	Author    *GHActor   `json:"author"`
	Labels    struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
}

// GHPR represents a GitHub pull request from GraphQL.
type GHPR struct {
	Number    int        `json:"number"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
	MergedAt  *time.Time `json:"mergedAt"`
	Author    *GHActor   `json:"author"`
}

// GHActor represents a GitHub user actor.
type GHActor struct {
	Login     string `json:"login"`
	AvatarUrl string `json:"avatarUrl"`
}

// GHCommit represents a commit from the REST API.
type GHCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author *struct {
		Login     string `json:"login"`
		AvatarUrl string `json:"avatar_url"`
	} `json:"author"`
}

// GHComment represents an issue comment from the REST API.
type GHComment struct {
	ID   int `json:"id"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}
