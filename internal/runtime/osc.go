package runtime

import "bytes"

// oscTap is a byte-level tracker observing the PTY output stream for OSC
// sequences: command 0/2 payloads become the OSC title and command 9 the OSC
// progress string. It never mutates the stream.
type oscTap struct {
	state oscState
	body  []byte
}

type oscState int

const (
	oscGround    oscState = iota
	oscEsc                // saw ESC, next byte decides
	oscBody               // inside OSC payload
	oscEscBody            // saw ESC inside payload (looking for ST)
	oscIgnore             // inside DCS/SOS/PM/APC payload
	oscEscIgnore          // saw ESC inside an ignored string
)

const oscMaxBody = 4096
const oscMaxChars = 512

// observe feeds output bytes through the tracker. Completed OSC sequences are
// returned as (command, payload) pairs split at the first ';'.
func (t *oscTap) observe(b []byte) [][2]string {
	var out [][2]string
	for _, c := range b {
		switch t.state {
		case oscGround:
			if c == 0x1b {
				t.state = oscEsc
			} else if c == 0x9d { // C1 OSC
				t.body = t.body[:0]
				t.state = oscBody
			}
		case oscEsc:
			switch c {
			case ']':
				t.body = t.body[:0]
				t.state = oscBody
			case 'P', '_', '^', 'X': // DCS, APC, PM, SOS
				t.state = oscIgnore
			default:
				t.state = oscGround
			}
		case oscBody:
			switch c {
			case 0x07: // BEL terminates
				if e := t.finish(); e[0] != "" {
					out = append(out, e)
				}
				t.state = oscGround
			case 0x1b:
				t.state = oscEscBody
			case 0x9c: // C1 ST
				if e := t.finish(); e[0] != "" {
					out = append(out, e)
				}
				t.state = oscGround
			default:
				if len(t.body) < oscMaxBody {
					t.body = append(t.body, c)
				} else {
					t.state = oscIgnore
				}
			}
		case oscEscBody:
			if c == '\\' {
				if e := t.finish(); e[0] != "" {
					out = append(out, e)
				}
				t.state = oscGround
			} else {
				if len(t.body) < oscMaxBody {
					t.body = append(t.body, 0x1b, c)
				}
				t.state = oscBody
			}
		case oscIgnore:
			switch c {
			case 0x07:
				t.state = oscGround
			case 0x1b:
				t.state = oscEscIgnore
			case 0x9c:
				t.state = oscGround
			}
		case oscEscIgnore:
			if c == '\\' {
				t.state = oscGround
			} else {
				t.state = oscIgnore
			}
		}
	}
	return out
}

// finish parses "command;payload". The payload is stripped of control
// characters and capped.
func (t *oscTap) finish() [2]string {
	body := t.body
	t.body = t.body[:0]
	if i := bytes.IndexByte(body, ';'); i >= 0 {
		return [2]string{string(body[:i]), sanitizeOSC(body[i+1:])}
	}
	return [2]string{string(body), ""}
}

func sanitizeOSC(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c >= 0x20 && c != 0x7f {
			out = append(out, c)
		}
	}
	s := string(out)
	if len([]rune(s)) > oscMaxChars {
		s = string([]rune(s)[:oscMaxChars])
	}
	return s
}
