// Package github is a thin GitHub REST API client scoped to the
// operations CloudOracle's PR-comment integration needs: listing,
// posting, and updating issue comments. It is deliberately not a full
// SDK — webhooks, branches, releases, reactions, and review comments
// are out of scope.
//
// The package only deserialises the fields it actually uses; unknown
// JSON keys in GitHub's responses are ignored. This keeps the package
// resilient to API additions without forcing dependency churn.
package github

import "time"

// Repo identifies a GitHub repository by its owner login and repo
// name. Both are required by every endpoint this package calls.
type Repo struct {
	Owner string // e.g. "Cro22"
	Name  string // e.g. "CloudOracle"
}

// Comment is a minimal projection of GitHub's issue/PR comment payload.
// We only deserialise the fields the marker-matching and update flow
// need; GitHub's full comment object is much larger and would tie us
// to fields we don't read.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}
