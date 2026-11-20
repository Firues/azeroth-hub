/*
	The locale dimension for everything the page renders itself.

	The backend builds its own sentences (the checklist, the health details,
	the errors) and translates them before they cross into the page; this
	module covers the other half - the strings that live in markup and in the
	state object. One dictionary per language, one key set, the English file
	being the reference every other file is checked against at compile time.

	A missing key is a loud TypeScript error, not a silent fallback: a language
	that drifted out of sync should not build, the same rule the Go side
	enforces with its parity test.

	`.svelte.ts`, not `.ts`: the current language is a $state, so switching it
	repaints every screen that reads a translation - which is what makes the
	switcher on the More screen work without a reload.
*/

import { en } from './i18n/en.js'

export type Lang = 'en' | 'ru' | 'zh' | 'es' | 'pt' | 'fr' | 'de'

/** The picker's list, in the same order the backend advertises. */
export const LANGS: { id: Lang; name: string }[] = [
	{ id: 'en', name: 'English' },
	{ id: 'ru', name: 'Русский' },
	{ id: 'zh', name: '简体中文' },
	{ id: 'es', name: 'Español' },
	{ id: 'pt', name: 'Português' },
	{ id: 'fr', name: 'Français' },
	{ id: 'de', name: 'Deutsch' }
]

export type Key = keyof typeof en

// The dictionaries, by code. Import order matches LANGS for the same reason.
import { ru } from './i18n/ru.js'
import { zh } from './i18n/zh.js'
import { es } from './i18n/es.js'
import { pt } from './i18n/pt.js'
import { fr } from './i18n/fr.js'
import { de } from './i18n/de.js'

// The dictionaries, by code. Import order matches LANGS for the same reason.
// The index signature is what lets a language carry the plural-form sub-keys
// the English reference has no use for (ru's few/many, zh's other).
const dicts: Record<Lang, Record<string, string>> = { en, ru, zh, es, pt, fr, de }

/** The BCP-47 tag each language formats numbers and dates with. */
export function localeOf(lang: Lang): string {
	switch (lang) {
		case 'pt':
			return 'pt-BR'
		case 'zh':
			return 'zh-CN'
		default:
			return lang
	}
}

class I18n {
	// The initial value is a placeholder: Hub.start() asks the backend for the
	// real choice before the first paint that matters, and every screen is
	// rendered through t(), so the one repaint covers them all.
	lang = $state<Lang>('en')

	set(l: Lang) {
		this.lang = l
		// Keep the document's declaration in step: it decides which glyphs the
		// font stack prefers, and a wrong one makes Chinese render in the
		// Latin font's fallbacks.
		document.documentElement.lang = localeOf(l)
	}
}

export const i18n = new I18n()

function lookup(lang: Lang, key: Key): string | undefined {
	return dicts[lang][key] ?? dicts.en[key]
}

/** The translation of key in the current language, with {name} params filled. */
export function t(key: Key, params?: Record<string, string | number>): string {
	let s = lookup(i18n.lang, key) ?? String(key)
	if (params) {
		for (const [name, value] of Object.entries(params)) {
			s = s.replaceAll(`{${name}}`, String(value))
		}
	}
	return s
}

/**
 * The plural category of n in the current language, per CLDR - the same
 * categories the Go side hand-codes for the languages it ships.
 */
export function pluralCat(n: number): string {
	const rules = new Intl.PluralRules(localeOf(i18n.lang))
	return rules.select(n)
}

/**
 * The bare plural form of key for n. The dictionary holds the forms under
 * key.one, key.few, key.many, key.other; a language that lacks a category
 * falls back to its other form. The key here is the GROUP's base - the
 * member keys are never passed directly.
 */
export function form(n: number, key: string): string {
	const c = pluralCat(n)
	return (
		dicts[i18n.lang][`${key}.${c}`] ??
		dicts[i18n.lang][`${key}.other`] ??
		dicts[i18n.lang][`${key}.one`] ??
		dicts.en[`${key}.${c}`] ??
		dicts.en[`${key}.other`] ??
		dicts.en[`${key}.one`] ??
		key
	)
}

/** "n <noun>" with the plural form the current language wants. */
export function P(n: number, key: string): string {
	return `${n} ${form(n, key)}`
}

/* Thin wrappers over the key families that are addressed by a backend id
   rather than by a literal key. They keep the casts in one place. */

/** A service state, as the bottom of each row reads it. */
export function stateLabel(state: string): string {
	return t(`state.${state}` as Key)
}

/** What a service is for, as the hero and the checklist use it. */
export function svcPlain(id: string): string {
	return t(`svc.${id}.plain` as Key)
}

export function svcRole(id: string): string {
	return t(`svc.${id}.role` as Key)
}

/** The next step for a failing check, keyed by the check's backend id. */
export function checkAdvice(id: string): string {
	return t(`advice.${id}` as Key)
}

/** What a kind of copy is, in words for a person who never heard of a dump. */
export function kindLabel(kind: string): string {
	return t(`kind.${kind}` as Key)
}

/** Race and class names by their database id; unknown ids show as the number. */
export function raceName(id: number | string): string {
	return t(`race.${id}` as Key)
}

export function className(id: number | string): string {
	return t(`class.${id}` as Key)
}
