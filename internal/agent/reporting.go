package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func reportPrintJobResult(apiBaseURL, bearerToken, jobID, status string, result map[string]any) error {
	apiBaseURL = strings.TrimSpace(apiBaseURL)
	jobID = strings.TrimSpace(jobID)
	status = strings.TrimSpace(status)
	if apiBaseURL == "" || jobID == "" || status == "" {
		return nil
	}

	apiBaseURL = strings.TrimRight(apiBaseURL, "/")
	url := fmt.Sprintf("%s/print-jobs/%s/result", apiBaseURL, jobID)

	payload := map[string]any{
		"status": status,
		"result": result,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if token := strings.TrimSpace(bearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway responded with status %d", resp.StatusCode)
	}

	return nil
}
