// Package cache provides in-memory and Redis-backed caching.
package cache

import (
	"fmt"

	"BrainForever/internal/store"
)

// ============================================================
// ExcerptValueDictCache — in-memory bidirectional cache
// for excerpt_value_dict (id <-> value mapping).
//
// With only 14 fixed entries, an in-memory map is far more
// efficient than Redis round-trips. Data is loaded once at
// startup via Load() and never modified afterwards, so Go's
// map concurrent-read safety applies — no locking needed.
// ============================================================

// ExcerptValueDictCache holds an in-memory bidirectional mapping
// between excerpt value IDs (SMALLINT) and their English value strings.
// Load once at startup, then read-only.
type ExcerptValueDictCache struct {
	idToVal map[int16]string // id -> value (forward)
	valToID map[string]int16 // value -> id (reverse)
}

// NewExcerptValueDictCache creates a new empty ExcerptValueDictCache.
// Call Load() to populate it with data.
func NewExcerptValueDictCache() *ExcerptValueDictCache {
	return &ExcerptValueDictCache{
		idToVal: make(map[int16]string, 14),
		valToID: make(map[string]int16, 14),
	}
}

// Load populates the cache from a list of ExcerptValueDict entries.
// Any existing data is replaced. Must be called once at startup
// (single-threaded init phase) before any concurrent reads.
func (c *ExcerptValueDictCache) Load(items []store.ExcerptValueDict) {
	c.idToVal = make(map[int16]string, len(items))
	c.valToID = make(map[string]int16, len(items))
	for _, item := range items {
		c.idToVal[item.ID] = item.Value
		c.valToID[item.Value] = item.ID
	}
}

// GetValueByID returns the English value string for the given numeric ID.
// Returns an empty string if the ID is not found.
func (c *ExcerptValueDictCache) GetValueByID(id int16) string {
	return c.idToVal[id]
}

// GetIDByValue returns the numeric ID for the given English value string.
// Returns 0 if the value is not found.
func (c *ExcerptValueDictCache) GetIDByValue(value string) int16 {
	return c.valToID[value]
}

// GetIDByValueOrPanic is a convenience wrapper that panics on lookup failure.
// Useful at startup when referencing known constants.
func (c *ExcerptValueDictCache) GetIDByValueOrPanic(value string) int16 {
	id := c.GetIDByValue(value)
	if id == 0 {
		panic(fmt.Sprintf("excerpt value dict: value %q not found", value))
	}
	return id
}

// GetAll returns a copy of the forward mapping (id -> value).
func (c *ExcerptValueDictCache) GetAll() map[int16]string {
	result := make(map[int16]string, len(c.idToVal))
	for k, v := range c.idToVal {
		result[k] = v
	}
	return result
}

// Exists checks whether a value string exists in the dictionary.
func (c *ExcerptValueDictCache) Exists(value string) bool {
	_, ok := c.valToID[value]
	return ok
}

// Size returns the number of entries in the cache.
func (c *ExcerptValueDictCache) Size() int {
	return len(c.idToVal)
}
