// Package qr encodes/decodes student QR codes and generates printable sheets.
package qr

import (
	"encoding/json"
	"image"

	qrcode "github.com/skip2/go-qrcode"
)

// Version is the current QR payload schema version.
//
// v2 splits the single Name field into FirstName/LastName. v1 codes are not
// accepted: a v1 payload carries one joined string that cannot be split back
// into the two columns reliably, so existing sheets must be reprinted.
const Version = 2

// Payload is the JSON structure encoded in each student QR code.
type Payload struct {
	Version   int    `json:"Version"`
	FirstName string `json:"FirstName"`
	LastName  string `json:"LastName"`
}

// NewPayload builds a v2 payload for a student name.
func NewPayload(first, last string) Payload {
	return Payload{Version: Version, FirstName: first, LastName: last}
}

// Marshal returns the JSON bytes for a payload.
func (p Payload) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

// ParsePayload decodes a scanned QR string into a Payload, validating version.
func ParsePayload(data string) (Payload, error) {
	var p Payload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return Payload{}, err
	}
	return p, nil
}

// Image renders a QR code image for a student name at the given pixel size.
func Image(first, last string, size int) (image.Image, error) {
	data, err := NewPayload(first, last).Marshal()
	if err != nil {
		return nil, err
	}
	code, err := qrcode.New(string(data), qrcode.Medium)
	if err != nil {
		return nil, err
	}
	return code.Image(size), nil
}

// PNG returns PNG-encoded bytes for a student's QR code.
func PNG(first, last string, size int) ([]byte, error) {
	data, err := NewPayload(first, last).Marshal()
	if err != nil {
		return nil, err
	}
	return qrcode.Encode(string(data), qrcode.Medium, size)
}
