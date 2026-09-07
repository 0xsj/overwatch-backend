package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/egress"
	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// The security core, and it needs no network at all. Every case here is an
// address a real target has resolved to for somebody.
func TestTheGuardRefusesEveryAddressThatIsNotTheInternet(t *testing.T) {
	strict := egress.NewGuard(egress.Policy{})

	for name, tc := range map[string]struct {
		addr    string
		allowed bool
	}{
		"a public v4":            {"93.184.216.34", true},
		"a public v6":            {"2606:2800:220:1:248:1893:25c8:1946", true},
		"loopback":               {"127.0.0.1", false},
		"loopback, another host": {"127.99.4.2", false},
		"loopback v6":            {"::1", false},
		"unspecified":            {"0.0.0.0", false},
		"unspecified v6":         {"::", false},
		"private 10/8":           {"10.1.2.3", false},
		"private 172.16/12":      {"172.20.0.5", false},
		"private 192.168/16":     {"192.168.1.1", false},
		"unique local v6":        {"fd00::1", false},
		"link-local":             {"169.254.1.1", false},

		// The one that matters most: every major cloud serves credentials here.
		"cloud metadata":     {"169.254.169.254", false},
		"link-local v6":      {"fe80::1", false},
		"multicast":          {"224.0.0.1", false},
		"carrier-grade NAT":  {"100.64.0.1", false},
		"benchmarking range": {"198.18.0.1", false},

		// The two a hand-rolled check misses. Both are IPv4 wearing IPv6, and
		// both pass every v4 rule written against an IPv6 address.
		"IPv4-mapped loopback": {"::ffff:127.0.0.1", false},
		"IPv4-mapped private":  {"::ffff:10.0.0.1", false},
		"NAT64 loopback":       {"64:ff9b::7f00:1", false},
	} {
		t.Run(name, func(t *testing.T) {
			addr := netip.MustParseAddr(tc.addr)
			err := strict.Allows(addr)
			if tc.allowed && err != nil {
				t.Errorf("refused %s: %v", tc.addr, err)
			}
			if !tc.allowed {
				if err == nil {
					t.Fatalf("allowed %s", tc.addr)
				}
				if !errors.Is(err, egress.ErrBlocked) {
					t.Errorf("refused %s as %v, want ErrBlocked", tc.addr, err)
				}
				// A refusal is a decision, not an outage. Retrying it spends the
				// budget re-refusing.
				if errors.Retryable(err) {
					t.Errorf("%s came back retryable", tc.addr)
				}
			}
		})
	}
}

// A guard with the rules hard-coded cannot be tested, because every test server
// binds loopback. Deny beats Allow, and the zero value is the strict one.
func TestPolicyOpensExactlyWhatItSaysAndDenyWins(t *testing.T) {
	loopback := netip.MustParseAddr("127.0.0.1")
	private := netip.MustParseAddr("10.0.0.1")

	if err := egress.NewGuard(egress.Policy{AllowLoopback: true}).Allows(loopback); err != nil {
		t.Errorf("AllowLoopback did not: %v", err)
	}
	if err := egress.NewGuard(egress.Policy{AllowLoopback: true}).Allows(private); err == nil {
		t.Error("AllowLoopback also opened private space")
	}
	if err := egress.NewGuard(egress.Policy{AllowPrivate: true}).Allows(private); err != nil {
		t.Errorf("AllowPrivate did not: %v", err)
	}

	explicit := egress.NewGuard(egress.Policy{
		Allow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	if err := explicit.Allows(private); err != nil {
		t.Errorf("an explicit Allow was ignored: %v", err)
	}

	both := egress.NewGuard(egress.Policy{
		Allow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Deny:  []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")},
	})
	if err := both.Allows(netip.MustParseAddr("10.2.0.1")); err != nil {
		t.Errorf("allowed range refused: %v", err)
	}
	if err := both.Allows(netip.MustParseAddr("10.1.0.1")); err == nil {
		t.Error("Deny did not beat Allow")
	}
}

func loopbackClient(t *testing.T, cfg egress.Config) *egress.Client {
	t.Helper()
	cfg.Guard = egress.NewGuard(egress.Policy{AllowLoopback: true})
	return egress.New(cfg)
}

func TestAGuardedFetchRecordsWhatItActuallyConnectedTo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("server", "nginx/1.25.3")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	res, err := loopbackClient(t, egress.Config{}).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || string(res.Body) != "hello" {
		t.Errorf("got %d %q", res.Status, res.Body)
	}
	if res.Header.Get("server") != "nginx/1.25.3" {
		t.Error("headers were not kept")
	}
	// The address is the point: it is what the guard already had to know, and
	// it is an observation this product exists to record.
	if !res.Addr.IsValid() || !res.Addr.IsLoopback() {
		t.Errorf("Addr is %v — the connection's real peer was not recorded", res.Addr)
	}
	if res.Duration <= 0 {
		t.Error("no duration")
	}
}

// The reason the guard is at dial time. The URL says a permitted host; the
// redirect goes somewhere it must not, and the check fires on the new dial
// rather than on a re-parse of the URL.
func TestARedirectIntoPrivateSpaceIsRefusedAtTheDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	_, err := loopbackClient(t, egress.Config{}).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("followed a redirect into link-local space")
	}
	if !errors.Is(err, egress.ErrBlocked) {
		t.Errorf("refused as %v, want ErrBlocked", err)
	}
}

func TestARedirectLoopIsCappedRatherThanFollowed(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()

	_, err := loopbackClient(t, egress.Config{MaxRedirects: 2}).Get(context.Background(), srv.URL)
	if !errors.Is(err, egress.ErrTooManyRedirects) {
		t.Fatalf("gave %v, want ErrTooManyRedirects", err)
	}
}

// A tool pointed at a large file must not take the process with it, and hitting
// the cap is a FACT about the artifact rather than something to infer from a
// length.
func TestABodyIsCappedAndSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer srv.Close()

	res, err := loopbackClient(t, egress.Config{MaxBody: 1000}).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Body) != 1000 {
		t.Errorf("kept %d bytes, want the cap of 1000", len(res.Body))
	}
	if !res.Truncated {
		t.Error("truncated is false on a truncated body")
	}

	exact, err := loopbackClient(t, egress.Config{MaxBody: 5000}).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if exact.Truncated {
		t.Error("a body exactly at the cap reported as truncated")
	}
}

func TestOnlyHTTPIsDialled(t *testing.T) {
	c := loopbackClient(t, egress.Config{})
	for _, u := range []string{"file:///etc/passwd", "gopher://x/", "ftp://example.com/"} {
		if _, err := c.Get(context.Background(), u); !errors.Is(err, egress.ErrNotHTTP) {
			t.Errorf("Get(%q) gave %v, want ErrNotHTTP", u, err)
		}
	}
}

func TestTheStrictGuardRefusesALoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	c := egress.New(egress.Config{Guard: egress.NewGuard(egress.Policy{})})
	_, err := c.Get(context.Background(), srv.URL)
	if !errors.Is(err, egress.ErrBlocked) {
		t.Fatalf("the strict guard allowed loopback: %v", err)
	}
}

func TestAnUnguardedClientIsUnconstructable(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New with a nil Guard returned a client")
		}
	}()
	egress.New(egress.Config{})
}

// The bypass Unmap exists for, and it is NOT the one people expect. netip's own
// IsPrivate and IsLoopback already understand a 4-in-6 address — but
// Prefix.Contains returns false across address families, so an explicit
// Deny of 10.0.0.0/8 is silently bypassed by ::ffff:10.0.0.1 unless the address
// is unmapped first. Found by mutation: deleting Unmap killed nothing.
func TestAnExplicitPrefixStillMatchesAnIPv4MappedAddress(t *testing.T) {
	denied := egress.NewGuard(egress.Policy{
		AllowPrivate: true,
		Deny:         []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	for _, addr := range []string{"10.0.0.1", "::ffff:10.0.0.1"} {
		if err := denied.Allows(netip.MustParseAddr(addr)); !errors.Is(err, egress.ErrBlocked) {
			t.Errorf("Deny 10.0.0.0/8 did not catch %s: %v", addr, err)
		}
	}

	allowed := egress.NewGuard(egress.Policy{
		Allow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	for _, addr := range []string{"10.0.0.1", "::ffff:10.0.0.1"} {
		if err := allowed.Allows(netip.MustParseAddr(addr)); err != nil {
			t.Errorf("Allow 10.0.0.0/8 did not cover %s: %v", addr, err)
		}
	}
}

// 224.0.0.1 is link-local multicast and is caught a branch earlier. Ordinary
// multicast needs its own case or the branch is unreachable and untested.
func TestOrdinaryMulticastIsRefused(t *testing.T) {
	if err := egress.NewGuard(egress.Policy{}).Allows(netip.MustParseAddr("239.1.2.3")); !errors.Is(err, egress.ErrBlocked) {
		t.Errorf("global-scope multicast was allowed: %v", err)
	}
}

// Control is handed a RESOLVED address. A name arriving there means something
// dialled without resolving, which this guard cannot judge — so it refuses
// rather than passing it through.
func TestControlRefusesWhatItCannotJudge(t *testing.T) {
	g := egress.NewGuard(egress.Policy{AllowLoopback: true})
	for _, address := range []string{
		"example.com:80", // never resolved
		"127.0.0.1",      // no port
		"",               // nothing at all
	} {
		if err := g.Control("tcp", address, nil); !errors.Is(err, egress.ErrBlocked) {
			t.Errorf("Control(%q) gave %v, want ErrBlocked", address, err)
		}
	}
	if err := g.Control("tcp", "127.0.0.1:80", nil); err != nil {
		t.Errorf("a resolved, permitted address was refused: %v", err)
	}
}

func TestTheZeroAddressIsNotAnAddress(t *testing.T) {
	if err := egress.NewGuard(egress.Policy{AllowLoopback: true, AllowPrivate: true}).
		Allows(netip.Addr{}); !errors.Is(err, egress.ErrBlocked) {
		t.Errorf("the zero Addr was allowed: %v", err)
	}
}
