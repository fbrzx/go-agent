package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ollamaClient struct {
	host   string
	model  string
	client *http.Client
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error"`
}

func NewOllamaClient(opts Options) Client {
	host := strings.TrimRight(opts.OllamaHost, "/")
	if host == "" {
		host = "http://localhost:11434"
	}

	return &ollamaClient{
		host:  host,
		model: opts.Model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *ollamaClient) Generate(ctx context.Context, messages []Message) (string, error) {
	payload := ollamaChatRequest{
		Model:  c.model,
		Stream: false,
	}

	payload.Messages = toOllamaMessages(messages)

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call ollama chat API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", fmt.Errorf("read ollama chat error body: %w", readErr)
		}
		if len(data) > 0 {
			return "", fmt.Errorf("ollama chat API error: %s", string(data))
		}
		return "", fmt.Errorf("ollama chat API returned status %s", resp.Status)
	}

	var parsed ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}

	if parsed.Error != "" {
		return "", fmt.Errorf("ollama chat error: %s", parsed.Error)
	}

	return parsed.Message.Content, nil
}

func (c *ollamaClient) GenerateStream(ctx context.Context, messages []Message, fn func(string) error) error {
	payload := ollamaChatRequest{
		Model:  c.model,
		Stream: true,
	}

	payload.Messages = toOllamaMessages(messages)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ollama stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create ollama stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("call ollama chat API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("read ollama chat error body: %w", readErr)
		}
		if len(data) > 0 {
			return fmt.Errorf("ollama chat API error: %s", string(data))
		}
		return fmt.Errorf("ollama chat API returned status %s", resp.Status)
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var chunk ollamaChatResponse
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode ollama stream response: %w", err)
		}

		if chunk.Error != "" {
			return fmt.Errorf("ollama chat error: %s", chunk.Error)
		}

		if chunk.Message.Content != "" {
			if err := fn(chunk.Message.Content); err != nil {
				return err
			}
		}

		if chunk.Done {
			return nil
		}
	}
}

func toOllamaMessages(messages []Message) []ollamaChatMessage {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]ollamaChatMessage, len(messages))
	for i := range messages {
		converted[i] = ollamaChatMessage(messages[i])
	}
	return converted
}
