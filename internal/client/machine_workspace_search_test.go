package client

import (
	"fmt"
	"os"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

type searchSpace struct{ machine, name, branch string }

// searchFixture has local: alpha-api, 日本語メモ and mini: beta-web, gamma (HQ-capable).
func searchFixture(t *testing.T) *machineClient {
	t.Helper()
	m := cycleFixture(t)
	cfg, err := config.Decode([]byte("[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\nworkspace_search='cmd+f'\n"))
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatal(cfg.Warnings, err)
	}
	m.size = tui.Winsize{Cols: 120, Rows: 40}
	m.mouse.Report(nil, m.size)
	spaces := map[string][]searchSpace{
		"local": {{"local", "alpha-api", "main"}, {"local", "日本語メモ", "feature/検索"}},
		"mini":  {{"mini", "beta-web", "release"}, {"mini", "gamma", ""}},
	}
	for id, list := range spaces {
		e := m.endpoints[id]
		e.profile.Label = id
		e.snapshot.SidebarConfig = cfg
		e.snapshot.Git = map[int]render.GitStatus{}
		for len(e.snapshot.State.Workspaces) < len(list) {
			e.snapshot.State.Apply(model.CmdNewWorkspace{})
		}
		for i, sp := range list {
			ws := e.snapshot.State.Workspaces[i]
			ws.Name, ws.CustomName = sp.name, true
			e.snapshot.Git[i] = render.GitStatus{Branch: sp.branch}
		}
		e.snapshot.State.Active = 0
	}
	return m
}

func searchKey(code rune, mod uv.KeyMod, text string) input.Event {
	return input.Event{Kind: input.KindKey, Key: uv.Key{Code: code, Mod: mod, Text: text}}
}

func typeSearch(t *testing.T, m *machineClient, text string) {
	t.Helper()
	for _, r := range text {
		if !m.workspaceSearchInput(searchKey(r, 0, string(r))) {
			t.Fatalf("%q not consumed", r)
		}
	}
}

func searchNames(m *machineClient) []string {
	var names []string
	for _, item := range m.workspaceSearchResults() {
		names = append(names, item.machine+"/"+item.name)
	}
	return names
}

func openSearch(t *testing.T, m *machineClient) {
	t.Helper()
	if !m.workspaceSearchInput(searchKey('f', uv.ModSuper, "")) || m.search == nil {
		t.Fatal("cmd+f did not open the search")
	}
}

func TestWorkspaceSearchOpensWithConfiguredKeyAndPrefixSlash(t *testing.T) {
	m := searchFixture(t)
	if m.workspaceSearchInput(searchKey('/', 0, "/")) || m.search != nil {
		t.Fatal("a bare / opened the search")
	}
	openSearch(t, m)
	// The release of the opening key is swallowed by the overlay.
	if !m.workspaceSearchInput(input.Event{Kind: input.KindKeyRelease, Key: uv.Key{Code: 'f', Mod: uv.ModSuper}}) {
		t.Fatal("release leaked")
	}
	m.search = nil

	m = searchFixture(t)
	local := m.endpoints["local"]
	local.snapshot.Prefix, local.snapshot.ModeHint = true, " PREFIX  esc cancel  ctrl+b send prefix"
	if m.workspaceSearchInput(searchKey('f', uv.ModSuper, "")) || m.search != nil {
		t.Fatal("cmd+f opened while a prefix was pending")
	}
	if !m.workspaceSearchInput(searchKey('/', 0, "/")) || m.search == nil {
		t.Fatal("prefix+/ did not open the search")
	}
	ended := false
	for _, message := range local.connection.queue {
		ended = ended || message.Type == "input" && message.EndPrefix
	}
	if !ended {
		t.Fatal("server prefix wait was left pending")
	}
}

func TestWorkspaceSearchDefaultPrefixSlashWithoutConfig(t *testing.T) {
	m := searchFixture(t)
	m.endpoints["local"].snapshot.SidebarConfig = config.Config{}
	m.endpoints["local"].snapshot.Prefix = true
	if !m.workspaceSearchInput(searchKey('/', 0, "/")) || m.search == nil {
		t.Fatal("default prefix+/ did not open the search")
	}
}

func TestWorkspaceSearchDoesNotOpenOverDialogOrWithoutBinding(t *testing.T) {
	m := searchFixture(t)
	m.endpoints["local"].snapshot.Dialog = &render.Dialog{}
	if m.workspaceSearchInput(searchKey('f', uv.ModSuper, "")) || m.search != nil {
		t.Fatal("opened over a dialog")
	}
	m = searchFixture(t)
	cfg, _ := config.Decode([]byte("[keys]\nworkspace_search=''\n"))
	m.endpoints["local"].snapshot.SidebarConfig = cfg
	if m.workspaceSearchInput(searchKey('f', uv.ModSuper, "")) || m.search != nil {
		t.Fatal("opened although the binding is unset")
	}
	m.endpoints["local"].snapshot.Prefix = true
	if m.workspaceSearchInput(searchKey('/', 0, "/")) || m.search != nil {
		t.Fatal("prefix+/ opened although the binding is unset")
	}
}

func TestWorkspaceSearchFiltersNamesMachinesBranchesAndJapanese(t *testing.T) {
	m := searchFixture(t)
	openSearch(t, m)
	all := searchNames(m)
	want := []string{"local/alpha-api", "local/日本語メモ", "mini/beta-web", "mini/gamma"}
	if fmt.Sprint(all) != fmt.Sprint(want) {
		t.Fatalf("order %v want %v", all, want)
	}
	for query, expect := range map[string][]string{
		"ALPHA":    {"local/alpha-api"},
		"mini":     {"mini/beta-web", "mini/gamma"},
		"release":  {"mini/beta-web"},
		"日本":       {"local/日本語メモ"},
		"検索":       {"local/日本語メモ"},
		"mini gam": {"mini/gamma"},
		"zzz":      nil,
	} {
		m.search.query, m.search.sel = nil, 0
		typeSearch(t, m, query)
		if got := searchNames(m); fmt.Sprint(got) != fmt.Sprint(expect) {
			t.Fatalf("%q -> %v want %v", query, got, expect)
		}
	}
}

func TestWorkspaceSearchEditingKeys(t *testing.T) {
	m := searchFixture(t)
	openSearch(t, m)
	typeSearch(t, m, "mini")
	m.workspaceSearchInput(searchKey(uv.KeyBackspace, 0, ""))
	if string(m.search.query) != "min" {
		t.Fatalf("backspace: %q", string(m.search.query))
	}
	m.workspaceSearchInput(searchKey('u', uv.ModCtrl, ""))
	if len(m.search.query) != 0 || len(m.workspaceSearchResults()) != 4 {
		t.Fatalf("ctrl+u: %q", string(m.search.query))
	}
	m.workspaceSearchInput(input.Event{Kind: input.KindPaste, Text: "日本\n"})
	if string(m.search.query) != "日本 " {
		t.Fatalf("paste: %q", string(m.search.query))
	}
}

func TestWorkspaceSearchSelectionEnterEscape(t *testing.T) {
	m := searchFixture(t)
	mini := m.endpoints["mini"]
	openSearch(t, m)
	for _, step := range []struct {
		key  input.Event
		want int
	}{
		{searchKey(uv.KeyDown, 0, ""), 1}, {searchKey('n', uv.ModCtrl, ""), 2}, {searchKey(uv.KeyDown, 0, ""), 3},
		{searchKey(uv.KeyDown, 0, ""), 3}, {searchKey('p', uv.ModCtrl, ""), 2}, {searchKey(uv.KeyUp, 0, ""), 1},
	} {
		m.workspaceSearchInput(step.key)
		if m.search.sel != step.want {
			t.Fatalf("selection %d want %d", m.search.sel, step.want)
		}
	}
	m.workspaceSearchInput(searchKey(uv.KeyDown, 0, ""))
	m.workspaceSearchInput(searchKey(uv.KeyEnter, 0, ""))
	target := mini.snapshot.State.Workspaces[0]
	if m.search != nil || m.pending == nil || m.pending.id != "mini" || m.pending.workspace != target.ID {
		t.Fatalf("enter did not move to mini/beta-web: search=%v pending=%+v", m.search, m.pending)
	}
	sent := false
	for _, message := range mini.connection.queue {
		sent = sent || message.Type == "workspace_select" && message.Workspace == target.ID
	}
	if !sent {
		t.Fatal("workspace_select not sent to mini")
	}

	m = searchFixture(t)
	openSearch(t, m)
	typeSearch(t, m, "gamma")
	m.workspaceSearchInput(searchKey(uv.KeyEscape, 0, ""))
	if m.search != nil || m.pending != nil || m.active != "local" || len(m.endpoints["mini"].connection.queue) != 0 {
		t.Fatal("escape changed something")
	}
	// Enter with no match keeps the overlay open and moves nowhere.
	openSearch(t, m)
	typeSearch(t, m, "zzz")
	m.workspaceSearchInput(searchKey(uv.KeyEnter, 0, ""))
	if m.search == nil || m.pending != nil {
		t.Fatal("enter with no result acted")
	}
}

func TestWorkspaceSearchHQCarriesGeneration(t *testing.T) {
	m := searchFixture(t)
	m.session = "local"
	mini := m.endpoints["mini"]
	mini.profile.Session = "session-A"
	ws := mini.snapshot.State.Workspaces[0]
	selected := oyagoiSelection{Machine: "mini", Session: "session-A", Workspace: ws.ID, Generation: ws.Generation}
	m.oyagoi = &selected
	openSearch(t, m)
	items := m.workspaceSearchResults()
	if len(items) == 0 || !items[0].hq || items[0].target.generation != ws.Generation {
		t.Fatalf("HQ should come first like the cycle keys: %+v", items)
	}
	m.workspaceSearchInput(searchKey(uv.KeyEnter, 0, ""))
	if m.pending == nil || m.pending.id != "mini" || m.pending.generation != ws.Generation {
		t.Fatalf("HQ identity lost: %+v", m.pending)
	}
}

func paintSearchUI(m *machineClient) *render.Output {
	in := *m.endpoints[m.active].snapshot
	out := render.Compose(in)
	m.drawWorkspaceSearchUI(out, in)
	return out
}

func searchClick(t *testing.T, x, y int) input.Event {
	return mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
}

func TestWorkspaceSearchMouseClickWheelAndOutside(t *testing.T) {
	m := searchFixture(t)
	openSearch(t, m)
	out := paintSearchUI(m)
	g := m.workspaceSearchGeometry(4)
	if got := out.Frame.Cell(g.list.X+2, g.list.Y).Content; got != "a" {
		t.Fatalf("first row not drawn: %q", got)
	}
	m.workspaceSearchInput(mouseEvent(t, "\x1b[<65;10;10M"))
	if m.search.sel != 1 {
		t.Fatalf("wheel down: %d", m.search.sel)
	}
	m.workspaceSearchInput(mouseEvent(t, "\x1b[<64;10;10M"))
	if m.search.sel != 0 {
		t.Fatalf("wheel up: %d", m.search.sel)
	}
	if !m.workspaceSearchInput(searchClick(t, g.list.X+3, g.list.Y+3)) || m.search != nil || m.pending == nil || m.pending.id != "mini" {
		t.Fatalf("click on the 4th row: search=%v pending=%+v", m.search, m.pending)
	}
	if !m.workspaceSearchInput(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dm", g.list.X+4, g.list.Y+4))) {
		t.Fatal("release after the click leaked to a pane")
	}
	if m.workspaceSearchInput(mouseEvent(t, "\x1b[<0;5;30m")) {
		t.Fatal("the swallowed release flag stayed set")
	}

	m = searchFixture(t)
	openSearch(t, m)
	m.workspaceSearchInput(searchClick(t, 0, 39))
	if m.search != nil || m.pending != nil {
		t.Fatal("outside click must only close")
	}
}

func TestWorkspaceSearchNoMatchShowsNotice(t *testing.T) {
	m := searchFixture(t)
	openSearch(t, m)
	typeSearch(t, m, "zzz")
	out := paintSearchUI(m)
	g := m.workspaceSearchGeometry(0)
	row := ""
	for x := g.list.X; x < g.list.X+g.list.W; x++ {
		row += out.Frame.Cell(x, g.list.Y).Content
	}
	if !containsText(row, "該当なし") {
		t.Fatalf("row %q", row)
	}
}

func containsText(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestWorkspaceSearchSidebarButtonOpensAndKeepsDensityButton(t *testing.T) {
	m := searchFixture(t)
	local := m.endpoints["local"].snapshot
	local.State.SidebarVisible = true
	local.State.Cols = 120
	paintSearchUI(m)
	b := m.searchButtons.sidebar
	if b.W != 2 || b.H != 1 {
		t.Fatalf("no search button: %+v", b)
	}
	area := local.State.SidebarArea()
	label := render.WorkspaceDensityLabel(local.State.WorkspaceDensity)
	densityX := area.X + area.W - 1 - len(label)
	if b.X+b.W > densityX || b.Y != area.Y {
		t.Fatalf("button %+v overlaps the density label at x=%d", b, densityX)
	}
	// The density label cell is not claimed by the search button.
	if m.workspaceSearchInput(searchClick(t, densityX+1, b.Y)) || m.search != nil {
		t.Fatal("density button click was taken")
	}
	// Neither is the new-workspace footer.
	if m.workspaceSearchInput(searchClick(t, render.SidebarNewRect(local.State).X+1, render.SidebarNewRect(local.State).Y)) || m.search != nil {
		t.Fatal("new button click was taken")
	}
	if !m.workspaceSearchInput(searchClick(t, b.X, b.Y)) || m.search == nil {
		t.Fatal("tap did not open the search")
	}
}

func TestWorkspaceSearchMobileSwitcherRowOpens(t *testing.T) {
	m := searchFixture(t)
	local := m.endpoints["local"].snapshot
	local.State.MobileWidthThreshold = 200
	local.MobileSwitcher = true
	if !local.State.Mobile() {
		t.Fatal("fixture is not mobile")
	}
	out := paintSearchUI(m)
	r := m.searchButtons.mobile
	if r.W != local.State.Cols || r.Y != 2 || m.searchButtons.sidebar.W != 0 {
		t.Fatalf("mobile row %+v sidebar %+v", r, m.searchButtons.sidebar)
	}
	if got := out.Frame.Cell(2, 2).Content; got != "🔍" {
		t.Fatalf("glyph cell %q", got)
	}
	if !m.workspaceSearchInput(searchClick(t, 5, 2)) || m.search == nil {
		t.Fatal("tap on the row did not open the search")
	}
	m.search = nil
	// Rows of the list (y >= 3) are not claimed.
	if m.workspaceSearchInput(searchClick(t, 5, 3)) {
		t.Fatal("list row click was taken")
	}
	// Without the open switcher there is no button.
	local.MobileSwitcher = false
	paintSearchUI(m)
	if m.searchButtons.mobile.W != 0 {
		t.Fatal("button shown with the switcher closed")
	}
}

// Through the client, a click on the strip's » cell reaches the server and restores the sidebar.
func TestCollapsedStripExpandClickThroughClientActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(1, 0, 160, 35)
	s := m.endpoints[m.active].snapshot.State
	if s.SidebarVisible || s.SidebarArea().W != model.SidebarCollapsedWidth {
		t.Fatalf("not a strip: visible=%t width=%d", s.SidebarVisible, s.SidebarArea().W)
	}
	// The strip's other cells keep their meaning: no hit covers the top cell.
	for _, hit := range m.sidebar.Hits {
		if !hit.Right && contains(hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, 0, 0) {
			t.Fatalf("a machine hit covers the » cell: %+v", hit)
		}
	}
	clickCell(t, h, 0, 0)
	h.until("sidebar restored", func() bool {
		st := m.endpoints[m.active].snapshot.State
		return st.SidebarVisible && !st.SidebarHidden && st.SidebarArea().W == st.ExpandedSidebarWidth() && st.SidebarArea().W >= model.SidebarMinDefault
	})
}

// With live servers, the real prefix key then / opens the search and the server
// leaves its prefix wait; a machine name and Enter then move there.
func TestWorkspaceSearchPrefixSlashLeavesServerPrefixActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(0, 0, 160, 35)
	active := m.endpoints[m.active]
	if err := m.input(mouseEventKey(t, "\x02")); err != nil {
		t.Fatal(err)
	}
	h.until("server waits for the prefixed key", func() bool { return active.snapshot.Prefix })
	if err := m.input(mouseEventKey(t, "/")); err != nil {
		t.Fatal(err)
	}
	if m.search == nil {
		t.Fatal("prefix+/ did not open the search")
	}
	h.until("server prefix wait ended", func() bool { return !active.snapshot.Prefix })
	if err := m.input(mouseEventKey(t, "\x1b")); err != nil {
		t.Fatal(err)
	}
	if m.search != nil {
		t.Fatal("escape did not close the search")
	}
	// The prefix again, a machine name and Enter move to that machine's space.
	if err := m.input(mouseEventKey(t, "\x02")); err != nil {
		t.Fatal(err)
	}
	h.until("server waits for the prefixed key again", func() bool { return active.snapshot.Prefix })
	for _, raw := range []string{"/", "r", "e", "m", "o", "t", "e", "\r"} {
		if err := m.input(mouseEventKey(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	h.until("moved to remote", func() bool { return m.search == nil && m.pending == nil && m.active == "remote" })
}

func mouseEventKey(t *testing.T, raw string) input.Event {
	t.Helper()
	var framer input.Framer
	events := framer.Push([]byte(raw))
	if len(events) == 0 {
		events = framer.FlushTimeout()
	}
	if len(events) != 1 {
		t.Fatalf("key framing %q: %+v", raw, events)
	}
	return events[0]
}

func TestWorkspaceSearchRanksNameStartThenNameThenBranchThenMachine(t *testing.T) {
	m := searchFixture(t)
	// Cycle order is alpha-api, 日本語メモ, beta-web, tau.
	m.endpoints["mini"].snapshot.State.Workspaces[1].Name = "tau"
	openSearch(t, m)
	for query, want := range map[string][]string{
		"t": {"mini/tau", "mini/beta-web", "local/日本語メモ"},                    // name start, name middle, branch
		"e": {"mini/beta-web", "local/日本語メモ"},                                // name middle before branch, though the cycle order is the reverse
		"a": {"local/alpha-api", "mini/beta-web", "mini/tau", "local/日本語メモ"}, // equal ranks keep the cycle order
		"":  {"local/alpha-api", "local/日本語メモ", "mini/beta-web", "mini/tau"}, // no query: cycle order
		"i": {"local/alpha-api", "mini/beta-web", "mini/tau"},                // "mini" machine matches rank last
	} {
		m.search.query, m.search.sel = nil, 0
		typeSearch(t, m, query)
		if got := searchNames(m); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%q -> %v want %v", query, got, want)
		}
		if m.search.sel != 0 {
			t.Fatalf("%q: selection starts at %d", query, m.search.sel)
		}
	}
}

func emulatedScreen(t *testing.T, m *machineClient, path string) *vt.Emulator {
	t.Helper()
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	emu := vt.NewEmulator(m.size.Cols, m.size.Rows)
	if _, err := emu.Write(data); err != nil {
		t.Fatal(err)
	}
	return emu
}

// Wide (Japanese) names and branches: the box has a border like the menus,
// every row of it ends at the same right edge with one background, and no
// cell outside the box differs from the screen without the overlay.
func TestWorkspaceSearchBoxWithWideTextThroughEmulator(t *testing.T) {
	m := searchFixture(t)
	file, err := os.CreateTemp(t.TempDir(), "term")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m.out = file
	closed := emulatedScreen(t, m, file.Name())
	openSearch(t, m)
	open := emulatedScreen(t, m, file.Name())
	g := m.workspaceSearchGeometry(4)
	b := g.box
	inBox := func(x, y int) bool { return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H }
	for y := 0; y < m.size.Rows; y++ {
		for x := 0; x < m.size.Cols; x++ {
			if inBox(x, y) {
				continue
			}
			a, c := closed.CellAt(x, y), open.CellAt(x, y)
			if a.Content != c.Content || a.Width != c.Width || fmt.Sprint(a.Style.Bg) != fmt.Sprint(c.Style.Bg) {
				t.Fatalf("cell %d,%d outside the box changed: %+v -> %+v", x, y, a, c)
			}
		}
	}
	for y := b.Y; y < b.Y+b.H; y++ {
		if left, right := open.CellAt(b.X, y).Content, open.CellAt(b.X+b.W-1, y).Content; y > b.Y && y < b.Y+b.H-1 && (left != "│" || right != "│") {
			t.Fatalf("row %d has no border: %q %q", y, left, right)
		}
		bgs := map[string]bool{}
		for x := b.X + 1; x < b.X+b.W-1; x++ {
			c := open.CellAt(x, y)
			if c.Width == 0 && c.Content == "" {
				continue // trailing half of a wide glyph; the emulator keeps no style for it
			}
			bgs[fmt.Sprint(c.Style.Bg)] = true
		}
		if y > b.Y && y < b.Y+b.H-1 && len(bgs) != 1 {
			t.Fatalf("row %d background is uneven: %v", y, bgs)
		}
	}
	if top := open.CellAt(b.X, b.Y).Content + open.CellAt(b.X+b.W-1, b.Y+b.H-1).Content; top != "┌┘" {
		t.Fatalf("corners %q", top)
	}
	if got := open.CellAt(g.list.X+2, g.list.Y+1).Content; got != "日" {
		t.Fatalf("Japanese row not drawn: %q", got)
	}
}

func TestWorkspaceSearchButtonYieldsToOverlays(t *testing.T) {
	covers := map[string]func(*render.Input){
		"menu":      func(v *render.Input) { v.Menu = &render.Menu{} },
		"dialog":    func(v *render.Input) { v.Dialog = &render.Dialog{} },
		"help":      func(v *render.Input) { v.Help = &render.HelpInput{} },
		"settings":  func(v *render.Input) { v.Settings = &render.Settings{} },
		"navigator": func(v *render.Input) { v.Navigator = &render.Navigator{} },
		"copy":      func(v *render.Input) { v.Copy = &render.CopyMark{} },
	}
	for name, cover := range covers {
		t.Run(name, func(t *testing.T) {
			m := searchFixture(t)
			snap := m.endpoints["local"].snapshot
			paintSearchUI(m)
			b := m.searchButtons.sidebar
			if b.W == 0 {
				t.Fatal("no button without an overlay")
			}
			cover(snap)
			out := paintSearchUI(m)
			if m.searchButtons.sidebar.W != 0 || m.searchButtons.mobile.W != 0 {
				t.Fatal("button kept while an overlay is open")
			}
			if got := out.Frame.Cell(b.X, b.Y).Content; got == "🔍" {
				t.Fatal("🔍 drawn over the overlay")
			}
			// A press on the old button cell goes on to the overlay.
			m.searchButtons.sidebar = b
			if m.workspaceSearchInput(searchClick(t, b.X, b.Y)) || m.search != nil {
				t.Fatal("click was taken from the overlay")
			}
		})
	}
}

func TestWorkspaceSearchOpeningKeyReleaseDoesNotLinger(t *testing.T) {
	m := searchFixture(t)
	openSearch(t, m)
	if !m.workspaceKeyReleases['f'] {
		t.Fatal("fixture: opening key release not recorded")
	}
	m.workspaceSearchInput(input.Event{Kind: input.KindKeyRelease, Key: uv.Key{Code: 'f', Mod: uv.ModSuper}})
	m.workspaceSearchInput(searchKey(uv.KeyEscape, 0, ""))
	if m.search != nil {
		t.Fatal("not closed")
	}
	// Later 'f' in a pane: neither the press nor its release is taken.
	press := searchKey('f', 0, "f")
	if m.workspaceSearchInput(press) || m.workspaceCycleInput(press) {
		t.Fatal("press of f was taken")
	}
	release := input.Event{Kind: input.KindKeyRelease, Key: uv.Key{Code: 'f'}}
	if m.workspaceSearchInput(release) || m.workspaceCycleInput(release) {
		t.Fatal("release of f was eaten")
	}
}

func TestWorkspaceSearchDropsUnknownInputWhileOpen(t *testing.T) {
	m := searchFixture(t)
	unknown := input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[?1;2c")}
	if m.workspaceSearchInput(unknown) {
		t.Fatal("unknown input taken while closed")
	}
	openSearch(t, m)
	if !m.workspaceSearchInput(unknown) {
		t.Fatal("unknown input leaked while open")
	}
}
