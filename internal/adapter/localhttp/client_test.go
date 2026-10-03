package localhttp

import "testing"

func TestOnlyLoopbackHTTP(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:50021", "http://localhost:8178/", "http://[::1]:1"} {
		if _, err := ParseBaseURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://example.com", "http://192.168.1.1:50021", "http://localhost/remote",
		"http://x@localhost", "http://127.0.0.1:1?x=1", "http://example.com"} {
		if _, err := ParseBaseURL(bad); err == nil {
			t.Errorf("%s を受け付けた", bad)
		}
	}
}
