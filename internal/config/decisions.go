package config

func (c Config) ScreenPrompts() bool {
	return c.Decisions.ScreenPrompts == nil || *c.Decisions.ScreenPrompts
}
