// Package toolset provides shared utility functions.
package toolset

// MapGet looks up a key in a map and returns the value and a bool indicating success.
// It is a generic helper to avoid separate lookup functions per type.
func MapGet[K comparable, V any](m map[K]V, key K) (V, bool) {
	v, ok := m[key]
	return v, ok
}
