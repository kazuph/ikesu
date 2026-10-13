//go:build darwin

package server

// The existing Darwin kill(-pgid, 0) probe already decides group liveness.
func processGroupHasLiveMembers(int) bool { return true }
