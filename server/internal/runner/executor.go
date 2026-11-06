package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type OSExecutor struct{}
type boundedLog struct {
	mu        sync.Mutex
	file      *os.File
	output    bytes.Buffer
	limit     int64
	truncated bool
}

func (w *boundedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	left := w.limit - int64(w.output.Len())
	if left < int64(len(p)) {
		w.truncated = true
		if left < 0 {
			left = 0
		}
		p = p[:left]
	}
	if len(p) > 0 {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
		_, _ = w.output.Write(p)
	}
	return n, nil
}

func (OSExecutor) Run(ctx context.Context, command Command) (Result, error) {
	if len(command.Args) == 0 {
		return Result{ExitCode: -1}, errors.New("missing Docker arguments")
	}
	if err := os.MkdirAll(filepath.Dir(command.LogPath), 0o700); err != nil {
		return Result{ExitCode: -1}, err
	}
	f, err := os.OpenFile(command.LogPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	defer f.Close()
	limit := command.MaxLogBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 256 << 10
	}
	log := &boundedLog{file: f, limit: limit}
	cmd := exec.CommandContext(ctx, "docker", command.Args...)
	cmd.Dir = command.Dir
	cmd.Stdin = strings.NewReader(command.Stdin)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.WaitDelay = 3 * time.Second
	// The Docker client may use the operator's local context, but none of this
	// environment or Docker configuration is passed or mounted into a container.
	for _, key := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	err = cmd.Run()
	result := Result{ExitCode: 0, Output: log.output.String(), Truncated: log.truncated}
	if err != nil {
		result.ExitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if exit != nil {
			return result, nil
		}
		return result, err
	}
	return result, nil
}
