package client

import "golang.org/x/term"

type attachTerminal struct{ original *term.State }

func (t *attachTerminal) enter(fd int) error {
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	if t.original == nil {
		t.original = old
	}
	return nil
}

func (t *attachTerminal) restore(fd int) error { return term.Restore(fd, t.original) }
