// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package clearbit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/httpclient"
)

// Client represents a Clearbit API client
type Client struct {
	config     Config
	httpClient *httpclient.Client
}

// FindCompanyByName searches for a company by name using Clearbit's Company API
func (c *Client) FindCompanyByName(ctx context.Context, name string) (*ClearbitCompany, error) {
	// Build the URL with query parameters
	u, err := url.Parse(fmt.Sprintf("%s/v1/domains/find", c.config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("name", name)
	u.RawQuery = q.Encode()

	var company ClearbitCompany
	err = c.makeRequest(ctx, u.String(), &company)
	if err != nil {
		return nil, err
	}
	return &company, nil
}

// FindCompanyByDomain searches for a company by domain using Clearbit's Company API
func (c *Client) FindCompanyByDomain(ctx context.Context, domain string) (*ClearbitCompany, error) {
	// Build the URL with query parameters
	u, err := url.Parse(fmt.Sprintf("%s/v2/companies/find", c.config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("domain", domain)
	u.RawQuery = q.Encode()

	var company ClearbitCompany
	err = c.makeRequest(ctx, u.String(), &company)
	if err != nil {
		return nil, err
	}
	return &company, nil
}

// SuggestCompanies searches for company suggestions using Clearbit's Autocomplete API
func (c *Client) SuggestCompanies(ctx context.Context, query string) ([]ClearbitCompanySuggestion, error) {
	// Build the URL with query parameters
	u, err := url.Parse(fmt.Sprintf("%s/v1/companies/suggest", c.config.AutocompleteBaseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse autocomplete base URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	var suggestions []ClearbitCompanySuggestion
	err = c.makeRequest(ctx, u.String(), &suggestions)
	if err != nil {
		return nil, err
	}
	return suggestions, nil
}

// makeRequest performs the HTTP request to Clearbit API using the generic HTTP client
func (c *Client) makeRequest(ctx context.Context, url string, model any) error {
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", c.config.APIKey),
	}

	resp, err := c.httpClient.Request(ctx, http.MethodGet, url, nil, headers)
	if err != nil {
		// Handle specific Clearbit API errors
		if httpErr, ok := err.(*httpclient.RetryableError); ok {
			switch httpErr.StatusCode {
			case http.StatusNotFound:
				return errors.NewNotFound("company not found")
			case http.StatusBadRequest, http.StatusUnprocessableEntity:
				return errors.NewValidation("invalid request", err)
			default:
				return errors.NewUnexpected("unexpected error", err)
			}
		}
		return errors.NewUnexpected("request failed", err)
	}

	if err := json.Unmarshal(resp.Body, &model); err != nil {
		return errors.NewUnexpected("failed to decode response", err)
	}

	return nil
}

// IsReady checks if the Clearbit API is reachable
func (c *Client) IsReady(ctx context.Context) error {

	resp, err := c.httpClient.Request(ctx, http.MethodGet, c.config.BaseURL, nil, nil)
	if err != nil {
		return errors.NewUnexpected("failed to check if Clearbit API is reachable", err)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.NewUnexpected("clearbit API is not reachable", fmt.Errorf("status code: %d", resp.StatusCode))
	}

	return nil

}

// NewClient creates a new Clearbit API client
func NewClient(config Config) *Client {
	httpConfig := httpclient.Config{
		Timeout:      config.Timeout,
		MaxRetries:   config.MaxRetries,
		RetryDelay:   config.RetryDelay,
		RetryBackoff: true,
	}

	return &Client{
		config:     config,
		httpClient: httpclient.NewClient(httpConfig),
	}
}
