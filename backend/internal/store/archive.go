package store

import "relay-server/internal/model"

type TaskArchive interface {
	UpsertTask(task *model.Task) error
	AppendEvent(event model.Event) error
	ListTasks(filter model.TaskFilter) ([]*model.Task, error)
	UpsertSession(session *model.Session) error
	ListSessions(filter model.SessionFilter) ([]*model.Session, error)
	AuthenticateOperator(username, password string) (*model.Operator, error)
	GetOperatorByKey(operatorKey string) (*model.Operator, error)
	Close() error
}

type noopArchive struct{}

func (noopArchive) UpsertTask(*model.Task) error                      { return nil }
func (noopArchive) AppendEvent(model.Event) error                     { return nil }
func (noopArchive) ListTasks(model.TaskFilter) ([]*model.Task, error) { return nil, nil }
func (noopArchive) UpsertSession(*model.Session) error                { return nil }
func (noopArchive) ListSessions(model.SessionFilter) ([]*model.Session, error) {
	return nil, nil
}
func (noopArchive) AuthenticateOperator(string, string) (*model.Operator, error) {
	return nil, nil
}
func (noopArchive) GetOperatorByKey(string) (*model.Operator, error) { return nil, nil }
func (noopArchive) Close() error                                     { return nil }
