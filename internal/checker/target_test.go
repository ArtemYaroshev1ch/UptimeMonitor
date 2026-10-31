package checker

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip      string
		blocked bool
	}{
		{"93.184.215.14", false},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
		{"100.63.255.255", false}, // just below CGNAT
		{"127.0.0.1", true},
		{"127.255.255.254", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // cloud metadata
		{"100.64.0.1", true},      // CGNAT
		{"0.0.0.0", true},
		{"0.1.2.3", true},
		{"224.0.0.1", true},
		{"239.255.255.250", true},
		{"240.0.0.1", true},
		{"255.255.255.255", true},
		{"::1", true},
		{"::", true},
		{"fc00::1", true},
		{"fd12:3456::1", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"::ffff:127.0.0.1", true}, // IPv4-mapped loopback
		{"::ffff:10.0.0.1", true},
		{"::ffff:8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.blocked, IsBlockedIP(netip.MustParseAddr(tt.ip)))
		})
	}
}

func TestIsBlockedIPZeroValue(t *testing.T) {
	t.Parallel()
	assert.True(t, IsBlockedIP(netip.Addr{}), "an invalid address is never allowed")
}

func TestValidateURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{name: "https", url: "https://example.com"},
		{name: "http with path, query and port", url: "http://example.com:8080/health?full=1"},
		{name: "public IPv4", url: "http://93.184.215.14/"},
		{name: "public IPv6", url: "http://[2606:4700:4700::1111]/"},
		{name: "digits inside a name are fine", url: "https://1.example.com"},
		{name: "hex-looking name with a real TLD", url: "https://cafe.de"},

		{name: "empty", url: "", wantErr: ErrURLInvalid},
		{name: "relative", url: "/health", wantErr: ErrURLInvalid},
		{name: "no host", url: "http://", wantErr: ErrURLInvalid},
		{name: "port only", url: "http://:80", wantErr: ErrURLInvalid},
		{name: "unparsable", url: "http://exa mple.com", wantErr: ErrURLInvalid},
		{name: "opaque", url: "http:example.com", wantErr: ErrURLInvalid},
		{name: "ftp", url: "ftp://example.com", wantErr: ErrURLScheme},
		{name: "file", url: "file:///etc/passwd", wantErr: ErrURLInvalid},
		{name: "gopher", url: "gopher://example.com", wantErr: ErrURLScheme},
		{name: "upper-case scheme", url: "HTTP://example.com", wantErr: nil},
		{name: "credentials", url: "https://user:pass@example.com", wantErr: ErrURLCredentials},
		{name: "too long", url: "https://example.com/" + strings.Repeat("a", MaxURLLength), wantErr: ErrURLTooLong},

		{name: "loopback", url: "http://127.0.0.1:8080/", wantErr: ErrURLBlocked},
		{name: "IPv6 loopback", url: "http://[::1]/", wantErr: ErrURLBlocked},
		{name: "IPv4-mapped IPv6", url: "http://[::ffff:127.0.0.1]/", wantErr: ErrURLBlocked},
		{name: "metadata endpoint", url: "http://169.254.169.254/latest/meta-data/", wantErr: ErrURLBlocked},
		{name: "private", url: "http://192.168.0.1/", wantErr: ErrURLBlocked},
		{name: "localhost", url: "http://localhost:9090/metrics", wantErr: ErrURLBlocked},
		{name: "localhost with trailing dot", url: "http://LOCALHOST./", wantErr: ErrURLBlocked},
		{name: "localhost subdomain", url: "http://api.localhost/", wantErr: ErrURLBlocked},
		{name: "decimal IP", url: "http://2130706433/", wantErr: ErrURLBlocked},
		{name: "hex IP", url: "http://0x7f000001/", wantErr: ErrURLBlocked},
		{name: "short IP", url: "http://127.1/", wantErr: ErrURLBlocked},
		{name: "hex dotted IP", url: "http://0x7f.0.0.1/", wantErr: ErrURLBlocked},
	}

	policy := NewTargetPolicy(false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := policy.ValidateURL(tt.url)

			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateURLAllowPrivate(t *testing.T) {
	t.Parallel()
	policy := NewTargetPolicy(true)

	require.NoError(t, policy.ValidateURL("http://localhost:8080/healthz"))
	require.NoError(t, policy.ValidateURL("http://10.0.0.5/"))
	// Only the address check is relaxed, not the syntax rules.
	require.ErrorIs(t, policy.ValidateURL("ftp://10.0.0.5/"), ErrURLScheme)
	require.ErrorIs(t, policy.ValidateURL("http://u:p@10.0.0.5/"), ErrURLCredentials)
}
