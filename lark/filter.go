package lark

import (
	"container/list"
	"sync"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
)

type SelfFilter struct {
	cfg     config.LarkConfig
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	now     func() time.Time
}

type selfEntry struct {
	id        string
	expiresAt time.Time
}

func NewSelfFilter(cfg config.LarkConfig) *SelfFilter {
	return &SelfFilter{cfg: cfg, entries: map[string]*list.Element{}, order: list.New(), now: time.Now}
}

func (f *SelfFilter) Ignore(ev Event) bool {
	if f.ignoreSenderType(ev.SenderType) {
		return true
	}
	if ev.SenderAppID != "" && ev.SenderAppID == f.cfg.Ignore.SelfAppID {
		return true
	}
	if f.cfg.Trigger.IgnoreUpdateEvents && ev.EventType == EventMessagePatch {
		return true
	}
	if f.cfg.Trigger.IgnoreCardEvents && ev.EventType == EventCard {
		return true
	}
	if ev.MessageID == "" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune()
	_, ok := f.entries[ev.MessageID]
	return ok
}

func (f *SelfFilter) ignoreSenderType(senderType string) bool {
	for _, ignored := range f.cfg.Ignore.SenderTypes {
		if senderType == ignored {
			return true
		}
	}
	return false
}

func (f *SelfFilter) Remember(messageID string) {
	if messageID == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if elem, ok := f.entries[messageID]; ok {
		f.order.Remove(elem)
	}
	entry := selfEntry{id: messageID, expiresAt: f.now().Add(f.cfg.Ignore.MessageIDTTL.Duration)}
	f.entries[messageID] = f.order.PushBack(entry)
	f.prune()
}

func (f *SelfFilter) prune() {
	now := f.now()
	for elem := f.order.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(selfEntry)
		if now.After(entry.expiresAt) || len(f.entries) > f.cfg.Ignore.MaxMessageIDs {
			delete(f.entries, entry.id)
			f.order.Remove(elem)
		}
		elem = next
	}
}
