package model

type DevRelease struct {
	Base
	Key          string `json:"id" gorm:"size:64;uniqueIndex"`
	RunID        uint   `json:"runId" gorm:"index"`
	ProjectID    uint   `json:"projectId" gorm:"index"`
	ActorID      uint   `json:"actorId" gorm:"index"`
	SourceHash   string `json:"sourceHash" gorm:"size:64"`
	ArtifactHash string `json:"artifactHash" gorm:"size:64"`
	Status       string `json:"status" gorm:"size:32"`
	PackagePath  string `json:"-" gorm:"size:1024"`
	Manifest     string `json:"-" gorm:"type:text"`
}

func (DevRelease) TableName() string { return "sys_dev_releases" }

type DevDeployment struct {
	Base
	ReleaseID   uint   `json:"releaseId" gorm:"uniqueIndex:idx_release_target"`
	TargetID    string `json:"targetId" gorm:"size:128;uniqueIndex:idx_release_target"`
	Status      string `json:"status" gorm:"size:32"`
	URL         string `json:"url" gorm:"size:255"`
	ContainerID string `json:"-" gorm:"size:128"`
	Error       string `json:"error,omitempty" gorm:"type:text"`
}

func (DevDeployment) TableName() string { return "sys_dev_deployments" }
