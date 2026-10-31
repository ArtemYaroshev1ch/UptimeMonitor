package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// classify maps a transport error to an ErrorKind. The order matters: a
// blocked address is reported as such even though it surfaces as a dial
// error, and a DNS lookup that timed out is a DNS problem.
func classify(err error) domain.ErrorKind {
	switch {
	case isBlocked(err):
		return domain.ErrorBlockedTarget
	case errors.Is(err, errTooManyRedirects):
		return domain.ErrorUnexpectedStatus
	case isTLS(err):
		return domain.ErrorTLS
	case isDNS(err):
		return domain.ErrorDNS
	case isTimeout(err):
		return domain.ErrorTimeout
	case isConnection(err):
		return domain.ErrorConnection
	default:
		return domain.ErrorUnknown
	}
}

func isBlocked(err error) bool {
	_, ok := errors.AsType[blockedError](err)
	return ok || errors.Is(err, ErrURLBlocked)
}

// isTLS covers certificate problems (expired, self-signed or unknown CA,
// wrong host name) and handshake failures.
func isTLS(err error) bool {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return true
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return true
	}
	if _, ok := errors.AsType[tls.AlertError](err); ok {
		return true
	}
	return false
}

func isDNS(err error) bool {
	_, ok := errors.AsType[*net.DNSError](err)
	return ok
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	netErr, ok := errors.AsType[net.Error](err)
	return ok && netErr.Timeout()
}

// isConnection covers refused and reset connections and a peer that closed
// the connection mid-response.
func isConnection(err error) bool {
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}
