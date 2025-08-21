package pkgmgr

import (
	"context"
	"os"
	"testing"
)

func TestAptInstallDryRun(t *testing.T) {
	os.Setenv("EGRESS", "0")
	AdminOverride = true
	resp := AptInstall(context.Background(), AptInstallRequest{Packages: []string{"sl"}, DryRun: true})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", resp.ExitCode)
	}
	if len(resp.Installed) != 1 || resp.Installed[0] != "sl" {
		t.Fatalf("unexpected installed %v", resp.Installed)
	}
}

func TestAptInstallDisabled(t *testing.T) {
	os.Setenv("EGRESS", "0")
	AdminOverride = false
	resp := AptInstall(context.Background(), AptInstallRequest{Packages: []string{"sl"}, DryRun: true})
	if resp.ExitCode == 0 {
		t.Fatalf("expected non-zero exit when disabled")
	}
}

func TestPipInstallDryRun(t *testing.T) {
	os.Setenv("EGRESS", "0")
	AdminOverride = true
	resp := PipInstall(context.Background(), PipInstallRequest{Packages: []string{"requests"}, DryRun: true})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", resp.ExitCode)
	}
	if len(resp.Installed) != 1 || resp.Installed[0] != "requests" {
		t.Fatalf("unexpected installed %v", resp.Installed)
	}
}

func TestNpmInstallDryRun(t *testing.T) {
	os.Setenv("EGRESS", "0")
	AdminOverride = true
	resp := NpmInstall(context.Background(), NpmInstallRequest{Packages: []string{"left-pad"}, DryRun: true})
	if resp.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", resp.ExitCode)
	}
	if len(resp.Installed) != 1 || resp.Installed[0] != "left-pad" {
		t.Fatalf("unexpected installed %v", resp.Installed)
	}
}
