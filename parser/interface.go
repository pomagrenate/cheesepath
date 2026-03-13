// Package parser provides output parsers that convert raw LLM text into typed values.
package parser

// OutputParser transforms a raw LLM string into a typed value.
type OutputParser[T any] interface {
	Parse(raw string) (T, error)
}
