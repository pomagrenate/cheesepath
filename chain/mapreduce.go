package chain

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TextSplitter splits text into overlapping chunks of approximately ChunkSize characters.
type TextSplitter struct {
	ChunkSize    int // target characters per chunk
	ChunkOverlap int // overlap characters between consecutive chunks
}

// Split divides text into overlapping chunks. Each chunk is at most ChunkSize
// characters, with ChunkOverlap characters shared with the previous chunk.
func (s *TextSplitter) Split(text string) []string {
	if s.ChunkSize <= 0 {
		s.ChunkSize = 2000
	}
	if s.ChunkOverlap < 0 || s.ChunkOverlap >= s.ChunkSize {
		s.ChunkOverlap = s.ChunkSize / 10
	}
	if len(text) <= s.ChunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + s.ChunkSize
		if end > len(text) {
			end = len(text)
		}
		// Try to break on a newline to avoid splitting mid-word.
		if end < len(text) {
			if idx := strings.LastIndex(text[start:end], "\n"); idx > 0 {
				end = start + idx + 1
			}
		}
		chunks = append(chunks, text[start:end])
		next := end - s.ChunkOverlap
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return chunks
}

// MapReduceOption configures a MapReduceChain.
type MapReduceOption func(*MapReduceChain)

// WithConcurrency sets the maximum number of parallel map calls.
func WithConcurrency(n int) MapReduceOption { return func(c *MapReduceChain) { c.concurrency = n } }

// WithChunkSize sets the target chunk size for the TextSplitter.
func WithChunkSize(n int) MapReduceOption { return func(c *MapReduceChain) { c.splitter.ChunkSize = n } }

// WithChunkOverlap sets the overlap between chunks for the TextSplitter.
func WithChunkOverlap(n int) MapReduceOption {
	return func(c *MapReduceChain) { c.splitter.ChunkOverlap = n }
}

// WithMapVar overrides the template variable name used to inject each chunk.
func WithMapVar(name string) MapReduceOption { return func(c *MapReduceChain) { c.mapVar = name } }

// WithCombineVar overrides the template variable name for the combined map outputs.
func WithCombineVar(name string) MapReduceOption {
	return func(c *MapReduceChain) { c.combineVar = name }
}

// MapReduceChain processes large texts by:
//  1. Splitting the input into chunks with TextSplitter.
//  2. Running mapChain over each chunk (optionally in parallel).
//  3. Concatenating map outputs and running reduceChain to produce the final result.
type MapReduceChain struct {
	splitter    TextSplitter
	mapChain    *LLMChain
	reduceChain *LLMChain
	mapVar      string // variable name for chunk in mapChain template (default: "text")
	combineVar  string // variable name for combined outputs in reduceChain (default: "text")
	concurrency int    // max parallel map goroutines (default: 4)
}

// NewMapReduceChain creates a MapReduceChain.
// mapChain is applied to each chunk; reduceChain receives all map outputs joined by "\n\n".
func NewMapReduceChain(mapChain, reduceChain *LLMChain, opts ...MapReduceOption) *MapReduceChain {
	c := &MapReduceChain{
		splitter:    TextSplitter{ChunkSize: 2000, ChunkOverlap: 200},
		mapChain:    mapChain,
		reduceChain: reduceChain,
		mapVar:      "text",
		combineVar:  "text",
		concurrency: 4,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Run splits text, maps mapChain over each chunk, then reduces to a final answer.
// extraVars are merged into both map and reduce chain inputs.
func (c *MapReduceChain) Run(ctx context.Context, text string, extraVars map[string]any) (string, error) {
	chunks := c.splitter.Split(text)
	if len(chunks) == 0 {
		return "", fmt.Errorf("mapreduce: no chunks produced from input")
	}

	// Map phase: process each chunk (with bounded concurrency).
	results := make([]string, len(chunks))
	errs := make([]error, len(chunks))

	sem := make(chan struct{}, c.concurrency)
	var wg sync.WaitGroup

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, ch string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			vars := make(map[string]any, len(extraVars)+1)
			for k, v := range extraVars {
				vars[k] = v
			}
			vars[c.mapVar] = ch

			out, err := c.mapChain.Invoke(ctx, ChainInput{Vars: vars})
			results[idx] = out
			errs[idx] = err
		}(i, chunk)
	}
	wg.Wait()

	// Collect errors.
	for i, err := range errs {
		if err != nil {
			return "", fmt.Errorf("mapreduce: map chunk %d: %w", i, err)
		}
	}

	// Reduce phase: combine all map outputs and run the reduce chain.
	combined := strings.Join(results, "\n\n---\n\n")
	reduceVars := make(map[string]any, len(extraVars)+1)
	for k, v := range extraVars {
		reduceVars[k] = v
	}
	reduceVars[c.combineVar] = combined

	final, err := c.reduceChain.Invoke(ctx, ChainInput{Vars: reduceVars})
	if err != nil {
		return "", fmt.Errorf("mapreduce: reduce: %w", err)
	}
	return final, nil
}
