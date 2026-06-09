// Package state provides persistent storage for ORYX daemon state.
package state

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

type Severity string

const (
	SeverityInfo      Severity = "info"
	SeverityWarning   Severity = "warning"
	SeverityCritical  Severity = "critical"
	SeverityEmergency Severity = "emergency"
)

// ProcessInfo holds a snapshot of one process for anomaly detection.
type ProcessInfo struct {
	Name string `json:"name"`
	RSS  int    `json:"rss"` // kilobytes
	PID  int    `json:"pid"`
}

type Alert struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Severity  Severity  `json:"severity"`
	CreatedAt time.Time `json:"created_at"`
	Resolved  bool      `json:"resolved"`
}

type CheckResult struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // ok, warning, critical
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

type DB struct {
	db *bbolt.DB
}

func Open(path string) (*DB, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	d := &DB{db: db}
	if err := d.init(); err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	return d, nil
}

func (d *DB) init() error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("alerts"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("results"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("snapshots"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("fix_tickets"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("tool_log"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("conversations"))
		return err
	})
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) CreateAlert(title, body string, severity Severity) (*Alert, error) {
	alert := &Alert{
		Title:     title,
		Body:      body,
		Severity:  severity,
		CreatedAt: time.Now(),
		Resolved:  false,
	}
	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("alerts"))
		id, _ := b.NextSequence()
		alert.ID = id
		data, err := json.Marshal(alert)
		if err != nil {
			return err
		}
		return b.Put(itob(alert.ID), data)
	})
	return alert, err
}

func (d *DB) UnresolvedAlerts() ([]Alert, error) {
	var alerts []Alert
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("alerts"))
		return b.ForEach(func(k, v []byte) error {
			var a Alert
			if err := json.Unmarshal(v, &a); err != nil {
				return nil // skip corrupt
			}
			if !a.Resolved {
				alerts = append(alerts, a)
			}
			return nil
		})
	})
	return alerts, err
}

func (d *DB) SaveResult(name, status, message string) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("results"))
		r := CheckResult{
			Name:      name,
			Status:    status,
			Message:   message,
			CheckedAt: time.Now(),
		}
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		return b.Put([]byte(name), data)
	})
}

func (d *DB) GetResult(name string) (*CheckResult, error) {
	var r CheckResult
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("results"))
		data := b.Get([]byte(name))
		if data == nil {
			return fmt.Errorf("no result for %s", name)
		}
		return json.Unmarshal(data, &r)
	})
	return &r, err
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
	return b
}

// --- Fix ticket storage (for LLM auto-fix escalation) ---

// FixTicket represents an anomaly that needs LLM-based remediation.
type FixTicket struct {
	ID          uint64    `json:"id"`
	CheckName   string    `json:"check_name"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	SystemState string    `json:"system_state"`
	Attempts    int       `json:"attempts"`
	Resolved    bool      `json:"resolved"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateTicket stores a new fix ticket and returns it.
func (d *DB) CreateTicket(checkName, severity, message, systemState string) (*FixTicket, error) {
	t := &FixTicket{
		CheckName:   checkName,
		Severity:    severity,
		Message:     message,
		SystemState: systemState,
		Attempts:    0,
		Resolved:    false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fix_tickets"))
		id, _ := b.NextSequence()
		t.ID = id
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		return b.Put(itob(t.ID), data)
	})
	return t, err
}

// GetTicket retrieves a fix ticket by ID.
func (d *DB) GetTicket(id uint64) (*FixTicket, error) {
	var t FixTicket
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fix_tickets"))
		data := b.Get(itob(id))
		if data == nil {
			return fmt.Errorf("ticket %d not found", id)
		}
		return json.Unmarshal(data, &t)
	})
	return &t, err
}

// GetUnresolvedTickets returns all unresolved tickets, newest first.
func (d *DB) GetUnresolvedTickets() ([]FixTicket, error) {
	var tickets []FixTicket
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fix_tickets"))
		return b.ForEach(func(k, v []byte) error {
			var t FixTicket
			if err := json.Unmarshal(v, &t); err != nil {
				return nil // skip corrupt
			}
			if !t.Resolved {
				tickets = append(tickets, t)
			}
			return nil
		})
	})
	// Reverse for newest-first
	for i, j := 0, len(tickets)-1; i < j; i, j = i+1, j-1 {
		tickets[i], tickets[j] = tickets[j], tickets[i]
	}
	return tickets, err
}

// MarkResolved sets a fix ticket as resolved.
func (d *DB) MarkResolved(id uint64) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fix_tickets"))
		data := b.Get(itob(id))
		if data == nil {
			return fmt.Errorf("ticket %d not found", id)
		}
		var t FixTicket
		if err := json.Unmarshal(data, &t); err != nil {
			return err
		}
		t.Resolved = true
		t.UpdatedAt = time.Now()
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		return b.Put(itob(id), data)
	})
}

// IncrementAttempts increments the attempt counter on a fix ticket.
func (d *DB) IncrementAttempts(id uint64) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fix_tickets"))
		data := b.Get(itob(id))
		if data == nil {
			return fmt.Errorf("ticket %d not found", id)
		}
		var t FixTicket
		if err := json.Unmarshal(data, &t); err != nil {
			return err
		}
		t.Attempts++
		t.UpdatedAt = time.Now()
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		return b.Put(itob(id), data)
	})
}

// SaveSnapshot stores a key-value snapshot (disk%, mem%, authfail count, etc.)
func (d *DB) SaveSnapshot(key, value string) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("snapshots"))
		return b.Put([]byte(key), []byte(value))
	})
}

// GetSnapshot retrieves a snapshot value by key.
func (d *DB) GetSnapshot(key string) (string, error) {
	var val string
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("snapshots"))
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("no snapshot for %s", key)
		}
		val = string(data)
		return nil
	})
	return val, err
}

// SaveProcessSnapshot stores the top-N process list as JSON.
func (d *DB) SaveProcessSnapshot(procs []ProcessInfo) error {
	data, err := json.Marshal(procs)
	if err != nil {
		return err
	}
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("snapshots"))
		return b.Put([]byte("processes"), data)
	})
}

// GetProcessSnapshot retrieves the last stored process snapshot.
func (d *DB) GetProcessSnapshot() ([]ProcessInfo, error) {
	var procs []ProcessInfo
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("snapshots"))
		data := b.Get([]byte("processes"))
		if data == nil {
			return fmt.Errorf("no process snapshot")
		}
		return json.Unmarshal(data, &procs)
	})
	return procs, err
}

