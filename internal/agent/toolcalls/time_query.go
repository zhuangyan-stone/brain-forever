package toolcalls

import "time"

// GetLocalTime returns the current local time formatted as a string.
func GetLocalTime() string {
	return time.Now().Local().Format(time.DateTime)
}
