// Package qr encodes/decodes student QR codes and generates printable sheets.
package qr

import (
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// DefaultBaseURL is the base the encoded name is appended to when no base is
// configured. It points at the static page that renders a scanned code back as
// a QR image, so a code scanned by a phone camera opens something meaningful
// rather than showing an opaque string.
const DefaultBaseURL = "https://pglass.github.io/checkin2/qr/"

// Codes encode a URL of the form <base>/<NameBase64>, where NameBase64 is
// Base64URL(First + "+" + Last) -- RFC 4648 section 5, the URL-safe alphabet,
// with padding omitted so the value needs no escaping in a path segment.
//
// The name is joined with "+" because that character cannot appear in the
// base64url alphabet, so splitting the decoded text on it is unambiguous even
// when a name itself contains spaces or punctuation.
const nameSep = "+"

// nameEncoding is base64url without padding: "=" is legal in a path but is
// needlessly escaped by some scanners and link handlers.
var nameEncoding = base64.URLEncoding.WithPadding(base64.NoPadding)

// EncodeName returns the NameBase64 segment for a student name:
// Base64URL(First + "+" + Last).
func EncodeName(first, last string) string {
	return nameEncoding.EncodeToString([]byte(first + nameSep + last))
}

// URL returns the full QR payload for a student: base joined to the encoded
// name. A base with or without a trailing slash produces the same result.
func URL(base, first, last string) string {
	return strings.TrimRight(base, "/") + "/" + EncodeName(first, last)
}

// ParseURL extracts a student name from a scanned QR payload. The payload is
// expected to be a URL whose final path segment is Base64URL(First+"+"+Last);
// the base is not checked, so a code still scans after the configured base URL
// changes (a reprint would otherwise be needed for every existing card).
func ParseURL(data string) (first, last string, err error) {
	seg := strings.TrimSpace(data)
	// Drop any query or fragment before taking the last path segment, so a URL
	// decorated by a scanner or link handler still parses.
	if i := strings.IndexAny(seg, "?#"); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.TrimRight(seg, "/")
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	if seg == "" {
		return "", "", errors.New("qr: no name segment in payload")
	}

	raw, err := nameEncoding.DecodeString(seg)
	if err != nil {
		return "", "", fmt.Errorf("qr: name segment is not base64url: %w", err)
	}

	first, last, ok := strings.Cut(string(raw), nameSep)
	if !ok {
		return "", "", fmt.Errorf("qr: decoded name %q has no %q separator", raw, nameSep)
	}
	if first == "" || last == "" {
		return "", "", fmt.Errorf("qr: decoded name %q is missing a part", raw)
	}
	return first, last, nil
}

// Image renders a QR code image for a student name at the given pixel size.
func Image(base, first, last string, size int) (image.Image, error) {
	code, err := qrcode.New(URL(base, first, last), qrcode.Medium)
	if err != nil {
		return nil, err
	}
	return code.Image(size), nil
}

// PNG returns PNG-encoded bytes for a student's QR code.
func PNG(base, first, last string, size int) ([]byte, error) {
	return qrcode.Encode(URL(base, first, last), qrcode.Medium, size)
}
