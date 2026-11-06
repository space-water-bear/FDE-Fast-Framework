package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

var allowedExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".txt": true, ".zip": true,
}

// SaveUpload 保存上传文件并登记 sys_files
func SaveUpload(fh *multipart.FileHeader, uploaderID uint, uploader, dir string, maxSizeMB int) (*model.File, error) {
	if maxSizeMB > 0 && fh.Size > int64(maxSizeMB)*1024*1024 {
		return nil, errors.New("文件大小超过限制")
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !allowedExts[ext] {
		return nil, errors.New("不支持的文件类型: " + ext)
	}
	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	monthDir := time.Now().Format("200601")
	sub := filepath.Join(dir, monthDir)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return nil, err
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	name := hex.EncodeToString(buf) + ext
	dst, err := os.Create(filepath.Join(sub, name))
	if err != nil {
		return nil, err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return nil, err
	}

	file := &model.File{
		Name:       fh.Filename,
		URL:        "/uploads/" + monthDir + "/" + name,
		Ext:        ext,
		Size:       fh.Size,
		UploaderID: uploaderID,
		Uploader:   uploader,
	}
	if err := global.DB.Create(file).Error; err != nil {
		return nil, err
	}
	return file, nil
}

func ListFiles(page, pageSize int, keyword string) ([]model.File, int64, error) {
	var list []model.File
	var total int64
	q := global.DB.Model(&model.File{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR uploader LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// DeleteFile 删除记录与磁盘文件
func DeleteFile(id uint, dir string) error {
	var f model.File
	if err := global.DB.First(&f, id).Error; err != nil {
		return err
	}
	rel := strings.TrimPrefix(f.URL, "/uploads/")
	if rel != "" {
		_ = os.Remove(filepath.Join(dir, rel))
	}
	return global.DB.Delete(&f).Error
}
