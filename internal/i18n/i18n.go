// Package i18n holds every user-visible string the backend can put on screen.
//
// It exists for the same reason frontend/src/lib/words.ts does: a sentence
// about the server is written once, in one place, and nowhere else. The
// difference is the direction of the lookup - the backend assembles sentences
// (the checklist, health details, error paths) and therefore has to pick the
// words itself, before the string crosses into the page.
//
// The rules:
//
//   - Russian is the source of truth. Its dictionary entries are the exact
//     strings that used to be inline in the code, byte for byte, so tests that
//     pin UI text keep working under Set(Ru).
//   - Every other language is a translation. A key missing from a language
//     falls back to English, and a key missing there returns the key itself -
//     a visible key beats a silent empty string.
//   - Language is a process-wide runtime choice, not a build tag: Set() is
//     cheap and safe from any goroutine, so the panel can switch language
//     while it runs.
//
// Strings that only ever reach the file log (logf, logger.Info/Error) stay
// inline in their files on purpose: they are for whoever debugs the panel,
// and translating them adds churn without a reader.
package i18n

import (
	"fmt"
	"sync/atomic"
)

// The languages the panel ships. Adding one means: a dictionary file in this
// package, an entry here, a dictionary file in frontend/src/lib/i18n, an
// entry in the frontend locale list - and nothing else.
const (
	En = "en"
	Ru = "ru"
	Zh = "zh"
	Es = "es"
	Pt = "pt"
	Fr = "fr"
	De = "de"
)

// Languages is the full set, in the order the language picker lists them:
// English first (the public default), Russian next (the author's), then the
// others by how many people speak them.
var Languages = []string{En, Ru, Zh, Es, Pt, Fr, De}

// Names is each language's own name for itself, for pickers. It is not
// translated on purpose: a person finds their language in it, not in the
// language they are currently reading.
var Names = map[string]string{
	En: "English",
	Ru: "Русский",
	Zh: "简体中文",
	Es: "Español",
	Pt: "Português",
	Fr: "Français",
	De: "Deutsch",
}

// dicts is filled by each dictionary file's init().
var dicts = map[string]map[string]string{}

func register(lang string, d map[string]string) { dicts[lang] = d }

func known(lang string) bool { _, ok := dicts[lang]; return ok }

// Known reports whether lang is a code this binary can actually speak.
func Known(lang string) bool { return known(lang) }

var current atomic.Value // holds string

// Set switches the language. An unknown code is ignored rather than trusted:
// a settings file edited by hand must not leave the panel saying nothing.
func Set(lang string) {
	if known(lang) {
		current.Store(lang)
	}
}

// L is the current language code.
func L() string {
	if v, ok := current.Load().(string); ok && v != "" {
		return v
	}
	return En
}

// Init applies the saved choice, or detects one from the OS for a fresh
// install. An empty or unknown saved value means "no decision yet", which is
// exactly the case detection exists for.
func Init(saved string) {
	if known(saved) {
		Set(saved)
		return
	}
	Set(Detect())
}

// T returns the translation of key in the current language.
func T(key string) string {
	lang := L()
	if s, ok := dicts[lang][key]; ok {
		return s
	}
	if s, ok := dicts[En][key]; ok {
		return s
	}
	return key
}

// Tf is T with fmt arguments. With no arguments the text is returned as
// written - Sprintf would mangle a stray percent sign in a sentence that
// has no placeholders.
func Tf(key string, args ...any) string {
	lang := L()
	if s, ok := dicts[lang][key]; ok {
		if len(args) == 0 {
			return s
		}
		return fmt.Sprintf(s, args...)
	}
	if s, ok := dicts[En][key]; ok {
		if len(args) == 0 {
			return s
		}
		return fmt.Sprintf(s, args...)
	}
	return key
}

// cat returns the plural category of n for lang. Only the categories these
// sentences actually use are distinguished: Russian needs one/few/many, English
// and the Romance/Germanic languages need one/other, Chinese needs neither.
func cat(lang string, n int) string {
	switch lang {
	case Ru:
		if n%100 >= 11 && n%100 <= 14 {
			return "many"
		}
		switch n % 10 {
		case 1:
			return "one"
		case 2, 3, 4:
			return "few"
		}
		return "many"
	case Zh:
		return "other"
	default: // en, de, es, pt, fr
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

// P renders "n <noun>" with the plural form the current language wants.
//
// The dictionary must hold the forms under key.one, key.few, key.many,
// key.other. A language that lacks a category falls back to its other form:
// Russian has no other, English has no few. "1 копий" reads as a bug to a
// native speaker of any of these languages, which is the whole point.
func P(n int, key string) string {
	lang := L()
	c := cat(lang, n)
	for _, k := range []string{key + "." + c, key + ".other", key + ".one"} {
		if s, ok := dicts[lang][k]; ok {
			return fmt.Sprintf("%d %s", n, s)
		}
	}
	for _, k := range []string{key + "." + c, key + ".other", key + ".one"} {
		if s, ok := dicts[En][k]; ok {
			return fmt.Sprintf("%d %s", n, s)
		}
	}
	return fmt.Sprintf("%d %s", n, key)
}

// DecimalSep is the decimal mark a person of the current language expects:
// a comma everywhere here except English. Go's %f always writes a dot and
// knows nothing of locale, so the formatter swaps it by hand - the frontend
// does the same in bytes().
func DecimalSep() string {
	if L() == En {
		return "."
	}
	return ","
}
