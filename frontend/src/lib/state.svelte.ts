/*
	The state of the panel, in one place.

	Everything arrives by event. Nothing polls from here: the backend ticks once
	a second and emits only when something actually changed, so a repaint means
	there is something new to show.

	There is exactly one of these objects. Screens and components read it and
	call methods on it, so a value cannot be painted in two places with two
	drawings of it - which is how the six-screen version of this panel drifted
	apart in the first place.
*/

import * as api from './api.js'
import { t, i18n, type Lang } from './i18n.svelte.js'
import { bytes, duration } from './format.js'
import { CAN, CHECK_OWNER, RESTORABLE, SERVICE_NAME, SETTLED, type Route } from './words.js'

/**
 * How many checks the backend reports. Six: repack in place, three for MySQL,
 * two for the services. It also builds the list in one pass, so a shorter list
 * means "not judged yet" - see Hub.checksSettled.
 *
 * Duplicated from CHECK_OWNER on purpose, and the two must agree: a check in
 * CHECK_OWNER with no row to judge would be counted as a real failure the
 * moment the backend stops sending it. e2e asserts that.
 */
const EXPECTED_CHECKS = 6

// How long a read may take before the screen says so. Long enough that the
// usual warm read finishes first and nothing is printed, short enough that a
// cold one is announced before anyone wonders whether it hung.
const PENDING_NOTICE_MS = 700

export class Hub {
	services = $state<api.supervisor.ServiceStatus[]>([])
	checks = $state<api.health.Check[]>([])
	setup = $state<api.main.SetupItem[]>([])
	data = $state<api.main.DataSnapshot | null>(null)
	backup = $state<api.main.BackupStatus | null>(null)
	schedule = $state<api.main.ScheduleState | null>(null)

	characters = $state<api.realmdb.Characters | null>(null)
	accounts = $state<api.realmdb.Accounts | null>(null)
	charQuery = $state('')
	accountQuery = $state('')

	route = $state<Route>('server')
	last = $state<{ text: string; level: 'ok' | 'warn' | 'error' }>({ text: '—', level: 'ok' })

	// The two dialogs the backend is waiting on: both are closed by the event
	// that ends the wait. restorePoint and deletePoint are not that - the page
	// asks, and the answer goes back in a call it makes itself.
	confirmKill = $state<{ service: string; pid: number } | null>(null)
	restorePoint = $state<string | null>(null)
	deletePoint = $state<string | null>(null)

	// Every operation button goes dead while one is running, not just the ones
	// in the row that was clicked. A start takes three minutes and a
	// live-looking "Запустить мир" is an invitation to click it twice.
	// A counter rather than a boolean: two operations can overlap.
	busy = $state(0)
	loadingChars = $state(false)
	loadingAccounts = $state(false)

	// Guards against a second read while one is in flight, and remembers that a
	// trigger which arrived meanwhile still owes an answer.
	#charsBusy = false
	#charsAgain = false
	#accountsBusy = false
	#accountsAgain = false

	#setupSig = ''

	/* ------------------------------ derived ------------------------------ */

	get realm() {
		return this.data?.Realm ?? null
	}

	get points() {
		return this.data?.Backups?.Points ?? []
	}

	get backupsErr() {
		return this.data?.Backups?.Err ?? ''
	}

	get loaded() {
		return !!this.data?.Loaded
	}

	get realmRunning() {
		const world = this.stateOf('world')
		return world === 'running' || world === 'external'
	}

	/** Is a service up, whether this panel started it or not. */
	stateOf(id: string): string {
		const svc = this.services.find((s) => String(s.ID || '') === id)
		return svc ? String(svc.State || 'stopped') : 'stopped'
	}

	isUp(id: string): boolean {
		const st = this.stateOf(id)
		return st === 'running' || st === 'external'
	}

	get mysqlUp() {
		return this.isUp('mysql')
	}

	/** Names of the services still up, for "сначала остановите: …". */
	get upNames(): string[] {
		return this.services
			.filter((s) => s.State === 'running' || s.State === 'external')
			.map((s) => SERVICE_NAME[String(s.ID)] || String(s.ID))
	}

	get total() {
		return this.services.length
	}

	get upCount() {
		return this.services.filter((s) => s.State === 'running' || s.State === 'external').length
	}

	/**
	 * Has the first read landed at all.
	 *
	 * Null until it has. Every "nothing here yet" question has this as its
	 * answer, because a label that says "нет данных" before the first read is
	 * not true - nothing was looked at.
	 */
	get dataLanded() {
		return this.data !== null
	}

	/**
	 * Should the screen say it is still reading.
	 *
	 * Only once the wait has been long enough to be worth reading. A cold read
	 * of the schema sizes takes about ten seconds and a person absolutely needs
	 * to be told; a warm one takes a few hundred milliseconds, and printing
	 * "данные читаются…" for that long is a label nobody manages to read and
	 * everybody manages to see change. So the notice is delayed, and on the fast
	 * path it never appears at all - which is the same rule the badge follows,
	 * one mechanism for both: nothing that appears and is immediately replaced.
	 */
	dataPending = $state(false)

	#pendingNotice: ReturnType<typeof setTimeout> | null = null

	#armPendingNotice() {
		if (this.#pendingNotice) clearTimeout(this.#pendingNotice)
		this.#pendingNotice = setTimeout(() => {
			this.dataPending = !this.loaded
			this.#pendingNotice = null
		}, PENDING_NOTICE_MS)
	}

	#disarmPendingNotice() {
		if (this.#pendingNotice) {
			clearTimeout(this.#pendingNotice)
			this.#pendingNotice = null
		}
		this.dataPending = false
	}

	get moving() {
		return this.services.some((s) => s.State === 'starting' || s.State === 'stopping')
	}

	get failed() {
		return this.services.some((s) => s.State === 'failed')
	}

	/** The one line the hero says. A crash outranks a transition: it needs a person. */
	get headline(): { level: 'ok' | 'warn' | 'bad' | 'off'; text: string } {
		if (this.total === 0) return { level: 'off', text: t('hero.reading') }
		if (this.failed) return { level: 'bad', text: t('hero.failed') }
		if (this.moving) return { level: 'warn', text: t('hero.starting') }
		if (this.upCount === this.total) return { level: 'ok', text: t('hero.ok') }
		if (this.upCount > 0) return { level: 'warn', text: t('hero.partial') }
		return { level: 'off', text: t('hero.off') }
	}

	/**
	 * Is nobody waiting for the world right now?
	 *
	 * True when everything is off, nothing is on its way and nothing crashed -
	 * that is, the person stopped it themselves and can start it again with the
	 * button above. This is what separates "there is nothing to do" from "there
	 * is something to do, and it is not your fault".
	 */
	get worldIdle() {
		return this.total > 0 && this.upCount === 0 && !this.moving && !this.failed
	}

	/**
	 * Can the databases be read right now.
	 *
	 * Everything below the hero depends on this one fact. When MySQL is off, a
	 * read failure is not a fault to report: the stop ladder takes MySQL down
	 * after the world, so a person who pressed "Сервер выключен" has just made
	 * the database unreadable on purpose, and telling them so is the screen
	 * complaining about its own effect.
	 *
	 * A crashed service is exempt. "MySQL упал" while the rest is off is
	 * something nobody asked for and it does need a person.
	 */
	get dataReadable() {
		return this.mysqlUp || this.failed
	}

	/**
	 * What the hero says needs doing.
	 *
	 * Only problems a person did not cause by reading this screen.
	 *
	 * - "server" is not on the list: the button above already offers to start
	 *   it, and repeating it teaches the box to be ignored.
	 * - "database" is not on the list while MySQL is off for the same reason.
	 *   The stop ladder is the cause, not a fault.
	 * - The rest (repack in place, copy is fresh, room on the disk) are read
	 *   from folders, not from the database, so they stay true while the world
	 *   is off and stay worth reporting.
	 */
	get problems() {
		return this.setup.filter((s) => {
			if (s.OK) return false
			if (s.Key === 'server' && !this.moving) return false
			if (s.Key === 'database' && !this.dataReadable) return false
			return true
		})
	}

	/** Failing checks worth announcing, given what the services are doing right now. */
	get attention() {
		return this.checks.filter((c) => {
			if (c.OK) return false
			const owner = CHECK_OWNER[c.ID]
			if (!owner) return true
			return !!SETTLED[this.stateOf(owner)]
		})
	}

	can(act: 'start' | 'stop' | 'restart', id: string): boolean {
		return !!CAN[act][this.stateOf(id)]
	}

	get canStartAll() {
		// An empty snapshot means the first Status() has not landed yet, or
		// failed. Then everything stays enabled: refusing to act because the
		// panel cannot read its own state would be the worst failure mode.
		if (!this.services.length) return true
		return this.services.some((s) => CAN.start[s.State])
	}

	get canStopAll() {
		if (!this.services.length) return true
		return this.services.some((s) => CAN.stop[s.State])
	}

	get restoring() {
		return !!this.backup?.Running && this.backup?.Kind === 'restore'
	}

	/** Copy buttons and restore buttons follow this, and nothing else does. */
	get backupLocked() {
		return this.busy > 0 || !!this.backup?.Running
	}

	get backupProgress(): number {
		const st = this.backup
		if (!st?.Running) return 0
		const total = Number(st.Total) || 0
		const index = Number(st.Index) || 0
		return total > 0 ? Math.round((index / total) * 100) : 0
	}

	get charList() {
		const all = this.characters?.List ?? []
		const q = this.charQuery.trim().toLowerCase()
		if (!q) return all
		return all.filter(
			(c) =>
				String(c.Name || '').toLowerCase().includes(q) ||
				String(c.Account || '').toLowerCase().includes(q)
		)
	}

	get accountList() {
		const all = this.accounts?.List ?? []
		const q = this.accountQuery.trim().toLowerCase()
		if (!q) return all
		return all.filter(
			(a) =>
				String(a.Username || '').toLowerCase().includes(q) ||
				String(a.LastIP || '').toLowerCase().includes(q)
		)
	}

	/** Why a row may or may not be put back by a button. */
	isRestorable(kind: string) {
		return kind === RESTORABLE
	}

	/* ------------------------------ reporting ---------------------------- */

	say(text: string, level: 'ok' | 'warn' | 'error' = 'ok') {
		this.last = { text, level }
	}

	/** Every call to the backend goes through here and reports into the strip. */
	async run(fn: () => Promise<unknown>, what: string) {
		this.busy++
		this.say(what + t('act.pending'), 'warn')
		try {
			await fn()
			this.say(what + t('act.done'), 'ok')
		} catch (err) {
			this.say(what + t('act.error', { err: msg(err) }), 'error')
		} finally {
			this.busy--
		}
	}

	startAll() {
		return this.run(() => api.StartAll(), t('act.startAll'))
	}

	stopAll() {
		return this.run(() => api.StopAll(), t('act.stopAll'))
	}

	/* ------------------------- closing the panel ------------------------- */

	/**
	 * Set while the close window is up. See components/QuitAsk.svelte.
	 *
	 * Not a piece of status and not a decision anybody else may make: it is
	 * the answer to one click on one button, and it means nothing once the
	 * panel is gone.
	 */
	askQuit = $state(false)

	/**
	 * The close button.
	 *
	 * Anything running is worth asking about, not only the world. A world
	 * outlives the panel - it survives it (WinOps.Start hands it a pipe that
	 * cannot reach end of stream), and a world left behind cannot be stopped
	 * by the next panel, only killed - so the question is asked while the
	 * pipe is still held, when "server shutdown 1" can still reach it and the
	 * save pass still runs.
	 *
	 * One of three counts. MySQL and the login server are up almost whenever
	 * the world is, and a panel that quits over their heads leaves a database
	 * writing to nothing after a machine that had no business being shut down
	 * that way. What is running is named in the window, so the question is
	 * about the services that are actually there.
	 *
	 * Nothing running, nothing to ask.
	 */
	closeWindow() {
		if (this.upCount > 0) {
			this.askQuit = true
			return
		}
		api.Quit()
	}

	/**
	 * "Остановить и выйти": the stack goes down in order, and only then does
	 * the panel.
	 *
	 * A failed stop does NOT quit. It means the world refused to die, the
	 * panel is still the thing that can see that, and leaving now would put
	 * the person in front of a stopped panel and a live server.
	 */
	async stopAndQuit() {
		this.busy++
		this.say(t('quit.act.stopgo'), 'warn')
		try {
			await api.StopAll()
			await api.Quit()
		} catch (err) {
			this.say(t('quit.act.stopfail', { err: msg(err) }), 'error')
		} finally {
			this.busy--
		}
	}

	startService(id: string) {
		return this.run(() => api.Start(id), t('act.startSvc', { name: SERVICE_NAME[id] || id }))
	}

	stopService(id: string) {
		return this.run(() => api.Stop(id), t('act.stopSvc', { name: SERVICE_NAME[id] || id }))
	}

	restartService(id: string) {
		return this.run(() => api.Restart(id), t('act.restartSvc', { name: SERVICE_NAME[id] || id }))
	}

	// Opening a folder is not an operation on the server, so it must not grey
	// out the page even for a moment, and its failure is not a service failure.
	// The page never gets to name a directory - only this whitelist key goes
	// over the wire - so there is no string here that could be made to point
	// somewhere it should not.
	async openFolder(which: string, label: string) {
		try {
			await api.OpenFolder(which)
			this.say(t('folder.opened', { label }), 'ok')
		} catch (err) {
			this.say(label + ' — ' + msg(err), 'error')
		}
	}

	/* -------------------------------- копии ------------------------------ */

	/**
	 * Not this.run(): that would grey out the whole page for two minutes and
	 * write "готово" into a line that has nothing to do with it. A copy has its
	 * own progress box, its own cancel and its own outcome.
	 *
	 * A refusal from the backend is a real answer and belongs next to the
	 * buttons that caused it, not only in the strip at the bottom.
	 */
	async createBackup(kind: string) {
		try {
			await api.CreateBackup(kind)
		} catch (err) {
			this.say(t('backup.act.createFail', { err: msg(err) }), 'error')
			this.backupOutcome = { text: msg(err), level: 'error' }
		}
	}

	// The outcome of the last attempt, kept on screen afterwards: it is the
	// only place that says where the folder went. Set by renderBackupOutcome.
	backupOutcome = $state<{ text: string; level: 'ok' | 'error' } | null>(null)

	cancelBackup() {
		// Not this.run(): the backend answers a cancel with nothing to cancel
		// as a no-op, and a button that dies the instant the last dump
		// finishes would refuse the click already on its way.
		return api.CancelBackup()
	}

	/** Called after the rollback dialog was answered yes. */
	async restoreSnapshot(point: string) {
		this.restorePoint = null
		this.backupOutcome = null
		try {
			await api.RestoreSnapshot(point)
		} catch (err) {
			this.say(t('backup.act.restoreFail', { err: msg(err) }), 'error')
		}
	}

	/** Called after the delete dialog was answered yes. */
	async deleteBackup(point: string, size: number) {
		this.deletePoint = null
		try {
			await api.DeleteBackup(point)
			this.say(t('backup.act.deleted', { point, size: bytes(size) }), 'ok')
		} catch (err) {
			this.say(t('backup.act.deleteFail', { err: msg(err) }), 'error')
		}
	}

	answerConfirm(ok: boolean) {
		this.confirmKill = null
		return api.RespondConfirm(ok)
	}

	/* ------------------------------ напоминание -------------------------- */

	/**
	 * AutoRun is sent back from the last snapshot rather than as false. It has
	 * no checkbox on screen any more, and a setting turned on elsewhere must
	 * not be switched off by the control disappearing.
	 */
	saveSchedule(enabled: boolean, everyDays: number) {
		return this.run(
			() => api.SetSchedule(enabled, everyDays, !!this.schedule?.AutoRun),
			t('sched.act.saved')
		)
	}

	/* -------------------------------- данные ----------------------------- */

	refreshData() {
		return api.RefreshData()
	}

	async loadSetup() {
		try {
			this.setup = (await api.Setup()) || []
		} catch (err) {
			this.say(t('err.checklist', { err: msg(err) }), 'error')
		}
	}

	/** One read at a time; a trigger that arrives meanwhile is answered after. */
	async loadCharacters() {
		if (this.#charsBusy) {
			this.#charsAgain = true
			return
		}
		this.#charsBusy = true
		this.loadingChars = true
		try {
			this.characters = await api.Characters(0)
		} catch (err) {
			this.characters = { List: [], Total: 0, Err: msg(err) } as api.realmdb.Characters
		} finally {
			this.#charsBusy = false
			this.loadingChars = false
		}
		if (this.#charsAgain) {
			this.#charsAgain = false
			this.loadCharacters()
		}
	}

	async loadAccounts() {
		if (this.#accountsBusy) {
			this.#accountsAgain = true
			return
		}
		this.#accountsBusy = true
		this.loadingAccounts = true
		try {
			this.accounts = await api.Accounts(0)
		} catch (err) {
			this.accounts = { List: [], Total: 0, Err: msg(err) } as api.realmdb.Accounts
		} finally {
			this.#accountsBusy = false
			this.loadingAccounts = false
		}
		if (this.#accountsAgain) {
			this.#accountsAgain = false
			this.loadAccounts()
		}
	}

	/* -------------------------------- запуск ----------------------------- */

	/**
	 * Subscribing comes last and cannot be allowed to take the rest of the page
	 * down with it: a failure here would leave the interface showing stale state
	 * forever, with the buttons working and no explanation anywhere.
	 */
	async start() {
		// The language is the backend's decision (saved setting, or detected
		// from the OS on a fresh install). Asked before anything subscribes,
		// so the first screenful already speaks it; a failure here keeps the
		// placeholder, which is readable in one language rather than in none.
		try {
			i18n.set((await api.Language()) as Lang)
		} catch {
			/* the picker on More is the way back */
		}

		window.addEventListener('error', (e) =>
			this.say(t('err.ui', { m: e.message || t('err.unknown') }), 'error')
		)
		window.addEventListener('unhandledrejection', (e) =>
			this.say(t('err.generic', { m: msg(e.reason) }), 'error')
		)

		try {
			api.EventsOn('status', (d) => {
				this.services = (d?.Services as api.supervisor.ServiceStatus[]) || []
				// The checklist answers "is the realm playable now", and one of
				// its items is the server state, so it is re-read when the
				// services move. A signature rather than a deep compare: the
				// only thing that can invalidate it is a service changing
				// state, which is exactly what it encodes.
				const sig = this.services.map((s) => s.ID + ':' + s.State).join('|')
				if (sig !== this.#setupSig) {
					this.#setupSig = sig
					this.loadSetup()
				}
			})
			api.EventsOn('health', (d) => (this.checks = (d as api.health.Check[]) || []))
			api.EventsOn('data', (d) => {
				this.data = (d as api.main.DataSnapshot) ?? null
				// The read that mattered has landed, so the notice has done its
				// job. A snapshot that came back unreadable leaves it armed: the
				// refresh is still in flight and there is a reason to wait.
				if (this.loaded) this.#disarmPendingNotice()
				// Both of these read the snapshot this pass just stored, so
				// they cannot show a figure the tiles disagree with.
				this.loadSetup()
			})
			api.EventsOn('backup', (d) => this.onBackup(d as api.main.BackupStatus | null))
			api.EventsOn('schedule', (d) => (this.schedule = (d as api.main.ScheduleState) ?? null))
			api.EventsOn('operation', (d) =>
				this.say((d?.text as string) || '', ((d?.level as 'ok' | 'warn' | 'error') || 'ok'))
			)
			api.EventsOn('confirm-kill', (d) => {
				this.confirmKill = { service: String(d?.service || ''), pid: Number(d?.pid) || 0 }
			})
			// The backend gives up on a dialog nobody answers after its own
			// timeout and says so. Closing it here keeps the page from showing a
			// question that is no longer being listened to - clicking "да" on
			// that would silently do nothing.
			api.EventsOn('confirm-cancel', () => (this.confirmKill = null))
		} catch (err) {
			this.say(t('err.subscribe', { err: msg(err) }), 'error')
		}

		// Read once immediately. The status loop emits only on a change, so on a
		// freshly opened window there is nothing yet to hear. Each read is a
		// separate attempt, so a failure in one does not take the others down.
		//
		// The snapshot goes first and is not awaited in line: it is the read the
		// whole screen waits on, and queueing it behind Health() put a "данные
		// читаются…" on screen for as long as the checks took, on every start.
		// Health folds into the status loop and includes a bounded MySQL ping, so
		// that is the slowest of the bunch and the least related to the numbers.
		const dataRead = (async () => {
			this.#armPendingNotice()
			try {
				// The cached snapshot paints at once, and on a warm cache it
				// already has real numbers; RefreshData then asks for a fresh
				// read, which arrives as a 'data' event. On a cold cache Loaded
				// is false and the screens say they are loading rather than
				// claiming the realm is empty - which is the whole reason that
				// flag exists.
				this.data = await api.Data()
				api.RefreshData()
				} catch (err) {
					this.#disarmPendingNotice()
					this.say(t('err.data', { err: msg(err) }), 'error')
				}
			})()

			try {
				this.services = ((await api.Status())?.Services as api.supervisor.ServiceStatus[]) || []
			} catch (err) {
				this.say(t('err.status', { err: msg(err) }), 'error')
			}
		try {
			this.checks = (await api.Health()) || []
		} catch {
			/* the status loop emits health on its own schedule */
		}
		try {
			await this.loadSetup()
			this.onBackup(await api.BackupStatus())
			this.schedule = await api.Schedule()
		} catch (err) {
			this.say(t('err.backupstate', { err: msg(err) }), 'error')
		}

		await dataRead
	}

	/**
	 * The outcome box. A restore says something different from a backup on
	 * success: what is worth reading is not "how big was the copy" but "where
	 * is the state that was replaced".
	 */
	#onBackupOutcome(st: api.main.BackupStatus) {
		if (!st.At || st.Running) {
			this.backupOutcome = null
			return
		}

		if (st.Err) {
			this.backupOutcome = {
				text:
					st.Kind === 'restore'
						? t('backup.act.restoreFail', { err: st.Err })
						: t('backup.act.createFail', { err: st.Err }),
				level: 'error'
			}
			return
		}

		const where = st.Dir ? t('outcome.folder', { dir: st.Dir }) : ''
		const warn = st.Warnings?.length ? t('outcome.warn', { w: st.Warnings.join(' ') }) : ''

		this.backupOutcome =
			st.Kind === 'restore'
				? {
						text:
							t('outcome.restoreOk', {
								point: st.Point ? t('outcome.point', { name: st.Point }) : ''
							}) +
							t('outcome.restoreOk2') +
							(st.Dir ? t('outcome.restoreOk3', { dir: st.Dir }) : ''),
						level: 'ok'
					}
				: {
						text: t('outcome.backupOk', {
							size: bytes(st.SavedBytes),
							dur: duration(st.ElapsedSec),
							where
						}),
						level: 'ok'
					}
		if (warn) this.backupOutcome.text += warn
	}

	onBackup(st: api.main.BackupStatus | null) {
		this.backup = st
		if (st) this.#onBackupOutcome(st)
	}
}

function msg(err: unknown): string {
	if (err && typeof err === 'object' && 'message' in err) return String((err as Error).message)
	return String(err)
}

export const hub = new Hub()