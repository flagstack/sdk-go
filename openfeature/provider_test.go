package openfeature

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	switchonyourcode "github.com/switchonyourcode/sdk-go"
	of "github.com/open-feature/go-sdk/openfeature"
)

type configServerState struct {
	mu      sync.RWMutex
	version int
}

func (s *configServerState) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/sdk/v1/config" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer syoc_server_test" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	version := s.version
	s.mu.RUnlock()
	etag := fmt.Sprintf(`"v%d"`, version)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	_, _ = fmt.Fprintf(w, `{
  "schema_version": 1,
  "environment": {"id":"env-1","key":"production"},
  "flags": [
    {"id":"flag-bool","key":"new-checkout","kind":"boolean","default_value":false,"enabled":true,"variants":[],"policy":{},"revision":%d},
    {"id":"flag-number","key":"max-items","kind":"number","default_value":10,"enabled":true,"variants":[],"policy":{"fallthrough":{"variant":"default"}},"revision":1},
    {"id":"flag-fraction","key":"ratio","kind":"number","default_value":1.5,"enabled":true,"variants":[],"policy":{"fallthrough":{"variant":"default"}},"revision":1},
    {"id":"flag-json","key":"checkout-copy","kind":"json","default_value":{"title":"Checkout","steps":[1,2]},"enabled":true,"variants":[],"policy":{"fallthrough":{"variant":"default"}},"revision":1}
  ],
  "segments": []
}`, version)
}

func (s *configServerState) advance() {
	s.mu.Lock()
	s.version++
	s.mu.Unlock()
}

func newTestProvider(t *testing.T) (*Provider, *configServerState, *httptest.Server) {
	t.Helper()
	state := &configServerState{version: 1}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	provider, err := NewProvider(ProviderOptions{Client: switchonyourcode.ClientOptions{BaseURL: server.URL, ServerKey: "syoc_server_test"}})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return provider, state, server
}

func TestProviderImplementsOpenFeatureEvaluation(t *testing.T) {
	provider, _, server := newTestProvider(t)
	defer server.Close()
	defer provider.Shutdown()
	if err := provider.InitWithContext(context.Background(), of.EvaluationContext{}); err != nil {
		t.Fatal(err)
	}

	boolean := provider.BooleanEvaluation(context.Background(), "new-checkout", false, of.FlattenedContext{of.TargetingKey: "user-123"})
	if !boolean.Value || boolean.Reason != of.StaticReason || boolean.Variant != "on" {
		t.Fatalf("unexpected boolean result: %+v", boolean)
	}
	if boolean.FlagMetadata["switchonyourcode.environment"] != "production" || boolean.FlagMetadata["switchonyourcode.flag_id"] != "flag-bool" {
		t.Fatalf("unexpected metadata: %#v", boolean.FlagMetadata)
	}

	integer := provider.IntEvaluation(context.Background(), "max-items", 0, nil)
	if integer.Value != 10 || integer.Error() != nil {
		t.Fatalf("unexpected integer result: %+v err=%v", integer, integer.Error())
	}
	fraction := provider.IntEvaluation(context.Background(), "ratio", 7, nil)
	if fraction.Value != 7 || fraction.Error() == nil {
		t.Fatalf("expected non-integral number to use fallback with error: %+v", fraction)
	}

	object := provider.ObjectEvaluation(context.Background(), "checkout-copy", map[string]any{}, nil)
	value, ok := object.Value.(map[string]any)
	if !ok || value["title"] != "Checkout" || object.Error() != nil {
		t.Fatalf("unexpected object result: %#v err=%v", object.Value, object.Error())
	}
}

func TestProviderWorksThroughOpenFeatureClient(t *testing.T) {
	provider, _, server := newTestProvider(t)
	defer server.Close()
	defer of.Shutdown()

	const domain = "switchonyourcode-test"
	if err := of.SetNamedProviderAndWait(domain, provider); err != nil {
		t.Fatal(err)
	}
	client := of.NewClient(domain)
	value, err := client.BooleanValue(
		context.Background(),
		"new-checkout",
		false,
		of.NewEvaluationContext("user-123", map[string]any{"plan": "pro"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !value {
		t.Fatal("expected SwitchOnYourCode boolean through OpenFeature client")
	}
}

func TestProviderEmitsConfigurationChangedAfterInitialization(t *testing.T) {
	provider, state, server := newTestProvider(t)
	defer server.Close()
	defer provider.Shutdown()
	if err := provider.InitWithContext(context.Background(), of.EvaluationContext{}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-provider.EventChannel():
		t.Fatalf("initialization emitted unexpected event: %+v", event)
	default:
	}

	state.advance()
	result, err := provider.Client().Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Modified {
		t.Fatal("expected modified configuration")
	}
	select {
	case event := <-provider.EventChannel():
		if event.EventType != of.ProviderConfigChange || event.ProviderName != providerName {
			t.Fatalf("unexpected event: %+v", event)
		}
		if event.EventMetadata["environment"] != "production" {
			t.Fatalf("unexpected event metadata: %#v", event.EventMetadata)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for configuration change event")
	}
}
