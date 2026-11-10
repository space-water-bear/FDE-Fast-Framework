package model

import "time"

const DefaultProjectID uint = 1

// DevProject keeps development metadata and source roots separate per customer.
type DevProject struct {
	Base
	Key           string `json:"key" gorm:"size:48;uniqueIndex"`
	Name          string `json:"name" gorm:"size:128"`
	OwnerID       uint   `json:"ownerId" gorm:"index"`
	SourceCommit  string `json:"sourceCommit" gorm:"size:64"`
	WorkspacePath string `json:"-" gorm:"size:1024"`
}

func (DevProject) TableName() string { return "sys_dev_projects" }

// DevRun is durable even when the requesting browser disconnects.
type DevRun struct {
	Base
	Key                 string     `json:"key" gorm:"size:64;uniqueIndex"`
	ProjectID           uint       `json:"projectId" gorm:"index"`
	ActorID             uint       `json:"actorId" gorm:"index"`
	AgentID             uint       `json:"agentId" gorm:"index"`
	WorkspacePath       string     `json:"-" gorm:"size:1024"`
	WorkspaceBaseCommit string     `json:"workspaceBaseCommit" gorm:"size:64"`
	Attempts            int        `json:"attempts" gorm:"default:0"`
	CancelRequested     bool       `json:"cancelRequested" gorm:"default:false"`
	Diff                string     `json:"diff" gorm:"type:text"`
	SourceHash          string     `json:"sourceHash" gorm:"size:128"`
	CheckResult         string     `json:"checkResult" gorm:"type:text"`
	Result              string     `json:"result" gorm:"type:text"`
	StartedAt           *time.Time `json:"startedAt,omitempty"`
	FinishedAt          *time.Time `json:"finishedAt,omitempty"`
	Requirement         string     `json:"requirement" gorm:"type:text"`
	BaseCommit          string     `json:"baseCommit" gorm:"size:64"`
	State               string     `json:"state" gorm:"size:32;index"`
	Phase               string     `json:"phase" gorm:"size:32"`
	Error               string     `json:"error,omitempty" gorm:"type:text"`
}

func (DevRun) TableName() string { return "sys_dev_runs" }

type DevRunEvent struct {
	Base
	RunID   uint   `json:"runId" gorm:"index"`
	Kind    string `json:"kind" gorm:"size:64"`
	Message string `json:"message" gorm:"type:text"`
}

func (DevRunEvent) TableName() string { return "sys_dev_run_events" }
