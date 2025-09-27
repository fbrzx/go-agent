package llm

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

type openAIClient struct {
	client *openai.Client
	model  string
}

func NewOpenAIClient(opts Options) Client {
	cfg := openai.DefaultConfig(opts.OpenAIAPIKey)
	if opts.OpenAIBaseURL != "" {
		cfg.BaseURL = opts.OpenAIBaseURL
	}

	return &openAIClient{
		client: openai.NewClientWithConfig(cfg),
		model:  opts.Model,
	}
}

func (c *openAIClient) Generate(ctx context.Context, messages []Message) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
	}

	req.Messages = make([]openai.ChatCompletionMessage, len(messages))
	for i, msg := range messages {
		req.Messages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai chat completion returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}
