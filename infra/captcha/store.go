package captcha

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
)

// ISession is a cache storage interface for captcha session data.
type ISession interface {
	Get(key string) interface{}
	Set(key string, value interface{})
	Delete(key string)
}

// Global captcha recognition singleton

var (
	theCaptchaRWMutex   = new(sync.RWMutex)
	theCaptchaTable     map[string]string // filename -> captcha code
	theCaptchaTableKeys []string          // filenames
)

func readCaptchaIndies(fn string) (map[string]string, []string, error) {
	fileIndex, err := os.Open(fn)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = fileIndex.Close() }()

	m := map[string]string{}
	var keys []string

	lineScanner := bufio.NewScanner(fileIndex)

	for lineScanner.Scan() {
		line := lineScanner.Text()

		// split by tab
		if line != "" {
			kv := strings.Split(line, "\t")
			if len(kv) == 2 {
				m[kv[0]] = kv[1]
				keys = append(keys, kv[0])
			}
		}
	}

	if err := lineScanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to scan captcha index file: %w", err)
	}

	return m, keys, nil
}

// Refresh reloads the captcha table from the given directory.
func Refresh(dir string) (count int, err error) {
	fn := dir + "index.dat"

	if maps, keys, e := readCaptchaIndies(fn); e != nil {
		err = fmt.Errorf("failed to read captcha index data: %w", e)
		return
	} else {
		count = len(keys)

		theCaptchaRWMutex.Lock()
		defer theCaptchaRWMutex.Unlock()

		theCaptchaTable = maps
		theCaptchaTableKeys = keys
	}

	return
}

// GetOne returns a random captcha entry.
func GetOne() (sn, code string) {
	theCaptchaRWMutex.RLock() // read lock
	defer theCaptchaRWMutex.RUnlock()

	if theCaptchaTable == nil {
		return "", ""
	}

	count := len(theCaptchaTableKeys)

	if count == 0 {
		return "", ""
	}

	// pick a random entry:
	randIndex := rand.Intn(count)
	sn = theCaptchaTableKeys[randIndex]

	var ok bool
	if code, ok = theCaptchaTable[sn]; !ok {
		return sn, ""
	} else {
		return sn, code
	}
}

// makeCaptchaCacheKey builds a cache key for a captcha code.
// The key only depends on the action, because a session only needs one
// captcha code for a given action at any time.
func makeCaptchaCacheKey(action string) string {
	return fmt.Sprintf("CAPTCHA.%s", action)
}

// removeCaptchaCache deletes the cached captcha code.
func removeCaptchaCache(session ISession, action string) {
	key := makeCaptchaCacheKey(action)
	session.Delete(key)
}

// getCaptchaCache retrieves the cached captcha code for the given action.
func getCaptchaCache(session ISession, action string) string {
	key := makeCaptchaCacheKey(action)

	if di := session.Get(key); di == nil {
		return ""
	} else if code, ok := di.(string); !ok {
		return ""
	} else {
		return code
	}
}

// SetCaptchaCache stores a captcha code for the given action.
func SetCaptchaCache(session ISession, action, code string) {
	key := makeCaptchaCacheKey(action)
	session.Set(key, code)
}

// VerifyCaptchaCache verifies a captcha code for the given action.
func VerifyCaptchaCache(session ISession, action, code string) bool {
	codeInCache := getCaptchaCache(session, action)

	if codeInCache == "" || !strings.EqualFold(codeInCache, code) {
		return false
	}

	removeCaptchaCache(session, action) // clear the cached captcha after verification
	return true
}

// VerifyCaptchaCacheEx verifies a captcha code and distinguishes between
// "expired" (no captcha set) and "wrong" (code doesn't match).
// Returns:
//   - exists: true if a captcha was previously stored for this action
//   - matches: true if the provided code matches the stored captcha
func VerifyCaptchaCacheEx(session ISession, action, code string) (exists, matches bool) {
	codeInCache := getCaptchaCache(session, action)

	if codeInCache == "" {
		return false, false // never set or already consumed → expired
	}

	if !strings.EqualFold(codeInCache, code) {
		return true, false // exists but wrong
	}

	removeCaptchaCache(session, action) // consume on success
	return true, true
}
