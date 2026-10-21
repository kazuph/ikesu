package api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/envcompat"
)

// DefaultSession is used when --session is not given.
const DefaultSession = "default"

// Session directory layout under StateDir:
//
//	gtx.sock         API socket (newline-delimited JSON)
//	gtx-client.sock  attached display client
//	gtx.lock         held while a server is alive
//	server.log       server stdout/stderr

// StateDir is the gtx state root ($GTX_STATE_DIR or ~/.local/state/gtx).
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), BinaryNamespace()+"-state")
	}
	return stateRoot(BinaryNamespace(), os.Getenv, home)
}

// ValidSessionName reports whether name is safe to use as a directory.
func ValidSessionName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

// SessionDir returns the directory for a named session.
func SessionDir(name string) string {
	return filepath.Join(StateDir(), name)
}

// APISocketPath is the request socket inside a session directory.
func APISocketPath(sessionDir string) string {
	return filepath.Join(sessionDir, "gtx.sock")
}

// ClientSocketPath is the attach socket inside a session directory.
func ClientSocketPath(sessionDir string) string {
	return filepath.Join(sessionDir, "gtx-client.sock")
}

// LockPath is the duplicate-server lock inside a session directory.
// The running server holds it for its lifetime.
func LockPath(sessionDir string) string {
	return filepath.Join(sessionDir, "gtx.lock")
}

// SpawnLockPath is the spawn-coordination lock inside a session
// directory. A client holds it only while it forks the server; keeping it
// separate from LockPath lets the freshly spawned server acquire the
// liveness lock immediately, before the spawning client releases.
func SpawnLockPath(sessionDir string) string {
	return filepath.Join(sessionDir, "gtx-spawn.lock")
}

// LogPath is the server log inside a session directory.
func LogPath(sessionDir string) string {
	return filepath.Join(sessionDir, "server.log")
}

// APISocket returns the socket a CLI should dial. An explicit session name
// always wins; otherwise $GTX_SOCKET_PATH (set inside gtx panes) targets the
// owning session, and as a last resort the default session is used.
func APISocket(session string, explicit bool, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if !explicit {
		if path := envcompat.Lookup(getenv, "GTX_SOCKET_PATH"); path != "" && inheritedSocket(BinaryNamespace(), path, envcompat.Lookup(getenv, "GTX_SOCKET_PATH_EXPLICIT") == "1") {
			return path
		}
	}
	if session == "" {
		session = DefaultSession
	}
	home, err := os.UserHomeDir()
	root := stateRoot(BinaryNamespace(), getenv, home)
	if err != nil {
		root = filepath.Join(os.TempDir(), BinaryNamespace()+"-state")
	}
	return APISocketPath(filepath.Join(root, session))
}
