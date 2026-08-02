package odesli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sampleResponse is a trimmed real /links payload: one entity reachable on two
// platforms through a single provider, plus a second provider.
const sampleResponse = `{
  "entityUniqueId": "SPOTIFY_SONG::4uLU6hMCjMI75M1A2tKUQC",
  "userCountry": "US",
  "pageUrl": "https://song.link/s/4uLU6hMCjMI75M1A2tKUQC",
  "entitiesByUniqueId": {
    "SPOTIFY_SONG::4uLU6hMCjMI75M1A2tKUQC": {
      "id": "4uLU6hMCjMI75M1A2tKUQC",
      "type": "song",
      "title": "Never Gonna Give You Up",
      "artistName": "Rick Astley",
      "thumbnailUrl": "https://i.scdn.co/image/abc",
      "thumbnailWidth": 640,
      "thumbnailHeight": 640,
      "apiProvider": "spotify",
      "platforms": ["spotify"]
    },
    "ITUNES_SONG::1558533900": {
      "id": "1558533900",
      "type": "song",
      "title": "Never Gonna Give You Up",
      "artistName": "Rick Astley",
      "apiProvider": "itunes",
      "platforms": ["appleMusic", "itunes"]
    }
  },
  "linksByPlatform": {
    "spotify": {
      "url": "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC",
      "entityUniqueId": "SPOTIFY_SONG::4uLU6hMCjMI75M1A2tKUQC",
      "nativeAppUriMobile": "spotify:track:4uLU6hMCjMI75M1A2tKUQC"
    },
    "appleMusic": {
      "url": "https://music.apple.com/us/album/x/1558533900",
      "entityUniqueId": "ITUNES_SONG::1558533900"
    }
  }
}`

// serve returns a test server answering every request with status and body,
// and records the last request it saw.
func serve(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var got http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestResolveParsesFullResponse(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, sampleResponse)

	got, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "https://open.spotify.com/track/x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.PageURL != "https://song.link/s/4uLU6hMCjMI75M1A2tKUQC" {
		t.Errorf("PageURL = %q", got.PageURL)
	}
	if got.UserCountry != "US" {
		t.Errorf("UserCountry = %q, want US", got.UserCountry)
	}
	if len(got.LinksByPlatform) != 2 {
		t.Errorf("got %d platforms, want 2", len(got.LinksByPlatform))
	}
	if len(got.EntitiesByUniqueID) != 2 {
		t.Errorf("got %d entities, want 2", len(got.EntitiesByUniqueID))
	}
}

func TestResponseEntity(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, sampleResponse)

	got, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	e, ok := got.Entity()
	if !ok {
		t.Fatal("Entity() = not found; want the entity EntityUniqueID points at")
	}
	if e.Title != "Never Gonna Give You Up" || e.ArtistName != "Rick Astley" {
		t.Errorf("Entity = %+v", e)
	}
	if e.ThumbnailWidth != 640 || e.ThumbnailHeight != 640 {
		t.Errorf("thumbnail = %dx%d, want 640x640", e.ThumbnailWidth, e.ThumbnailHeight)
	}
	if e.APIProvider != "spotify" {
		t.Errorf("APIProvider = %q, want spotify", e.APIProvider)
	}
}

func TestResponseEntityMissing(t *testing.T) {
	t.Parallel()
	r := &Response{EntityUniqueID: "NOPE", EntitiesByUniqueID: map[string]Entity{}}
	if _, ok := r.Entity(); ok {
		t.Error("Entity() reported found for an id that is not in the map")
	}
}

func TestResponseLink(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, sampleResponse)

	got, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	u, ok := got.Link("appleMusic")
	if !ok {
		t.Fatal("Link(appleMusic) = not found")
	}
	if u != "https://music.apple.com/us/album/x/1558533900" {
		t.Errorf("Link(appleMusic) = %q", u)
	}
	if _, ok := got.Link("tidal"); ok {
		t.Error("Link(tidal) reported found; the fixture has no tidal link")
	}
}

// A platform key that is present but carries an empty URL is as useless as an
// absent one, and must not be reported as found.
func TestResponseLinkEmptyURL(t *testing.T) {
	t.Parallel()
	r := &Response{LinksByPlatform: map[string]Link{"spotify": {URL: ""}}}
	if _, ok := r.Link("spotify"); ok {
		t.Error("Link() reported found for an empty URL")
	}
}

// Odesli reports "I resolved your request but have nothing for it" with HTTP
// 200 and an empty pageUrl, which is easy to mistake for a successful lookup.
func TestResolveNoMatch(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, `{"entityUniqueId":"","pageUrl":""}`)

	_, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "https://example.com/not-music")
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Resolve = %v, want ErrNoMatch", err)
	}
}

func TestResolveSendsQueryParams(t *testing.T) {
	t.Parallel()
	srv, got := serve(t, http.StatusOK, sampleResponse)

	c := New(WithBaseURL(srv.URL), WithAPIKey("k3y"), WithUserCountry("CA"))
	if _, err := c.Resolve(t.Context(), "https://open.spotify.com/track/x"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	q := got.URL.Query()
	if q.Get("url") != "https://open.spotify.com/track/x" {
		t.Errorf("url = %q", q.Get("url"))
	}
	if q.Get("key") != "k3y" {
		t.Errorf("key = %q, want k3y", q.Get("key"))
	}
	if q.Get("userCountry") != "CA" {
		t.Errorf("userCountry = %q, want CA", q.Get("userCountry"))
	}
}

// An unset key or country must be omitted rather than sent empty: Odesli treats
// an empty userCountry differently from an absent one.
func TestResolveOmitsUnsetParams(t *testing.T) {
	t.Parallel()
	srv, got := serve(t, http.StatusOK, sampleResponse)

	if _, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	q := got.URL.Query()
	if q.Has("key") {
		t.Error("key sent when no API key was configured")
	}
	if q.Has("userCountry") {
		t.Error("userCountry sent when none was configured")
	}
}

func TestResolveSetsHeaders(t *testing.T) {
	t.Parallel()
	srv, got := serve(t, http.StatusOK, sampleResponse)

	if _, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ua := got.Header.Get("User-Agent"); ua != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, DefaultUserAgent)
	}
	if a := got.Header.Get("Accept"); a != "application/json" {
		t.Errorf("Accept = %q", a)
	}
}

func TestWithUserAgent(t *testing.T) {
	t.Parallel()
	srv, got := serve(t, http.StatusOK, sampleResponse)

	c := New(WithBaseURL(srv.URL), WithUserAgent("myapp/2.0"))
	if _, err := c.Resolve(t.Context(), "x"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ua := got.Header.Get("User-Agent"); ua != "myapp/2.0" {
		t.Errorf("User-Agent = %q, want myapp/2.0", ua)
	}
}

// The point of WithHTTPClient is that callers own instrumentation, so the
// client they pass must actually be the one used.
func TestWithHTTPClientIsUsed(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, sampleResponse)

	called := false
	custom := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	c := New(WithBaseURL(srv.URL), WithHTTPClient(custom))
	if _, err := c.Resolve(t.Context(), "x"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !called {
		t.Error("the supplied http.Client's transport was never used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResolveSurfacesHTTPError(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusTooManyRequests, `{"statusCode":429,"code":"too_many_requests"}`)

	_, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x")
	if err == nil {
		t.Fatal("Resolve on a 429 = nil, want an error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q does not mention the status code", err)
	}
}

// The error body goes into the message, so a huge one must not be pasted whole
// into a log line.
func TestResolveTruncatesLongErrorBody(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusInternalServerError, strings.Repeat("x", 5000))

	_, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x")
	if err == nil {
		t.Fatal("Resolve on a 500 = nil, want an error")
	}
	if len(err.Error()) > 400 {
		t.Errorf("error is %d bytes; the body should have been truncated", len(err.Error()))
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("truncated error should end in an ellipsis: %q", err)
	}
}

func TestResolveRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, `<html>nope</html>`)

	if _, err := New(WithBaseURL(srv.URL)).Resolve(t.Context(), "x"); err == nil {
		t.Fatal("Resolve on a non-JSON body = nil, want a decode error")
	}
}

func TestResolveHonorsContext(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t, http.StatusOK, sampleResponse)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := New(WithBaseURL(srv.URL)).Resolve(ctx, "x"); err == nil {
		t.Fatal("Resolve with a cancelled context = nil, want an error")
	}
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()
	c := New()
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.userAgent != DefaultUserAgent {
		t.Errorf("userAgent = %q, want %q", c.userAgent, DefaultUserAgent)
	}
	if c.http == nil || c.http.Timeout != defaultTimeout {
		t.Errorf("default http client timeout = %v, want %v", c.http.Timeout, defaultTimeout)
	}
	// The default transport must be the stdlib one: this package promises to add
	// no instrumentation of its own.
	if c.http.Transport != nil {
		t.Errorf("default Transport = %T, want nil (stdlib default)", c.http.Transport)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"elevenchars", 10, "elevenchar..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
