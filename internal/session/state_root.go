package session

import "path/filepath"

// stateRoot is the directory one daemon keeps every file it saves under. It is
// the resurrection directory itself: the folder the session state files sit
// in, and the parent of the per-feature folders beside them (pane history, the
// Inbox, the outbox, review notes, the applied-grants record, push
// registrations and the transfer journal).
//
// The empty value is the upstream layout: the shared folder under the user's
// state home, which every unscoped daemon of a machine writes to. A daemon that
// is given a directory of its own (DaemonConfig.StateDir) resolves every one of
// those paths inside it instead, so two daemons in one process, or two daemons
// of one user, never write to each other's sessions, history or Inbox.
//
// The value is a plain string: it is copied into the Manager and into every
// Session the Manager makes, and it never changes after the daemon starts, so
// nothing has to be locked.
type stateRoot string

// dir is the directory the daemon's state lives in. An empty root is the
// upstream default, which honours the test override in getResurrectionDir, so
// a test that redirects the state home keeps working unchanged.
func (r stateRoot) dir() string {
	if r != "" {
		return string(r)
	}
	return getResurrectionDir()
}

// path joins elements under the root.
func (r stateRoot) path(elem ...string) string {
	return filepath.Join(append([]string{r.dir()}, elem...)...)
}

// sessionPath is the state file of one session. The caller is responsible for
// having validated the name (see ValidateSessionName); this is a plain join and
// cannot defend itself.
func (r stateRoot) sessionPath(sessionName string) string {
	return r.path(sessionName + ".json")
}

// archiveDir holds state files that would not load, so they can be inspected
// instead of deleted.
func (r stateRoot) archiveDir() string {
	return r.path("archive")
}

// attentionPath is where the Inbox is saved: in a directory of its own so the
// session listing never reads it as a session.
func (r stateRoot) attentionPath() string {
	return r.path("attention", "items.json")
}

// outboxPath is where mail waiting for another machine is saved.
func (r stateRoot) outboxPath() string {
	return r.path("outbox", "mail.json")
}

// reviewNotesPath is where the review-note store is saved.
func (r stateRoot) reviewNotesPath() string {
	return r.path("review", "notes.json")
}

// appliedGrantsPath is where the record of the permission default in force at
// the last run lives.
func (r stateRoot) appliedGrantsPath() string {
	return r.path("grants", "applied.json")
}

// pushDir is where the registered phones and the VAPID key are kept.
func (r stateRoot) pushDir() string {
	return r.path("push")
}

// transferJournalDir is the folder that holds the transfer journal.
func (r stateRoot) transferJournalDir() string {
	return r.path("transfers")
}

// historyRoot is the folder one session's saved pane history sits under.
func (r stateRoot) historyRoot() string {
	return r.path(historyDirName)
}

// historyDir is where one session's saved pane history lives.
func (r stateRoot) historyDir(sessionName string) string {
	return filepath.Join(r.historyRoot(), sessionName)
}

// historyPath is the saved history file of one pane.
func (r stateRoot) historyPath(sessionName, windowID string) string {
	return filepath.Join(r.historyDir(sessionName), windowID+historyExt)
}
