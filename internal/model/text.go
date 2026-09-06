package model

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// Text requests are used by compaction and learning. They have no tools, no
// provider-side history, no retries, and a controller-admitted output limit.
func (c Client) CountText(ctx context.Context, instructions, prompt string) (int64, error) {
	r, err := c.API.Responses.InputTokens.Count(ctx, responses.InputTokenCountParams{
		Model: openai.String(c.Model), Instructions: openai.String(instructions),
		Input:     responses.InputTokenCountParamsInputUnion{OfString: openai.String(prompt)},
		Reasoning: shared.ReasoningParam{Effort: shared.ReasoningEffortLow},
	})
	if err != nil {
		return 0, safeError("count text input tokens", err)
	}
	if r.InputTokens <= 0 {
		return 0, errors.New("text token counter omitted usage")
	}
	return r.InputTokens, nil
}

func (c Client) Text(ctx context.Context, instructions, prompt string, maxOutput int64) (Reply, string, error) {
	r, err := c.API.Responses.New(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(c.Model), Instructions: openai.String(instructions),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(prompt)},
		MaxOutputTokens: openai.Int(maxOutput), Store: openai.Bool(false),
		Truncation: responses.ResponseNewParamsTruncationDisabled,
		Reasoning:  shared.ReasoningParam{Effort: shared.ReasoningEffortLow}, ServiceTier: responses.ResponseNewParamsServiceTierDefault,
	})
	if err != nil {
		return Reply{}, "", safeError("create text response", err)
	}
	return Reply{ID: r.ID, Usage: r.Usage, Status: string(r.Status), Raw: json.RawMessage(r.RawJSON())}, r.OutputText(), nil
}
