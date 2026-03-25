package main

import (
	"net/http"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/rancher/norman/clientbase"
)

var (
	ErrWaitRequiredResources      = errors.New("Wait required resources")
	ErrRancherCredentialsNotFound = errors.New("Rancher credentials not found")
	ErrRancherTokenNotFound       = errors.New("Rancher token not found")
)

// IsErrWaitRequiredResources returns true if the supplied error is or wraps an ErrWaitRequiredResources error.
func IsErrWaitRequiredResources(err error) bool {
	return errors.Is(err, ErrWaitRequiredResources)
}

// IsErrRancherCredentialsNotFound returns true if the supplied error is or wraps an ErrRancherCredentialsNotFound error.
func IsErrRancherCredentialsNotFound(err error) bool {
	return errors.Is(err, ErrRancherCredentialsNotFound)
}

// IsErrRancherTokenNotFound returns true if the supplied error is or wraps an ErrRancherTokenNotFound error.
func IsErrRancherTokenNotFound(err error) bool {
	return errors.Is(err, ErrRancherTokenNotFound)
}

func IsNotFound(err error) bool {
	return clientbase.IsNotFound(err)
}

// IsForbidden checks if the given APIError is a Forbidden HTTP statuscode
func IsForbidden(err error) bool {
	apiError, ok := err.(*clientbase.APIError)
	if !ok {
		return false
	}

	return apiError.StatusCode == http.StatusForbidden
}

// IsUnauthorized checks if the given APIError is an Unauthorized HTTP statuscode
func IsUnauthorized(err error) bool {
	apiError, ok := err.(*clientbase.APIError)
	if !ok {
		return false
	}

	return apiError.StatusCode == http.StatusUnauthorized
}
