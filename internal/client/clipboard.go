package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"runtime"
	"strings"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/render"
)

type clipboardContent struct {
	Text string `json:"text"`
	PNG  []byte `json:"png"`
}

type clipboardItem struct {
	modalText *string
	data      []byte
	paste     bool
}

type clipboardPaste struct{ held []clipboardItem }

func modalClipboardView(view *render.Input) bool {
	return view != nil && (view.Dialog != nil && view.Dialog.InputEdit || view.Navigator != nil && view.Navigator.Searching || view.Copy != nil && strings.HasSuffix(view.CopyHint, "  enter search  esc cancel"))
}

func readDesktopClipboard(ctx context.Context) (clipboardContent, error) {
	if runtime.GOOS != "darwin" {
		return clipboardContent{}, fmt.Errorf("unsupported: desktop clipboard paste on %s", runtime.GOOS)
	}
	// AppKit converts TIFF-only desktop images to PNG before transport.
	const script = `ObjC.import('AppKit');
const p = $.NSPasteboard.generalPasteboard;
let image = p.dataForType($.NSPasteboardTypePNG);
if (image.isNil()) {
  const tiff = p.dataForType($.NSPasteboardTypeTIFF);
  if (!tiff.isNil()) image = $.NSBitmapImageRep.imageRepWithData(tiff).representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $({}));
}
const text = p.stringForType($.NSPasteboardTypeString);
JSON.stringify({text: text.isNil() ? '' : ObjC.unwrap(text), png: image.isNil() ? null : ObjC.unwrap(image.base64EncodedStringWithOptions(0))});`
	data, err := childenv.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return clipboardContent{}, fmt.Errorf("read desktop clipboard: %w", err)
	}
	var content clipboardContent
	if err := json.Unmarshal(data, &content); err != nil {
		return content, fmt.Errorf("decode desktop clipboard: %w", err)
	}
	return content, nil
}

func uploadClipboardImage(ctx context.Context, profile machine.Profile, png []byte) (string, error) {
	c, err := config.Load()
	if err != nil {
		return "", err
	}
	managed, err := machine.PrepareSSH(c, profile.Target)
	if err != nil {
		return "", err
	}
	defer managed.Close()
	// mktemp creates a private file; no remote server or persistent state is touched.
	const script = `set -eu; f=$(mktemp "${TMPDIR:-/tmp}/gtx-clipboard.XXXXXX"); trap 'rm -f "$f" "$f.png"' EXIT HUP INT TERM; cat > "$f"; mv "$f" "$f.png"; printf '%s' "$f.png"; trap - EXIT HUP INT TERM`
	cmd := childenv.CommandContext(ctx, "ssh", managed.Args(machine.SSHArgs(profile, []string{"sh", "-c", script}))...)
	cmd.Stdin = bytes.NewReader(png)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("copy clipboard image: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	path := string(data)
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n\x00") || !strings.HasSuffix(path, ".png") {
		return "", fmt.Errorf("invalid remote clipboard image path")
	}
	return path, nil
}

func (m *machineClient) pasteClipboard() error {
	return m.startClipboardPaste(m.active)
}

func (m *machineClient) startClipboardPaste(id string) error {
	e := m.endpoints[id]
	if e.connection == nil || e.snapshot == nil {
		return fmt.Errorf("clipboard paste: machine is not connected")
	}
	if e.paste != nil {
		e.paste.held = append(e.paste.held, clipboardItem{paste: true})
		return nil
	}
	read := m.readClipboard
	if read == nil {
		read = readDesktopClipboard
	}
	connection, pane, profile := e.connection, e.snapshot.State.FocusedPane(), e.profile
	textOnly := modalClipboardView(e.snapshot)
	copySearch := e.snapshot.Copy != nil && strings.HasSuffix(e.snapshot.CopyHint, "  enter search  esc cancel")
	op := &clipboardPaste{}
	e.paste = op
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		content, err := read(m.ctx)
		text := content.Text
		if err == nil && len(content.PNG) > 0 && !textOnly {
			var path string
			path, err = uploadClipboardImage(m.ctx, profile, content.PNG)
			text = machine.Quote(path)
		}
		m.post(m.ctx, endpointEvent{formUpdate: func() {
			if e.paste != op {
				return
			}
			e.paste = nil
			if e.connection != connection || e.snapshot == nil || e.snapshot.State.FocusedPane() != pane || textOnly && !modalClipboardView(e.snapshot) || copySearch && (e.snapshot.Copy == nil || !strings.HasSuffix(e.snapshot.CopyHint, "  enter search  esc cancel")) {
				m.notify("clipboard paste cancelled: target pane changed")
				return
			}
			if err != nil {
				m.notify(err.Error())
			} else if text != "" {
				data := eventBytes(input.Event{Kind: input.KindPaste, Text: text})
				var sendErr error
				if textOnly {
					sendErr = m.sendModalPaste(id, text)
				} else {
					sendErr = connection.enqueue(msg{Type: "input", Data: base64.StdEncoding.EncodeToString(data)})
				}
				if sendErr != nil {
					m.notify(sendErr.Error())
				}
			}
			for _, held := range op.held {
				var sendErr error
				if held.modalText != nil {
					sendErr = m.sendModalPaste(id, *held.modalText)
				} else if held.paste {
					sendErr = m.startClipboardPaste(id)
				} else {
					sendErr = m.sendInput(id, held.data)
				}
				if sendErr != nil {
					m.notify(sendErr.Error())
				}
			}
		}})
	}()
	return nil
}

// A clipboard body is data, never a terminal stream. Negotiate the typed
// message so an older server cannot reinterpret its bytes as keys.
func (m *machineClient) sendModalPaste(id, text string) error {
	if text == "" {
		return nil
	}
	e := m.endpoints[id]
	if e.connection == nil {
		return fmt.Errorf("clipboard paste: machine is not connected")
	}
	if !endpoint.Allows(e.connection.writer.available, endpoint.RequiredGeneration("paste"), e.connection.writer.methods, "paste") {
		return fmt.Errorf("paste unsupported")
	}
	if e.paste != nil {
		e.paste.held = append(e.paste.held, clipboardItem{modalText: &text})
		return nil
	}
	return e.connection.enqueue(msg{Type: "paste", Data: base64.StdEncoding.EncodeToString([]byte(text))})
}
