package dispatch

import "testing"

func TestCommandLookupExactIDAndBasename(t *testing.T) {
	s := testStore(t)
	for _, job := range []struct{ id, path string }{
		{"j1", "/logs/j1-first.log"}, {"j10", "/logs/j10-wait-j1.log"},
		{"j20", "/logs/j20-wait-j999.log"}, {"j30", `/logs/j30-a%_\b.log`},
		{"j40", "/logs/j1.log"},
	} {
		if err := s.InsertCommand(job.id, "label", "cmd", "/", "", "agent", "", job.path); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ query, want string }{
		{"j1", "j1"}, {"j10", "j10"}, {"j999", ""}, {"j1-first", "j1"},
		{"j1-first.log", "j1"}, {"first", ""}, {"j1-fir", ""},
		{`j30-a%_\b`, "j30"}, {`j30-a%_\b.log`, "j30"}, {`j30-aXX\b`, ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			row, err := s.CommandRow(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			did, err := s.CommandDispatchID(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if row != nil || did != nil {
					t.Fatalf("unexpected match: %+v %v", row, did)
				}
				return
			}
			if row == nil || row.ID != tc.want || did == nil {
				t.Fatalf("got row=%+v dispatch=%v; want %s", row, did, tc.want)
			}
			wantID, err := s.CommandDispatchID(tc.want)
			if err != nil || wantID == nil || *did != *wantID {
				t.Fatalf("dispatch mismatch: %v %v %v", did, wantID, err)
			}
		})
	}
}
