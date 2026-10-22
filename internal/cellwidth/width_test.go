package cellwidth

import (
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"os"
	"testing"
)

func TestEveryUnicodeScalarMatchesRust022Oracle(t *testing.T) {
	data, err := os.ReadFile("testdata/unicode-width-0.2.2.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected [][3]int
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	index, count := 0, 0
	for r := 0; r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		for index < len(expected) && expected[index][1] < r {
			index++
		}
		width := 1
		if index < len(expected) && expected[index][0] <= r {
			width = expected[index][2]
		}
		if got := Scalar(rune(r)); got != width {
			t.Fatalf("U+%04X got %d expected %d", r, got, width)
		}
		count++
	}
	t.Logf("compared %d Unicode scalars against unicode-width 0.2.2", count)
}

func TestUnicodeScalarWidthsAndGraphemeComparison(t *testing.T) {
	for _, test := range []struct {
		value    string
		expected int
	}{
		{"ascii", 5}, {"全角", 4}, {"e\u0301", 1}, {"👩\u200d💻", 4}, {"❤️", 1}, {"☕︎", 2}, {"┌──┐", 4},
	} {
		got := String(test.value)
		t.Logf("%q unicode-width-0.2.2-scalar=%d ansi.StringWidth=%d", test.value, got, ansi.StringWidth(test.value))
		if got != test.expected {
			t.Fatalf("%q: %d != %d", test.value, got, test.expected)
		}
	}
	if got := Cut("全角abc", 2, 5); got != "角a" {
		t.Fatal(got)
	}
}
