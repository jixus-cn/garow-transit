package controller

import (
	"context"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func checkEvidenceWindow(t *testing.T, now int64) {
	t.Helper()
	const uid = 77
	entry := func(id string) model.QARequestMeta {
		return model.QARequestMeta{UserId: uid, TokenId: 3, RequestId: id, CreatedAt: now, Protocol: "responses"}
	}
	rec := func(id string, f model.QAFacts, end bool) {
		t.Helper()
		require.NoError(t, model.RecordQA(t.Context(), entry(id), f, 0, end))
	}
	page := func(f model.QAEvidenceFilter) model.QAEvidencePage {
		t.Helper()
		f.User = uid
		p, err := model.ReadQAEvidence(f, 0)
		require.NoError(t, err)
		return p
	}
	a := model.QAFacts{Client: "fixture", Chat: "e-chat", Submission: "same", Prompt: "repeat", InputState: "text"}
	rec("admitted", a, false)
	q := page(model.QAEvidenceFilter{})
	require.Len(t, q.Items, 1)
	require.Equal(t, "open", q.Items[0].Outcome)
	require.Equal(t, "repeat", q.Items[0].Prompt)
	a.Final = "first terminal"
	a.HTTPStatus = 200
	a.OutputState = "final"
	rec("admitted", a, true)
	rec("admitted", a, true)
	require.Equal(t, "open", page(model.QAEvidenceFilter{Snapshot: &q.Snapshot}).Items[0].Outcome)
	a.Final = "alternative"
	rec("branch", a, true)
	rec("unpaired", model.QAFacts{Client: "fixture", Final: "orphan", HTTPStatus: 200, OutputState: "final"}, true)
	rec("failed", model.QAFacts{Client: "fixture", Prompt: "same text", HTTPStatus: 503, OutputState: "http_error"}, true)
	all := page(model.QAEvidenceFilter{})
	require.EqualValues(t, 4, all.Total)
	rows := map[string]model.QAObservation{}
	for _, o := range all.Items {
		rows[o.RequestId] = o
	}
	require.Equal(t, "first terminal", rows["admitted"].Final)
	require.Equal(t, "alternative", rows["branch"].Final)
	require.Equal(t, "ambiguous", rows["branch"].Basis)
	require.Equal(t, "orphan", rows["unpaired"].Final)
	require.Zero(t, rows["unpaired"].TurnId)
	require.Equal(t, 503, rows["failed"].HTTPStatus)
	require.Empty(t, rows["failed"].Final)
	require.EqualValues(t, 2, page(model.QAEvidenceFilter{Chat: rows["branch"].ChatKey}).Total)
	p := page(model.QAEvidenceFilter{Size: 1})
	seen := []int64{p.Items[0].Id}
	rec("newer", model.QAFacts{Prompt: "another"}, true)
	for p.Next != "" {
		p = page(model.QAEvidenceFilter{Size: 1, Cursor: p.Next})
		require.Len(t, p.Items, 1)
		seen = append(seen, p.Items[0].Id)
	}
	var want []int64
	for _, o := range all.Items {
		want = append(want, o.Id)
	}
	require.Equal(t, want, seen)
	searched := page(model.QAEvidenceFilter{Query: "alternative"})
	require.Len(t, searched.Items, 1)
	require.Equal(t, rows["branch"].Id, searched.Items[0].Id)
	interrupted, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, model.RecordQA(interrupted, entry("no_write"), a, 0, false))
}
func TestQAEvidenceReadRequiresRootBrowser(t *testing.T) {
	for _, role := range []int{1, 10, 100} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/conversations/evidence", strings.NewReader("{}"))
		c.Set("id", 1)
		c.Set("role", role)
		if role != 100 {
			c.Set("session_id", "fixture")
			c.Set("auth_version", int64(1))
			c.Set("session_version", int64(1))
		}
		GetQAEvidence(c)
		require.Equal(t, 403, w.Code)
	}
}
