package render

import (
	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

func TestOyagoiMobileSectionOrderReview(t *testing.T) {
	s := testState(40, 35)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.Workspaces[2].Section = model.SectionFavorite
	rows := MobileRows(s, nil, s.Workspaces[1].ID)
	hq, favorite, spaces := -1, -1, -1
	for i, row := range rows {
		if row.Kind == RowMobileOyagoi {
			hq = i
		}
		if row.Kind == RowSection && row.Section == model.SectionFavorite {
			favorite = i
		}
		if row.Kind == RowMobileSpaces {
			spaces = i
		}
	}
	if hq < 0 || favorite <= hq || spaces <= favorite {
		t.Fatalf("incorrect order: %+v", rows)
	}
}

func TestOyagoiOfflineHasExplicitTargetReview(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Input: &local}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: 9, Offline: true}}
	ComposeMachines(local, &m)
	for _, hit := range m.Hits {
		if hit.Oyagoi && hit.Machine == "remote" && hit.Workspace == 9 {
			return
		}
	}
	t.Fatal("offline HQ has no target for unset")
}
