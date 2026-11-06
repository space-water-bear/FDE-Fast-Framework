package model

// File 上传文件记录
type File struct {
	Base
	Name       string `json:"name" gorm:"size:255"`
	URL        string `json:"url" gorm:"size:255"`
	Ext        string `json:"ext" gorm:"size:16"`
	Size       int64  `json:"size"`
	UploaderID uint   `json:"uploaderId" gorm:"index"`
	Uploader   string `json:"uploader" gorm:"size:64"`
}

func (File) TableName() string { return "sys_files" }
