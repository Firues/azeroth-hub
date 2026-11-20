package i18n

import "testing"

// TestDictionariesParity fails a build where a language drifted out of sync
// with the reference set. A key missing from a language would silently fall
// back to English at runtime; here it is a loud compile-adjacent error.
func TestDictionariesParity(t *testing.T) {
	reference := make(map[string]bool, len(dictEn))
	for k := range dictEn {
		reference[k] = true
	}

	for _, lang := range Languages {
		d, ok := dicts[lang]
		if !ok {
			t.Fatalf("language %q has no dictionary at all", lang)
		}
		for k := range reference {
			if _, ok := d[k]; !ok {
				t.Errorf("%s: missing key %q", lang, k)
			}
		}
		for k := range d {
			if !reference[k] {
				t.Errorf("%s: extra key %q (not in the reference set)", lang, k)
			}
		}
	}
}

func TestSetIgnoresUnknown(t *testing.T) {
	Set(Ru)
	Set("xx")
	if L() != Ru {
		t.Errorf("an unknown code must not change the language, got %q", L())
	}
	Set(En)
}

func TestTAndTf(t *testing.T) {
	Set(Ru)
	if got := T("start.db.prefix"); got != "БД" {
		t.Errorf("ru: got %q", got)
	}
	if got := Tf("start.port.timeout", "world", 8085, "30s"); got != "world: порт 8085 не открылся за 30s" {
		t.Errorf("ru: got %q", got)
	}
	Set(En)
	if got := Tf("start.port.timeout", "world", 8085, "30s"); got != "world: port 8085 did not open within 30s" {
		t.Errorf("en: got %q", got)
	}
	// A key nobody translated must surface as the key itself, never as "".
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key must return itself, got %q", got)
	}
}

func TestP(t *testing.T) {
	Set(Ru)
	for _, tc := range []struct {
		n    int
		key  string
		want string
	}{
		{1, "dur.day", "1 день"},
		{2, "dur.day", "2 дня"},
		{5, "dur.day", "5 дней"},
		{11, "dur.day", "11 дней"},
		{12, "dur.day", "12 дней"},
		{21, "dur.day", "21 день"},
		{112, "dur.day", "112 дней"},
	} {
		if got := P(tc.n, tc.key); got != tc.want {
			t.Errorf("ru: P(%d, %s) = %q, want %q", tc.n, tc.key, got, tc.want)
		}
	}
	Set(En)
	for _, tc := range []struct {
		n    int
		key  string
		want string
	}{
		{1, "dur.day", "1 day"},
		{2, "dur.day", "2 days"},
		{11, "dur.day", "11 days"},
	} {
		if got := P(tc.n, tc.key); got != tc.want {
			t.Errorf("en: P(%d, %s) = %q, want %q", tc.n, tc.key, got, tc.want)
		}
	}
	Set(Zh)
	if got := P(5, "dur.day"); got != "5 天" {
		t.Errorf("zh: P(5, dur.day) = %q, want %q", got, "5 天")
	}
}

func TestDecimalSep(t *testing.T) {
	Set(En)
	if DecimalSep() != "." {
		t.Errorf("en: want a dot, got %q", DecimalSep())
	}
	Set(Ru)
	if DecimalSep() != "," {
		t.Errorf("ru: want a comma, got %q", DecimalSep())
	}
}

func TestDetectIsAKnownLanguage(t *testing.T) {
	if !known(Detect()) {
		t.Errorf("Detect() returned %q, which has no dictionary", Detect())
	}
}
