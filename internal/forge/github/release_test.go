package github_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
)

const (
	releasesPath = "/api/v3/repos/o/r/releases"
	releasePath  = releasesPath + "/42"
	uploadsPath  = "/api/uploads/repos/o/r/releases/42/assets"
	octetStream  = "application/octet-stream"

	aZip        = "a.zip"
	bundleZip   = "bundle.zip"
	badPattern  = "bad pattern"
	invalidGlob = "Invalid glob pattern: "
	tagV1       = "v1"
	tagV100     = "v1.0.0"
	contentB    = "BBB"
	distA       = "dist/a.zip"
	distB       = "dist/b.zip"
	readmeMD    = "readme.md"
)

type seenRequest struct {
	Method, Path, RawQuery, ContentType, Body string
}

// releaseServer fakes the release endpoints and records every request. decoy receives anything sent to the
// upload_url of the create response.
type releaseServer struct {
	*httptest.Server
	decoy *httptest.Server

	mu       sync.Mutex
	requests []seenRequest
	decoyed  []string
	// status maps "METHOD path" to a failing status.
	status map[string]int
}

func newReleaseServer(t *testing.T, status map[string]int) *releaseServer {
	t.Helper()
	s := &releaseServer{status: status}
	s.decoy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.decoyed = append(s.decoyed, r.Method+" "+r.URL.String())
		s.mu.Unlock()
		w.WriteHeader(http.StatusTeapot)
	}))
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"),
			strings.TrimSpace(string(body))})
		s.mu.Unlock()
		if code := s.status[r.Method+" "+r.URL.Path]; code != 0 {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST " + releasesPath:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42,"url":"` + s.decoy.URL + `/release","upload_url":"` + s.decoy.URL +
				`/uploads/assets{?name,label}"}`))
		case "POST " + uploadsPath:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case "PATCH " + releasePath:
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	t.Cleanup(s.decoy.Close)
	return s
}

func (s *releaseServer) seen() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.requests...)
}

func (s *releaseServer) client(t *testing.T) *github.Client {
	t.Helper()
	cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: s.URL + apiPath}
	c, err := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, nil)).
		RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCreateReleaseBody(t *testing.T) {
	tests := map[string]struct {
		in   github.ReleaseInput
		want string
	}{
		"notes": {
			in: github.ReleaseInput{
				TagName:         tagV100,
				Name:            "1.0.0 (2026-01-01)",
				Body:            "## Features\n- x",
				TargetCommitish: "abc123",
			},
			want: `{"tag_name":"v1.0.0","target_commitish":"abc123","name":"1.0.0 (2026-01-01)","body":"## Features\n- x",` +
				`"draft":false,"prerelease":false,"generate_release_notes":false}`,
		},
		"empty notes, prerelease, unknown commit": {
			in: github.ReleaseInput{TagName: "v1.0.0-rc.1", Name: "rc", Prerelease: true},
			want: `{"tag_name":"v1.0.0-rc.1","name":"rc","draft":false,"prerelease":true,` +
				`"generate_release_notes":true}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newReleaseServer(t, nil)
			if err := srv.client(t).CreateRelease(context.Background(), tc.in, nil, ""); err != nil {
				t.Fatal(err)
			}
			got := srv.seen()
			if len(got) != 1 || got[0].Method != http.MethodPost || got[0].Path != releasesPath {
				t.Fatalf("requests = %+v", got)
			}
			if got[0].Body != tc.want {
				t.Errorf("body = %s\nwant   %s", got[0].Body, tc.want)
			}
		})
	}
}

func TestCreateReleaseWithAssets(t *testing.T) {
	dir := writeFiles(t, map[string]string{distB: contentB, distA: "AAA", "other/c.txt": "CCC"})
	tests := map[string]struct {
		assets *github.Assets
		want   map[string]string // upload name -> content, in order of upload below
		order  []string
	}{
		"glob": {
			assets: github.GlobAssets("dist/*.zip"),
			order:  []string{aZip, "b.zip"},
			want:   map[string]string{aZip: "AAA", "b.zip": contentB},
		},
		"list with a name override": {
			assets: github.ListAssets([]github.AssetEntry{{Path: distB, Name: bundleZip}, {Path: "./other/c.txt"}}),
			order:  []string{bundleZip, "c.txt"},
			want:   map[string]string{bundleZip: contentB, "c.txt": "CCC"},
		},
		"glob without match": {assets: github.GlobAssets("nothing/*"), want: map[string]string{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newReleaseServer(t, nil)
			in := github.ReleaseInput{TagName: tagV100, Name: "1.0.0", Body: "notes"}
			if err := srv.client(t).CreateRelease(context.Background(), in, tc.assets, dir); err != nil {
				t.Fatal(err)
			}
			got := srv.seen()
			if len(got) != 2+len(tc.order) {
				t.Fatalf("requests = %+v", got)
			}
			wantCreate := `{"tag_name":"v1.0.0","name":"1.0.0","body":"notes","draft":true,"prerelease":false,` +
				`"generate_release_notes":false}`
			if got[0].Body != wantCreate {
				t.Errorf("create body = %s\nwant          %s", got[0].Body, wantCreate)
			}
			for i, n := range tc.order {
				up := got[1+i]
				if up.Method != http.MethodPost || up.Path != uploadsPath || up.RawQuery != "name="+n ||
					up.ContentType != octetStream || up.Body != tc.want[n] {
					t.Errorf("upload %d = %+v, want %s with %q", i, up, n, tc.want[n])
				}
			}
			last := got[len(got)-1]
			if last.Method != http.MethodPatch || last.Path != releasePath || last.Body != `{"draft":false}` {
				t.Errorf("publish = %+v", last)
			}
			if len(srv.decoyed) != 0 {
				t.Errorf("requests sent to the upload_url of the response: %v", srv.decoyed)
			}
		})
	}
}

func TestCreateReleaseFailures(t *testing.T) {
	dir := writeFiles(t, map[string]string{aZip: "A"})
	assets := github.ListAssets([]github.AssetEntry{{Path: aZip}})
	tests := map[string]struct {
		status    map[string]int
		assets    *github.Assets
		wantMsg   string
		wantCount int
		wantLast  string
	}{
		"create fails": {
			status:    map[string]int{"POST " + releasesPath: http.StatusUnprocessableEntity},
			assets:    assets,
			wantMsg:   "Trouble communicating with GitHub while creating a release: ",
			wantCount: 1,
			wantLast:  "POST " + releasesPath,
		},
		"upload fails": {
			status:    map[string]int{"POST " + uploadsPath: http.StatusForbidden},
			assets:    assets,
			wantMsg:   "Trouble communicating with GitHub while uploading asset a.zip. Release has been created but not published!: ",
			wantCount: 2,
			wantLast:  "POST " + uploadsPath,
		},
		"publish fails": {
			status:    map[string]int{"PATCH " + releasePath: http.StatusForbidden},
			assets:    assets,
			wantMsg:   "Trouble communicating with GitHub while publishing release: ",
			wantCount: 3,
			wantLast:  "PATCH " + releasePath,
		},
		"missing file": {
			assets: github.ListAssets([]github.AssetEntry{{Path: "missing.zip"}}),
			wantMsg: "Could not read asset file missing.zip: no such file or directory " +
				"Release has been created but not published!",
			wantCount: 1,
			wantLast:  "POST " + releasesPath,
		},
		badPattern: {
			assets:    github.GlobAssets("dist/[a"),
			wantMsg:   invalidGlob,
			wantCount: 1,
			wantLast:  "POST " + releasesPath,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newReleaseServer(t, tc.status)
			err := srv.client(t).
				CreateRelease(context.Background(), github.ReleaseInput{TagName: tagV1, Name: "1"}, tc.assets, dir)
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want prefix %q", err, tc.wantMsg)
			}
			got := srv.seen()
			if len(got) != tc.wantCount {
				t.Fatalf("requests = %+v, want %d", got, tc.wantCount)
			}
			if l := got[len(got)-1]; l.Method+" "+l.Path != tc.wantLast {
				t.Errorf("last request = %s %s, want %s", l.Method, l.Path, tc.wantLast)
			}
		})
	}
}

func TestMissingAssetDiagnostic(t *testing.T) {
	srv := newReleaseServer(t, nil)
	assets := github.ListAssets([]github.AssetEntry{{Path: "missing.zip"}})
	err := srv.client(t).
		CreateRelease(context.Background(), github.ReleaseInput{TagName: tagV1, Name: "1"}, assets, t.TempDir())
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("err = %v", err)
	}
	if d.Code() != "remeris::step::could_not_read_asset_file" ||
		d.Help() != "This could be a permissions issue or the file may not exist relative to the current working directory." {
		t.Errorf("code = %q, help = %q", d.Code(), d.Help())
	}
}

func TestAssetDirectoryIsNotReadable(t *testing.T) {
	dir := writeFiles(t, map[string]string{"dist/x.txt": "x"})
	srv := newReleaseServer(t, nil)
	err := srv.client(t).CreateRelease(context.Background(), github.ReleaseInput{TagName: tagV1, Name: "1"},
		github.GlobAssets("dist"), dir)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not read asset file dist: is a directory ") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveAssets(t *testing.T) {
	dir := writeFiles(t, map[string]string{distB: "", distA: "", "dist/sub/c.zip": "", readmeMD: ""})
	tests := map[string]struct {
		assets  *github.Assets
		want    []github.Asset
		wantErr string
	}{
		"glob in order": {
			assets: github.GlobAssets("dist/*.zip"),
			want:   []github.Asset{{Path: distA}, {Path: distB}},
		},
		"recursive glob": {
			assets: github.GlobAssets("dist/**/*.zip"),
			want:   []github.Asset{{Path: distA}, {Path: distB}, {Path: "dist/sub/c.zip"}},
		},
		"no match is not an error": {assets: github.GlobAssets("nope/*")},
		badPattern:                 {assets: github.GlobAssets("dist/[a"), wantErr: invalidGlob},
		"list keeps order and names": {
			assets: github.ListAssets([]github.AssetEntry{{Path: readmeMD, Name: "R"}, {Path: "./dist/a.zip"}}),
			want:   []github.Asset{{Path: readmeMD, Name: "R"}, {Path: distA}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tc.assets.Resolve(os.DirFS(dir))
			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("asset %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestUploadName(t *testing.T) {
	tests := map[string]struct {
		in      github.Asset
		want    string
		wantErr bool
	}{
		"file name":     {in: github.Asset{Path: distA}, want: aZip},
		"explicit name": {in: github.Asset{Path: distA, Name: "x"}, want: "x"},
		"no file name":  {in: github.Asset{Path: "."}, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tc.in.UploadName()
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("got %q, %v", got, err)
			}
		})
	}
}

func TestDescribeRelease(t *testing.T) {
	dir := writeFiles(t, map[string]string{distB: "", distA: ""})
	tests := map[string]struct {
		in      github.ReleaseInput
		assets  *github.Assets
		want    []string
		wantErr string
	}{
		"release with notes": {
			in:   github.ReleaseInput{TagName: tagV100, Name: "1.0.0", Body: "## Notes\n- a"},
			want: []string{"Would create a release on GitHub with name 1.0.0 and tag v1.0.0 and body:\n## Notes\n- a"},
		},
		"prerelease, commit, generated body": {
			in: github.ReleaseInput{TagName: "v1.0.0-rc.1", Name: "rc", Prerelease: true, TargetCommitish: "abc"},
			want: []string{
				"Would create a prerelease on GitHub with name rc and tag v1.0.0-rc.1 at commit abc and autogenerated body",
			},
		},
		"glob assets": {
			in:     github.ReleaseInput{TagName: tagV1, Name: "1", Body: "n"},
			assets: github.GlobAssets("dist/*.zip"),
			want: []string{
				"Would create a release on GitHub with name 1 and tag v1 and body:\nn",
				"Would upload assets to GitHub:",
				"- a.zip from dist/a.zip",
				"- b.zip from dist/b.zip",
			},
		},
		"listed assets": {
			in:     github.ReleaseInput{TagName: tagV1, Name: "1"},
			assets: github.ListAssets([]github.AssetEntry{{Path: distA, Name: "A"}}),
			want: []string{
				"Would create a release on GitHub with name 1 and tag v1 and autogenerated body",
				"Would upload assets to GitHub:",
				"- A from dist/a.zip",
			},
		},
		badPattern: {
			in:      github.ReleaseInput{TagName: tagV1, Name: "1"},
			assets:  github.GlobAssets("["),
			wantErr: invalidGlob,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := github.DescribeRelease(tc.in, tc.assets, dir)
			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}
