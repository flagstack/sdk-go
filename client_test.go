package flagstack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func validConfigurationJSON(enabled bool) string {
	return fmt.Sprintf(`{
  "schema_version": 1,
  "environment": {"id":"env-1","key":"production"},
  "flags": [{
    "id":"flag-1","key":"new-checkout","kind":"boolean","default_value":false,
    "enabled":%t,"variants":[],"policy":{},"revision":1
  }],
  "segments": []
}`, enabled)
}

func TestClientRefreshETagAndTypedEvaluation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/sdk/v1/config" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fs_server_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(validConfigurationJSON(true)))
	}))
	defer server.Close()

	client, err := NewClientAndWait(context.Background(), ClientOptions{BaseURL: server.URL, ServerKey: "fs_server_test"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if !client.Ready() {
		t.Fatal("client should be ready")
	}
	if got := client.Boolean("new-checkout", false, EvaluationContext{}); !got {
		t.Fatal("expected enabled boolean flag to evaluate true")
	}
	result, err := client.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Modified {
		t.Fatal("304 refresh should not be modified")
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestClientRetainsLastKnownGoodConfiguration(t *testing.T) {
	var invalid atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if invalid.Load() {
			_, _ = w.Write([]byte(`{"schema_version":99}`))
			return
		}
		_, _ = w.Write([]byte(validConfigurationJSON(true)))
	}))
	defer server.Close()
	client, err := NewClientAndWait(context.Background(), ClientOptions{BaseURL: server.URL, ServerKey: "fs_server_test"})
	if err != nil {
		t.Fatal(err)
	}
	invalid.Store(true)
	if _, err := client.Refresh(context.Background()); err == nil {
		t.Fatal("expected invalid refresh to fail")
	}
	if got := client.Boolean("new-checkout", false, EvaluationContext{}); !got {
		t.Fatal("last known-good config was lost")
	}
}

func TestClientAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, ServerKey: "fs_server_test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Refresh(context.Background())
	if _, ok := err.(*AuthenticationError); !ok {
		t.Fatalf("error = %T, want *AuthenticationError", err)
	}
}

func TestClientTypedFallbackErrors(t *testing.T) {
	client, err := NewClient(ClientOptions{BaseURL: "https://flags.example.com", ServerKey: "fs_server_test"})
	if err != nil {
		t.Fatal(err)
	}
	details := client.BooleanDetails("missing", true, EvaluationContext{})
	if !details.Value || details.ErrorCode != ErrorProviderNotReady {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestPollingCanStartAndStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validConfigurationJSON(true)))
	}))
	defer server.Close()
	client, err := NewClientAndWait(context.Background(), ClientOptions{BaseURL: server.URL, ServerKey: "fs_server_test", PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.StartPolling(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	cancel()
	client.StopPolling()
}

func TestClientSubscriptionReceivesModifiedConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validConfigurationJSON(true)))
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, ServerKey: "fs_server_test"})
	if err != nil {
		t.Fatal(err)
	}
	changes := make(chan Configuration, 1)
	unsubscribe := client.Subscribe(func(configuration Configuration) { changes <- configuration })
	defer unsubscribe()
	if _, err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case configuration := <-changes:
		if configuration.Environment.Key != "production" {
			t.Fatalf("unexpected configuration: %+v", configuration)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription")
	}
}
