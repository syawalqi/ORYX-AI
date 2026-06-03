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
