package github

import "testing"

const gheRoot = "https://ghe.example.com/"

func TestDefaultUploadURL(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ api, want string }{
		"api v3 path":        {api: gheRoot + "api/v3/", want: gheRoot},
		"api v3 no slash":    {api: gheRoot + "api/v3", want: gheRoot},
		"host only":          {api: "https://ghe.example.com", want: gheRoot},
		"test server prefix": {api: "http://127.0.0.1:1234/prefix/api/v3/", want: "http://127.0.0.1:1234/prefix/"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := defaultUploadURL(tc.api); got != tc.want {
				t.Errorf("defaultUploadURL(%q) = %q, want %q", tc.api, got, tc.want)
			}
		})
	}
}
