// Package ui contains the Fyne GUI: main window, list, and dialogs.
package ui

import (
	"context"
	"time"

	"github.com/pglass/checkin/internal/store"
)

// timeFmt formats a check-in/out timestamp for the list; empty for nil.
func timeFmt(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("3:04 PM")
}

// loadRows fetches the current student rows from the store.
func loadRows(ctx context.Context, s *store.Store) ([]store.StudentRow, error) {
	return s.Students(ctx)
}
