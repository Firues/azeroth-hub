/*
	Numbers and timestamps as a person reads them — in the language the panel
	is speaking.

	Three rules that are not stylistic:

	- Plural forms belong to the language, and "1 проверок" reads as a bug to a
	  native speaker. The categories come from Intl.PluralRules; the words come
	  from the dictionary, through form() and P() in i18n.svelte.ts.
	- A value that was never read is a dash in muted type, never a zero. A zero
	  that means "I could not look" is the most misleading thing a status
	  screen can show, so the caller passes null and gets '—' back.
	- The byte units are a convention of the language, not a translation: this
	  table is where they live, because a formatter is the one place that knows
	  which convention its reader expects.
*/

import { i18n, localeOf } from './i18n.svelte.js'

// Byte units by language. Most languages keep the Latin KB/MB/GB; Russian
// writes КБ/МБ/ГБ, and French o-based units go by Ko/Mo/Go/To.
const BYTE_UNITS: Record<string, string[]> = {
	en: ['B', 'KB', 'MB', 'GB', 'TB'],
	zh: ['B', 'KB', 'MB', 'GB', 'TB'],
	es: ['B', 'KB', 'MB', 'GB', 'TB'],
	pt: ['B', 'KB', 'MB', 'GB', 'TB'],
	de: ['B', 'KB', 'MB', 'GB', 'TB'],
	ru: ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'],
	fr: ['o', 'Ko', 'Mo', 'Go', 'To']
}

// Time units by language, for the short "105,3 с" spellings.
const TIME_UNITS: Record<string, { s: string; m: string; h: string }> = {
	en: { s: 's', m: 'min', h: 'h' },
	zh: { s: '秒', m: '分', h: '小时' },
	es: { s: 's', m: 'min', h: 'h' },
	pt: { s: 's', m: 'min', h: 'h' },
	de: { s: 's', m: 'min', h: 'h' },
	ru: { s: 'с', m: 'мин', h: 'ч' },
	fr: { s: 's', m: 'min', h: 'h' }
}

// Bytes in the unit a person would say out loud. The step is 1024, and the
// unit is chosen so the number stays between 1 and 1023: "0,98 ГБ" and
// "1003 МБ" are the same size written two ways, and comparing them by eye is
// the only thing this figure is for.
export function bytes(n: number | null | undefined): string {
	const v = Number(n) || 0
	if (v <= 0) return '—'
	const units = BYTE_UNITS[i18n.lang] ?? BYTE_UNITS.en
	let i = 0
	let x = v
	while (x >= 1024 && i < units.length - 1) {
		x /= 1024
		i++
	}
	// toFixed writes a dot; swap in the separator the language wants, the same
	// manual swap the backend's humanBytes makes.
	const sep = i18n.lang === 'en' ? '.' : ','
	const digits = x < 10 && i > 0 ? 1 : 0
	return x.toFixed(digits).replace('.', sep) + ' ' + units[i]
}

export function num(n: number | null | undefined): string {
	const v = Number(n)
	if (!isFinite(v)) return '—'
	return new Intl.NumberFormat(localeOf(i18n.lang)).format(v)
}

// Seconds as a person reads them. A full copy is about a minute and three
// quarters, and "105,3 с" is a number nobody converts in their head.
export function duration(sec: number | null | undefined): string {
	const v = Math.max(0, Math.round(Number(sec) || 0))
	const u = TIME_UNITS[i18n.lang] ?? TIME_UNITS.en
	if (v < 60) return v + ' ' + u.s
	const m = Math.floor(v / 60)
	const s = v % 60
	if (m < 60) return s ? `${m} ${u.m} ${s} ${u.s}` : `${m} ${u.m}`
	return `${Math.floor(m / 60)} ${u.h} ${m % 60} ${u.m}`
}

// A timestamp from the backend, or '' for "never": Go sends the zero time for
// that, and "01.01.0001" on screen reads as a bug rather than as an answer.
export function stamp(t: unknown): string {
	const s = String(t || '')
	if (!s || s.startsWith('0001-01-01')) return ''
	const d = new Date(s)
	if (isNaN(d.getTime())) return ''
	return d.toLocaleString(localeOf(i18n.lang), {
		day: '2-digit',
		month: '2-digit',
		year: 'numeric',
		hour: '2-digit',
		minute: '2-digit'
	})
}

export function timeOnly(t: unknown): string {
	const s = String(t || '')
	if (!s || s.startsWith('0001-01-01')) return ''
	const d = new Date(s)
	if (isNaN(d.getTime())) return ''
	return d.toLocaleString(localeOf(i18n.lang), {
		day: '2-digit',
		month: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	})
}

// A backend detail can run to a paragraph - the MySQL ping failure explains
// the whole port-versus-protocol distinction - and a two-line banner helps
// nobody. The full text is one click away.
export function shorten(text: unknown, max: number): string {
	const t = String(text || '')
		.replace(/\s+/g, ' ')
		.trim()
	if (t.length <= max) return t
	return t.slice(0, max - 1).replace(/[\s,;:.]+$/, '') + '…'
}
