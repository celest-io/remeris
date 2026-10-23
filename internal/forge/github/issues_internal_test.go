package github

import "testing"

func TestGraphqlEndpoint(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		apiBase string
		want    string
		wantErr bool
	}{
		"github.com":         {apiBase: "https://api.github.com/", want: "https://api.github.com/graphql"},
		"enterprise server":  {apiBase: "https://ghe.example/api/v3/", want: "https://ghe.example/api/graphql"},
		"enterprise port":    {apiBase: "https://ghe.example:8443/api/v3", want: "https://ghe.example:8443/api/graphql"},
		"data residency":     {apiBase: "https://api.corp.ghe.com/", want: "https://api.corp.ghe.com/graphql"},
		"query is dropped":   {apiBase: "https://ghe.example/api/v3/?x=1", want: "https://ghe.example/api/graphql"},
		"unknown path":       {apiBase: "https://ghe.example/custom/", wantErr: true},
		"no host":            {apiBase: "/api/v3/", wantErr: true},
		"not a url":          {apiBase: "://", wantErr: true},
		"api path elsewhere": {apiBase: "https://ghe.example/v3/", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := graphqlEndpoint(tc.apiBase)
			if tc.wantErr {
				if err == nil || err.Error() != "The GitHub API URL has no known GraphQL endpoint" {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}
