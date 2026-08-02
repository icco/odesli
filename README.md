# odesli

[![Go Reference](https://pkg.go.dev/badge/github.com/icco/odesli.svg)](https://pkg.go.dev/github.com/icco/odesli)
[![Test Go](https://github.com/icco/odesli/actions/workflows/test.yml/badge.svg)](https://github.com/icco/odesli/actions/workflows/test.yml)

A Go client for the [Odesli](https://odesli.co) (song.link / album.link) API.

Give it a link to a song or album on any one streaming service and it returns the equivalent link on every other service Odesli knows about, plus a shareable song.link page that redirects each visitor to whichever service they actually use.

No API key required. [Request one](https://odesli.co/#contact) if you need more than 10 requests per minute.

```
go get github.com/icco/odesli
```

## Usage

```go
c := odesli.New()

res, err := c.Resolve(ctx, "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC")
if err != nil {
  return err
}

fmt.Println(res.PageURL) // https://song.link/s/4uLU6hMCjMI75M1A2tKUQC

// What Odesli matched the link to.
if e, ok := res.Entity(); ok {
  fmt.Printf("%s — %s\n", e.ArtistName, e.Title)
}

// One specific platform.
if u, ok := res.Link("appleMusic"); ok {
  fmt.Println("Apple Music:", u)
}

// Or all of them.
for platform, link := range res.LinksByPlatform {
  fmt.Printf("%-14s %s\n", platform, link.URL)
}
```

### Options

```go
c := odesli.New(
  odesli.WithAPIKey(os.Getenv("ODESLI_API_KEY")), // raises the rate limit
  odesli.WithUserCountry("CA"),                   // changes which platforms come back
  odesli.WithUserAgent("myapp/1.0 (+https://example.com)"),
  odesli.WithHTTPClient(myInstrumentedClient),
)
```

## Notes

- **No match is not an HTTP error.** Odesli answers a link it cannot resolve with HTTP 200 and an empty `pageUrl`, which is easy to mistake for a successful lookup. `Resolve` returns `ErrNoMatch` for that case, so check it with `errors.Is`.
- **You own the HTTP client.** The default is a plain `http.Client` with a 15 second timeout and the stdlib transport. This package pulls in no tracing, metrics, or logging dependencies — pass `WithHTTPClient` to supply an instrumented one.
- **A platform is not a provider.** `Entity.APIProvider` is the service Odesli got a record from; `Entity.Platforms` lists every platform that record serves, which is often more than one. `itunes` and `appleMusic` share a provider, as do `youtube` and `youtubeMusic`.
- **`userCountry` changes the answer.** Which platforms appear, and the regional URLs within them, depend on it. Odesli defaults to `US` when you don't set one.
- **Read only, one endpoint.** This wraps `/links`, which is the only endpoint Odesli's public API exposes.
- No third-party dependencies.

## License

MIT
