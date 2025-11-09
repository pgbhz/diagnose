package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"telbot/gemini"
)

// DiagnosisReport models the structured response expected from Gemini.
type DiagnosisReport struct {
	Answer    bool   `json:"Answer"`
	Rationale string `json:"Rationale"`
}

// CancerClassifier abstracts how we decide whether an image likely shows mouth cancer.
type CancerClassifier func(ctx context.Context, imagePath string) (bool, string, error)

func getGeminiClient() (*gemini.Client, error) {
	geminiClientOnce.Do(func() {
		geminiClient, geminiClientErr = gemini.NewClient()
	})
	return geminiClient, geminiClientErr
}

func classifyWithGemini(ctx context.Context, imagePath string) (bool, string, error) {
	report, err := analyzeMouthPhoto(ctx, imagePath)
	if err != nil {
		return false, "", err
	}
	return report.Answer, report.Rationale, nil
}

func analyzeMouthPhoto(ctx context.Context, imagePath string) (*DiagnosisReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}

	mimeType := detectMimeType(data, imagePath)

	client, err := getGeminiClient()
	if err != nil {
		return nil, fmt.Errorf("init gemini client: %w", err)
	}

	prompt := "You are assessing a medical photo of the inside of a human mouth. Determine whether the photo shows signs consistent with oral or mouth cancer. Respond ONLY with JSON that matches this exact schema: {\"Answer\": boolean, \"Rationale\": string}. Set Answer to true only if the image likely shows mouth cancer."

	parts := []gemini.Part{
		{Text: prompt},
		{
			InlineData: &gemini.InlineData{
				MimeType: mimeType,
				Data:     base64.StdEncoding.EncodeToString(data),
			},
		},
	}

	opts := &gemini.GenerateOptions{
		ResponseMimeType: "application/json",
		ResponseSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"Answer":    map[string]any{"type": "boolean"},
				"Rationale": map[string]any{"type": "string"},
			},
			"required": []string{"Answer", "Rationale"},
		},
	}

	raw, err := client.AskWithParts(ctx, parts, opts)
	if err != nil {
		return nil, fmt.Errorf("gemini ask: %w", err)
	}

	var report DiagnosisReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("parse gemini response: %w", err)
	}

	return &report, nil
}
