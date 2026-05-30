package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketSessions    = []byte("sessions")
	bucketDedupe      = []byte("dedupe")
	bucketUnknownChat = []byte("unknown_chat")
)

type BoltStore struct {
	db *bolt.DB
}

func OpenBolt(path string) (*BoltStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state dir failed, %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt state failed, %w", err)
	}
	store := &BoltStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *BoltStore) init() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketSessions, bucketDedupe, bucketUnknownChat} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q failed, %w", string(name), err)
			}
		}
		return nil
	})
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) GetSession(ctx context.Context, key string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var rec SessionRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		value := b.Get([]byte(key))
		if value == nil {
			return nil
		}
		if err := json.Unmarshal(value, &rec); err != nil {
			return fmt.Errorf("unmarshal session failed, %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if rec.Key == "" {
		return nil, nil
	}
	return &rec, nil
}

func (s *BoltStore) PutSession(ctx context.Context, rec SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal session failed, %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSessions).Put([]byte(rec.Key), value)
	})
}

func (s *BoltStore) DeleteSession(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSessions).Delete([]byte(key))
	})
}

func (s *BoltStore) CleanupSessions(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return cleanupSessions(tx.Bucket(bucketSessions), now)
	})
}

func (s *BoltStore) Cleanup(ctx context.Context, now time.Time, unknownChatInterval time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := cleanupSessions(tx.Bucket(bucketSessions), now); err != nil {
			return err
		}
		if err := cleanupTimeBucket(tx.Bucket(bucketDedupe), now, func(expiresAt time.Time) bool { return now.After(expiresAt) }); err != nil {
			return fmt.Errorf("cleanup dedupe failed, %w", err)
		}
		if unknownChatInterval > 0 {
			if err := cleanupTimeBucket(tx.Bucket(bucketUnknownChat), now, func(last time.Time) bool { return now.Sub(last) >= unknownChatInterval }); err != nil {
				return fmt.Errorf("cleanup unknown chat failed, %w", err)
			}
		}
		return nil
	})
}

func cleanupSessions(b *bolt.Bucket, now time.Time) error {
	return b.ForEach(func(k, v []byte) error {
		var rec SessionRecord
		if err := json.Unmarshal(v, &rec); err != nil {
			return fmt.Errorf("unmarshal session failed, %w", err)
		}
		if (!rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt)) || (!rec.IdleUntil.IsZero() && now.After(rec.IdleUntil)) {
			return b.Delete(k)
		}
		return nil
	})
}

func cleanupTimeBucket(b *bolt.Bucket, now time.Time, expired func(time.Time) bool) error {
	return b.ForEach(func(k, v []byte) error {
		var ts time.Time
		if err := ts.UnmarshalBinary(v); err != nil {
			return b.Delete(k)
		}
		if expired(ts) {
			return b.Delete(k)
		}
		return nil
	})
}

func (s *BoltStore) MarkSeen(ctx context.Context, key string, ttl time.Duration, now time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	expiresAt := now.Add(ttl)
	seen := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDedupe)
		value := b.Get([]byte(key))
		if value != nil {
			var old time.Time
			if err := old.UnmarshalBinary(value); err == nil && now.Before(old) {
				seen = true
				return nil
			}
		}
		encoded, err := expiresAt.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal expiry failed, %w", err)
		}
		return b.Put([]byte(key), encoded)
	})
	if err != nil {
		return false, err
	}
	return seen, nil
}

func (s *BoltStore) AllowUnknownChatReply(ctx context.Context, chatID string, interval time.Duration, now time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	allowed := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketUnknownChat)
		value := b.Get([]byte(chatID))
		if value != nil {
			var last time.Time
			if err := last.UnmarshalBinary(value); err == nil && now.Sub(last) < interval {
				return nil
			}
		}
		encoded, err := now.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal unknown chat timestamp failed, %w", err)
		}
		allowed = true
		return b.Put([]byte(chatID), encoded)
	})
	if errors.Is(err, context.Canceled) {
		return false, err
	}
	return allowed, err
}
