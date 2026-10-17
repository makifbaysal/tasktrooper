package urlguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestHumanConfiguredAllowsThisMachineAndTheLocalNetwork(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		answers []string
	}{
		{"loopback literal", "127.0.0.1:9090", nil},
		{"loopback v6", "[::1]:9090", nil},
		{"localhost name", "localhost:9090", []string{"127.0.0.1"}},
		{"rfc1918", "10.0.0.5:9090", nil},
		{"home network", "192.168.1.20:8428", nil},
		{"ula", "[fd12::1]", nil},
		{"private via dns", "prometheus.lan", []string{"192.168.1.20"}},
		{"public", "prometheus-prod-01.grafana.net", []string{"34.120.1.2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := urlguard.HumanConfigured()
			if tc.answers != nil {
				hostOnly := tc.host
				if i := strings.LastIndex(hostOnly, ":"); i > 0 && !strings.HasSuffix(hostOnly, "]") {
					hostOnly = hostOnly[:i]
				}
				p.Resolver = newResolver().sequence(hostOnly, tc.answers)
			}
			if _, err := p.Validate(context.Background(), "http://"+tc.host+"/api/v1/query"); err != nil {
				t.Fatalf("Validate(%s) = %v, want allowed", tc.host, err)
			}
		})
	}
}

func TestHumanConfiguredStillRefusesCloudMetadata(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		answers []string
	}{
		{"aws/gcp v4 metadata", "169.254.169.254", nil},
		{"link-local v6", "[fe80::1]", nil},
		{"aws v6 metadata", "[fd00:ec2::254]", nil},
		{"gce v6 metadata", "[fd20:ce::254]", nil},
		{"alibaba metadata inside cgnat", "100.100.100.200", nil},
		{"alibaba metadata via dns", "prom.lan", []string{"100.100.100.200"}},
		{"alibaba metadata v4-mapped", "[::ffff:100.100.100.200]", nil},
		{"alibaba metadata via nat64", "[64:ff9b::100.100.100.200]", nil},
		{"gcp metadata name", "metadata.google.internal", []string{"10.0.0.1"}},
		{"gcp metadata name, fqdn", "metadata.google.internal.", []string{"10.0.0.1"}},
		{"gcp metadata name, upper case", "METADATA.GOOGLE.INTERNAL", []string{"10.0.0.1"}},
		{"name resolving to metadata", "prom.evil.test", []string{"169.254.169.254"}},
		{"v4-mapped metadata", "[::ffff:169.254.169.254]", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := urlguard.HumanConfigured()
			if tc.answers != nil {
				p.Resolver = newResolver().sequence(strings.TrimSuffix(strings.ToLower(tc.host), "."), tc.answers)
			}
			if _, err := p.Validate(context.Background(), "http://"+tc.host+"/"); !errors.Is(err, urlguard.ErrBlocked) {
				t.Fatalf("Validate(%s) = %v, want ErrBlocked", tc.host, err)
			}
		})
	}
}

func TestHumanConfiguredPrecheckRefusesMetadataNamesWithoutDNS(t *testing.T) {
	p := urlguard.HumanConfigured()
	for _, raw := range []string{
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/latest/meta-data/",
	} {
		if _, err := p.Precheck(raw); !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("Precheck(%q) = %v, want ErrBlocked", raw, err)
		}
	}
	if _, err := p.Precheck("http://localhost:9090"); err != nil {
		t.Fatalf("Precheck(localhost) = %v", err)
	}
}

func TestHumanConfiguredRefusesARebindToMetadataAtDial(t *testing.T) {
	h := newHarness()
	h.policy = urlguard.HumanConfigured()
	h.policy.Resolver = h.resolver
	h.policy.Dialer = h.dialer.dial
	h.resolver.sequence("rebind.test", []string{"192.168.1.20"}, []string{"169.254.169.254"})

	if _, err := h.policy.Validate(context.Background(), "http://rebind.test/"); err != nil {
		t.Fatalf("the first answer is private and must pass: %v", err)
	}
	if _, err := h.policy.Client(5 * time.Second).Get("http://rebind.test/"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("the rebound metadata address must be refused at dial time, got %v", err)
	}
	for _, addr := range h.dialer.addresses() {
		if strings.HasPrefix(addr, "169.254.") {
			t.Fatalf("the guard connected to %s", addr)
		}
	}
}

func TestEveryPolicyRefusesTheMetadataAddressesByName(t *testing.T) {
	policies := map[string]urlguard.Policy{
		"public only":      urlguard.PublicOnly(),
		"human configured": urlguard.HumanConfigured(),
		"loopback allowed": func() urlguard.Policy { p := urlguard.PublicOnly(); p.AllowLoopback = true; return p }(),
	}
	for name, p := range policies {
		for _, host := range []string{"100.100.100.200", "[fd00:ec2::254]", "[fd20:ce::254]"} {
			_, err := p.Validate(context.Background(), "http://"+host+"/")
			if !errors.Is(err, urlguard.ErrBlocked) || !strings.Contains(err.Error(), "cloud metadata") {
				t.Fatalf("%s: Validate(%s) = %v, want a cloud metadata refusal", name, host, err)
			}
		}
	}
}

func TestSameHostRedirectRefusesASchemeChange(t *testing.T) {
	p := urlguard.HumanConfigured()
	endpoint, err := url.Parse("https://prom.example.test/api/v1/query")
	if err != nil {
		t.Fatal(err)
	}
	check := p.SameHostRedirect(endpoint)
	for _, target := range []string{"http://prom.example.test/api/v1/query", "http://prom.example.test:443/api/v1/query"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := check(req, nil); !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("redirect to %s = %v, want ErrBlocked", target, err)
		}
	}
}
