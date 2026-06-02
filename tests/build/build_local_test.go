package build_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildLocalScriptProducesRunnableBinary(t *testing.T) {
	repo := filepath.Join("..", "..")
	installDir := t.TempDir()

	cmd := exec.Command("./scripts/build-local.sh")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "SUPER_AGENT_INSTALL_DIR="+installDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-local failed: %v\n%s", err, output)
	}

	binary := filepath.Join(installDir, "super-agent")
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("binary mode = %v, want executable", info.Mode())
	}

	help := exec.Command(binary, "-h")
	if output, err := help.CombinedOutput(); err != nil {
		t.Fatalf("binary -h failed: %v\n%s", err, output)
	}
}
