// Package runner checks customer code only inside disposable Docker containers.
package runner

import (
	"context"
	"time"
)

const Profile = "business-v1"
const Image = "fde-runner:go1.26.0-node22.22.3-pnpm11.23.0"

type Request struct {
	Workspace            string
	OutputDir            string
	Profile              string
	ExpectedSnapshotHash string
	GOARCH               string
	Timeout              time.Duration
}

// Command describes a host Docker client invocation, never a customer shell command.
type Command struct {
	Args        []string
	Dir         string
	Stdin       string
	LogPath     string
	MaxLogBytes int64
}
type Result struct {
	ExitCode  int
	Output    string
	Truncated bool
}
type Executor interface {
	Run(context.Context, Command) (Result, error)
}

type Step struct {
	Name       string `json:"name"`
	ExitCode   int    `json:"exitCode"`
	LogPath    string `json:"logPath"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"durationMs"`
}
type Report struct {
	ID           string            `json:"id"`
	Profile      string            `json:"profile"`
	SnapshotHash string            `json:"snapshotHash"`
	Success      bool              `json:"success"`
	FailedStep   string            `json:"failedStep,omitempty"`
	Error        string            `json:"error,omitempty"`
	ReportPath   string            `json:"reportPath"`
	ArtifactsDir string            `json:"artifactsDir"`
	Artifacts    map[string]string `json:"artifacts"`
	GOOS         string            `json:"goos"`
	GOARCH       string            `json:"goarch"`
	ImageID      string            `json:"imageId"`
	Toolchain    map[string]string `json:"toolchain"`
	Steps        []Step            `json:"steps"`
	StartedAt    time.Time         `json:"startedAt"`
	FinishedAt   time.Time         `json:"finishedAt"`
}

type Runner struct{ executor Executor }

func New(executor Executor) *Runner {
	if executor == nil {
		executor = OSExecutor{}
	}
	return &Runner{executor: executor}
}
func Check(ctx context.Context, req Request) (Report, error) { return New(nil).Check(ctx, req) }
