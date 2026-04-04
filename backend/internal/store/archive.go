package store

import "relay-server/internal/model"

type TaskArchive interface {
	UpsertTask(task *model.Task) error
	AppendEvent(event model.Event) error
	ListTasks(filter model.TaskFilter) ([]*model.Task, error)
	Close() error
}

type noopArchive struct{}

func (noopArchive) UpsertTask(*model.Task) error                      { return nil }
func (noopArchive) AppendEvent(model.Event) error                     { return nil }
func (noopArchive) ListTasks(model.TaskFilter) ([]*model.Task, error) { return nil, nil }
func (noopArchive) Close() error                                      { return nil }
