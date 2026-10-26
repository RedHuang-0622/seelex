package core

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

type sessionForkLineage = applicationsession.ForkLineageApplication
type sessionForkLineageDeps = applicationsession.ForkLineageDeps

func newSessionForkLineage(deps sessionForkLineageDeps) *sessionForkLineage {
	return applicationsession.NewForkLineage(deps)
}

type sessionRestoringShell = applicationsession.RestoringShellApplication
type sessionRestoringShellDeps = applicationsession.RestoringShellDeps

func newSessionRestoringShell(deps sessionRestoringShellDeps) *sessionRestoringShell {
	return applicationsession.NewRestoringShell(deps)
}
