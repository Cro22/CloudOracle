package llm

import (
	"CloudOracle/internal/config"
	"net/http"
)

// newHTTPClient builds the *http.Client used by every LLM provider. It plugs
// the retry transport in front of http.DefaultTransport when retries are
// enabled (cfg.MaxRetries > 0); otherwise it returns a plain client. Either
// way, cfg.RequestTimeout is the per-request budget — the transport only
// retries within that window.
func newHTTPClient(cfg config.LLMConfig) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport
	if cfg.MaxRetries > 0 {
		transport = newRetryTransport(transport, cfg.MaxRetries, cfg.BaseDelay, cfg.MaxDelay)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   cfg.RequestTimeout,
	}
}
