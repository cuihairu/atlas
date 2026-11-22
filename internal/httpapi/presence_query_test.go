package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
)

// serverPresenceResp decodes GET /v1/directory/servers/{id}/presence.
type serverPresenceResp struct {
	ServerID    string            `json:"server_id"`
	OnlineCount int64             `json:"online_count"`
	Characters  []model.Character `json:"characters"`
	NextCursor  string            `json:"next_cursor"`
}

// decodeBody decodes a JSON response body into out.
func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestServerPresenceRosterAndCount drives GET /v1/directory/servers/{id}/presence
// (PP-3 查询 API): the roster lists only online characters, online_count is the
// store-level total (it does not shrink with paging), and the cursor walks the
// roster by character ID like the sibling characters endpoint.
func TestServerPresenceRosterAndCount(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	const (
		acctA = 2001101
		acctB = 2001102
		chIDA = 9001101
		chIDB = 9001102
	)
	seedChar(t, ts, acctA, chIDA, "game-1001")
	seedChar(t, ts, acctB, chIDB, "game-1001")

	// Nobody online: zero count, empty roster.
	var pr serverPresenceResp
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence"), &pr)
	if pr.OnlineCount != 0 || len(pr.Characters) != 0 {
		t.Fatalf("empty roster = %+v, want count 0 / no characters", pr)
	}

	// One login: count 1, roster is exactly that character.
	postPresence(t, ts, "game-1001", loginEvent("pq-s1", acctA, chIDA))
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence"), &pr)
	if pr.OnlineCount != 1 || len(pr.Characters) != 1 || pr.Characters[0].CharacterID != chIDA {
		t.Fatalf("one online = %+v, want count 1 / char %d", pr, chIDA)
	}
	if pr.Characters[0].PresenceStatus != model.PresenceOnline {
		t.Fatalf("roster presence = %q, want online", pr.Characters[0].PresenceStatus)
	}

	// Second login: count 2 — and with limit=1 the count must stay 2, proving
	// it is a total, not len(page).
	postPresence(t, ts, "game-1001", loginEvent("pq-s2", acctB, chIDB))
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=1"), &pr)
	if pr.OnlineCount != 2 || len(pr.Characters) != 1 || pr.NextCursor == "" {
		t.Fatalf("paged roster = %+v, want count 2 / page 1 / next cursor", pr)
	}

	// The cursor walks the rest of the roster. Like the sibling characters
	// endpoint, the last page still carries a next_cursor — the walk ends on
	// the empty page after it.
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=1&cursor="+pr.NextCursor), &pr)
	if pr.OnlineCount != 2 || len(pr.Characters) != 1 || pr.Characters[0].CharacterID != chIDB {
		t.Fatalf("roster page 2 = %+v, want count 2 / char %d", pr, chIDB)
	}
	lastCursor := pr.NextCursor
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=1&cursor="+lastCursor), &pr)
	if len(pr.Characters) != 0 {
		t.Fatalf("roster page 3 = %+v, want empty (walk ends on the page past the last)", pr)
	}

	// A logout moves the count back down and drops the character from the roster.
	postPresence(t, ts, "game-1001", logoutEvent("pq-s1", model.SessionEndNormal))
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-1001/presence"), &pr)
	if pr.OnlineCount != 1 || len(pr.Characters) != 1 || pr.Characters[0].CharacterID != chIDB {
		t.Fatalf("after logout = %+v, want count 1 / char %d", pr, chIDB)
	}

	// A server with no characters is an empty 200, not an error.
	decodeBody(t, getJSON(t, ts, "/v1/directory/servers/game-ghost/presence"), &pr)
	if pr.OnlineCount != 0 || len(pr.Characters) != 0 {
		t.Fatalf("unknown server = %+v, want empty", pr)
	}
}

// TestAdminSearchPresenceFilter pins the presence predicate on the admin
// character search (PP-3 角色在线查询): online returns only characters with an
// open session, offline only reported-off ones, unknown the never-seen, and an
// invalid value is a 400.
func TestAdminSearchPresenceFilter(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	const (
		acctA = 2001201
		acctB = 2001202
		acctC = 2001203
		chIDA = 9001201
		chIDB = 9001202
		chIDC = 9001203
	)
	seedChar(t, ts, acctA, chIDA, "game-1001")
	seedChar(t, ts, acctB, chIDB, "game-1001")
	seedChar(t, ts, acctC, chIDC, "game-1001")

	// A online, B offline (login+logout), C never seen.
	postPresence(t, ts, "game-1001", loginEvent("as-1", acctA, chIDA))
	postPresence(t, ts, "game-1001", loginEvent("as-2", acctB, chIDB))
	postPresence(t, ts, "game-1001", logoutEvent("as-2", model.SessionEndNormal))

	search := func(presence string) (ids []int64, status int) {
		t.Helper()
		path := "/v1/admin/characters/search?server_id=game-1001"
		if presence != "" {
			path += "&presence=" + presence
		}
		resp := getJSON(t, ts, path)
		status = resp.StatusCode
		if status != http.StatusOK {
			resp.Body.Close()
			return nil, status
		}
		var body struct {
			Characters []model.Character `json:"characters"`
		}
		decodeBody(t, resp, &body)
		for _, ch := range body.Characters {
			ids = append(ids, ch.CharacterID)
		}
		return ids, status
	}

	if ids, st := search("online"); st != http.StatusOK || len(ids) != 1 || ids[0] != chIDA {
		t.Fatalf("online filter = %v (status %d), want only char %d", ids, st, chIDA)
	}
	if ids, st := search("offline"); st != http.StatusOK || len(ids) != 1 || ids[0] != chIDB {
		t.Fatalf("offline filter = %v (status %d), want only char %d", ids, st, chIDB)
	}
	if ids, st := search("unknown"); st != http.StatusOK || len(ids) != 1 || ids[0] != chIDC {
		t.Fatalf("unknown filter = %v (status %d), want only char %d", ids, st, chIDC)
	}
	if ids, st := search(""); st != http.StatusOK || len(ids) != 3 {
		t.Fatalf("no filter = %v (status %d), want all 3", ids, st)
	}
	if _, st := search("teleport"); st != http.StatusBadRequest {
		t.Fatalf("invalid presence = status %d, want 400", st)
	}
}

// postPresence posts one presence event batch over the registry channel and
// requires the whole request to be accepted (200).
func postPresence(t *testing.T, ts *httptest.Server, serverID string, ev model.PresenceEvent) {
	t.Helper()
	resp := postJSON(t, ts, "/v1/registry/servers/"+serverID+"/presence", model.PresenceReport{Events: []model.PresenceEvent{ev}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presence %s/%s: expected 200, got %d", ev.Kind, ev.SessionID, resp.StatusCode)
	}
}
