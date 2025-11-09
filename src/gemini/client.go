package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultModel   = "gemini-2.0-flash-lite"
	defaultBaseURL = "https://generativelanguage.googleapis.com"
	requestPathFmt = "%s/v1beta/models/%s:generateContent?key=%s"
)

// Client wraps the Gemini REST API for text generation use cases.
type Client struct {
	apiKey            string
	model             string
	baseURL           string
	httpClient        *http.Client
	systemInstruction string
}

// Option configures a Client instance.
type Option func(*Client)

// WithAPIKey overrides the API key used by the client.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithModel sets the target Gemini model name when generating content.
func WithModel(model string) Option {
	return func(c *Client) {
		if model != "" {
			c.model = model
		}
	}
}

// WithHTTPClient assigns a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithBaseURL changes the base URL used for API calls. Primarily intended for testing.
func WithBaseURL(base string) Option {
	return func(c *Client) {
		if base != "" {
			c.baseURL = base
		}
	}
}

// WithSystemInstruction sets a default system instruction that is sent with every prompt.
func WithSystemInstruction(instruction string) Option {
	return func(c *Client) {
		c.systemInstruction = instruction
	}
}

// NewClient constructs a Gemini client, loading configuration from environment variables if needed.
func NewClient(opts ...Option) (*Client, error) {
	_ = godotenv.Load()

	client := &Client{
		model:   defaultModel,
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	if client.apiKey == "" {
		client.apiKey = os.Getenv("GEMINI_API_KEY")
	}

	if client.apiKey == "" {
		return nil, errors.New("gemini: API key not provided; set GEMINI_API_KEY environment variable or .env value")
	}

	return client, nil
}

// GenerateOptions tunes the behaviour of Ask requests.
type GenerateOptions struct {
	Temperature      *float64
	TopP             *float64
	TopK             *int
	MaxOutputTokens  *int
	ResponseMimeType string
	ResponseSchema   any
}

// Part represents a single prompt component that can include text or inline binary data.
type Part struct {
	Text       string
	InlineData *InlineData
}

// InlineData holds base64-encoded binary payloads that Gemini can interpret, such as images.
type InlineData struct {
	MimeType string
	Data     string
}

type generateRequest struct {
	Contents          []content         `json:"contents"`
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type generationConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	TopK             *int            `json:"topK,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type generateResponse struct {
	Candidates []candidate `json:"candidates"`
	Error      *apiError   `json:"error"`
}

type candidate struct {
	Content content `json:"content"`
}

type apiError struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// Ask sends a free-form prompt to the configured Gemini model and returns the textual response.
func (c *Client) Ask(ctx context.Context, prompt string, opts *GenerateOptions) (string, error) {
	if prompt == "" {
		return "", errors.New("gemini: prompt must not be empty")
	}

	return c.AskWithParts(ctx, []Part{{Text: prompt}}, opts)
}

// AskWithParts sends a prompt composed of multiple parts, supporting multimodal requests.
func (c *Client) AskWithParts(ctx context.Context, parts []Part, opts *GenerateOptions) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("gemini: at least one part is required")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	reqPayload, err := c.buildRequestFromParts(parts, opts)
	if err != nil {
		return "", err
	}

	return c.doGenerate(ctx, reqPayload)
}

func (c *Client) buildRequestFromParts(parts []Part, opts *GenerateOptions) (*generateRequest, error) {
	reqParts := make([]part, 0, len(parts))
	for idx, p := range parts {
		rp := part{}
		if strings.TrimSpace(p.Text) != "" {
			rp.Text = p.Text
		}
		if p.InlineData != nil {
			if p.InlineData.MimeType == "" {
				return nil, fmt.Errorf("gemini: part %d inline data missing mime type", idx)
			}
			if p.InlineData.Data == "" {
				return nil, fmt.Errorf("gemini: part %d inline data missing data", idx)
			}
			rp.InlineData = &inlineData{
				MimeType: p.InlineData.MimeType,
				Data:     p.InlineData.Data,
			}
		}

		if rp.Text == "" && rp.InlineData == nil {
			return nil, fmt.Errorf("gemini: part %d contained no usable data", idx)
		}

		reqParts = append(reqParts, rp)
	}

	req := &generateRequest{
		Contents: []content{
			{
				Parts: reqParts,
			},
		},
	}

	if c.systemInstruction != "" {
		req.SystemInstruction = &content{
			Parts: []part{{Text: c.systemInstruction}},
		}
	}

	cfg, err := buildGenerationConfig(opts)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		req.GenerationConfig = cfg
	}

	return req, nil
}

func buildGenerationConfig(opts *GenerateOptions) (*generationConfig, error) {
	if opts == nil {
		return nil, nil
	}

	cfg := &generationConfig{
		Temperature:      opts.Temperature,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		MaxOutputTokens:  opts.MaxOutputTokens,
		ResponseMimeType: opts.ResponseMimeType,
	}

	if opts.ResponseSchema != nil {
		raw, err := json.Marshal(opts.ResponseSchema)
		if err != nil {
			return nil, fmt.Errorf("gemini: marshal response schema: %w", err)
		}
		cfg.ResponseSchema = raw
	}

	if cfg.Temperature == nil && cfg.TopP == nil && cfg.TopK == nil && cfg.MaxOutputTokens == nil && cfg.ResponseMimeType == "" && len(cfg.ResponseSchema) == 0 {
		return nil, nil
	}

	return cfg, nil
}

func extractText(resp generateResponse) (string, error) {
	if len(resp.Candidates) == 0 {
		return "", errors.New("gemini: response contained no candidates")
	}

	var builder strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(part.Text)
		}
	}

	if builder.Len() == 0 {
		return "", errors.New("gemini: candidate did not contain text parts")
	}

	return builder.String(), nil
}

func (c *Client) endpoint() string {
	base := strings.TrimSuffix(c.baseURL, "/")
	return fmt.Sprintf(requestPathFmt, base, url.PathEscape(c.model), url.QueryEscape(c.apiKey))
}

func (c *Client) doGenerate(ctx context.Context, reqPayload *generateRequest) (string, error) {
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := c.endpoint()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini: http call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp generateResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("gemini: decode response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("gemini: API error %s (%d): %s", apiResp.Error.Status, apiResp.Error.Code, apiResp.Error.Message)
	}

	text, err := extractText(apiResp)
	if err != nil {
		return "", err
	}

	return text, nil
}
