package assembly

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
)

type sessionColdRead = applicationsessioncold.ReadApplication
type sessionColdReadDeps = applicationsessioncold.ReadDeps
type sessionColdReadResult = applicationsessioncold.ReadResult

func newSessionColdRead(deps sessionColdReadDeps) *sessionColdRead {
	return applicationsessioncold.NewRead(deps)
}

type sessionColdHistory = applicationsessioncold.HistoryApplication
type sessionColdHistoryDeps = applicationsessioncold.HistoryDeps
type sessionColdHistoryResult = applicationsessioncold.HistoryResult

func newSessionColdHistory(deps sessionColdHistoryDeps) *sessionColdHistory {
	return applicationsessioncold.NewHistory(deps)
}

type sessionColdRestore = applicationsessioncold.RestoreApplication
type sessionColdRestoreDeps = applicationsessioncold.RestoreDeps

func newSessionColdRestore(deps sessionColdRestoreDeps) *sessionColdRestore {
	return applicationsessioncold.NewRestore(deps)
}

type sessionColdWorkspace = applicationsessioncold.WorkspaceApplication
type sessionColdWorkspaceDeps = applicationsessioncold.WorkspaceDeps

func newSessionColdWorkspace(deps sessionColdWorkspaceDeps) *sessionColdWorkspace {
	return applicationsessioncold.NewWorkspace(deps)
}

type sessionColdActivation = applicationsessioncold.ActivationApplication
type sessionColdActivationDeps = applicationsessioncold.ActivationDeps
type sessionColdActivationResult = applicationsessioncold.ActivationResult

func newSessionColdActivation(deps sessionColdActivationDeps) *sessionColdActivation {
	return applicationsessioncold.NewActivation(deps)
}

type sessionColdTask = applicationsessioncold.TaskApplication
type sessionColdTaskDeps = applicationsessioncold.TaskDeps

func newSessionColdTask(deps sessionColdTaskDeps) *sessionColdTask {
	return applicationsessioncold.NewTask(deps)
}

type sessionColdView = applicationsessioncold.ViewApplication
type sessionColdViewDeps = applicationsessioncold.ViewDeps

func newSessionColdView(deps sessionColdViewDeps) *sessionColdView {
	return applicationsessioncold.NewView(deps)
}

type sessionColdCommit = applicationsessioncold.CommitApplication
type sessionColdCommitDeps = applicationsessioncold.CommitDeps
type sessionColdCommitResult = applicationsessioncold.CommitResult

func newSessionColdCommit(deps sessionColdCommitDeps) *sessionColdCommit {
	return applicationsessioncold.NewCommit(deps)
}

type sessionColdPublish = applicationsessioncold.PublishApplication
type sessionColdPublishDeps = applicationsessioncold.PublishDeps

func newSessionColdPublish(deps sessionColdPublishDeps) *sessionColdPublish {
	return applicationsessioncold.NewPublish(deps)
}
