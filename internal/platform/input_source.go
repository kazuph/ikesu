package platform

import (
	"github.com/kazuph/goterrace/internal/childenv"
	"runtime"
	"strings"
)

// InputSource runs on the attached host only. Run is injectable so tests never
// select a real keyboard layout or request operating-system permissions.
type InputSource struct {
	Run      func(string, ...string) ([]byte, error)
	prefix   bool
	previous string
}

func (s *InputSource) Prefix(active, enabled bool) error {
	active = active && enabled
	if active == s.prefix {
		return nil
	}
	s.prefix = active
	if runtime.GOOS != "darwin" {
		return nil
	}
	run := s.Run
	if run == nil {
		run = func(name string, args ...string) ([]byte, error) { return childenv.Command(name, args...).Output() }
	}
	if active {
		out, err := run("/usr/bin/osascript", "-l", "JavaScript", "-e", asciiInputSource)
		if err != nil {
			s.prefix = false
			return err
		}
		s.previous = strings.TrimSpace(string(out))
		return nil
	}
	if s.previous == "" {
		return nil
	}
	previous := s.previous
	s.previous = ""
	_, err := run("/usr/bin/osascript", "-l", "JavaScript", "-e", restoreInputSource, previous)
	return err
}

const asciiInputSource = `ObjC.import('Carbon');
const current=$.TISCopyCurrentKeyboardInputSource();
const id=ObjC.unwrap($.TISGetInputSourceProperty(current,$.kTISPropertyInputSourceID));
const ascii=$.TISCopyCurrentASCIICapableKeyboardLayoutInputSource();
if (ascii) {const result=$.TISSelectInputSource(ascii); $.CFRelease(ascii); if(result!==0) throw Error('TISSelectInputSource failed: '+result);}
$.CFRelease(current);
id;`
const restoreInputSource = `ObjC.import('Carbon');
function run(argv) {
 const filter=$.NSMutableDictionary.dictionary;
 filter.setObjectForKey($(argv[0]),$.kTISPropertyInputSourceID);
 const sources=$.TISCreateInputSourceList(filter,false);
 if(!sources || $.CFArrayGetCount(sources)===0) throw Error('previous input source unavailable');
 const source=$.CFArrayGetValueAtIndex(sources,0);
 const result=$.TISSelectInputSource(source); $.CFRelease(sources);
 if(result!==0) throw Error('TISSelectInputSource failed: '+result);
}`
