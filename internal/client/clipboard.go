package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
)

type clipboardContent struct {
	Text string `json:"text"`
	PNG  []byte `json:"png"`
}

type clipboardItem struct {
	data  []byte
	paste bool
}

type clipboardPaste struct{ held []clipboardItem }

func readDesktopClipboard(ctx context.Context) (clipboardContent, error) {
	if runtime.GOOS != "darwin" {
		return clipboardContent{}, fmt.Errorf("unsupported: desktop clipboard paste on %s", runtime.GOOS)
	}
	// AppKit converts TIFF-only desktop images to PNG before transport.
	const script = `ObjC.import('AppKit');
const p = $.NSPasteboard.generalPasteboard;
let image = p.dataForType($.NSPasteboardTypePNG);
if (!image) {
  const tiff = p.dataForType($.NSPasteboardTypeTIFF);
  if (tiff) image = $.NSBitmapImageRep.imageRepWithData(tiff).representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $({}));
}
const text = p.stringForType($.NSPasteboardTypeString);
JSON.stringify({text: text ? ObjC.unwrap(text) : '', png: image ? ObjC.unwrap(image.base64EncodedStringWithOptions(0)) : null});`
	data, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
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
	cmd := exec.CommandContext(ctx, "ssh", managed.Args(machine.SSHArgs(profile, []string{"sh", "-c", script}))...)
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
	op := &clipboardPaste{}
	e.paste = op
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		content, err := read(m.ctx)
		text := content.Text
		if err == nil && len(content.PNG) > 0 {
			var path string
			path, err = uploadClipboardImage(m.ctx, profile, content.PNG)
			text = machine.Quote(path)
		}
		m.post(m.ctx, endpointEvent{formUpdate: func() {
			if e.paste != op {
				return
			}
			e.paste = nil
			if e.connection != connection || e.snapshot == nil || e.snapshot.State.FocusedPane() != pane {
				m.notify("clipboard paste cancelled: target pane changed")
				return
			}
			if err != nil {
				m.notify(err.Error())
			} else {
				data := eventBytes(input.Event{Kind: input.KindPaste, Text: text})
				if sendErr := connection.enqueue(msg{Type: "input", Data: base64.StdEncoding.EncodeToString(data)}); sendErr != nil {
					m.notify(sendErr.Error())
				}
			}
			for _, held := range op.held {
				var sendErr error
				if held.paste {
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
