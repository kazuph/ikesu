package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kazuph/ikesu/internal/plugin"
)

func writePluginInstallPreview(out io.Writer, preview plugin.InstallPreview, source string) {
	m := preview.Manifest
	fmt.Fprintf(out, "Plugin install preview:\n  id: %s\n  name: %s\n  version: %s\n  source: %s\n  commit: %s\n  actions: %d\n  startup commands: %d\n  events: %d\n  panes: %d\n  link handlers: %d\n  build commands: %d\n", m.ID, m.Name, m.Version, source, preview.Source.ResolvedCommit, len(m.Actions), len(m.Startup), len(m.Events), len(m.Panes), len(m.LinkHandlers), len(m.Build))
	for _, b := range m.Build {
		fmt.Fprintf(out, "    build: %s\n", strings.Join(b.Command, " "))
	}
	for _, a := range m.Actions {
		fmt.Fprintf(out, "    action %s: %s\n", a.ID, strings.Join(a.Command, " "))
	}
	for _, e := range m.Events {
		fmt.Fprintf(out, "    event %s: %s\n", e.On, strings.Join(e.Command, " "))
	}
	for _, p := range m.Panes {
		fmt.Fprintf(out, "    pane %s: %s\n", p.ID, strings.Join(p.Command, " "))
	}
}
