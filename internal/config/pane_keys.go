package config

func (c Config) RenamePaneKeys() map[rune]bool {
	return c.keyList(c.Keys.RenamePane, []string{"prefix+shift+p"})
}
func (c Config) EditScrollbackKeys() map[rune]bool {
	return c.keyList(c.Keys.EditScrollback, []string{"prefix+e"})
}
func (c Config) FocusHistoryBackKeys() map[rune]bool {
	return c.keyList(c.Keys.FocusHistoryBack, nil)
}
func (c Config) FocusHistoryForwardKeys() map[rune]bool {
	return c.keyList(c.Keys.FocusHistoryForward, nil)
}
