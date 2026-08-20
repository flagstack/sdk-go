package flagstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
)

const defaultPollInterval = 30 * time.Second
const maxConfigurationBytes = 10 << 20

type ClientOptions struct {
	BaseURL                string
	ServerKey              string
	HTTPClient             *http.Client
	PollInterval           time.Duration
	OnConfigurationChanged func(Configuration)
	OnError                func(error)
}

type Client struct {
	baseURL                string
	serverKey              string
	httpClient             *http.Client
	pollInterval           time.Duration
	onConfigurationChanged func(Configuration)
	onError                func(error)

	mu            sync.RWMutex
	configuration *Configuration
	flags         map[string]Flag
	etag          string
	pollCancel    context.CancelFunc
	pollWG        sync.WaitGroup
	listeners     map[uint64]func(Configuration)
	nextListener  uint64
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("FlagStack base URL must be an absolute http(s) URL")
	}
	serverKey := strings.TrimSpace(options.ServerKey)
	if !strings.HasPrefix(serverKey, "fs_server_") {
		return nil, fmt.Errorf("Go SDK requires a FlagStack server key (fs_server_...)")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	pollInterval := options.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	if pollInterval < 0 {
		return nil, fmt.Errorf("poll interval must not be negative")
	}
	return &Client{
		baseURL:                baseURL,
		serverKey:              serverKey,
		httpClient:             httpClient,
		pollInterval:           pollInterval,
		onConfigurationChanged: options.OnConfigurationChanged,
		onError:                options.OnError,
		flags:                  make(map[string]Flag),
		listeners:              make(map[uint64]func(Configuration)),
	}, nil
}

func NewClientAndWait(ctx context.Context, options ClientOptions) (*Client, error) {
	client, err := NewClient(options)
	if err != nil {
		return nil, err
	}
	if _, err := client.Refresh(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) Refresh(ctx context.Context) (RefreshResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/sdk/v1/config", nil)
	if err != nil {
		return RefreshResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.serverKey)
	c.mu.RLock()
	etag := c.etag
	hasConfiguration := c.configuration != nil
	c.mu.RUnlock()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RefreshResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		if !hasConfiguration {
			return RefreshResult{}, &ConfigurationError{Message: "received 304 before any configuration was loaded"}
		}
		configuration, _ := c.Configuration()
		return RefreshResult{Modified: false, Configuration: configuration}, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return RefreshResult{}, &AuthenticationError{Message: fmt.Sprintf("FlagStack SDK authentication failed with status %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RefreshResult{}, &HTTPError{Message: fmt.Sprintf("FlagStack SDK request failed with status %d", resp.StatusCode), StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigurationBytes+1))
	if err != nil {
		return RefreshResult{}, err
	}
	if len(body) > maxConfigurationBytes {
		return RefreshResult{}, &ConfigurationError{Message: "FlagStack configuration response exceeds 10 MiB"}
	}
	configuration, err := DecodeConfiguration(body)
	if err != nil {
		return RefreshResult{}, &ConfigurationError{Message: err.Error()}
	}

	c.mu.Lock()
	modified := c.configuration == nil || !reflect.DeepEqual(*c.configuration, configuration)
	stored := cloneConfiguration(configuration)
	c.configuration = &stored
	c.flags = make(map[string]Flag, len(stored.Flags))
	for _, flag := range stored.Flags {
		c.flags[flag.Key] = flag
	}
	c.etag = resp.Header.Get("ETag")
	callback := c.onConfigurationChanged
	listeners := make([]func(Configuration), 0, len(c.listeners))
	for _, listener := range c.listeners {
		listeners = append(listeners, listener)
	}
	c.mu.Unlock()

	if modified {
		if callback != nil {
			callback(cloneConfiguration(configuration))
		}
		for _, listener := range listeners {
			listener(cloneConfiguration(configuration))
		}
	}
	return RefreshResult{Modified: modified, Configuration: cloneConfiguration(configuration)}, nil
}

func (c *Client) StartPolling(ctx context.Context) error {
	c.mu.Lock()
	if c.pollCancel != nil {
		c.mu.Unlock()
		return fmt.Errorf("FlagStack polling is already running")
	}
	pollCtx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	interval := c.pollInterval
	c.pollWG.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.pollWG.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				if _, err := c.Refresh(pollCtx); err != nil && !errors.Is(err, context.Canceled) && c.onError != nil {
					c.onError(err)
				}
			}
		}
	}()
	return nil
}

func (c *Client) StopPolling() {
	c.mu.Lock()
	cancel := c.pollCancel
	c.pollCancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
		c.pollWG.Wait()
	}
}

func (c *Client) Close() { c.StopPolling() }

func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configuration != nil
}

func (c *Client) Configuration() (Configuration, bool) {
	c.mu.RLock()
	if c.configuration == nil {
		c.mu.RUnlock()
		return Configuration{}, false
	}
	configuration := *c.configuration
	c.mu.RUnlock()
	return cloneConfiguration(configuration), true
}

func (c *Client) Subscribe(listener func(Configuration)) func() {
	if listener == nil {
		return func() {}
	}
	c.mu.Lock()
	id := c.nextListener
	c.nextListener++
	c.listeners[id] = listener
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.listeners, id)
		c.mu.Unlock()
	}
}

func (c *Client) RawDetails(flagKey string, ctx EvaluationContext) RawEvaluationDetails {
	c.mu.RLock()
	if c.configuration == nil {
		c.mu.RUnlock()
		return RawEvaluationDetails{Variant: "default", Reason: ReasonError, ErrorCode: ErrorProviderNotReady, ErrorMessage: "FlagStack client has no configuration"}
	}
	flag, exists := c.flags[flagKey]
	environmentID := c.configuration.Environment.ID
	segments := append([]Segment(nil), c.configuration.Segments...)
	c.mu.RUnlock()
	if !exists {
		return RawEvaluationDetails{Variant: "default", Reason: ReasonError, ErrorCode: ErrorFlagNotFound, ErrorMessage: fmt.Sprintf("flag %q was not found", flagKey)}
	}
	return Evaluate(flag, environmentID, ctx, segments)
}

func (c *Client) FlagInfo(flagKey string) (FlagInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.configuration == nil {
		return FlagInfo{}, false
	}
	flag, ok := c.flags[flagKey]
	if !ok {
		return FlagInfo{}, false
	}
	return FlagInfo{
		ID: flag.ID, Key: flag.Key, Kind: flag.Kind, Enabled: flag.Enabled, Revision: flag.Revision,
		Environment: c.configuration.Environment,
	}, true
}

func (c *Client) Boolean(flagKey string, fallback bool, ctx EvaluationContext) bool {
	return c.BooleanDetails(flagKey, fallback, ctx).Value
}

func (c *Client) BooleanDetails(flagKey string, fallback bool, ctx EvaluationContext) EvaluationDetails[bool] {
	raw, flag, ok := c.evaluate(flagKey, "boolean", ctx)
	if !ok {
		return EvaluationDetails[bool]{Value: fallback, Variant: raw.Variant, Reason: raw.Reason, RuleID: raw.RuleID, ErrorCode: raw.ErrorCode, ErrorMessage: raw.ErrorMessage}
	}
	var value bool
	if err := json.Unmarshal(raw.Value, &value); err != nil {
		return typeDecodeError(fallback, raw, err)
	}
	_ = flag
	return typedDetails(value, raw)
}

func (c *Client) String(flagKey, fallback string, ctx EvaluationContext) string {
	return c.StringDetails(flagKey, fallback, ctx).Value
}

func (c *Client) StringDetails(flagKey, fallback string, ctx EvaluationContext) EvaluationDetails[string] {
	raw, _, ok := c.evaluate(flagKey, "string", ctx)
	if !ok {
		return EvaluationDetails[string]{Value: fallback, Variant: raw.Variant, Reason: raw.Reason, RuleID: raw.RuleID, ErrorCode: raw.ErrorCode, ErrorMessage: raw.ErrorMessage}
	}
	var value string
	if err := json.Unmarshal(raw.Value, &value); err != nil {
		return typeDecodeError(fallback, raw, err)
	}
	return typedDetails(value, raw)
}

func (c *Client) Number(flagKey string, fallback float64, ctx EvaluationContext) float64 {
	return c.NumberDetails(flagKey, fallback, ctx).Value
}

func (c *Client) NumberDetails(flagKey string, fallback float64, ctx EvaluationContext) EvaluationDetails[float64] {
	raw, _, ok := c.evaluate(flagKey, "number", ctx)
	if !ok {
		return EvaluationDetails[float64]{Value: fallback, Variant: raw.Variant, Reason: raw.Reason, RuleID: raw.RuleID, ErrorCode: raw.ErrorCode, ErrorMessage: raw.ErrorMessage}
	}
	var value float64
	if err := json.Unmarshal(raw.Value, &value); err != nil {
		return typeDecodeError(fallback, raw, err)
	}
	return typedDetails(value, raw)
}

func (c *Client) JSON(flagKey string, fallback any, ctx EvaluationContext) any {
	return c.JSONDetails(flagKey, fallback, ctx).Value
}

func (c *Client) JSONDetails(flagKey string, fallback any, ctx EvaluationContext) EvaluationDetails[any] {
	raw, _, ok := c.evaluate(flagKey, "json", ctx)
	if !ok {
		return EvaluationDetails[any]{Value: fallback, Variant: raw.Variant, Reason: raw.Reason, RuleID: raw.RuleID, ErrorCode: raw.ErrorCode, ErrorMessage: raw.ErrorMessage}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw.Value)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return typeDecodeError(fallback, raw, err)
	}
	return typedDetails(value, raw)
}

func (c *Client) evaluate(flagKey, expectedKind string, ctx EvaluationContext) (RawEvaluationDetails, Flag, bool) {
	c.mu.RLock()
	if c.configuration == nil {
		c.mu.RUnlock()
		return RawEvaluationDetails{Variant: "default", Reason: ReasonError, ErrorCode: ErrorProviderNotReady, ErrorMessage: "FlagStack client has no configuration"}, Flag{}, false
	}
	flag, exists := c.flags[flagKey]
	environmentID := c.configuration.Environment.ID
	segments := append([]Segment(nil), c.configuration.Segments...)
	c.mu.RUnlock()
	if !exists {
		return RawEvaluationDetails{Variant: "default", Reason: ReasonError, ErrorCode: ErrorFlagNotFound, ErrorMessage: fmt.Sprintf("flag %q was not found", flagKey)}, Flag{}, false
	}
	if flag.Kind != expectedKind {
		return RawEvaluationDetails{Variant: "default", Reason: ReasonError, ErrorCode: ErrorTypeMismatch, ErrorMessage: fmt.Sprintf("flag %q is %s, not %s", flagKey, flag.Kind, expectedKind)}, flag, false
	}
	return Evaluate(flag, environmentID, ctx, segments), flag, true
}

func typedDetails[T any](value T, raw RawEvaluationDetails) EvaluationDetails[T] {
	return EvaluationDetails[T]{Value: value, Variant: raw.Variant, Reason: raw.Reason, RuleID: raw.RuleID, ErrorCode: raw.ErrorCode, ErrorMessage: raw.ErrorMessage}
}

func typeDecodeError[T any](fallback T, raw RawEvaluationDetails, err error) EvaluationDetails[T] {
	return EvaluationDetails[T]{Value: fallback, Variant: "default", Reason: ReasonError, ErrorCode: ErrorTypeMismatch, ErrorMessage: err.Error(), RuleID: raw.RuleID}
}
