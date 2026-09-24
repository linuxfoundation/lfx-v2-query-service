// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package filter

import (
	"container/list"
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

// programCache stores compiled CEL programs with TTL and LRU eviction: once
// full, inserting a new expression evicts the least-recently-used entry
// rather than refusing to cache it.
type programCache struct {
	mu      sync.Mutex
	cache   map[string]*list.Element
	order   *list.List // front = most recently used
	maxSize int
}

type cacheEntry struct {
	key       string
	program   cel.Program
	expiresAt time.Time
}

// isExpired checks if the cache entry has expired
func (ce *cacheEntry) isExpired() bool {
	return time.Now().After(ce.expiresAt)
}

// newProgramCache creates an empty programCache with the given capacity.
func newProgramCache(maxSize int) *programCache {
	return &programCache{
		cache:   make(map[string]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
	}
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
		env:          env,
		programCache: newProgramCache(MaxCacheSize),
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

// getOrCompileProgram retrieves a cached program or compiles a new one.
// Compilation happens outside the cache lock so a slow compile never blocks
// lookups or inserts for unrelated expressions; a duplicate concurrent
// compile of the same expression is harmless, since put simply overwrites
// or moves the existing entry to the front either way.
func (f *CELFilter) getOrCompileProgram(expression string) (cel.Program, error) {
	if prg, ok := f.programCache.get(expression); ok {
		return prg, nil
	}

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

	f.programCache.put(expression, prg)

	return prg, nil
}

// get retrieves a program from cache if not expired, marking it as the
// most-recently-used entry. Removes expired entries immediately when detected.
func (pc *programCache) get(expression string) (cel.Program, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	elem, exists := pc.cache[expression]
	if !exists {
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)
	if entry.isExpired() {
		pc.removeLocked(elem)
		return nil, false
	}

	pc.order.MoveToFront(elem)
	return entry.program, true
}

// put adds or refreshes a program in the cache with a new TTL, evicting the
// least-recently-used entry if the cache is at capacity (must not be called
// with the lock held).
func (pc *programCache) put(expression string, program cel.Program) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if elem, exists := pc.cache[expression]; exists {
		entry := elem.Value.(*cacheEntry)
		entry.program = program
		entry.expiresAt = time.Now().Add(CacheTTL)
		pc.order.MoveToFront(elem)
		return
	}

	if pc.order.Len() >= pc.maxSize {
		pc.evictOldestLocked()
	}

	elem := pc.order.PushFront(&cacheEntry{
		key:       expression,
		program:   program,
		expiresAt: time.Now().Add(CacheTTL),
	})
	pc.cache[expression] = elem
}

// removeLocked deletes an entry from both the map and the LRU list (must be
// called with the lock held).
func (pc *programCache) removeLocked(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(pc.cache, entry.key)
	pc.order.Remove(elem)
}

// evictOldestLocked removes the least-recently-used entry, if any (must be
// called with the lock held).
func (pc *programCache) evictOldestLocked() {
	if oldest := pc.order.Back(); oldest != nil {
		pc.removeLocked(oldest)
	}
}
