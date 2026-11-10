package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	"github.com/gin-gonic/gin"
)

func developmentReleaseDTO(release model.DevRelease) gin.H {
	var manifest any
	_ = json.Unmarshal([]byte(release.Manifest), &manifest)
	return gin.H{"id": release.Key, "status": release.Status, "sourceHash": release.SourceHash, "artifactHash": release.ArtifactHash, "manifest": manifest, "downloadUrl": "/api/dev/releases/" + release.Key + "/download"}
}

func CreateDevelopmentRelease(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	release, err := service.CreateDevelopmentRelease(c.Request.Context(), scope, c.Param("key"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, developmentReleaseDTO(*release))
}
func GetDevelopmentRelease(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	release, err := service.VerifiedDevelopmentRelease(scope, c.Param("releaseId"))
	if err != nil {
		utils.Forbidden(c, err.Error())
		return
	}
	data := developmentReleaseDTO(*release)
	var deployments []model.DevDeployment
	_ = global.DB.Where("release_id = ?", release.ID).Find(&deployments).Error
	data["deployments"] = deployments
	utils.OK(c, data)
}
func ListDevelopmentTargets(c *gin.Context) {
	if _, ok := engineeringRequestScope(c); !ok {
		return
	}
	utils.OK(c, gin.H{"list": []gin.H{{"id": "local-linux", "name": "本机隔离 Linux 首次安装测试环境（" + global.Config.Development.TargetArch + "）", "kind": "docker-first-install"}}})
}
func PreviewDevelopmentRelease(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	result, err := service.DeployDevelopmentRelease(c.Request.Context(), scope, c.Param("releaseId"), "", false, true)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, result)
}
func DeployDevelopmentRelease(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in struct {
		TargetID string `json:"targetId"`
		Approve  bool   `json:"approve"`
	}
	if c.ShouldBindJSON(&in) != nil {
		utils.Fail(c, "参数错误")
		return
	}
	result, err := service.DeployDevelopmentRelease(c.Request.Context(), scope, c.Param("releaseId"), in.TargetID, in.Approve, false)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, result)
}

func DownloadDevelopmentRelease(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	release, err := service.VerifiedDevelopmentRelease(scope, c.Param("releaseId"))
	if err != nil {
		utils.Forbidden(c, err.Error())
		return
	}
	file, err := os.CreateTemp("", "fde-release-*.zip")
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	name := file.Name()
	defer os.Remove(name)
	writer := zip.NewWriter(file)
	err = filepath.WalkDir(release.PackagePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(release.PackagePath, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		out, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := writer.Close()
	fileErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = fileErr
	}
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	c.FileAttachment(name, "fde-"+release.Key+".zip")
}
