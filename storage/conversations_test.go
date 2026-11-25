package storage

import (
	"context"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

func TestConversationsKeyUsageAndExport(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// The schema step is idempotent: running it again must not fail.
	if _, err := db.Pool.Exec(ctx, requestConversationSchema); err != nil {
		t.Fatal(err)
	}
	r := &Recorder{db: db}
	now := time.Now().UTC().Truncate(time.Second)
	rec := func(id, conv string, ago time.Duration, model, status, key string, in, out int, credits float64) *telemetry.RequestRecord {
		return &telemetry.RequestRecord{ID: id, Time: now.Add(-ago), Endpoint: "claude", Model: model, AccountID: "a1",
			AccountEmail: "a@x.io", APIKeyID: key, UserAgent: "claude-cli/2.0.1 (external, cli)", Status: status,
			InputTokens: in, OutputTokens: out, CacheRead: 10, Credits: credits, ConversationID: conv,
			RequestBody: `{"messages":[]}`}
	}
	r.writeRequests([]*telemetry.RequestRecord{
		rec("c1-1", "sess_one", 50*time.Minute, "claude-sonnet-4.5", "success", "k1", 100, 10, 0.5),
		rec("c1-2", "sess_one", 40*time.Minute, "claude-sonnet-4.5", "error", "k1", 0, 0, 0),
		rec("c1-3", "sess_one", 5*time.Minute, "claude-opus-4.5", "cancelled", "k1", 200, 20, 1),
		rec("c2-1", "sess_two", 20*time.Minute, "claude-haiku-4.5", "success", "k2", 1, 1, 0.1),
		rec("c3-1", "sess_three", 30*time.Hour, "claude-haiku-4.5", "success", "k1", 7, 7, 0.2),
		rec("none", "", 1*time.Minute, "claude-haiku-4.5", "success", "", 1, 1, 0),
	})
	if s := r.SinkStats(); s.Failed != 0 || s.Written != 6 {
		t.Fatalf("write stats = %+v", s)
	}

	got, err := r.GetRequest(ctx, "c1-1")
	if err != nil || got.ConversationID != "sess_one" {
		t.Fatalf("GetRequest = %+v, %v", got, err)
	}

	convs, err := r.QueryConversations(ctx, telemetry.ConversationFilter{Since: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 || convs[0].ID != "sess_one" || convs[1].ID != "sess_two" {
		t.Fatalf("conversations = %+v", convs)
	}
	c := convs[0]
	if c.Requests != 3 || c.Errors != 1 || c.Cancelled != 1 || c.InputTokens != 300 || c.OutputTokens != 30 || c.CacheRead != 30 ||
		c.Credits != 1.5 || len(c.Models) != 2 || len(c.Accounts) != 1 || len(c.APIKeys) != 1 || c.UserAgent == "" ||
		!c.FirstSeen.Equal(now.Add(-50*time.Minute)) || !c.LastSeen.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("summary = %+v", c)
	}

	// Keyset pagination and search.
	page2, err := r.QueryConversations(ctx, telemetry.ConversationFilter{BeforeID: "sess_one", Limit: 10})
	if err != nil || len(page2) != 2 || page2[0].ID != "sess_two" || page2[1].ID != "sess_three" {
		t.Fatalf("page 2 = %+v, %v", page2, err)
	}
	found, err := r.QueryConversations(ctx, telemetry.ConversationFilter{Search: "haiku"})
	if err != nil || len(found) != 2 {
		t.Fatalf("search by model = %+v, %v", found, err)
	}
	found, err = r.QueryConversations(ctx, telemetry.ConversationFilter{Search: "sess_tw"})
	if err != nil || len(found) != 1 || found[0].ID != "sess_two" {
		t.Fatalf("search by id = %+v, %v", found, err)
	}

	reqs, err := r.ConversationRequests(ctx, "sess_one", 0)
	if err != nil || len(reqs) != 3 || reqs[0].ID != "c1-1" || reqs[2].ID != "c1-3" || reqs[0].RequestBody != "" {
		t.Fatalf("timeline = %+v, %v", reqs, err)
	}
	if reqs, err := r.ConversationRequests(ctx, "", 0); err != nil || len(reqs) != 0 {
		t.Fatalf("empty id must match nothing: %+v, %v", reqs, err)
	}

	days, err := r.KeyDailyUsage(ctx, "k1", now.Add(-14*24*time.Hour), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var reqN, inN int64
	for _, d := range days {
		reqN += d.Requests
		inN += d.InputTokens
	}
	if reqN != 4 || inN != 307 {
		t.Fatalf("daily usage = %+v", days)
	}

	var ids []string
	err = r.ExportRequests(ctx, telemetry.LogFilter{APIKeyID: "k1", Since: now.Add(-24 * time.Hour)}, 2, func(x *telemetry.RequestRecord) error {
		if x.RequestBody != "" {
			t.Fatalf("export must not include bodies")
		}
		ids = append(ids, x.ID)
		return nil
	})
	if err != nil || len(ids) != 2 || ids[0] != "c1-3" || ids[1] != "c1-2" {
		t.Fatalf("export = %v, %v", ids, err)
	}

	// The request search also matches conversation ids.
	recs, err := r.QueryRequests(ctx, telemetry.LogFilter{Search: "sess_two"})
	if err != nil || len(recs) != 1 || recs[0].ID != "c2-1" {
		t.Fatalf("search = %+v, %v", recs, err)
	}
}

// A database created before the conversation column existed gains it on start.
func TestConversationColumnAddedToExistingTable(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'kiro_request_logs' AND column_name = 'conversation_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("conversation_id column: %d, %v", n, err)
	}
}
