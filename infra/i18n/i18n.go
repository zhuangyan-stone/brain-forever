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

	// initialized guards against double-initialization.
	initialized bool
)

// Init initializes the i18n system by loading all .toml translation files
// from the specified directory (e.g., "lang/local" or "lang/remote").
//
// Translation files are organized as:
//
//	{langDir}/en.toml                          -top-level, no prefix
//	{langDir}/zh-CN.toml                       -top-level, no prefix
//	{langDir}/en/tools/current_time.toml        -subdirectory, prefixed with filename
//	{langDir}/zh-CN/tools/web_search.toml       -subdirectory, prefixed with filename
//
// Files in subdirectories have their message IDs automatically prefixed
// with the file name (without extension) to avoid key collisions.
// For example, a message with ID "description" in .../current_time.toml
// becomes "current_time-description".
//
// Top-level files (e.g., en.toml) keep their message IDs as-is.
func Init(langDir string) {
	if initialized {
		return
	}
	initialized = true

	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// Recursively load all TOML translation files from the directory tree
	var files []string
	err := filepath.WalkDir(langDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".toml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[i18n] failed to walk translation directory: %v\n", err)
		return
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "[i18n] no translation files found in %s\n", langDir)
		return
	}

	for _, file := range files {
		// Determine if this file is in a subdirectory (needs prefix) or top-level (no prefix).
		relPath, _ := filepath.Rel(langDir, file)
		dir := filepath.Dir(relPath)

		if dir == "." {
			// Top-level file (e.g., en.toml) -load as-is, no prefix.
			if _, err := bundle.LoadMessageFile(file); err != nil {
				fmt.Fprintf(os.Stderr, "[i18n] failed to load translation file %s: %v\n", file, err)
			}
		} else {
			// Subdirectory file (e.g., en/tools/current_time.toml) -prefix message IDs.
			if err := loadWithPrefix(file, langDir); err != nil {
				fmt.Fprintf(os.Stderr, "[i18n] failed to load translation file %s: %v\n", file, err)
			}
		}
	}
}

// loadWithPrefix parses a .toml translation file and registers all messages
// with their IDs prefixed by the file name (without extension).
//
// For example, if the file is "current_time.toml" and contains a message with ID "description",
// it will be registered as "current_time-description".
//
// The file path determines the language tag from the directory structure.
// The expected path format is: {langDir}/{language_tag}/.../{filename}.toml
// e.g., "lang/local/en/tools/current_time.toml" �?language tag "en"
//
//	"lang/local/zh-CN/tools/web_search.toml" �?language tag "zh-CN"
func loadWithPrefix(filePath string, langDir string) error {
	// Read the file content
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Determine the language tag from the directory structure.
	// We use filepath.Rel to get the relative path from langDir,
	// then extract the first component as the language tag.
	// e.g., relPath = "en/tools/current_time.toml" �?parts[0] = "en"
	//        relPath = "zh-CN/tools/web_search.toml" �?parts[0] = "zh-CN"
	relPath, _ := filepath.Rel(langDir, filePath)
	parts := strings.SplitN(relPath, string(filepath.Separator), 3)
	var langTag string
	if len(parts) >= 1 {
		langTag = parts[0]
	} else {
		langTag = "en"
	}
	tag, err := language.Parse(langTag)
	if err != nil {
		// Fallback to English if the language tag is invalid
		tag = language.English
	}

	// Parse the TOML file into a generic map.
	// go-i18n's TOML format uses sections as message IDs:
	//   [description]
	//   other = "..."
	var rawData map[string]interface{}
	if err := toml.Unmarshal(data, &rawData); err != nil {
		return fmt.Errorf("failed to unmarshal toml: %w", err)
	}

	// Extract the file name without extension as the prefix
	prefix := strings.TrimSuffix(filepath.Base(filePath), ".toml")

	// Iterate over each section (message ID) in the file
	for sectionName, sectionData := range rawData {
		msgMap, ok := sectionData.(map[string]interface{})
		if !ok {
			continue
		}

		msg := &i18n.Message{ID: prefix + "-" + sectionName}

		// Extract plural forms from the section
		if other, ok := msgMap["other"]; ok {
			msg.Other = fmt.Sprintf("%v", other)
		}
		if zero, ok := msgMap["zero"]; ok {
			msg.Zero = fmt.Sprintf("%v", zero)
		}
		if one, ok := msgMap["one"]; ok {
			msg.One = fmt.Sprintf("%v", one)
		}
		if two, ok := msgMap["two"]; ok {
			msg.Two = fmt.Sprintf("%v", two)
		}
		if few, ok := msgMap["few"]; ok {
			msg.Few = fmt.Sprintf("%v", few)
		}
		if many, ok := msgMap["many"]; ok {
			msg.Many = fmt.Sprintf("%v", many)
		}
		if desc, ok := msgMap["description"]; ok {
			msg.Description = fmt.Sprintf("%v", desc)
		}

		if err := bundle.AddMessages(tag, msg); err != nil {
			return fmt.Errorf("failed to add message %s: %w", msg.ID, err)
		}
	}

	return nil
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
// It uses language.Matcher to match against the bundle's registered language tags,
// so "zh" will be correctly matched to "zh-CN" if that's what the bundle supports.
func GetAcceptLanguage(acceptLang string) string {
	if acceptLang == "" {
		return defaultLang.String()
	}

	// Parse the Accept-Language header
	tags, _, err := language.ParseAcceptLanguage(acceptLang)
	if err != nil || len(tags) == 0 {
		return defaultLang.String()
	}

	// Try to find a supported language using the bundle's matcher
	matcher := language.NewMatcher(bundle.LanguageTags())
	_, i, _ := matcher.Match(tags...)

	if i < len(bundle.LanguageTags()) {
		return bundle.LanguageTags()[i].String()
	}

	return defaultLang.String()
}
