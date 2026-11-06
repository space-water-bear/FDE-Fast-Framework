package model

type RuntimeInstallation struct {
	Base
	ReleaseID    string `json:"releaseId" gorm:"size:64;uniqueIndex"`
	SourceHash   string `json:"sourceHash" gorm:"size:64"`
	ManifestHash string `json:"manifestHash" gorm:"size:64"`
	Complete     bool   `json:"complete"`
}

func (RuntimeInstallation) TableName() string { return "sys_runtime_installations" }
