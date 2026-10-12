package guard

type Transfer struct {
	Root, Pane, Bin, MarkerPath, Executable, Active string
	Config                                          Config
	Meta                                            Marker
	Injected, Prompt                                bool
	Mode, Unsupported, Issue                        string
}

func (a *Assets) Transfer() Transfer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Transfer{a.Root, a.Pane, a.Bin, a.MarkerPath, a.Executable, a.active, a.Config, a.meta, a.injected, a.prompt, a.mode, a.unsupported, a.issue}
}
func Import(t Transfer) *Assets {
	return &Assets{Root: t.Root, Pane: t.Pane, Bin: t.Bin, MarkerPath: t.MarkerPath, Executable: t.Executable, active: t.Active, Config: t.Config, meta: t.Meta, injected: t.Injected, prompt: t.Prompt, mode: t.Mode, unsupported: t.Unsupported, issue: t.Issue}
}
