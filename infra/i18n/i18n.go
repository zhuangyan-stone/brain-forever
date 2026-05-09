// Package i18n provides internationalization support for the BrainForever application.
//
// It uses go-i18n with TOML translation files to provide localized strings for:
//   - System prompts sent to the AI API (need translation)
//   - Tool descriptions sent to the AI API (need translation)
//   - Messages sent to the frontend (need translation)
//   - Logs and console output (remain in English)
//
// Usage:
//
//	// Get a localized string with template data
//	msg := i18n.T("system_prompt")
//
//	// Get a localized string in a specific language
//	msg := i18n.TL("zh-CN", "system_prompt")
package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var (
	// bundle is the global i18n bundle that holds all translations.
	bundle *i18n.Bundle

	// localizerCache caches localizer instances by language tag.
	localizerCache sync.Map

	// defaultLang is the default language tag used when no language is specified.
	defaultLang = language.English
)

// init loads translation files from the i18n directory.
func init() {
	// Determine the i18n directory path.
	// First try relative to the executable, then fall back to the source directory.
	i18nDir := findI18nDir()

	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// Load all TOML translation files
	files, err := filepath.Glob(filepath.Join(i18nDir, "*.toml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[i18n] failed to list translation files: %v\n", err)
		return
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "[i18n] no translation files found in %s\n", i18nDir)
		return
	}

	for _, file := range files {
		if _, err := bundle.LoadMessageFile(file); err != nil {
			fmt.Fprintf(os.Stderr, "[i18n] failed to load translation file %s: %v\n", file, err)
		}
	}
}

// findI18nDir attempts to locate the i18n directory.
// It checks several common locations relative to the working directory.
func findI18nDir() string {
	// Check common locations
	candidates := []string{
		"lang",
		"./lang",
		"../lang",
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	// Fallback: use the current working directory
	return "lang"
}

// getLocalizer returns a localizer for the given language tag.
// It caches localizer instances for performance.
func getLocalizer(lang string) *i18n.Localizer {
	if lang == "" {
		lang = defaultLang.String()
	}

	if cached, ok := localizerCache.Load(lang); ok {
		return cached.(*i18n.Localizer)
	}

	// Parse the language tag
	tag, err := language.Parse(lang)
	if err != nil {
		// Fall back to English if the language tag is invalid
		tag = language.English
	}

	localizer := i18n.NewLocalizer(bundle, tag.String())
	localizerCache.Store(lang, localizer)
	return localizer
}

// T returns the localized string for the given message ID using the default language (English).
// templateData is optional and can be used to fill in template placeholders.
//
// Example:
//
//	i18n.T("system_prompt")
func T(messageID string, templateData ...map[string]interface{}) string {
	return TL(defaultLang.String(), messageID, templateData...)
}

// TL returns the localized string for the given message ID in the specified language.
// templateData is optional and can be used to fill in template placeholders.
//
// Example:
//
//	i18n.TL("zh-CN", "system_prompt")
//	i18n.TL("en", "search_no_results")
func TL(lang, messageID string, templateData ...map[string]interface{}) string {
	localizer := getLocalizer(lang)

	config := &i18n.LocalizeConfig{
		MessageID: messageID,
	}

	if len(templateData) > 0 {
		config.TemplateData = templateData[0]
	}

	localized, err := localizer.Localize(config)
	if err != nil {
		// Fallback: try the default language
		fallbackLocalizer := getLocalizer(defaultLang.String())
		config.TemplateData = nil
		if len(templateData) > 0 {
			config.TemplateData = templateData[0]
		}
		fallback, fbErr := fallbackLocalizer.Localize(config)
		if fbErr != nil {
			// If all else fails, return the message ID itself
			return messageID
		}
		return fallback
	}

	return localized
}

// GetLanguageFromAcceptLanguage parses the Accept-Language header and returns
// the best matching language tag supported by the bundle.
// Returns "en" if no match is found.
//
// Example:
//
//	lang := i18n.GetLanguageFromAcceptLanguage("zh-CN,zh;q=0.9,en;q=0.8")
//	// Returns "zh-CN"
func GetLanguageFromAcceptLanguage(acceptLanguage string) string {
	if acceptLanguage == "" {
		return defaultLang.String()
	}

	// Parse the Accept-Language header
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(tags) == 0 {
		return defaultLang.String()
	}

	// Try to find a supported language
	matcher := language.NewMatcher(bundle.LanguageTags())
	_, i, _ := matcher.Match(tags...)

	if i < len(bundle.LanguageTags()) {
		return bundle.LanguageTags()[i].String()
	}

	return defaultLang.String()
}

// SupportedLanguages returns a list of supported language tags.
func SupportedLanguages() []string {
	tags := bundle.LanguageTags()
	result := make([]string, len(tags))
	for i, tag := range tags {
		result[i] = tag.String()
	}
	return result
}

// SetDefaultLanguage sets the default language for the T() function.
func SetDefaultLanguage(lang string) {
	tag, err := language.Parse(lang)
	if err == nil {
		defaultLang = tag
	}
}

// MustLocalize is like TL but panics if the message ID is not found.
// Useful for messages that must always be present.
func MustLocalize(lang, messageID string, templateData ...map[string]interface{}) string {
	localizer := getLocalizer(lang)

	config := &i18n.LocalizeConfig{
		MessageID: messageID,
	}

	if len(templateData) > 0 {
		config.TemplateData = templateData[0]
	}

	localized, err := localizer.Localize(config)
	if err != nil {
		panic(fmt.Sprintf("[i18n] missing translation for %s in %s: %v", messageID, lang, err))
	}

	return localized
}

// Tf is a convenience wrapper around T that uses fmt.Sprintf-style arguments.
// The messageID is used as the format string after localization.
//
// Example:
//
//	i18n.Tf("search_parse_error", "en", "parse error: %v", err)
func Tf(messageID string, args ...interface{}) string {
	msg := T(messageID)
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// TLf is like Tf but for a specific language.
func TLf(lang, messageID string, args ...interface{}) string {
	msg := TL(lang, messageID)
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// GetAcceptLanguage extracts the language preference from the request.
// This is a helper for HTTP handlers to determine the user's language.
func GetAcceptLanguage(acceptLang string) string {
	// Simple parsing: take the first language tag
	if acceptLang == "" {
		return defaultLang.String()
	}

	// Split by comma and take the first one
	parts := strings.Split(acceptLang, ",")
	if len(parts) == 0 {
		return defaultLang.String()
	}

	// Split by semicolon to remove quality value
	lang := strings.Split(strings.TrimSpace(parts[0]), ";")[0]

	// Validate the language tag
	_, err := language.Parse(lang)
	if err != nil {
		return defaultLang.String()
	}

	return lang
}
