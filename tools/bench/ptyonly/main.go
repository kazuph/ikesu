// Command ptyonly isolates ikesu's output path: it runs a command on a 160x45
// PTY and drains it with no UI, optionally feeding an emulator, to measure
// how much of the multiplexer's time each layer costs.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/pprof"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func main() {
	mode := flag.String("mode", "read", "read | vt")
	bufSize := flag.Int("buf", 64*1024, "read buffer size")
	prof := flag.String("cpuprofile", "", "write CPU profile")
	flag.Parse()
	if *prof != "" {
		pf, _ := os.Create(*prof)
		pprof.StartCPUProfile(pf)
		defer pprof.StopCPUProfile()
	}
	if *mode == "feed" {
		feed()
		return
	}
	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 45, Cols: 160})
	if err != nil {
		panic(err)
	}
	var emu *vt.Emulator
	if *mode == "vt" {
		emu = vt.NewEmulator(160, 45)
		emu.SetScrollbackSize(1000)
		go io.Copy(io.Discard, emu)
	}
	start := time.Now()
	buf := make([]byte, *bufSize)
	var reads, total int
	for {
		n, err := f.Read(buf)
		if n > 0 {
			reads++
			total += n
			if emu != nil {
				emu.Write(buf[:n])
			}
		}
		if err != nil {
			break
		}
	}
	_ = cmd.Wait()
	el := time.Since(start)
	fmt.Fprintf(os.Stderr, "mode=%s elapsed=%.2fs reads=%d bytes=%d avg=%d\n", *mode, el.Seconds(), reads, total, total/max(reads, 1))
}

// feed writes the 2,000,000-line workload straight into the emulator
// (no PTY), in the 1 KiB chunks the macOS PTY delivers.
func feed() {
	emu := vt.NewEmulator(160, 45)
	emu.SetScrollbackSize(1000)
	go io.Copy(io.Discard, emu)
	var data []byte
	for i := 0; i < 2000000; i++ {
		data = fmt.Appendf(data, "%d line of throughput test\r\n", i)
	}
	start := time.Now()
	for off := 0; off < len(data); off += 1024 {
		emu.Write(data[off:min(off+1024, len(data))])
	}
	fmt.Fprintf(os.Stderr, "mode=feed elapsed=%.2fs bytes=%d\n", time.Since(start).Seconds(), len(data))
}
