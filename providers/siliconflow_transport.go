package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// siliconflowErrorRewriter maps SiliconFlow JSON errors ({code,message}) into an OpenAI-shaped body
// so langchaingo populates errResp.Error.Message (otherwise logs show "400: ").
type siliconflowErrorRewriter struct {
	base http.RoundTripper
}

func (t *siliconflowErrorRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	host := strings.ToLower(req.URL.Host)
	if !strings.Contains(host, "siliconflow") {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	var sf struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &sf) != nil || sf.Message == "" {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	wrapped := map[string]any{
		"error": map[string]any{
			"message": fmt.Sprintf("[%d] %s", sf.Code, sf.Message),
			"type":    "siliconflow_error",
		},
	}
	out, _ := json.Marshal(wrapped)
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}
