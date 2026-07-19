// Package qr encodes/decodes student QR codes and generates printable sheets.
package qr

import (
	"encoding/json"
	"image"

	qrcode "github.com/skip2/go-qrcode"
)

// Version is the current QR payload schema version.
const Version = 1

// Payload is the JSON structure encoded in each student QR code.
type Payload struct {
	Version int    `json:"Version"`
	Name    string `json:"Name"`
}

// NewPayload builds a v1 payload for a student name.
func NewPayload(name string) Payload {
	return Payload{Version: Version, Name: name}
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
func Image(name string, size int) (image.Image, error) {
	data, err := NewPayload(name).Marshal()
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
func PNG(name string, size int) ([]byte, error) {
	data, err := NewPayload(name).Marshal()
	if err != nil {
		return nil, err
	}
	return qrcode.Encode(string(data), qrcode.Medium, size)
}
