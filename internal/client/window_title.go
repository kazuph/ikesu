package client

import (
	"io"

	"github.com/kazuph/goterrace/internal/render"
)

type windowTitleState struct {
	shared  render.WindowTitleState
	session string
	groups  []render.MachineGroup
}

func (title *windowTitleState) painted(out io.Writer, frame render.Input) error {
	return title.shared.Painted(out, frame, title.session, title.groups)
}
