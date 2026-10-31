// Package checker performs HTTP/HTTPS checks of monitored URLs. Because the
// URLs come from users, it also decides which targets may be contacted at
// all (SSRF protection).
package checker

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

// MaxURLLength bounds a monitored URL. Longer URLs are almost certainly not
// a site to watch, and the value is stored and returned on every request.
const MaxURLLength = 2048

// Reasons a URL is rejected. They are shown to the client as the issue of
// the "url" field.
var (
	ErrURLInvalid     = errors.New("must be a valid absolute URL")
	ErrURLScheme      = errors.New("must use http or https")
	ErrURLCredentials = errors.New("must not contain a username or password")
	ErrURLTooLong     = errors.New("must be at most 2048 characters")
	ErrURLBlocked     = errors.New("must point to a public address")
)

// cgnat is the carrier-grade NAT range (RFC 6598), not covered by
// netip.Addr.IsPrivate. Cloud providers use it for internal services too.
var (
	cgnat       = netip.MustParsePrefix("100.64.0.0/10")
	thisNetwork = netip.MustParsePrefix("0.0.0.0/8")   // RFC 1122: "this host on this network"
	reserved    = netip.MustParsePrefix("240.0.0.0/4") // RFC 1112, includes 255.255.255.255
)

// IsBlockedIP reports whether ip is an address user-supplied URLs must not
// reach: loopback, private (RFC 1918, IPv6 ULA), link-local (including the
// cloud metadata endpoint 169.254.169.254), CGNAT, multicast, unspecified
// and reserved addresses. IPv4-mapped IPv6 addresses (::ffff:127.0.0.1) are
// judged by their IPv4 part, otherwise they would be a bypass.
func IsBlockedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		cgnat.Contains(ip) ||
		thisNetwork.Contains(ip) ||
		reserved.Contains(ip)
}

// TargetPolicy decides which URLs may be monitored.
type TargetPolicy struct {
	allowPrivate bool
}

// NewTargetPolicy returns a policy. allowPrivate (ALLOW_PRIVATE_TARGETS)
// permits internal addresses; it exists for local development only.
func NewTargetPolicy(allowPrivate bool) TargetPolicy {
	return TargetPolicy{allowPrivate: allowPrivate}
}

// ValidateURL checks a URL before it is stored. It is a syntactic check only:
// a hostname may still resolve to a private address, or start to later (DNS
// rebinding). That is why the checker verifies every address again when it
// connects. Returned errors are one of the ErrURL* values.
func (p TargetPolicy) ValidateURL(raw string) error {
	if len(raw) > MaxURLLength {
		return ErrURLTooLong
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return ErrURLInvalid
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrURLScheme
	}
	// Credentials in the URL would be stored in plain text, returned by the
	// API and written to logs.
	if u.User != nil {
		return ErrURLCredentials
	}
	host := u.Hostname()
	if host == "" {
		return ErrURLInvalid
	}
	if p.allowPrivate {
		return nil
	}
	return checkHost(host)
}

func checkHost(host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if IsBlockedIP(ip) {
			return ErrURLBlocked
		}
		return nil
	}

	name := strings.TrimSuffix(strings.ToLower(host), ".")
	// RFC 6761: "localhost" and its subdomains always mean this machine.
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return ErrURLBlocked
	}
	// A name whose last label is numeric ("2130706433", "0x7f.1", "127.1")
	// is not a DNS name (no top-level domain is numeric) but a shorthand IP
	// form that some resolvers expand to 127.0.0.1.
	if isNumericLabel(name[strings.LastIndexByte(name, '.')+1:]) {
		return ErrURLBlocked
	}
	return nil
}

// isNumericLabel reports whether s is a decimal number or a 0x-prefixed hex
// number, the forms inet_aton accepts in an IPv4 address.
func isNumericLabel(s string) bool {
	digits := s
	isHex := false
	if rest, ok := strings.CutPrefix(s, "0x"); ok {
		digits, isHex = rest, true
	}
	if digits == "" {
		return isHex // a bare "0x" is a valid zero in inet_aton
	}
	for _, c := range digits {
		isDigit := c >= '0' && c <= '9'
		isHexDigit := isHex && c >= 'a' && c <= 'f'
		if !isDigit && !isHexDigit {
			return false
		}
	}
	return true
}
