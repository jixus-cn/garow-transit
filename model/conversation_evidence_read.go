package model

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type QAObservation struct {
	Id         int64  `json:"id"`
	OutcomeId  int64  `json:"outcome_id,omitempty"`
	RequestId  string `json:"request_id"`
	UserId     int    `json:"user_id"`
	CreatedAt  int64  `json:"created_at"`
	ObservedAt int64  `json:"finished_at,omitempty"`
	TurnId     int64  `json:"turn_id,omitempty"`
	ChatKey    string `json:"chat_key"`
	ChatBasis  string `json:"chat_basis"`
	Basis      string `json:"basis"`
	Protocol   string `json:"protocol"`
	Prompt     string `json:"prompt"`
	Final      string `json:"final"`
	Parts      string `json:"part_layout,omitempty"`
	InputState string `json:"input_state"`
	Outcome    string `json:"outcome"`
	HTTPStatus int    `json:"http_status,omitempty" gorm:"column:http_status"`
}
type QAEvidenceFilter struct {
	User     int    `json:"user_id"`
	Start    int64  `json:"start_timestamp"`
	End      int64  `json:"end_timestamp"`
	Chat     string `json:"chat_key"`
	Query    string `json:"q"`
	Cursor   string `json:"cursor"`
	Size     int    `json:"page_size"`
	Id       int64  `json:"record_id"`
	Snapshot *int64 `json:"snapshot"`
}
type evidenceCursor struct {
	Snapshot, At, Id, Oldest int64
	Filter                   string
}
type QAEvidencePage struct {
	Items            []QAObservation `json:"items"`
	Next             string          `json:"next_cursor,omitempty"`
	Snapshot         int64           `json:"snapshot"`
	RetentionChanged bool            `json:"retention_changed"`
	Total            int64           `json:"total"`
}

var ErrEvidenceFilter = errors.New("invalid evidence filter")

// The join reads individual request observations, not the mutable QA turn projection.
// TextRef only refers to a canonical body that cannot be replaced by another final.
// Link observations and outcomes arriving after the watermark are invisible.
func ReadQAEvidence(f QAEvidenceFilter, cutoff int64) (QAEvidencePage, error) {
	p := QAEvidencePage{Items: []QAObservation{}}
	if f.Size == 0 {
		f.Size = 50
	}
	if f.User < 0 || f.Start < 0 || f.End < 0 || (f.End > 0 && f.End < f.Start) || f.Size < 1 || f.Size > 100 || f.Id < 0 || len(f.Query) > 200 || len(f.Chat) > 64 || len(f.Cursor) > 512 {
		return p, ErrEvidenceFilter
	}
	c := evidenceCursor{Filter: qaDigest(fmt.Sprint(f.User), fmt.Sprint(f.Start), fmt.Sprint(f.End), f.Chat, f.Query, fmt.Sprint(f.Id))}
	db := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	var maxID int64
	if err := db.Model(&QAEvidence{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
		return p, err
	}
	if f.Cursor != "" {
		expected := c.Filter
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		if e != nil || common.Unmarshal(b, &c) != nil || c.Filter != expected || c.Id <= 0 || c.Snapshot < c.Id || c.At < 0 || c.Oldest < 0 {
			return p, ErrEvidenceFilter
		}
	} else if f.Snapshot != nil {
		if *f.Snapshot < 0 {
			return p, ErrEvidenceFilter
		}
		c.Snapshot = *f.Snapshot
	} else {
		c.Snapshot = maxID
	}
	if c.Snapshot > maxID && (f.Cursor == "" || c.Oldest >= cutoff) {
		return p, ErrEvidenceFilter
	}
	p.Snapshot = c.Snapshot
	const tid = "COALESCE(NULLIF(o.turn_id,0),i.turn_id,0)"
	const fields = `i.id, i.request_id, i.user_id,i.created_at,i.protocol,COALESCE(o.id,0) AS outcome_id,COALESCE(o.observed_at,0) AS observed_at,
 COALESCE(l.chat_key,NULLIF(o.chat_key,''),i.chat_key,'') AS chat_key,COALESCE(l.chat_basis,NULLIF(o.chat_basis,''),i.chat_basis,'') AS chat_basis,
 COALESCE(NULLIF(o.basis,''),i.basis,'') AS basis, COALESCE(NULLIF(i.body,''),pt.prompt,'') AS prompt, COALESCE(NULLIF(o.body,''),ft.final,'') AS final,
 i.parts, CASE WHEN i.text_ref>0 AND pt.id IS NULL THEN 'reference_expired' ELSE i.capture_state END AS input_state, CASE WHEN o.text_ref>0 AND ft.id IS NULL THEN 'reference_expired' ELSE COALESCE(o.capture_state,'open') END AS outcome,COALESCE(o.http_status,0) AS http_status,` + tid + " AS turn_id"
	base := db.Table("conversation_qa_evidence AS i").Select(fields).
		Joins("LEFT JOIN conversation_qa_evidence AS o ON o.request_id=i.request_id AND o.phase='outcome' AND o.id<=?", c.Snapshot).
		Joins("LEFT JOIN conversation_qa_turns AS pt ON pt.id=i.text_ref AND pt.created_at>=?", cutoff).
		Joins("LEFT JOIN conversation_qa_turns AS ft ON ft.id=o.text_ref AND ft.created_at>=?", cutoff).
		Joins("LEFT JOIN conversation_qa_evidence AS l ON l.id=(SELECT MAX(m.id) FROM conversation_qa_evidence AS m WHERE m.phase='link' AND m.turn_id="+tid+" AND m.id<=? AND m.created_at>=?)", c.Snapshot, cutoff).
		Where("i.phase='input' AND i.id<=? AND i.created_at>=?", c.Snapshot, max(f.Start, cutoff))
	if f.User > 0 {
		base = base.Where("i.user_id = ?", f.User)
	}
	if f.Id > 0 {
		base = base.Where("i.id = ?", f.Id)
	}
	if f.End > 0 {
		base = base.Where("i.created_at <= ?", f.End)
	}
	query := func() *gorm.DB {
		q := db.Table("(?) AS obs", base)
		if f.Chat == "unassigned" {
			q = q.Where("chat_key = ''")
		} else if f.Chat != "" {
			q = q.Where("chat_key = ?", f.Chat)
		}
		if f.Query != "" {
			s := "%" + strings.ToLower(strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)) + "%"
			q = q.Where("(LOWER(prompt) LIKE ? ESCAPE '!' OR LOWER(final) LIKE ? ESCAPE '!')", s, s)
		}
		return q
	}
	var stats struct{ N, Oldest int64 }
	if err := query().Select("COUNT(*) AS n, COALESCE(MIN(created_at),0) AS oldest").Scan(&stats).Error; err != nil {
		return p, err
	}
	p.Total = stats.N
	if f.Cursor == "" {
		c.Oldest = stats.Oldest
	} else {
		p.RetentionChanged = c.Oldest != 0 && cutoff > c.Oldest
	}
	asc := f.Chat != "" && f.Chat != "unassigned"
	q := query().Order("created_at DESC, id DESC")
	if asc {
		q = query().Order("created_at, id")
	}
	if f.Cursor != "" {
		op := "<"
		if asc {
			op = ">"
		}
		q = q.Where("(created_at "+op+" ? OR (created_at = ? AND id "+op+" ?))", c.At, c.At, c.Id)
	}
	if err := q.Limit(f.Size + 1).Find(&p.Items).Error; err != nil {
		return p, err
	}
	n, bytes := 0, 0
	for _, v := range p.Items {
		size := len(v.Prompt) + len(v.Final) + len(v.Parts)
		if n == f.Size || (n > 0 && bytes+size > 4<<20) {
			break
		}
		n++
		bytes += size
	}
	if n < len(p.Items) {
		p.Items = p.Items[:n]
		last := p.Items[n-1]
		c.At, c.Id = last.CreatedAt, last.Id
		b, _ := common.Marshal(c)
		p.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	for i := range p.Items {
		r := &p.Items[i]
		if strings.HasPrefix(r.InputState, "legacy") {
			var masked bool
			r.Prompt, masked = MaskQACredentials(r.Prompt)
			if masked {
				r.InputState = "redacted"
			}
		}
		if r.Outcome == "legacy_turn" {
			r.Final, _ = MaskQACredentials(r.Final)
		}
	}
	return p, nil
}
