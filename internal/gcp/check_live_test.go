package gcp

import (
	"context"
	"os"
	"testing"
)

// Runs only with SECDROP_LIVE=1; prints what the sign-in screen would show.
func TestLiveCheck(t *testing.T) {
	if os.Getenv("SECDROP_LIVE") == "" {
		t.Skip("set SECDROP_LIVE=1")
	}
	id, err := Check(context.Background())
	t.Logf("email=%q project=%q err=%v", id.Email, id.Project, err)
}
