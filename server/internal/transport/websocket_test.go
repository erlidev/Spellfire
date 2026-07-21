package transport

import (
	"net/http/httptest"
	"testing"
)

func TestAllowedOrigin(t *testing.T) {
	tests := []struct {
		origin, host string
		want         bool
	}{
		{"", "game.example.com", true},
		{"https://game.example.com", "game.example.com", true},
		{"http://localhost:5173", "localhost:8080", true},
		{"https://evil.example", "game.example.com", false},
		{"://bad", "game.example.com", false},
	}
	for _, test := range tests {
		r := httptest.NewRequest("GET", "http://"+test.host+"/ws", nil)
		r.Host = test.host
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		if got := allowedOrigin(r); got != test.want {
			t.Errorf("origin %q host %q = %v, want %v", test.origin, test.host, got, test.want)
		}
	}
}
