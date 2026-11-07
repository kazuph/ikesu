package render

import (
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestOyagoiMobileMovesWorkspaceAheadOfFavorites(t *testing.T) {
	s := testState(40, 35)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Workspaces[1].Section = model.SectionFavorite
	rows := MobileRows(s, nil, s.Workspaces[1].ID)
	first, count := -1, 0
	for i, row := range rows {
		if row.Kind == RowWorkspace && row.Ws == 1 {
			if first < 0 {
				first = i
			}
			count++
		}
	}
	if first != 1 || count != 1 {
		t.Fatalf("HQ must follow the first heading once: first=%d count=%d rows=%+v", first, count, rows)
	}
}

func TestOyagoiMobileShowsForeignWorkspaceFirst(t *testing.T) {
	s := testState(40, 35)
	rows := MobileRowsWithForeignOyagoi(s, nil, 0, "remote · mini")
	if len(rows) < 2 || rows[0].Kind != RowMobileOyagoi || rows[1].Kind != RowMobileForeignOyagoi {
		t.Fatalf("foreign HQ missing: %+v", rows)
	}
}
