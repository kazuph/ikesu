package vt

import (
	"bytes"
	"encoding/gob"
	"testing"
)

func TestOldHandoffWithoutKeyboardFieldsRetainsExplicitDepth(t *testing.T) {
	a, b := NewEmulator(8, 2), NewEmulator(8, 2)
	defer a.Close()
	defer b.Close()
	data, err := a.MarshalHandoff()
	if err != nil {
		t.Fatal(err)
	}
	var current terminalTransfer
	if err = gob.NewDecoder(bytes.NewReader(data)).Decode(&current); err != nil {
		t.Fatal(err)
	}
	// This wire type has only fields available in an old server; no new state.
	legacy := struct {
		Screens  [2]screenTransfer
		Keyboard int
	}{current.Screens, 2}
	var wire bytes.Buffer
	if err = gob.NewEncoder(&wire).Encode(legacy); err != nil {
		t.Fatal(err)
	}
	if err = b.UnmarshalHandoff(wire.Bytes()); err != nil {
		t.Fatal(err)
	}
	if !b.KeyboardLegacy() || !b.KbdExplicit() {
		t.Fatal("old explicit state was invented or lost")
	}
	b.WriteString("\x1b[<u")
	if !b.KbdExplicit() {
		t.Fatal("old depth lost")
	}
	b.WriteString("\x1b[<u")
	if b.KbdExplicit() || b.KeyboardLegacy() {
		t.Fatal("old stack did not clear")
	}
}

func TestKeyboardFlagsStackAndModifyOtherKeys(t *testing.T) {
	e := NewEmulator(8, 2)
	defer e.Close()
	for _, step := range []struct {
		seq               string
		flags, depth, mok int
	}{
		{"\x1b[>3u", 3, 1, 0}, {"\x1b[>8u\x1b[>4;1m", 8, 2, 1},
		{"\x1b[=16;2u", 24, 2, 1}, {"\x1b[=8;3u", 16, 2, 1},
		{"\x1b[<u", 3, 1, 1}, {"\x1b[<2u\x1b[>4;0m", 0, 0, 0},
	} {
		e.WriteString(step.seq)
		flags, mok := e.KeyboardModes()
		if flags != step.flags || mok != step.mok || len(e.keyboard.Stack) != step.depth {
			t.Fatalf("%q: flags=%d mok=%d depth=%d", step.seq, flags, mok, len(e.keyboard.Stack))
		}
	}
}

func TestHandoffPreservesKeyboardValuesAndStack(t *testing.T) {
	a, b := NewEmulator(8, 2), NewEmulator(8, 2)
	defer a.Close()
	defer b.Close()
	a.WriteString("\x1b[>3u\x1b[>31u\x1b[>4;2m")
	data, err := a.MarshalHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.UnmarshalHandoff(data); err != nil {
		t.Fatal(err)
	}
	for _, term := range []*Emulator{a, b} {
		if flags, mok := term.KeyboardModes(); flags != 31 || mok != 2 {
			t.Fatalf("handoff: %d %d", flags, mok)
		}
		term.WriteString("\x1b[<u")
		if flags, mok := term.KeyboardModes(); flags != 3 || mok != 2 {
			t.Fatalf("stack: %d %d", flags, mok)
		}
		term.WriteString("\x1b[<u")
		if term.KbdExplicit() {
			t.Fatal("stack did not empty")
		}
	}
}
