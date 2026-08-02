// Package odesli is a client for the Odesli (song.link / album.link) API.
//
// Odesli takes a link to a song or album on any one streaming service and
// returns the equivalent link on every other service it knows about, plus a
// shareable song.link landing page that redirects each visitor to whichever
// service they use.
//
// The default HTTP client is a plain one with a timeout. Pass WithHTTPClient to
// supply your own — an otelhttp-wrapped transport, say — so this package takes
// no instrumentation dependency of its own.
package odesli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is the public Odesli endpoint.
const DefaultBaseURL = "https://api.song.link/v1-alpha.1/links"

// DefaultUserAgent identifies this client to Odesli. Override it with
// WithUserAgent to identify your application instead.
const DefaultUserAgent = "github.com/icco/odesli"

// defaultTimeout bounds a request when the caller supplies no HTTP client.
const defaultTimeout = 15 * time.Second

// ErrNoMatch is returned when Odesli resolves the request but has no landing
// page for it. Odesli reports this with HTTP 200 and an empty pageUrl rather
// than an error status, so it is easy to mistake for a successful lookup.
var ErrNoMatch = errors.New("odesli: no pageUrl for link")

// Client talks to the Odesli API.
type Client struct {
	http      *http.Client
	baseURL   string
	apiKey    string
	country   string
	userAgent string
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets an Odesli API key, which raises the rate limit. Requests
// without one are limited to 10 per minute.
func WithAPIKey(k string) Option {
	return func(c *Client) { c.apiKey = k }
}

// WithUserCountry sets the ISO 3166-1 alpha-2 country code sent to Odesli.
// It changes which platforms and regional URLs come back. Odesli defaults to
// "US" when this is unset.
func WithUserCountry(cc string) Option {
	return func(c *Client) { c.country = cc }
}

// WithBaseURL overrides the API base URL, mostly for tests.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithHTTPClient overrides the HTTP client. Use it to supply your own timeout,
// transport, or instrumentation; this package adds none of its own.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithUserAgent overrides the User-Agent header. Odesli asks that clients
// identify themselves, so set this to something that names your application.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// New returns a Client. With no options it uses a plain HTTP client with a
// 15 second timeout and talks to the public endpoint.
func New(opts ...Option) *Client {
	c := &Client{
		http:      &http.Client{Timeout: defaultTimeout},
		baseURL:   DefaultBaseURL,
		userAgent: DefaultUserAgent,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Entity is one platform's record of a song or album: what Odesli matched the
// link to on that service.
type Entity struct {
	// ID is the identifier on APIProvider's own service, not a global one.
	ID   string `json:"id"`
	Type string `json:"type"` // "song" or "album"

	// Title and ArtistName are absent for some providers.
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`

	ThumbnailURL    string `json:"thumbnailUrl"`
	ThumbnailWidth  int    `json:"thumbnailWidth"`
	ThumbnailHeight int    `json:"thumbnailHeight"`

	// APIProvider is the service Odesli got this record from. Platforms lists
	// every platform served by that record, which is often more than one:
	// "itunes" and "appleMusic" share a provider, as do "youtube" and
	// "youtubeMusic".
	APIProvider string   `json:"apiProvider"`
	Platforms   []string `json:"platforms"`
}

// Link is where to find the entity on one platform.
type Link struct {
	// URL is the web link for the platform.
	URL string `json:"url"`
	// EntityUniqueID keys into Response.EntitiesByUniqueID.
	EntityUniqueID string `json:"entityUniqueId"`
	// NativeAppURIMobile and NativeAppURIDesktop open the platform's app
	// directly. Both are absent for platforms with no app deep link.
	NativeAppURIMobile  string `json:"nativeAppUriMobile"`
	NativeAppURIDesktop string `json:"nativeAppUriDesktop"`
}

// Response is an Odesli /links result.
type Response struct {
	// EntityUniqueID identifies the entity Odesli matched the request to. It
	// keys into EntitiesByUniqueID.
	EntityUniqueID string `json:"entityUniqueId"`
	// UserCountry is the country Odesli resolved against, which may differ from
	// what you asked for.
	UserCountry string `json:"userCountry"`
	// PageURL is the shareable song.link landing page.
	PageURL string `json:"pageUrl"`

	// LinksByPlatform is keyed by platform name: "spotify", "appleMusic",
	// "youtube", "tidal", "amazonMusic", and so on. Which keys are present
	// depends on the entity and the country.
	LinksByPlatform map[string]Link `json:"linksByPlatform"`
	// EntitiesByUniqueID holds the per-provider records the links point at.
	EntitiesByUniqueID map[string]Entity `json:"entitiesByUniqueId"`
}

// Entity returns the record for the entity Odesli matched, and whether it was
// present. Use it for the title, artist, and artwork of the resolved item.
func (r *Response) Entity() (Entity, bool) {
	e, ok := r.EntitiesByUniqueID[r.EntityUniqueID]
	return e, ok
}

// Link returns the URL for one platform (for example "spotify"), and whether
// Odesli had one.
func (r *Response) Link(platform string) (string, bool) {
	l, ok := r.LinksByPlatform[platform]
	if !ok || l.URL == "" {
		return "", false
	}
	return l.URL, true
}

// Resolve looks up a streaming link and returns everything Odesli knows about
// it. It returns ErrNoMatch when Odesli answers successfully but has no landing
// page for the link.
func (c *Client) Resolve(ctx context.Context, link string) (*Response, error) {
	q := url.Values{}
	q.Set("url", link)
	if c.country != "" {
		q.Set("userCountry", c.country)
	}
	if c.apiKey != "" {
		q.Set("key", c.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req) //nolint:gosec // G704: the endpoint is the caller's to configure
	if err != nil {
		return nil, fmt.Errorf("odesli request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("odesli %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	if out.PageURL == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoMatch, link)
	}
	return &out, nil
}

// truncate clips s to n bytes, appending an ellipsis if it had to cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
