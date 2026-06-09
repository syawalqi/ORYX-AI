package state

import (
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"
)

// ToolLogEntry records a single tool call for audit purposes.
type ToolLogEntry struct {
	ID        uint64    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool"`
	Args      string    `json:"args"`
	Result    string    `json:"result"` // first 200 chars of output
	Success   bool      `json:"success"`
	Duration  string    `json:"duration"`
	Iteration int       `json:"iteration"`
}

// AppendToolLog adds a tool call record to the audit log.
func (d *DB) AppendToolLog(tool, args, result string, success bool, duration string, iteration int) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("tool_log"))
		id, _ := b.NextSequence()
		entry := ToolLogEntry{
			ID:        id,
			Timestamp: time.Now(),
			Tool:      tool,
			Args:      args,
			Result:    result,
			Success:   success,
			Duration:  duration,
			Iteration: iteration,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put(itob(entry.ID), data)
	})
}

// GetToolLogs returns the most recent N tool log entries, newest first.
func (d *DB) GetToolLogs(n int) ([]ToolLogEntry, error) {
	var entries []ToolLogEntry
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("tool_log"))
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var e ToolLogEntry
			if err := json.Unmarshal(v, &e); err != nil {
				continue
			}
			entries = append(entries, e)
			if len(entries) >= n {
				break
			}
		}
		return nil
	})
	return entries, err
}
