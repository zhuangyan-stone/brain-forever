package i18n

import "fmt"

// TLFile wraps a translation file name and provides localized access
// to messages within that file. Each message ID is automatically prefixed
// with the file name (e.g., "current_time-description") to avoid key
// collisions across different tool files.
type TLFile struct {
	// File is the base name (without extension) of the .toml file,
	// e.g., "current_time", "web_search", "traits_extract".
	File string
}

// T returns the localized string for the given message ID using the default language.
// It automatically prefixes the messageID with the file name.
func (f *TLFile) T(messageID string, templateData ...map[string]interface{}) string {
	return TL(defaultLang.String(), f.File+"-"+messageID, templateData...)
}

// TL returns the localized string for the given message ID in the specified language.
// It automatically prefixes the messageID with the file name.
func (f *TLFile) TL(lang, messageID string, templateData ...map[string]interface{}) string {
	return TL(lang, f.File+"-"+messageID, templateData...)
}

// MustLocalize is like TL but panics if the message ID is not found.
func (f *TLFile) MustLocalize(lang, messageID string, templateData ...map[string]interface{}) string {
	return MustLocalize(lang, f.File+"-"+messageID, templateData...)
}

// ============================================================
// TLTools -unified manager for all tool translation files
// ============================================================

// TLTools manages TLFile instances for all tool translation files.
// Tools are organized in the tools/ subdirectory (e.g., lang/en/tools/).
// Each tool's .toml file name (without extension) serves as the tool name.
type TLTools struct {
	tools map[string]*TLFile
}

// NewTLTools creates a TLTools instance and registers all known tools.
// The toolNames parameter lists all tool file base names (without .toml extension).
func NewTLTools(toolNames ...string) *TLTools {
	t := &TLTools{
		tools: make(map[string]*TLFile, len(toolNames)),
	}
	for _, name := range toolNames {
		t.tools[name] = &TLFile{File: name}
	}
	return t
}

// TL returns the localized string for the given tool's message ID
// in the specified language. It automatically prefixes the messageID
// with the tool name.
//
// Example:
//
//	i18n.Tools.TL("zh-CN", "current_time", "description")
func (t *TLTools) TL(lang, toolName, messageID string, templateData ...map[string]interface{}) string {
	tf, ok := t.tools[toolName]
	if !ok {
		return fmt.Sprintf("[unknown tool: %s]", toolName)
	}
	return tf.TL(lang, messageID, templateData...)
}

// T is like TL but uses the default language.
func (t *TLTools) T(toolName, messageID string, templateData ...map[string]interface{}) string {
	return t.TL(defaultLang.String(), toolName, messageID, templateData...)
}

// GetTool returns the TLFile for the given tool name.
// Returns nil if the tool is not registered.
func (t *TLTools) GetTool(toolName string) *TLFile {
	return t.tools[toolName]
}

// ============================================================
// Predefined instances
// ============================================================

// SystemPrompt provides access to system_prompt.toml messages.
var SystemPrompt = &TLFile{File: "system_prompt"}

// Tools is the global manager for all tool translation files.
// Tools are registered here; add new tools to the list as they are created.
var Tools = NewTLTools(
	"current_time",
	"web_search",
	"traits_extract",
	"trip_traits",
	"personal_trait_search",
)
