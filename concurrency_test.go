package switchonyourcode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestClientConcurrentRefreshEvaluationAndSubscriptions(t *testing.T) {
	var enabled atomic.Bool
	enabled.Store(true)
	var revision atomic.Int64
	revision.Store(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentRevision := revision.Load()
		etag := fmt.Sprintf(`"v%d"`, currentRevision)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		payload := strings.Replace(
			validConfigurationJSON(enabled.Load()),
			`"revision":1`,
			fmt.Sprintf(`"revision":%d`, currentRevision),
			1,
		)
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client, err := NewClientAndWait(context.Background(), ClientOptions{
		BaseURL:   server.URL,
		ServerKey: "syoc_server_test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for reader := 0; reader < 8; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 250; iteration++ {
				_ = client.Boolean("new-checkout", false, EvaluationContext{TargetingKey: "user-123"})
				if _, ok := client.Configuration(); !ok {
					select {
					case errCh <- fmt.Errorf("configuration disappeared during concurrent evaluation"):
					default:
					}
					return
				}
				if _, ok := client.FlagInfo("new-checkout"); !ok {
					select {
					case errCh <- fmt.Errorf("flag info disappeared during concurrent evaluation"):
					default:
					}
					return
				}
				unsubscribe := client.Subscribe(func(Configuration) {})
				unsubscribe()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for iteration := 0; iteration < 75; iteration++ {
			enabled.Store(iteration%2 == 0)
			revision.Add(1)
			if _, refreshErr := client.Refresh(context.Background()); refreshErr != nil {
				select {
				case errCh <- refreshErr:
				default:
				}
				return
			}
		}
	}()

	wg.Wait()
	select {
	case concurrentErr := <-errCh:
		t.Fatal(concurrentErr)
	default:
	}
}
