package urlguard_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestDialContextConnectsToTheAddressItVetted(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		if c, err := l.Accept(); err == nil {
			_ = c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	resolver := newResolver().sequence("kafka.lan", []string{"127.0.0.1"}, []string{"169.254.169.254"})
	var dialed []string
	p := urlguard.HumanConfigured()
	p.Resolver = resolver
	p.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}

	conn, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("kafka.lan", port))
	if err != nil {
		t.Fatalf("DialContext = %v, want a connection", err)
	}
	_ = conn.Close()
	if len(dialed) != 1 || dialed[0] != net.JoinHostPort("127.0.0.1", port) {
		t.Fatalf("dialed %v, want only the vetted 127.0.0.1", dialed)
	}
	if n := resolver.lookups("kafka.lan"); n != 1 {
		t.Fatalf("%d lookups, want 1", n)
	}

	if _, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("kafka.lan", port)); !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("a rebind to metadata = %v, want ErrBlocked", err)
	}
}

func TestDialContextRefusesMetadataAndAZeroPolicy(t *testing.T) {
	p := urlguard.HumanConfigured()
	p.Resolver = newResolver().sequence("broker.lan", []string{"100.100.100.200"})
	p.Dialer = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("nothing may be dialled")
		return nil, nil
	}
	for _, addr := range []string{"169.254.169.254:9092", "[fd00:ec2::254]:9092", "metadata.google.internal:9092", "broker.lan:9092"} {
		if _, err := p.DialContext(context.Background(), "tcp", addr); !errors.Is(err, urlguard.ErrBlocked) {
			t.Errorf("DialContext(%s) = %v, want ErrBlocked", addr, err)
		}
	}
	var zero urlguard.Policy
	zero.Dialer = p.Dialer
	if _, err := zero.DialContext(context.Background(), "tcp", "93.184.216.34:9092"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Errorf("the zero policy dialled: %v", err)
	}
}

func TestPrecheckHostJudgesLiteralsAndDeniedNamesOffline(t *testing.T) {
	p := urlguard.HumanConfigured()
	p.Resolver = newResolver()
	for _, ok := range []string{"kafka.lan:9092", "localhost", "[::1]:9092", "10.0.0.5:9093", "b-1.msk.amazonaws.com:9096"} {
		if err := p.PrecheckHost(ok); err != nil {
			t.Errorf("PrecheckHost(%s) = %v, want allowed", ok, err)
		}
	}
	for _, bad := range []string{"169.254.169.254:9092", "[fd20:ce::254]:9092", "metadata:9092", "Metadata.Google.Internal", "", ":9092"} {
		if err := p.PrecheckHost(bad); !errors.Is(err, urlguard.ErrBlocked) {
			t.Errorf("PrecheckHost(%q) = %v, want ErrBlocked", bad, err)
		}
	}
	if err := urlguard.PublicOnly().PrecheckHost("127.0.0.1:9092"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Errorf("a public-only policy admitted loopback: %v", err)
	}
}
