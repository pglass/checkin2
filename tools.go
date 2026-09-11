//go:build tools

// Package tools pins build-time-only tool dependencies so `go mod tidy` keeps
// them in go.mod/go.sum. They are never compiled into the app.
//
//   - goversioninfo: generates the Windows versioninfo resource (fyne.syso) that
//     scripts/build-windows.sh links into checkin.exe so Explorer shows a File version.
package tools

import _ "github.com/josephspurrier/goversioninfo/cmd/goversioninfo"
