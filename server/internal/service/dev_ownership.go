package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"fde-admin/internal/model"
)

const developmentOwnershipPath = "fde/assets/ownership.json"

type developmentFileOwnership struct {
	ObjectKey    string `json:"objectKey,omitempty"`
	Kind         string `json:"kind"`
	TemplateHash string `json:"templateHash,omitempty"`
	CurrentHash  string `json:"currentHash"`
}
type developmentOwnershipManifest struct {
	Version int                                 `json:"version"`
	Files   map[string]developmentFileOwnership `json:"files"`
}

func emptyDevelopmentOwnership() developmentOwnershipManifest {
	return developmentOwnershipManifest{Version: 1, Files: map[string]developmentFileOwnership{}}
}
func parseDevelopmentOwnership(raw []byte) (developmentOwnershipManifest, error) {
	var manifest developmentOwnershipManifest
	if json.Unmarshal(raw, &manifest) != nil || manifest.Version != 1 || manifest.Files == nil {
		return manifest, errors.New("业务文件所有权清单无效")
	}
	for path, owner := range manifest.Files {
		if path == developmentOwnershipPath || allowedDevelopmentPath(path, true) != nil || owner.CurrentHash == "" {
			return manifest, fmt.Errorf("业务文件所有权无效: %s", path)
		}
	}
	return manifest, nil
}
func readDevelopmentOwnership(root string) (developmentOwnershipManifest, error) {
	raw, err := readRootedFile(root, developmentOwnershipPath)
	if os.IsNotExist(err) {
		return emptyDevelopmentOwnership(), nil
	}
	if err != nil {
		return developmentOwnershipManifest{}, err
	}
	return parseDevelopmentOwnership(raw)
}
func baselineDevelopmentOwnership(ctx context.Context, run model.DevRun) (developmentOwnershipManifest, error) {
	if run.WorkspaceBaseCommit == "" {
		return emptyDevelopmentOwnership(), nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "show", run.WorkspaceBaseCommit+":"+developmentOwnershipPath)
	raw, err := cmd.Output()
	if err != nil {
		if err := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "cat-file", "-e", run.WorkspaceBaseCommit+"^{commit}").Run(); err != nil {
			return developmentOwnershipManifest{}, errors.New("任务源码基线无效")
		}
		return emptyDevelopmentOwnership(), nil
	}
	return parseDevelopmentOwnership(raw)
}
func developmentPathInBaseline(run model.DevRun, path string) bool {
	if run.WorkspaceBaseCommit == "" {
		return false
	}
	return exec.Command("git", "-C", run.WorkspacePath, "cat-file", "-e", run.WorkspaceBaseCommit+":"+path).Run() == nil
}
func validateDevelopmentOwnership(run model.DevRun, manifest developmentOwnershipManifest) error {
	baseline, err := baselineDevelopmentOwnership(context.Background(), run)
	if err != nil {
		return err
	}
	for path, owner := range manifest.Files {
		if developmentPathInBaseline(run, path) {
			if _, ok := baseline.Files[path]; !ok {
				return fmt.Errorf("不能接管已有框架或无关文件: %s", path)
			}
		}
		raw, err := readRootedFile(run.WorkspacePath, path)
		if err != nil {
			return err
		}
		if sha256Hex(string(raw)) != owner.CurrentHash {
			return fmt.Errorf("业务文件所有权哈希已变化: %s", path)
		}
	}
	return nil
}
func saveDevelopmentOwnership(root string, manifest developmentOwnershipManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeRootedFile(root, developmentOwnershipPath, raw)
}

// Only business changes and host-frozen task metadata may enter a promoted baseline.
func developmentPromotionPaths(ctx context.Context, run model.DevRun, manifest developmentOwnershipManifest) ([]string, error) {
	var paths []string
	seen := map[string]bool{}
	for _, args := range [][]string{{"diff", "--name-only", "--no-ext-diff", "-z", run.WorkspaceBaseCommit}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
		raw, err := exec.CommandContext(ctx, "git", append([]string{"-C", run.WorkspacePath}, args...)...).Output()
		if err != nil {
			return nil, err
		}
		for _, path := range strings.Split(string(raw), "\x00") {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if path != developmentOwnershipPath && path != "fde/requirements/"+run.Key+".json" {
				if _, ok := manifest.Files[path]; !ok {
					return nil, fmt.Errorf("候选包含未接管或框架变更: %s", path)
				}
			}
			if _, err := readRootedFile(run.WorkspacePath, path); err != nil {
				return nil, fmt.Errorf("候选不能删除文件: %s", path)
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}
