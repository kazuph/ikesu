//go:build darwin && signed_app_integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Capture before TestMain isolates HOME and other test state.
var signedTestApp = os.Getenv("IKESU_SIGNED_TEST_APP")

func TestMacDialogInstallDeveloperIDSignature(t *testing.T) {
	if signedTestApp == "" {
		t.Fatal("IKESU_SIGNED_TEST_APP must name an existing Developer ID app")
	}
	for _, tc := range []struct {
		name, app string
		accepted  bool
	}{
		{"expected-developer", signedTestApp, true},
		{"different-signer", "/System/Applications/TextEdit.app", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if info, err := os.Stat(tc.app); err != nil || !info.IsDir() {
				t.Fatalf("real app fixture unavailable: %s: %v", tc.app, err)
			}
			root := t.TempDir()
			// Alias only the bundle path; never change the installed signed app.
			if err := os.Symlink(tc.app, filepath.Join(root, "IkesuDecisionDialog.app")); err != nil {
				t.Fatal(err)
			}
			err := verifyMacDialogSignature(root)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v: %v", tc.accepted, err)
			}
			if err != nil && strings.Contains(err.Error(), "invalid requirement specification") {
				t.Fatal(err)
			}
			t.Logf("app=%s accepted=%v error=%v", tc.app, tc.accepted, err)
		})
	}
}
