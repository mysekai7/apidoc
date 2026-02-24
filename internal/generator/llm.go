package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client is an OpenAI-compatible chat completions client.
type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature float64
	HTTPClient  *http.Client
	Logger      *slog.Logger
}

var sleepFn = time.Sleep

func (c *Client) Chat(systemPrompt, userPrompt string) (string, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	payload := map[string]interface{}{
		"model":       c.Model,
		"max_tokens":  c.MaxTokens,
		"temperature": c.Temperature,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if c.Logger != nil {
		c.Logger.Debug("llm request", "url", endpoint, "system", systemPrompt, "user", userPrompt)
	}

	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				sleepFn(backoff(attempt))
				continue
			}
			return "", err
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				sleepFn(backoff(attempt))
				continue
			}
			return "", err
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("llm error status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
			if attempt < maxRetries {
				wait := backoff(attempt)
				if resp.StatusCode == http.StatusTooManyRequests {
					if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
						if secs, err := strconv.Atoi(ra); err == nil {
							wait = time.Duration(secs) * time.Second
						}
					}
				}
				sleepFn(wait)
				continue
			}
			return "", lastErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("llm error status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
		}

		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return "", err
		}
		if len(out.Choices) == 0 {
			return "", errors.New("llm response has no choices")
		}
		content := out.Choices[0].Message.Content
		if c.Logger != nil {
			c.Logger.Debug("llm response", "content", content)
		}
		return content, nil
	}
	if lastErr == nil {
		lastErr = errors.New("llm request failed")
	}
	return "", lastErr
}

func (c *Client) ChatJSON(systemPrompt, userPrompt string, out interface{}) error {
	content, err := c.Chat(systemPrompt, userPrompt)
	if err != nil {
		return err
	}
	content = stripMarkdownCodeBlock(content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return err
	}
	return nil
}

func stripMarkdownCodeBlock(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			trimmed = trimmed[idx+1:]
		}
		if end := strings.LastIndex(trimmed, "```"); end != -1 {
			trimmed = trimmed[:end]
		}
		return strings.TrimSpace(trimmed)
	}
	return trimmed
}

func backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	return time.Second << attempt
}
