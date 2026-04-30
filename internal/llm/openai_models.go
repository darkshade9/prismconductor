package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// decodeOpenAIModels parses the {data: [{id}]} envelope shared by OpenAI,
// LiteLLM's /v1/models passthrough, and LM Studio's /v1/models. Returns IDs
// sorted alphabetically.
func decodeOpenAIModels(r io.Reader) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		// Gemini's OpenAI-compat /models endpoint returns IDs in its native
		// form `models/<id>` (e.g. `models/gemini-2.0-flash`). The chat
		// completions surface, however, expects the bare ID. Strip the prefix
		// here so the dropdown surfaces a model the user can immediately use.
		// LM Studio / OpenAI / LiteLLM never emit this prefix, so the trim is a
		// no-op for them.
		id := strings.TrimPrefix(m.ID, "models/")
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// httpGetWithBearer issues a GET with optional Bearer auth and returns the
// body bytes on a 2xx response. Non-2xx becomes an error with the status text
// so the UI's Test connection button surfaces "401 Unauthorized" verbatim.
func httpGetWithBearer(ctx context.Context, client *http.Client, url, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(http.StatusText(resp.StatusCode)))
	}
	return body, nil
}
