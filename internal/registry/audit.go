package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const auditFile = "audit.log"

// AuditRecord is one append-only entry describing a lifecycle action.
type AuditRecord struct {
	Time     time.Time         `json:"time"`
	Action   string            `json:"action"`             // create, up, down, destroy, restart, inject, ...
	Target   string            `json:"target,omitempty"`   // instance name
	Kind     string            `json:"kind,omitempty"`     // blueprint kind
	Provider string            `json:"provider,omitempty"` // driver
	Params   map[string]string `json:"params,omitempty"`
	Result   string            `json:"result"` // ok | error
	Error    string            `json:"error,omitempty"`
}

// Audit appends a record to the audit log. Auditing must never break a command,
// so write errors are swallowed after a best-effort attempt.
func (s *Store) Audit(rec AuditRecord) {
	if rec.Time.IsZero() {
		rec.Time = time.Now()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(s.dir, auditFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// AuditOK records a successful action.
func (s *Store) AuditOK(action, target, kind, prov string, params map[string]string) {
	s.Audit(AuditRecord{Action: action, Target: target, Kind: kind, Provider: prov, Params: params, Result: "ok"})
}

// AuditErr records a failed action.
func (s *Store) AuditErr(action, target string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.Audit(AuditRecord{Action: action, Target: target, Result: "error", Error: msg})
}

// ReadAudit returns up to limit most-recent audit records (newest last). A limit
// <= 0 returns all records.
func (s *Store) ReadAudit(limit int) ([]AuditRecord, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, auditFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []AuditRecord
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue // tolerate a corrupt line rather than fail the whole read
		}
		recs = append(recs, r)
	}
	if limit > 0 && len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	return recs, nil
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
