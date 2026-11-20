<!--
  Making a copy and putting one back.

  Two buttons, because there really are two kinds and the difference decides
  whether the server has to be stopped:

    "без остановки сервера" - taken on a running database, so nothing has to
    be shut down. Four files you can read yourself. Rolling the database back to
    one of these needs the repack's own script, so this row has no button.
    "полная" - the whole database in one piece, with the internal files that
    cannot be restored without them. Therefore this row can be rolled back with
    a button. Only possible with the server stopped, and the line under each
    button says whether that is true right now.
-->
<script lang="ts">
	import { Button } from '$lib/components/ui/button'
	import { Progress } from '$lib/components/ui/progress'
	import { hub } from '$lib/state.svelte.js'
	import { bytes, num, duration, stamp } from '$lib/format.js'
	import { t, P, form, kindLabel } from '$lib/i18n.svelte.js'

	// The interval the checklist on the server screen uses to decide whether
	// the newest copy is fresh, so the reminder and the checklist cannot
	// disagree about the same folder.
	let enabled = $state(false)
	let everyDays = $state(7)
	let seeded = ''

	$effect(() => {
		const s = hub.schedule
		if (!s) return
		// Seed the controls only when a genuinely new snapshot arrives. Writing
		// them on every paint is what reverted the reminder the moment the box
		// was ticked: the snapshot still said "off" because nothing had been
		// saved yet.
		const sig = [!!s.Enabled, s.EveryDays, !!s.AutoRun].join('|')
		if (sig !== seeded) {
			seeded = sig
			enabled = !!s.Enabled
			everyDays = s.EveryDays || 7
		}
	})

	const st = $derived(hub.backup)
	const running = $derived(!!st?.Running)
	const isRestore = $derived(running && st?.Kind === 'restore')
	const points = $derived(hub.points)

	const disk = $derived.by(() => {
		const inv = hub.data?.Backups
		const free = Number(inv?.FreeBytes) || 0
		const total = Number(inv?.TotalDiskBytes) || 0
		if (free <= 0 || total <= 0) return null
		const used = total - free
		return {
			free,
			used,
			text: t('disk.free', { free: bytes(free), total: bytes(total) }),
			pct: Math.min(100, Math.round((used / total) * 100))
		}
	})

	const reminder = $derived.by(() => {
		const s = hub.schedule
		if (!s) return ''
		if (s.Err) return s.Err
		if (!s.Enabled) return t('reminder.off')
		if (s.Due) return t('reminder.due')
		if (s.DaysLeft > 0) return t('reminder.nextIn', { n: s.DaysLeft, word: form(s.DaysLeft, 'backup.days') })
		return t('reminder.today')
	})
</script>

<div class="mx-auto flex max-w-4xl flex-col gap-4">
	<header>
		<h1 class="font-serif text-2xl text-[#f2eadb]">{t('backups.title')}</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			{t('backups.intro1')} <code class="text-foreground">backups\</code>. {t('backups.intro2')}
		</p>
	</header>

	<!-- ========================= сделать копию ======================== -->
	<section class="gold-frame rounded-xl px-5 py-4">
		<div class="mb-3 flex items-baseline gap-3">
			<span class="text-[10px] tracking-[0.14em] text-muted-foreground uppercase">{t('makeCopy')}</span>
			<span class="text-[11px] text-muted-foreground/70">
				{!hub.loaded ? t('reading.folder')
					: !points.length ? t('noPoints')
					: t('lastCopy', { t: stamp(points[0].Created) || t('unknownDate') })}
			</span>
		</div>

		<div class="flex flex-col gap-3">
			<div class="flex flex-wrap items-start gap-3 rounded-lg border border-border/60 bg-card/40 px-4 py-3">
				<div class="min-w-64 flex-1">
					<div class="text-sm font-medium">{t('kind.logical.long')}</div>
					<p class="mt-1 text-xs leading-relaxed text-muted-foreground">
						{t('kind.logical.desc')}
					</p>
					<div class="mt-1.5 text-[11px] {hub.mysqlUp ? 'text-ok' : 'text-warn'}">
						{hub.mysqlUp ? t('kind.logical.up') : t('kind.logical.down')}
					</div>
				</div>
				<Button
					data-role="backup-logical"
					disabled={hub.backupLocked || !hub.mysqlUp}
					onclick={() => hub.createBackup('logical')}
				>
					{t('btn.create')}
				</Button>
			</div>

			<div class="flex flex-wrap items-start gap-3 rounded-lg border border-border/60 bg-card/40 px-4 py-3">
				<div class="min-w-64 flex-1">
					<div class="text-sm font-medium">{t('kind.physical.long')}</div>
					<p class="mt-1 text-xs leading-relaxed text-muted-foreground">
						{t('kind.physical.desc')}
					</p>
					<div class="mt-1.5 text-[11px] {hub.upNames.length ? 'text-warn' : 'text-ok'}">
						{hub.upNames.length ? t('kind.physical.blocked') : t('kind.physical.ready')}
					</div>
				</div>
				<Button
					data-role="backup-physical"
					variant="outline"
					disabled={hub.backupLocked || hub.upNames.length > 0}
					onclick={() => hub.createBackup('physical')}
				>
					{t('btn.create')}
				</Button>
			</div>
		</div>

		<!-- Progress. A full copy is about two minutes and 385 МБ, so a button
		     that showed nothing for two minutes would be pressed twice. The same
		     box draws a restore, which is three steps and about a minute: one
		     progress box, because a second one would be a second drawing of the
		     same state. -->
		{#if running}
			<div data-role="backup-progress" class="mt-4 rounded-lg border border-border/60 px-4 py-3">
				<Progress value={hub.backupProgress} class="h-1.5" />
				<div class="mt-2 flex items-center gap-3 text-xs">
					<span class="text-foreground">
						{st?.Step}{st?.Total ? t('progress.step', { i: st.Index, total: st.Total }) : ''}
					</span>
					<span class="text-muted-foreground">
						{st ? duration(st.ElapsedSec) : ''}
						{st?.Bytes ? ' · ' + bytes(st.Bytes) : ''}
					</span>
					<!-- Cancel is the one button that must stay live exactly
					     while the others are dead - except during a restore,
					     which cannot be undone once the archive is unpacked
					     over the live database folder. Hidden rather than
					     disabled: a greyed button invites the task manager,
					     which would leave mysql\data half replaced. -->
					{#if !isRestore}
						<Button
							size="xs"
							variant="outline"
							data-role="backup-cancel"
							class="ml-auto"
							onclick={() => hub.cancelBackup()}>{t('btn.cancel')}</Button
						>
					{/if}
				</div>
			</div>
		{/if}

		{#if hub.backupOutcome}
			<div
				data-role="backup-result"
				class="mt-3 rounded-lg border px-4 py-3 text-xs leading-relaxed
					{hub.backupOutcome.level === 'error'
					? 'border-bad/50 bg-bad/5 text-bad'
					: 'border-ok/50 bg-ok/5 text-ok'}"
			>
				{hub.backupOutcome.text}
			</div>
		{/if}
	</section>

	<!-- ========================== напоминание ========================== -->
	<!-- One checkbox and one field, which is what it actually does: say when it
	     has been a while. The interval is the same figure the checklist on
	     "Сервер" uses to judge whether the newest copy is fresh, so the two
	     cannot disagree about the same folder. -->
	<section class="gold-frame rounded-xl px-5 py-4">
		<div class="flex flex-wrap items-start gap-3">
			<div class="min-w-64 flex-1">
				<div class="text-sm font-medium">{t('reminder.title')}</div>
				<p class="mt-1 text-xs text-muted-foreground">
					{t('reminder.desc')}
				</p>
				<div class="mt-1.5 text-[11px] {hub.schedule?.Due ? 'text-warn' : 'text-muted-foreground/70'}">
					{reminder}
					{#if hub.schedule?.LastRun}
						· {t('reminder.lastCopy', { t: stamp(hub.schedule.LastRun) })}
					{:else if hub.schedule?.Enabled}
						{t('reminder.never')}
					{/if}
				</div>
			</div>

			<label class="flex items-center gap-2 text-xs">
				<input
					type="checkbox"
					data-role="sched-enabled"
					bind:checked={enabled}
					class="size-4 accent-[var(--gold)]"
				/>
				{t('reminder.enabled')}
			</label>
			<label class="flex items-center gap-2 text-xs">
				{t('reminder.every')}
				<input
					type="number"
					data-role="sched-days"
					bind:value={everyDays}
					min="1"
					max="365"
					disabled={!enabled}
					class="w-16 rounded border border-input bg-input/30 px-2 py-1 text-xs disabled:opacity-40"
				/>
				{t('reminder.days')}
			</label>
			<Button data-role="sched-save" size="sm" onclick={() => hub.saveSchedule(enabled, everyDays)}>
				{t('btn.save')}
			</Button>
		</div>
	</section>

	<!-- ============================ копии ============================== -->
	<section class="gold-frame rounded-xl px-5 py-4">
		<div class="mb-3 flex items-baseline gap-3">
			<span class="text-[10px] tracking-[0.14em] text-muted-foreground uppercase">{t('ondisk')}</span>
			<span class="text-[11px] text-muted-foreground/70">
				{points.length
					? `${P(points.length, 'backups.pieces')} · ${bytes(hub.data?.Backups?.TotalBytes)}`
					: ''}
			</span>
		</div>

		{#if hub.backupsErr}
			<p class="text-xs text-warn">{hub.backupsErr}</p>
		{/if}

		<ul data-role="points" class="flex flex-col gap-2">
			{#each points as p (p.Name)}
				<li class="flex flex-wrap items-center gap-3 rounded border border-border/60 bg-card/40 px-3 py-2">
					<div class="min-w-64 flex-1">
						<div class="flex items-center gap-2">
							<span class="text-sm" title={p.Path}>{p.Name}</span>
							<span
								class="rounded border px-1.5 py-0.5 text-[10px]
									{p.Kind === 'physical' ? 'border-gold-dim/50 text-gold' : 'border-border text-muted-foreground'}"
							>
								{kindLabel(p.Kind) || p.Kind}
							</span>
						</div>
						<div class="mt-0.5 text-[11px] text-muted-foreground/70">
							{stamp(p.Created) || t('unknownDate')} · {bytes(p.SizeBytes)} ·
							{P(p.Files, 'files.count')}
						</div>
						<!-- Only where there is no button. A row that can be restored
						     says so on the button, and a line repeating the button's
						     own words is the second place to get one fact wrong.
						     A row that cannot be restored still needs the line: it is
						     the only place saying that a rollback exists and where to
						     reach it, and backups\restore-all-db.cmd takes the newest
						     folder rather than the chosen one. -->
						{#if p.Note}
							<div class="mt-1 text-[11px] text-warn">{p.Note}</div>
						{:else if !hub.isRestorable(p.Kind)}
							<div class="mt-1 text-[11px] text-muted-foreground/60">
								{t('restoredByScript')}
							</div>
						{/if}
					</div>

					<div class="flex items-center gap-2">
						{#if hub.isRestorable(p.Kind)}
							<Button
								data-role="restore"
								data-point={p.Name}
								size="sm"
								variant="outline"
								class="border-bad/50 text-bad hover:bg-bad/10 hover:text-bad"
								disabled={hub.backupLocked}
								onclick={() => (hub.restorePoint = p.Name)}
							>
								{t('restore.btn')}
							</Button>
						{/if}
						<!-- Delete is on every row and is the quieter of the two
						     buttons: the red one means "this replaces the database",
						     and a row with two red buttons would say neither thing.
						     The weight belongs in the dialog, which names the folder
						     and says there is no way back. -->
						<Button
							data-role="delete"
							data-point={p.Name}
							size="sm"
							variant="ghost"
							class="text-muted-foreground hover:text-foreground"
							disabled={hub.backupLocked}
							onclick={() => (hub.deletePoint = p.Name)}
						>
							{t('btn.delete')}
						</Button>
					</div>
				</li>
			{/each}
		</ul>

		{#if hub.loaded && !hub.backupsErr && !points.length}
			<p class="text-sm text-muted-foreground">
				{t('empty.backups.pre')} <code>backups\</code>{t('empty.backups.post')}
			</p>
		{/if}

		{#if disk}
			<div class="mt-4 border-t border-border pt-3">
				<div class="mb-1 text-[11px] text-muted-foreground">{disk.text}</div>
				<Progress value={disk.pct} class="h-1.5" />
			</div>
		{/if}
	</section>
</div>