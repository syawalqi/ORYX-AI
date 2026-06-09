package state

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
	"github.com/syawalqi/oryx/llm"
)

// Conversation represents a saved chat session with its message history.
type Conversation struct {
	ID        string       `json:"id"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Messages  []llm.Message `json:"messages"`
	Model     string       `json:"model"`
	Summary   string       `json:"summary"` // first user message as summary
}

// SaveConversation persists a conversation to the store.
func (d *DB) SaveConversation(id string, msgs []llm.Message, model string) error {
	summary := ""
	for _, m := range msgs {
		if m.Role == llm.RoleUser && m.Content != "" {
			summary = m.Content
			if len(summary) > 80 {
				summary = summary[:80]
			}
			break
		}
	}

	conv := Conversation{
		ID:        id,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  msgs,
		Model:     model,
		Summary:   summary,
	}

	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("conversations"))
		// Check if exists — preserve CreatedAt
		if existing := b.Get([]byte(id)); existing != nil {
			var old Conversation
			if err := json.Unmarshal(existing, &old); err == nil {
				conv.CreatedAt = old.CreatedAt
			}
		}
		data, err := json.Marshal(conv)
		if err != nil {
			return err
		}
		return b.Put([]byte(id), data)
	})
}

// LoadConversation retrieves a saved conversation by ID.
func (d *DB) LoadConversation(id string) (*Conversation, error) {
	var conv Conversation
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("conversations"))
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("conversation %q not found", id)
		}
		return json.Unmarshal(data, &conv)
	})
	return &conv, err
}

// ListConversations returns all saved conversations, newest first.
func (d *DB) ListConversations() ([]Conversation, error) {
	var convs []Conversation
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("conversations"))
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var conv Conversation
			if err := json.Unmarshal(v, &conv); err != nil {
				continue
			}
			convs = append(convs, conv)
		}
		return nil
	})
	return convs, err
}

// DeleteConversation removes a saved conversation.
func (d *DB) DeleteConversation(id string) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("conversations"))
		return b.Delete([]byte(id))
	})
}
