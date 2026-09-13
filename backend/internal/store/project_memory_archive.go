package store

import (
	"context"
	"time"

	"relay-server/internal/model"
)

// ProjectMemoryArchive is optional so existing test archives and legacy
// integrations remain source-compatible. MySQLArchive implements it in
// production; Memory provides an in-process implementation for tests.
type ProjectMemoryArchive interface {
	UpsertProjectScope(context.Context, model.ProjectScope) error
	GetProjectScope(context.Context, int64, string, string) (*model.ProjectScope, error)
	ListProjectScopes(context.Context, int64, string) ([]model.ProjectScope, error)
	UpsertProjectScopeLocation(context.Context, model.ProjectScopeLocation) error
	UpsertProjectAgentBinding(context.Context, model.ProjectAgentBinding) error
	GetProjectScopeForAgent(context.Context, int64, string, string) (*model.ProjectScope, error)
	ListProjectMemories(context.Context, model.ProjectMemoryFilter) ([]model.ProjectMemory, error)
	GetProjectMemory(context.Context, int64, string, string) (*model.ProjectMemory, error)
	ListProjectMemoryVersions(context.Context, int64, string, string) ([]model.ProjectMemory, error)
	UpsertProjectMemory(context.Context, model.ProjectMemory) error
	EditProjectMemoryVersion(context.Context, int64, string, string, model.ProjectMemory) error
	MarkProjectMemoryDeleted(context.Context, int64, string, string, int64) error
	CreateProjectMemoryJob(context.Context, model.ProjectMemoryJob) error
	ScheduleProjectMemoryJob(context.Context, model.ProjectMemoryJob) (*model.ProjectMemoryJob, bool, error)
	GetProjectMemoryJob(context.Context, int64, string) (*model.ProjectMemoryJob, error)
	ListProjectMemoryJobs(context.Context, int64, string, int) ([]model.ProjectMemoryJob, error)
	ListClaimableProjectMemoryJobs(context.Context, time.Time, int) ([]model.ProjectMemoryJob, error)
	ClaimProjectMemoryJob(context.Context, string, string, time.Time, time.Time, int) (*model.ProjectMemoryJob, error)
	RenewProjectMemoryJob(context.Context, string, string, int64, time.Time) (bool, error)
	UpdateProjectMemoryJob(context.Context, model.ProjectMemoryJob) error
	FinalizeProjectMemoryJob(context.Context, model.ProjectMemoryJob, string) (*model.ProjectMemoryJob, error)
	MarkProjectMemoryJobManualVisible(context.Context, int64, string, string) (bool, error)
	AppendProjectMemoryJobEvent(context.Context, model.ProjectMemoryJobEvent) error
	ListProjectMemoryJobEvents(context.Context, int64, string, int64, bool) ([]model.ProjectMemoryJobEvent, error)
	ReconcileProjectMemories(context.Context, int64, string, int64, []model.ProjectMemoryCandidate, []model.ProjectMemoryInvalidation, *model.ProjectMemoryBriefCandidate) (model.ProjectMemoryReconcileResult, error)
	GetProjectMemoryBrief(context.Context, int64, string) (*model.ProjectMemoryBrief, error)
	UpsertProjectMemoryBrief(context.Context, model.ProjectMemoryBrief) error
	RecordProjectMemoryUsage(context.Context, model.ProjectMemoryUsage) error
	RecordProjectMemoryAudit(context.Context, model.ProjectMemoryAudit) error
	UpdateProjectMemorySourceAvailability(context.Context, int64, string, []model.ProjectMemorySourceAvailability) (int64, error)
	MaintainProjectMemoryRetention(context.Context, int, int, int) (model.ProjectMemoryRetentionResult, error)
	PurgeDeletedProjectMemories(context.Context, time.Time, int) (int64, error)
}
