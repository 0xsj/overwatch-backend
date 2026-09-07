package egress

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const (
	DefaultMaxBody      = 16 << 20
	DefaultMaxRedirects = 5
	DefaultUserAgent    = "overwatch/1 (+https://github.com/0xsj/overwatch-backend)"
)

type Config struct {
	Guard     *Guard
	UserAgent string

	ConnectTimeout        time.Duration
	TLSTimeout            time.Duration
	ResponseHeaderTimeout time.Duration
	IdleTimeout           time.Duration

	MaxBody      int64
	MaxRedirects int
}

func (c Config) withDefaults() Config {
	if c.UserAgent == "" {
		c.UserAgent = DefaultUserAgent
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.TLSTimeout == 0 {
		c.TLSTimeout = 5 * time.Second
	}
	if c.ResponseHeaderTimeout == 0 {
		c.ResponseHeaderTimeout = 10 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 30 * time.Second
	}
	if c.MaxBody == 0 {
		c.MaxBody = DefaultMaxBody
	}
	if c.MaxRedirects == 0 {
		c.MaxRedirects = DefaultMaxRedirects
	}
	return c
}

type Hop struct {
	URL    string
	Status int
}

type Response struct {
	Status    int
	Header    http.Header
	Body      []byte
	Truncated bool

	// Addr is what was actually connected to, taken from the connection rather
	// than from a second resolution that may answer differently. It is an
	// observation, not diagnostics.
	Addr      netip.Addr
	Redirects []Hop
	Duration  time.Duration
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if cfg.Guard == nil {
		panic("egress: New with a nil Guard — an unguarded outbound client is the thing this package exists to prevent")
	}
	cfg = cfg.withDefaults()

	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout, Control: cfg.Guard.Control}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   cfg.TLSTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		IdleConnTimeout:       cfg.IdleTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Transport: transport,
			// Nil, and it stays nil: two targets sharing a jar is one target's
			// session reaching another's host.
			Jar: nil,
		},
	}
}

func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.Invalid, "egress: unusable url")
	}
	return c.Do(req)
}

func (c *Client) Do(req *http.Request) (*Response, error) {
	if err := dialable(req.URL); err != nil {
		return nil, err
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}

	var hops []Hop
	var addr netip.Addr
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			if ap, err := netip.ParseAddrPort(info.Conn.RemoteAddr().String()); err == nil {
				addr = ap.Addr().Unmap()
			}
		},
	}))

	client := *c.http
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if err := dialable(r.URL); err != nil {
			return err
		}
		if len(via) > c.cfg.MaxRedirects {
			return fmt.Errorf("egress: %w after %d", ErrTooManyRedirects, len(via))
		}
		hops = append(hops, Hop{URL: r.URL.String(), Status: r.Response.StatusCode})
		return nil
	}

	started := time.Now()
	res, err := client.Do(req)
	if err != nil {
		return nil, translate(err, req.URL)
	}
	defer res.Body.Close()

	// One byte past the limit, so hitting it is distinguishable from a body that
	// happens to be exactly the limit. Truncated is a fact about the artifact
	// and has to be recorded rather than inferred from a length.
	body, err := io.ReadAll(io.LimitReader(res.Body, c.cfg.MaxBody+1))
	if err != nil {
		return nil, errors.Wrap(err, errors.Unavailable, "egress: read body")
	}
	truncated := int64(len(body)) > c.cfg.MaxBody
	if truncated {
		body = body[:c.cfg.MaxBody]
	}

	return &Response{
		Status:    res.StatusCode,
		Header:    res.Header,
		Body:      body,
		Truncated: truncated,
		Addr:      addr,
		Redirects: hops,
		Duration:  time.Since(started),
	}, nil
}

func dialable(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("egress: %w", ErrNotHTTP)
	}
	return nil
}

// translate keeps a refusal distinguishable from an outage. url.Error wraps
// everything the transport returns, including the Control hook's refusal, and a
// caller that cannot tell them apart retries a permanent policy decision.
func translate(err error, u *url.URL) error {
	switch {
	case errors.Is(err, ErrBlocked):
		return err
	case errors.Is(err, ErrTooManyRedirects), errors.Is(err, ErrNotHTTP):
		return err
	case errors.Is(err, context.Canceled):
		return errors.Wrap(err, errors.Canceled, "egress: "+u.Host)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.Wrap(err, errors.Timeout, "egress: "+u.Host)
	}
	return errors.Wrap(err, errors.Unavailable, "egress: "+u.Host)
}
