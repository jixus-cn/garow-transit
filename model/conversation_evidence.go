package model

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strconv"
	"time"
)

// Immutable input/outcome observations share the canonical turn text when possible.
// A linkage conflict or an orphan has its own text; its existence never depends on pairing.
type QAEvidence struct {
	Id           int64  `gorm:"primaryKey"`
	RequestId    string `gorm:"size:64;uniqueIndex:idx_qa_observation,priority:1"`
	Phase        string `gorm:"size:12;uniqueIndex:idx_qa_observation,priority:2;index:idx_qa_evidence_turn,priority:2;index:idx_qa_evidence_window,priority:1"`
	CreatedAt    int64  `gorm:"autoCreateTime:false;index:idx_qa_evidence_window,priority:2"`
	ObservedAt   int64  `gorm:"autoCreateTime:false"`
	UserId       int    `gorm:"index"`
	TurnId       int64  `gorm:"index:idx_qa_evidence_turn,priority:1"`
	TextRef      int64
	ChatKey      string `gorm:"size:64;index"`
	ChatBasis    string `gorm:"size:16"`
	Basis        string `gorm:"size:20"`
	CaptureState string `gorm:"size:80"`
	Protocol     string `gorm:"size:20"`
	HTTPStatus   int
	Parts        string `gorm:"type:text"`
	Body         string `gorm:"size:1048576"`
}

func (QAEvidence) TableName() string { return "conversation_qa_evidence" }

var qaEvidenceGate = make(chan struct{}, 1)

// RecordQA commits admission before relay and completion before returning. An
// outcome can repair a failed admission in the same transaction. A restarted
// process cannot mistake an existing admission for a completed observation.
func RecordQA(ctx context.Context, entry QARequestMeta, f QAFacts, cutoff int64, finish bool) error {
	if entry.CreatedAt < cutoff {
		return nil
	}
	select {
	case qaEvidenceGate <- struct{}{}:
		defer func() { <-qaEvidenceGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var receipt QAReceipt
		err := tx.Where("request_id = ?", entry.RequestId).Take(&receipt).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && (receipt.State != "started" || !finish) {
			return nil
		}
		base := QAEvidence{RequestId: entry.RequestId, UserId: entry.UserId, CreatedAt: entry.CreatedAt, ObservedAt: time.Now().Unix(), Protocol: entry.Protocol}
		scope := qaDigest(strconv.Itoa(entry.UserId), strconv.Itoa(entry.TokenId), f.Client)
		if f.Chat != "" {
			base.ChatKey = qaDigest(scope, "chat", f.Chat)
			base.ChatBasis = "client"
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			input := base
			input.Phase = "input"
			input.CaptureState = f.InputState
			input.Parts = f.Parts
			if input.CaptureState == "" {
				if f.Prompt != "" {
					input.CaptureState = "text"
				} else {
					input.CaptureState = "no_input"
				}
			}
			save := func(r QAReceipt, turn QATurn) error {
				input.Basis = r.Basis
				input.TurnId = r.TurnId
				if turn.Id != 0 && turn.Prompt == f.Prompt && f.Prompt != "" {
					input.TextRef = turn.Id
				} else {
					input.Body = f.Prompt
				}
				if turn.ChatKey != "" {
					input.ChatKey = turn.ChatKey
					input.ChatBasis = turn.ChatBasis
				}
				if err := tx.Create(&input).Error; err != nil {
					return err
				}
				receipt = r
				receipt.State = "started"
				return tx.Create(&receipt).Error
			}
			if f.Prompt != "" {
				start := f
				start.Final = ""
				start.Response = ""
				start.Issued = nil
				if err := projectQA(tx, entry, start, cutoff, 0, true, save); err != nil {
					return err
				}
			} else if err := save(QAReceipt{RequestId: entry.RequestId, Scope: scope, CreatedAt: entry.CreatedAt}, QATurn{}); err != nil {
				return err
			}
		}
		if !finish {
			return nil
		}
		if receipt.Scope != scope {
			return errors.New("QA admission scope mismatch")
		}
		return projectQA(tx, entry, f, cutoff, receipt.TurnId, true, func(r QAReceipt, t QATurn) error {
			out := base
			out.Phase = "outcome"
			out.HTTPStatus = f.HTTPStatus
			out.CaptureState = f.OutputState
			out.TurnId = r.TurnId
			out.Basis = r.Basis
			if r.State != "linked" {
				out.Basis = r.State
			}
			if t.Id != 0 {
				if err := tx.Select("id", "chat_key", "chat_basis", "final", "answer_state").Where("id = ? AND scope = ?", t.Id, scope).Take(&t).Error; err != nil {
					return err
				}
				if r.TurnId != 0 && t.ChatKey != "" {
					out.ChatKey = t.ChatKey
					out.ChatBasis = t.ChatBasis
				}
				if r.TurnId != 0 && t.AnswerState == "ambiguous" {
					out.Basis = "ambiguous"
				}
			}
			if f.Final != "" && t.Id != 0 && t.Final == f.Final {
				out.TextRef = t.Id
			} else {
				out.Body = f.Final
			}
			if err := tx.Create(&out).Error; err != nil {
				return err
			}
			return tx.Model(&QAReceipt{}).Where("request_id = ? AND state = ?", r.RequestId, "started").Updates(map[string]any{"state": r.State, "basis": r.Basis, "turn_id": r.TurnId}).Error
		})
	})
}
