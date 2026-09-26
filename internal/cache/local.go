package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/preburn/preburn/internal/clock"
)

// LocalCache holds values in process memory. An entry expires once the time
// to live has passed since it was set. When a Set would exceed the maximum
// entry count, the least recently used entry is evicted. It reads time from a
// clock.Clock, so tests control expiry. Create one with NewLocalCache. It is
// safe for concurrent use.
type LocalCache[Key comparable, Value any] struct {
	mutex          sync.Mutex
	clock          clock.Clock
	timeToLive     time.Duration
	maximumEntries int
	elements       map[Key]*list.Element
	recency        *list.List
}

type localEntry[Key comparable, Value any] struct {
	key       Key
	value     Value
	expiresAt time.Time
}

// NewLocalCache returns an empty LocalCache that reads time from timeSource,
// expires entries timeToLive after they are set and holds at most
// maximumEntries entries. It panics if timeToLive is not positive or
// maximumEntries is below 1.
func NewLocalCache[Key comparable, Value any](timeSource clock.Clock, timeToLive time.Duration, maximumEntries int) *LocalCache[Key, Value] {
	if timeToLive <= 0 || maximumEntries < 1 {
		panic(fmt.Sprintf("invalid local cache limits time_to_live=%s maximum_entries=%d", timeToLive, maximumEntries))
	}
	return &LocalCache[Key, Value]{
		clock:          timeSource,
		timeToLive:     timeToLive,
		maximumEntries: maximumEntries,
		elements:       map[Key]*list.Element{},
		recency:        list.New(),
	}
}

// Get returns the value stored under key and true, or the zero value and false
// when the key is missing or its entry has expired. A hit makes the entry the
// most recently used.
func (cache *LocalCache[Key, Value]) Get(key Key) (Value, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	var zero Value
	element, found := cache.elements[key]
	if !found {
		return zero, false
	}
	entry := element.Value.(*localEntry[Key, Value])
	if !cache.clock.Now().Before(entry.expiresAt) {
		cache.remove(element)
		return zero, false
	}
	cache.recency.MoveToFront(element)
	return entry.value, true
}

// Set stores value under key as the most recently used entry and restarts its
// time to live. If the cache then holds more than the maximum entry count, it
// evicts the least recently used entry.
func (cache *LocalCache[Key, Value]) Set(key Key, value Value) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if element, found := cache.elements[key]; found {
		cache.remove(element)
	}
	entry := &localEntry[Key, Value]{key: key, value: value, expiresAt: cache.clock.Now().Add(cache.timeToLive)}
	cache.elements[key] = cache.recency.PushFront(entry)
	if cache.recency.Len() > cache.maximumEntries {
		cache.remove(cache.recency.Back())
	}
}

// Delete removes the entry stored under key, if there is one.
func (cache *LocalCache[Key, Value]) Delete(key Key) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if element, found := cache.elements[key]; found {
		cache.remove(element)
	}
}

// Clear removes every entry.
func (cache *LocalCache[Key, Value]) Clear() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	clear(cache.elements)
	cache.recency.Init()
}

func (cache *LocalCache[Key, Value]) remove(element *list.Element) {
	entry := cache.recency.Remove(element).(*localEntry[Key, Value])
	delete(cache.elements, entry.key)
}
