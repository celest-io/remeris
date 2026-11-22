package github

import (
	"testing"

	gh "github.com/google/go-github/v92/github"
)

func TestGraphQLURL(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		opts []gh.ClientOptionsFunc
		want string
	}{
		"github.com": {want: "https://api.github.com/graphql"},
		"an api. host (GHE.com)": {
			opts: []gh.ClientOptionsFunc{
				gh.WithEnterpriseURLs("https://api.octocorp.ghe.com/", "https://uploads.octocorp.ghe.com/"),
			},
			want: "https://api.octocorp.ghe.com/graphql",
		},
		"Enterprise Server": {
			opts: []gh.ClientOptionsFunc{gh.WithEnterpriseURLs("https://ghe.example.com/", "https://ghe.example.com/")},
			want: "https://ghe.example.com/api/graphql",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api, err := gh.NewClient(tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got := (&Client{Client: api}).graphQLURL(); got != tc.want {
				t.Fatalf("graphQLURL = %q, want %q", got, tc.want)
			}
		})
	}
}
