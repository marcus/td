package syncclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncStatusPreservesFallbackHTTPStatuses(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = fmt.Fprint(w, `{"code":"unsupported","message":"old server"}`)
			}))
			defer server.Close()
			client := New(server.URL, "key", "device")
			_, err := client.SyncStatusContext(context.Background(), "project")
			if !IsHTTPStatus(err, code) {
				t.Fatalf("error = %v, want HTTP %d", err, code)
			}
		})
	}
}
