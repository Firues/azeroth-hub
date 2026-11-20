/*
	The vocabulary of the panel: the tables that are NOT text.

	The words moved to lib/i18n/<lang>.ts when the panel learned languages -
	this file keeps only what every language shares, and it keeps it for the
	same reason as before: each of these tables is the single place that
	decides one piece of behaviour.

	SERVICE_NAME stays here too. "Worldserver" is what the file is called in
	every language - the technical block shows the file's own name, and a
	file name is not translated.
*/

export type Route = 'server' | 'backups' | 'more'

// The file's own name, for the technical block.
export const SERVICE_NAME: Record<string, string> = {
	mysql: 'MySQL',
	auth: 'Authserver',
	world: 'Worldserver'
}

// A service in one of these states has settled: what it reports now is the
// truth about it, so a failing check is a real problem worth announcing. A
// service that is stopped or starting is a different case - the port of a
// worldserver you deliberately stopped is supposed to be closed - and
// reporting that as "attention needed" would train the user to ignore the
// badge, which is worse than not having one.
export const SETTLED: Record<string, boolean> = {
	running: true,
	external: true,
	failed: true
}

// Which service each check belongs to. null means "belongs to the repack
// itself", so it is always judged on its own. An id that is not listed here
// is treated the same way, which is the safe default: an unknown check that
// fails is reported rather than swallowed.
export const CHECK_OWNER: Record<string, string | null> = {
	repack: null,
	'mysql.process': 'mysql',
	'mysql.port': 'mysql',
	'mysql.ping': 'mysql',
	'auth.port': 'auth',
	'world.process': 'world'
}

// Which check ids have an "and here is what to do" sentence in the
// dictionaries. A check without one shows its failure and stops there, rather
// than showing the raw key a missing translation would fall back to.
export const HAS_ADVICE: Record<string, boolean> = {
	repack: true,
	'mysql.process': true,
	'mysql.port': true,
	'mysql.ping': true,
	'auth.port': true,
	'world.process': true
}

// Which row buttons make sense in which state. Start on a service that is
// already up does nothing but produce "уже запущен - ничего не делаю", and a
// button that looks live but cannot act is worse than a greyed one: the user
// clicks it, waits, and learns nothing. This table decides, and it is the only
// place that decides.
export const CAN: Record<string, Record<string, boolean>> = {
	start: { stopped: true, failed: true },
	stop: { running: true, external: true, starting: true, failed: true },
	restart: { running: true, external: true, failed: true }
}

// The kind the panel can put back by itself. The others restore through the
// repack's own script, which takes the newest folder it finds rather than the
// one that was asked for - so no button is offered on them.
export const RESTORABLE = 'physical'

// The list of characters can run to a couple of thousand rows and the DOM is
// not free. The search still looks at every one of them - only the painting is
// capped, and the count above the list says how many matched.
export const RENDER_MAX = 400
