// Package wire is the binary protocol two TUIOS peers speak on a socket: the
// message-type numbering, the frame layout, the gob payload codec and the
// bounds that keep a frame from hurting the process that reads it.
//
// It is deliberately small. Nothing here knows what a session, a window, a
// pane or a client is: a message is a type, some bytes and an optional request
// id, and the payloads are the caller's own types, registered with encoding/gob
// by the caller. The payload DTOs, their gob registration and the state they
// carry stay in the session package, which aliases what it re-exports here, so
// the two agree on every number and byte while a program that wants only the
// framing does not have to link a session to get it.
//
// What it does carry is the whole security envelope of the protocol, because
// that belongs to the bytes rather than to the reader: the 16 MiB frame
// ceiling and the smaller per-type limits, the depth scan every gob payload
// passes before encoding/gob sees it, request ids that travel in the frame
// header, and the FIFO memory budget a reader charges a large frame to.
package wire

import "fmt"

// MessageType identifies the type of protocol message.
type MessageType uint8

const (
	// Client -> Server messages
	MsgHello            MessageType = iota + 1 // Initial handshake with client info
	MsgAttach                                  // Attach to a session
	MsgDetach                                  // Detach from session (session persists)
	MsgNew                                     // Create a new session
	MsgList                                    // List available sessions
	MsgKill                                    // Kill/terminate a session
	MsgInput                                   // Keyboard/mouse input bytes
	MsgResize                                  // Terminal resize event
	MsgPing                                    // Asks for a MsgPong; a client sends it after a large input as a barrier (paste_retry.go)
	MsgCreatePTY                               // Create new PTY in session
	MsgClosePTY                                // Close a PTY
	MsgListPTYs                                // Reserved: list PTYs (no sender)
	MsgFocusPTY                                // Reserved: focus PTY (never dispatched)
	MsgGetState                                // Reserved: get session state (no sender)
	MsgUpdateState                             // Update session state
	MsgSubscribePTY                            // Subscribe to PTY output
	MsgUnsubscribePTY                          // Unsubscribe from PTY output
	MsgGetTerminalState                        // Get terminal state for a PTY
	MsgExecuteCommand                          // Execute a tape command (routed to TUI)
	MsgSendKeys                                // Reserved: send-keys moved to the JSON verb plane
	MsgSetConfig                               // Reserved: set-config moved to the JSON verb plane
	MsgCapturePane                             // Reserved: capture-pane moved to the JSON verb plane

	// Server -> Client messages
	MsgWelcome       // Response to Hello with server info
	MsgAttached      // Successfully attached to session
	MsgDetached      // Confirm detach
	MsgSessionList   // List of sessions
	MsgOutput        // Reserved: never sent
	MsgError         // Error message
	MsgPong          // Response to MsgPing
	MsgSessionEnded  // Session terminated
	MsgWindowChanged // Reserved: never sent
	MsgPTYList       // Reserved: response to MsgListPTYs
	MsgPTYCreated    // New PTY created
	MsgPTYClosed     // PTY closed
	MsgPTYOutput     // Output from a specific PTY
	MsgStateData     // Reserved: response to MsgGetState
	MsgTerminalState // Terminal state for a PTY (screen + scrollback)
	MsgCommandResult // Result of a remote command execution
	MsgRemoteCommand // Remote command from daemon to TUI client for execution
	MsgGetLogs       // Request to retrieve daemon logs
	MsgLogsData      // Response with log entries
	MsgQueryWindows  // Reserved: list-windows moved to the JSON verb plane
	MsgWindowList    // Reserved: response to MsgQueryWindows
	MsgQuerySession  // Reserved: session-info moved to the JSON verb plane
	MsgSessionInfo   // Reserved: response to MsgQuerySession

	// Multi-client support messages
	MsgStateSync     // Broadcast state update to all clients in session
	MsgClientJoined  // Notification that another client joined the session
	MsgClientLeft    // Notification that another client left the session
	MsgSessionResize // Session effective size changed (min of all clients)
	MsgForceRefresh  // Reserved: refreshes ride the client event channel now
	// MsgRequestFullSync is declared and never sent. No daemon has ever had a
	// handler for it, and the case it was meant for, a client that missed a
	// state sync, is handled where the sync is queued instead: the queue keeps
	// the newest snapshot rather than the first (see app.OS.QueueStateSync), so
	// there is nothing to re-request. It stays in place because the iota order
	// is the wire format and removing it would move every value below it.
	MsgRequestFullSync

	// Appended after all existing types to keep every value above stable for
	// older clients that share this iota order.
	MsgResurrect  // Restore a saved session on demand (cold-start restore)
	MsgPTYResized // A pane's emulator changed size at this point in its output stream
	// MsgAgentMail is one message stored in the session's agent ring, pushed to
	// every attached TUI client at the moment it is stored. It exists because an
	// attached client is the one reader that cannot poll: an idle client must do
	// no work, and a message an agent leaves has to reach the person anyway.
	MsgAgentMail
	// MsgHostsChanged tells every attached TUI client that the daemon's [hosts]
	// table changed: a host was added, removed or redialed. It is pushed for
	// the same reason MsgAgentMail is: a client whose daemon has no hosts stops
	// polling for them, so the only way it can learn about the first host is
	// to be told. It carries no listing; the client polls once on receipt.
	MsgHostsChanged
	// MsgReadDir asks the daemon to list a directory, and MsgDirListing is the
	// answer. Appended, so every value above stays where an older peer expects
	// it; see the note on ProtocolVersion.
	//
	// The rail's file section used to read the filesystem itself. That was the
	// same machine as the pane for as long as a pane could only be on this
	// machine, and federation ended that: a client attached to a session on
	// another host was listing its own disk and reporting that the pane's
	// directory did not exist. The daemon that owns the pane owns the
	// filesystem the pane is on, so the listing is asked for rather than taken.
	//
	// Over a link it keeps to the read rules file-list keeps to
	// (verb_files_confine.go). A machine that may list panes is not thereby
	// one that may list ~/.ssh or /proc.
	MsgReadDir
	MsgDirListing
	// MsgClientFocus reports whether the host terminal of a TUI client has
	// focus, from the terminal's focus events (DECSET 1004). A client sends it
	// only to a daemon whose welcome set ClientFocus, so an older daemon never
	// receives a type it would refuse. See client_focus.go.
	MsgClientFocus
	// MsgLayoutTree carries one workspace's BSP tree as a client just shaped
	// it, as an op the daemon applies and versions. A client sends it only to
	// a daemon whose welcome set LayoutTreeOps. See layout_tree.go.
	MsgLayoutTree
	// MsgTypeAtPrompt asks the daemon to write text to a pane only if the
	// pane's shell holds the terminal, and MsgPromptTyped says whether it did.
	// The daemon owns the PTY, so it is the one side that can see a daemon
	// pane at its prompt, and checking and writing in one step leaves no gap
	// for a program to start in between. A client sends it only to a daemon
	// whose welcome set TypeAtPrompt. See type_at_prompt.go.
	MsgTypeAtPrompt
	MsgPromptTyped
	// MsgClientGraphics updates the graphics a TUI client's terminal draws,
	// after the hello: an SSH client's terminal is asked for DA1 only once
	// the session runs. A client sends it only to a daemon whose welcome set
	// ClientGraphics. See client_graphics.go.
	MsgClientGraphics
	// MsgMasterLayout carries one workspace's master-stack shape (the master
	// position and count) as an op the daemon applies and versions. A client
	// sends it only to a daemon whose welcome set MasterLayoutOps. See
	// master_layout.go.
	MsgMasterLayout
	// MsgClientActivity says the person at a TUI client gave input: a key, a
	// paste, a click, a drag or a wheel turn. The window_size latest policy
	// reads it to pick the client whose size the session takes. A client
	// sends it only to a daemon whose welcome set WindowSize. See
	// window_size.go.
	MsgClientActivity
	// MsgWatchDir asks the daemon to say when the names in one folder change,
	// and MsgDirChanged is it saying so. One watch per connection: a new
	// MsgWatchDir replaces the last, and an empty folder ends it. The rail's
	// files section asks for it when it lists a folder this client cannot
	// watch itself, because the folder is on another machine. A client sends
	// it only to a daemon whose welcome set DirWatch. See daemon_dirwatch.go.
	MsgWatchDir
	MsgDirChanged
	// MsgSidebarVisibility says whether the session shows the sidebar rail,
	// as an op the daemon applies and versions. A client sends it only to a
	// daemon whose welcome set SidebarOps. See sidebar_visibility.go.
	MsgSidebarVisibility
	// MsgSessionUsed says the person used the client's session: a key, a
	// paste, a click, a drag or a wheel turn from the terminal the client
	// runs in. Keys a script sends through the client (send-keys, a tape)
	// are not reported. A bare attach picks the session used last. A client
	// sends it only to a daemon whose welcome set SessionUsed. See
	// session_used.go.
	MsgSessionUsed
	// MsgSessionsChanged tells a client that the session listing changed: a
	// session, a window, a title, a folder or an agent state it shows. It has
	// no payload. The client asks for the listing again. The daemon sends it
	// only to a client whose hello set ListingPush. See
	// daemon_listing_push.go.
	MsgSessionsChanged
	// MsgPaneMark tells a client that a pane it does not stream rang the
	// bell or printed, once until the client subscribes to the pane or drops
	// it. The daemon sends it only to a client whose hello set PaneMarks.
	// See pane_marks.go.
	MsgPaneMark
)

// NumMessageTypes is the number of message types the protocol defines, which
// is also the value of the last one: they are numbered from 1 with no gaps.
// A type past it is one no build has ever sent.
const NumMessageTypes = int(MsgPaneMark)

// MessageTypeName returns a human-readable name for a message type.
func MessageTypeName(t MessageType) string {
	if int(t) < len(messageTypeNames) {
		if name := messageTypeNames[t]; name != "" {
			return name
		}
	}
	return fmt.Sprintf("Unknown(%d)", t)
}
