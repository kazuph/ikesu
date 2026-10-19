//go:build perf

package perf

import (
	"strings"
	"testing"
)

func TestRelativeMemoryMedianIgnoresSinglePeakAndRecordsMaximum(t *testing.T) {
	for _, tc := range []struct {
		name         string
		medianKiB    int64
		wantJudgment string
	}{
		{"unchanged median", 30 * 1024, "cur 30.000 <= base 30.000"},
		{"at 1.10 limit", 33 * 1024, "cur 33.000 <= base 30.000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var base, cur []result
			for n := 0; n < relRuns; n++ {
				base = append(base, result{rssKB: 30 * 1024})
				cur = append(cur, result{rssKB: tc.medianKiB})
			}
			cur[0].rssKB = 35*1024 + 512
			base[0].rssKB = cur[0].rssKB
			before := rep.Len()
			judgeRelative(t, "median-unit", cur, base)
			reportTable("median-unit", cur)
			output := rep.String()[before:]
			if !strings.Contains(output, tc.wantJudgment+" x 1.10 (= 33.000) PASS") {
				t.Fatal("memory judgment must use both medians", output)
			}
			for _, line := range strings.Split(output, "\n") {
				if strings.HasPrefix(line, "max ") && strings.Contains(line, "35.5") {
					return
				}
			}
			t.Fatal("maximum RSS must remain in the recorded table", output)
		})
	}
}
