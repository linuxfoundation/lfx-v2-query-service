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

// isExpired checks if the cache entry has expired
func (ce *cacheEntry) isExpired() bool {
	return time.Now().After(ce.expiresAt)
}

// NewCELFilter creates a new CEL-based resource filter
func NewCELFilter() (*CELFilter, error) {
	// Create CEL environment with safe variable exposure
	env, err := cel.NewEnv(
		cel.Variable("data", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("resource_type", cel.StringType),
		cel.Variable("id", cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELFilter{
		env: env,
		programCache: &programCache{
			cache:   make(map[string]*cacheEntry),
			maxSize: MaxCacheSize,
		},
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
			slog.WarnContext(ctx, "failed to evaluate resource against filter",
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

	slog.DebugContext(ctx, "CEL filter applied",
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
// Uses double-checked locking to prevent race conditions
func (f *CELFilter) getOrCompileProgram(expression string) (cel.Program, error) {
	// First check: try to get from cache without write lock (fast path)
	if prg := f.programCache.get(expression); prg != nil {
		return prg, nil
	}

	// Acquire write lock for compilation
	f.programCache.mu.Lock()
	defer f.programCache.mu.Unlock()

	// Second check: another goroutine might have compiled it while we waited for the lock
	if entry, exists := f.programCache.cache[expression]; exists && !entry.isExpired() {
		return entry.program, nil
	}

	// Compile new program (only one goroutine reaches here per expression)
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

	// Cache the program (already holding write lock)
	f.programCache.putLocked(expression, prg)

	return prg, nil
}

// get retrieves a program from cache if not expired
// Removes expired entries immediately when detected
func (pc *programCache) get(expression string) cel.Program {
	// First attempt: fast path with read lock
	pc.mu.RLock()
	entry, exists := pc.cache[expression]
	if !exists {
		pc.mu.RUnlock()
		return nil
	}

	// Check if expired (still under read lock)
	if entry.isExpired() {
		pc.mu.RUnlock()

		// Upgrade to write lock to delete expired entry
		pc.mu.Lock()
		// Double-check it still exists and is still expired
		if entry, exists := pc.cache[expression]; exists && entry.isExpired() {
			delete(pc.cache, expression)
		}
		pc.mu.Unlock()

		return nil
	}

	program := entry.program
	pc.mu.RUnlock()
	return program
}

// putLocked adds a program to the cache with TTL (must be called with lock held)
func (pc *programCache) putLocked(expression string, program cel.Program) {
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
