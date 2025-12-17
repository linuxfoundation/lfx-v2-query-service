// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package filter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

const (
	// MaxExpressionLength is the maximum allowed length for a CEL expression
	MaxExpressionLength = 1000

	// EvaluationTimeout is the maximum time allowed to evaluate a single resource
	EvaluationTimeout = 100 * time.Millisecond

	// MaxCacheSize is the maximum number of compiled programs to cache
	MaxCacheSize = 100

	// CacheTTL is the time-to-live for cached programs
	CacheTTL = 5 * time.Minute
)

// CELFilter implements ResourceFilter using Common Expression Language
type CELFilter struct {
	env          *cel.Env
	programCache *programCache
	logger       *slog.Logger
}

// programCache stores compiled CEL programs with TTL
type programCache struct {
	mu      sync.RWMutex
	cache   map[string]*cacheEntry
	maxSize int
}

type cacheEntry struct {
	program   cel.Program
	expiresAt time.Time
}

// NewCELFilter creates a new CEL-based resource filter
func NewCELFilter(logger *slog.Logger) (*CELFilter, error) {
	// Create CEL environment with safe variable exposure
	env, err := cel.NewEnv(
		cel.Variable("data", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("resource_type", cel.StringType),
		cel.Variable("id", cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &CELFilter{
		env: env,
		programCache: &programCache{
			cache:   make(map[string]*cacheEntry),
			maxSize: MaxCacheSize,
		},
		logger: logger,
	}, nil
}

// Filter applies a CEL expression filter to resources
func (f *CELFilter) Filter(ctx context.Context, resources []model.Resource, expression string) ([]model.Resource, error) {
	// If no expression provided, return all resources
	if expression == "" {
		return resources, nil
	}

	// Validate expression length
	if len(expression) > MaxExpressionLength {
		return nil, fmt.Errorf("filter expression exceeds maximum length of %d characters", MaxExpressionLength)
	}

	// Get or compile program
	prg, err := f.getOrCompileProgram(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid filter expression: %w", err)
	}

	// Filter resources
	filtered := make([]model.Resource, 0, len(resources))
	for _, resource := range resources {
		// Check context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Evaluate with timeout
		match, err := f.evaluateResource(ctx, prg, resource)
		if err != nil {
			// Log evaluation error but continue (lenient mode)
			f.logger.Warn("failed to evaluate resource against filter",
				"resource_id", resource.ID,
				"resource_type", resource.Type,
				"error", err,
			)
			continue
		}

		if match {
			filtered = append(filtered, resource)
		}
	}

	f.logger.Debug("CEL filter applied",
		"expression", expression,
		"input_count", len(resources),
		"output_count", len(filtered),
	)

	return filtered, nil
}

// evaluateResource evaluates a single resource against the CEL program
func (f *CELFilter) evaluateResource(ctx context.Context, prg cel.Program, resource model.Resource) (bool, error) {
	// Create timeout context
	evalCtx, cancel := context.WithTimeout(ctx, EvaluationTimeout)
	defer cancel()

	// Prepare evaluation variables
	vars := map[string]any{
		"data":          resource.Data,
		"resource_type": resource.Type,
		"id":            resource.ID,
	}

	// Evaluate expression
	result, _, err := prg.ContextEval(evalCtx, vars)
	if err != nil {
		return false, fmt.Errorf("evaluation error: %w", err)
	}

	// Check if result is boolean
	boolResult, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression must return boolean, got %T", result.Value())
	}

	return boolResult, nil
}

// getOrCompileProgram retrieves a cached program or compiles a new one
func (f *CELFilter) getOrCompileProgram(expression string) (cel.Program, error) {
	// Try to get from cache first
	if prg := f.programCache.get(expression); prg != nil {
		return prg, nil
	}

	// Compile new program
	ast, issues := f.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compilation error: %w", issues.Err())
	}

	// Check output type - must be boolean
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("expression must return boolean, got %s", ast.OutputType())
	}

	// Create program
	prg, err := f.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program creation error: %w", err)
	}

	// Cache the program
	f.programCache.put(expression, prg)

	return prg, nil
}

// get retrieves a program from cache if not expired
func (pc *programCache) get(expression string) cel.Program {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	entry, exists := pc.cache[expression]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.program
}

// put adds a program to the cache with TTL
func (pc *programCache) put(expression string, program cel.Program) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Clean up expired entries if cache is full
	if len(pc.cache) >= pc.maxSize {
		pc.cleanupExpiredLocked()
	}

	// If still full after cleanup, remove oldest entry
	if len(pc.cache) >= pc.maxSize {
		// Simple eviction: just skip caching this program
		return
	}

	pc.cache[expression] = &cacheEntry{
		program:   program,
		expiresAt: time.Now().Add(CacheTTL),
	}
}

// cleanupExpiredLocked removes expired entries (must be called with lock held)
func (pc *programCache) cleanupExpiredLocked() {
	now := time.Now()
	for key, entry := range pc.cache {
		if now.After(entry.expiresAt) {
			delete(pc.cache, key)
		}
	}
}
