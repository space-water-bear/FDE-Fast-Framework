package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/initialize"
	_ "fde-admin/internal/router" // Compile generated business registrations into runtime commands.
	"fde-admin/internal/runner"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fde prepare-image|snapshot|check|pack|inspect|install|install-upgrade|upgrade|rollback")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "upgrade", "rollback":
		configPath := flags.String("config", "", "stopped target runtime configuration")
		oldManifest := flags.String("from-manifest", "", "verified old release manifest")
		newManifest := flags.String("manifest", "", "verified new release manifest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		var result initialize.RuntimeUpgradeResult
		if args[0] == "upgrade" {
			result, err = initialize.UpgradeRuntime(ctx, cfg, *configPath, *oldManifest, *newManifest)
		} else {
			result, err = initialize.RollbackRuntime(ctx, cfg, *configPath, *oldManifest, *newManifest)
		}
		printJSON(result)
		return err
	case "install-upgrade":
		oldPath := flags.String("from-package", "", "verified installed baseline package")
		newPath := flags.String("package", "", "verified new release package")
		target := flags.String("target", "", "existing target directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := delivery.InstallUpgrade(ctx, *oldPath, *newPath, *target)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	case "prepare-image":
		output := flags.String("output", "./runner-output", "trusted image build logs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return runner.PrepareImage(ctx, *output)
	case "snapshot":
		workspace := flags.String("workspace", ".", "customer source workspace")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		hash, err := runner.SnapshotHash(*workspace)
		if err != nil {
			return err
		}
		printJSON(map[string]string{"snapshotHash": hash})
		return nil
	case "check":
		workspace := flags.String("workspace", "", "customer source workspace")
		output := flags.String("output", "", "output outside source workspace")
		hash := flags.String("snapshot", "", "expected source hash")
		arch := flags.String("arch", "arm64", "Linux architecture")
		timeout := flags.Duration("timeout", 30*time.Minute, "maximum verification duration")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		report, err := runner.Check(ctx, runner.Request{Workspace: *workspace, OutputDir: *output, ExpectedSnapshotHash: *hash, GOARCH: *arch, Timeout: *timeout})
		printJSON(report)
		return err
	case "pack":
		workspace := flags.String("workspace", "", "verified source workspace")
		report := flags.String("checks", "", "successful report.json")
		output := flags.String("output", "", "version package directory")
		commit := flags.String("commit", "", "source baseline commit")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		pkg, err := delivery.Pack(ctx, delivery.PackRequest{Workspace: *workspace, CheckReportPath: *report, OutputDir: *output, SourceCommit: *commit})
		if err != nil {
			return err
		}
		printJSON(pkg)
		return nil
	case "inspect":
		path := flags.String("package", "", "version package")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		pkg, err := delivery.Inspect(*path)
		if err != nil {
			return err
		}
		printJSON(pkg)
		return nil
	case "install":
		path := flags.String("package", "", "verified version package")
		target := flags.String("target", "", "first-install test directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := delivery.InstallContext(ctx, *path, *target)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	default:
		return errors.New("unknown operation; no arbitrary command execution is supported")
	}
}
