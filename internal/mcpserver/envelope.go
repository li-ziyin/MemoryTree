package mcpserver

import (
	"encoding/json"

	"github.com/li-ziyin/memorytree/internal/tokens"
)

const trustUntrusted = "untrusted-history"

// Envelope is the single response shape shared by all tools.
type Envelope struct {
	Data  any        `json:"data"`
	Meta  Meta       `json:"meta"`
	Error *ToolError `json:"error,omitempty"`
}

// Meta carries response metadata.
type Meta struct {
	Total           int      `json:"total"`
	TokensEstimated int      `json:"tokens_estimated"`
	IndexGeneration int64    `json:"index_generation"`
	Warnings        []string `json:"warnings"`
}

// ToolError is a stable, machine-readable tool failure.
type ToolError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []contentBlock `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

func (s *Server) envelope(data any, total int, extraWarnings ...string) *Envelope {
	warnings := make([]string, 0, len(s.warnings)+len(extraWarnings))
	warnings = append(warnings, s.warnings...)
	warnings = append(warnings, extraWarnings...)
	return &Envelope{
		Data: data,
		Meta: Meta{
			Total:           total,
			TokensEstimated: estimateTokens(data),
			IndexGeneration: s.generation,
			Warnings:        warnings,
		},
	}
}

func (s *Server) errorEnvelope(err *ToolError) *Envelope {
	warnings := make([]string, len(s.warnings))
	copy(warnings, s.warnings)
	return &Envelope{
		Data: nil,
		Meta: Meta{
			IndexGeneration: s.generation,
			Warnings:        warnings,
		},
		Error: err,
	}
}

func toolResultFromEnvelope(env *Envelope, isError bool) toolResult {
	text, err := json.Marshal(env)
	if err != nil {
		return toolResult{
			Content: []contentBlock{{
				Type: "text",
				Text: `{"error":{"code":"INTERNAL_ERROR","message":"cannot encode response"}}`,
			}},
			IsError: true,
		}
	}
	return toolResult{
		Content:           []contentBlock{{Type: "text", Text: string(text)}},
		StructuredContent: env,
		IsError:           isError,
	}
}

// estimateTokens approximates tokens for the data payload using the shared
// estimator in internal/tokens (ASCII 0.25, non-ASCII 1).
func estimateTokens(v any) int {
	return tokens.Estimate(v)
}
