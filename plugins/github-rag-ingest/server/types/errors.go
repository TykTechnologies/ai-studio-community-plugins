package types

import "errors"

// Repository validation errors
var (
	ErrRepositoryNameRequired       = errors.New("repository name is required")
	ErrRepositoryURLRequired        = errors.New("repository URL is required")
	ErrRepositoryBranchRequired     = errors.New("repository branch is required")
	ErrRepositoryDatasourceRequired = errors.New("datasource ID is required")
	ErrInvalidChunkSize             = errors.New("chunk size must be greater than 0")
	ErrInvalidChunkOverlap          = errors.New("chunk overlap must be >= 0 and < chunk size")
)

// Storage errors
var (
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrJobNotFound        = errors.New("job not found")
	ErrSecretNotFound     = errors.New("secret not found")
)

// Git errors
var (
	ErrGitCloneFailed  = errors.New("failed to clone repository")
	ErrGitFetchFailed  = errors.New("failed to fetch repository")
	ErrInvalidAuthType = errors.New("invalid authentication type")
)
