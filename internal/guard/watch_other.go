//go:build !darwin

package guard

func watchAssets(*Assets, []string, func()) bool { return false }
func unwatchAssets(*Assets)                      {}
