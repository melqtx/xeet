package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"
)

func TestAddColumnAppendsFocusAndFetches(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.columns[0].loading = false

	cmd := m.addColumn(ColumnSpec{Kind: FeedBookmarks, AccountID: "42"})

	if cmd == nil {
		t.Fatal("addColumn returned no command; the new column would never fetch")
	}
	if len(m.columns) != 2 {
		t.Fatalf("addColumn left %d columns, want 2", len(m.columns))
	}
	added := m.columns[1]
	if added.feed != FeedBookmarks || added.accountID != "42" {
		t.Fatalf("added column = {feed:%v account:%q}, want bookmarks on 42", added.feed, added.accountID)
	}
	if !added.loading {
		t.Error("added column is not loading; it has no page yet")
	}
	if m.focus != 1 {
		t.Fatalf("focus = %d, want the new column at 1", m.focus)
	}
	if added.id == m.columns[0].id {
		t.Error("added column reused an existing id; page routing would collide")
	}
}

func TestAddColumnRefusesFifth(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(maxColumns, FeedForYou, "", ""))

	cmd := m.addColumn(ColumnSpec{Kind: FeedFollowing})

	if len(m.columns) != maxColumns {
		t.Fatalf("addColumn grew to %d columns past the cap", len(m.columns))
	}
	if cmd == nil || !strings.Contains(m.toast, "4 columns") {
		t.Fatalf("refusal toast = %q, want a 4-column notice", m.toast)
	}
}

func TestAddColumnDowngradesWezTermMode(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.imageMode = imageModeWezTerm

	m.addColumn(ColumnSpec{Kind: FeedForYou})

	if m.imageMode != imageModeANSI {
		t.Fatalf("imageMode = %q, want ansi fallback for multiple columns", m.imageMode)
	}
	if m.imageNote != multiColumnImageNote {
		t.Fatalf("imageNote = %q, want the multi-column explanation", m.imageNote)
	}
}

func TestRemoveColumnDropsStalePagesByID(t *testing.T) {
	m := modelWithNamedColumns("left", "right")
	removedID := m.columns[m.focus].id

	m.focus = 0
	m.removeColumn()

	if len(m.columns) != 1 {
		t.Fatalf("removeColumn left %d columns, want 1", len(m.columns))
	}
	before := len(m.columns[0].posts)
	stale := pageMsg{colID: removedID, seq: 0, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "ghost"}}}}
	m = update(t, m, stale)
	if len(m.columns[0].posts) != before {
		t.Error("a page for the removed column changed the survivor's posts")
	}
}

func TestRemoveColumnRefusesLastAndClampsFocus(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))

	m.removeColumn()

	if len(m.columns) != 1 {
		t.Fatalf("removeColumn dropped the last column, %d left", len(m.columns))
	}
	if !strings.Contains(m.toast, "at least one") {
		t.Fatalf("refusal toast = %q, want a keep-one notice", m.toast)
	}

	m = modelWithNamedColumns("one", "two", "three")
	m.focus = 2
	m.removeColumn()
	if m.focus != 1 {
		t.Fatalf("focus = %d after removing the last column, want clamped to 1", m.focus)
	}
}

func TestSetColumnAccountKeepsFeedAndBumpsSeq(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedSearch, Query: "golang"}})
	c := m.cur()
	c.loading = false
	c.feedSeq = 3

	cmd := m.setColumnAccount("42")

	if cmd == nil {
		t.Fatal("setColumnAccount returned no command; the new account would never fetch")
	}
	if c.accountID != "42" {
		t.Fatalf("accountID = %q, want 42", c.accountID)
	}
	if c.feed != FeedSearch || c.searchQuery != "golang" {
		t.Fatalf("feed state = {%v %q}, want the search on golang kept", c.feed, c.searchQuery)
	}
	if c.feedSeq != 4 {
		t.Fatalf("feedSeq = %d, want a bump past in-flight pages", c.feedSeq)
	}
	if !c.loading {
		t.Error("column is not loading after the account switch")
	}

	stale := pageMsg{colID: c.id, seq: 3, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "old"}}}}
	m = update(t, m, stale)
	if len(m.cur().posts) != 0 {
		t.Error("a page from the old account landed after the switch")
	}
}

func TestSetColumnAccountResetsListNamePlaceholder(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedList, ListID: "123"}})
	c := m.cur()
	c.listName = "Frens"
	c.loading = false

	m.setColumnAccount("84")

	if c.listName != c.listID {
		t.Fatalf("listName = %q, want the id placeholder until the new account names it", c.listName)
	}
	if c.listID != "123" {
		t.Fatalf("listID = %q, want the same list kept", c.listID)
	}
}

func TestSetColumnAccountSameAccountToasts(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedForYou, AccountID: "42"}})
	m.cur().loading = false
	seq := m.cur().feedSeq

	m.setColumnAccount("42")

	if m.cur().feedSeq != seq {
		t.Error("same-account switch bumped feedSeq; nothing should refetch")
	}
	if !strings.Contains(m.toast, "already") {
		t.Fatalf("toast = %q, want an already-there notice", m.toast)
	}
}
