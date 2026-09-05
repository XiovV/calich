package caldavserver

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestRequestBaseURL(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "plain HTTP request uses its own Host",
			req:  &http.Request{Host: "calendar.internal:8080", Header: http.Header{}},
			want: "http://calendar.internal:8080",
		},
		{
			name: "TLS request is https",
			req:  &http.Request{Host: "calendar.internal", Header: http.Header{}, TLS: &tls.ConnectionState{}},
			want: "https://calendar.internal",
		},
		{
			name: "X-Forwarded-Proto overrides TLS state",
			req: &http.Request{Host: "calendar.internal", TLS: &tls.ConnectionState{}, Header: http.Header{
				"X-Forwarded-Proto": {"http"},
			}},
			want: "http://calendar.internal",
		},
		{
			name: "X-Forwarded-Host overrides the request's own Host",
			req: &http.Request{Host: "127.0.0.1:8080", Header: http.Header{
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"calendar.example.com"},
			}},
			want: "https://calendar.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestBaseURL(tt.req); got != tt.want {
				t.Fatalf("RequestBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
