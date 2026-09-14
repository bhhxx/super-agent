//go:build linux

package tools

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type bubblewrapSandbox struct {
	bwrap   string
	prlimit string
	config  SandboxConfig
}

func newPlatformSandbox(config SandboxConfig) (commandSandbox, error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, errors.New("strict sandbox requires bubblewrap: " + err.Error())
	}
	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		return nil, errors.New("strict sandbox requires prlimit: " + err.Error())
	}
	return &bubblewrapSandbox{bwrap: bwrap, prlimit: prlimit, config: config}, nil
}

func (s *bubblewrapSandbox) wrap(workspaceRoot, cwd, name string, commandArgs []string) (string, []string, string, error) {
	if workspaceRoot == "" {
		return "", nil, "", errors.New("sandbox workspace is not configured")
	}
	workspaceRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "", nil, "", err
	}
	if cwd == "" {
		cwd = workspaceRoot
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return "", nil, "", err
	}
	absCWD, err = filepath.EvalSymlinks(absCWD)
	if err != nil {
		return "", nil, "", err
	}
	rel, err := filepath.Rel(workspaceRoot, absCWD)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, "", errors.New("sandbox cwd is outside workspace")
	}

	args := []string{
		"--die-with-parent", "--new-session", "--unshare-all",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp", "--tmpfs", "/var/tmp",
		"--bind", workspaceRoot, workspaceRoot,
		"--proc", "/proc", "--dev", "/dev",
		"--dir", "/tmp/super-agent-home", "--dir", "/tmp/super-agent-cache",
		"--setenv", "HOME", "/tmp/super-agent-home",
		"--setenv", "TMPDIR", "/tmp",
		"--setenv", "XDG_CACHE_HOME", "/tmp/super-agent-cache",
		"--setenv", "GOCACHE", "/tmp/super-agent-cache/go-build",
		"--chdir", absCWD,
	}
	if s.config.AllowNetwork {
		args = append(args, "--share-net")
	}
	args = append(args,
		"--", s.prlimit,
		"--cpu="+strconv.Itoa(s.config.CPUSeconds),
		"--as="+strconv.FormatInt(s.config.MemoryBytes, 10),
		"--nproc="+strconv.Itoa(s.config.MaxProcesses),
		"--nofile="+strconv.Itoa(s.config.MaxOpenFiles),
		"--", name,
	)
	args = append(args, commandArgs...)
	return s.bwrap, args, workspaceRoot, nil
}
