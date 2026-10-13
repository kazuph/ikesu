//go:build darwin

package platform

import (
	"github.com/kazuph/goterrace/internal/childenv"
	"strconv"
	"strings"
)

func OpenJSONLFiles(pid int) []string {
	output, err := childenv.Command("/usr/sbin/lsof", "-a", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "n") && strings.HasSuffix(line, ".jsonl") {
			paths = append(paths, line[1:])
		}
	}
	return paths
}
