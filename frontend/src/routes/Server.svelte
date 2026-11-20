<!--
  The main screen.

  One button at the top answers "is the world up, and how do I start it". Below
  it, always visible: what the three services are doing with a control for each,
  the realm checklist, and the per-database breakdown. Nothing here is behind a
  click, because the button that started a three-minute world is the only thing
  a person needs and the rest is there for the person who wants to know why.
-->
<script lang="ts">
	import { Button } from '$lib/components/ui/button'
	import { hub } from '$lib/state.svelte.js'
	import { bytes, num, stamp } from '$lib/format.js'
	import { t, P, form, stateLabel, svcPlain, svcRole } from '$lib/i18n.svelte.js'
	import { SERVICE_NAME } from '$lib/words.js'
	import Dot from '$lib/components/Dot.svelte'

	const ids = ['mysql', 'auth', 'world'] as const

	const hero = $derived(hub.headline)
	const allUp = $derived(hub.total > 0 && hub.upCount === hub.total)

	// The one line under the button: how much of the server is up, in the same
	// words as the checklist below it.
	const sub = $derived.by(() => {
		if (!hub.total) return t('sub.reading')
		if (allUp) return t('sub.allUp')
		if (hub.moving) return t('sub.moving')
		return t('sub.someUp', {
			up: hub.upCount,
			total: hub.total,
			word: form(hub.upCount, 'server.services')
		})
	})

	const realm = $derived(hub.realm)

	/**
	 * The checklist rows that passed.
	 *
	 * The ones that failed are not here - they are in the problems box above,
	 * said in words meant for a person. Repeating them here is what made
	 * this block unreadable: one fact on one screen, three times, the two
	 * latter copies carrying a raw Go error between them.
	 *
	 * "database" is dropped as a duplicate and not as a formatter: its detail
	 * names the databases, their size and the accounts, and all three of those
	 * numbers are already on this screen - the size and the accounts in the
	 * tiles above, the tables in the breakdown below. A row that says nothing
	 * the screen does not already say is a row the eye reads past.
	 */
	const passed = $derived(hub.setup.filter((s) => s.OK && s.Key !== 'database'))

	// Two figures, both out of one snapshot pass, so a total and the list it
	// summarises cannot disagree. The copy count is gone: it was readable with
	// the world off (it comes from backups\, not from the database), which made
	// it the one tile left standing on an otherwise empty screen, and it belongs
	// to the copies screen.
	const tiles = $derived.by(() => [
		{
			cap: t('tile.dbsizes'),
			val: realm?.TotalBytes ? bytes(realm.TotalBytes) : null,
			sub: realm?.Databases?.length ? P(realm.Databases.length, 'db.count') : ''
		},
		{
			cap: t('tile.chars'),
			val: realm?.Characters ? num(realm.Characters) : null,
			sub: realm?.Accounts
				? t('tile.charsSub', { n: num(realm.Accounts), word: form(realm.Accounts, 'accts.on') })
				: ''
		},
		{
			cap: t('tile.accounts'),
			val: realm?.Accounts ? num(realm.Accounts) : null,
			sub: realm?.Databases?.length ? t('tile.acctsSub') : ''
		}
	])
</script>

<div class="mx-auto flex max-w-5xl flex-col gap-4">
	<!-- ============================ состояние =========================== -->
	<!--
	  The hero used to be here: a title, a paragraph and a big "Запустить сервер".
	  All of it was a second copy. The button called the same StartAll as the one
	  in the service block two hundred pixels below, and the title repeated the
	  brand that is in the sidebar. What survived from it is one line - is the
	  world playable right now - because that sentence exists nowhere else. The
	  three dots below say which service is down; they do not say whether the
	  game can be played.
	-->
	<section class="flex flex-wrap items-center justify-between gap-4 px-1">
		<div class="flex min-w-0 items-center gap-2 text-lg">
			<Dot state={hub.failed ? 'failed' : allUp ? 'running' : hub.moving ? 'starting' : 'stopped'} />
			<span
				data-role="headline"
				class={hero.level === 'bad' ? 'text-bad' : hero.level === 'ok' ? 'text-ok' : hero.level === 'warn' ? 'text-warn' : 'text-muted-foreground'}
			>
				{hero.text}
			</span>
		</div>
		<p class="text-xs text-muted-foreground">{sub}</p>
	</section>

	<!-- ========================== что делать =========================== -->
	<!-- Shown only when something is wrong. A world that is merely stopped is
	     not a problem to report, and a checklist that is always on screen is a
	     checklist nobody reads. -->
	{#if hub.problems.length}
		<section
			data-role="problems"
			class="rounded-lg border border-warn/40 bg-warn/5 px-4 py-3 text-sm"
		>
			<div class="mb-1 font-medium text-warn">{t('problems.title')}</div>
			<ul class="flex flex-col gap-2">
				{#each hub.problems as p (p.Key)}
					<li class="flex flex-wrap items-baseline gap-x-2">
						<span class="text-foreground">{p.Label} — {p.Detail}</span>
						{#if p.Fix}<span class="text-xs text-muted-foreground">{p.Fix}</span>{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	<!-- ======================== состояние реалма ======================== -->
	<!--
	  A section that can say nothing is worse than no section. Three tiles of
	  dashes next to a world that is deliberately switched off are noise, and
	  the read error that goes with them is the stop ladder doing its job, not
	  a fault: the ladder takes MySQL down after the world, so anyone who
	  pressed "Сервер выключен" has just made the database unreachable on
	  purpose.

	  So the whole block is conditional on `dataReadable`, and with the world
	  off the screen is the button and nothing else. When the world is on it
	  says what the realm is made of, which is a fair thing to read while you
	  wait for it to boot.

	  "Копий на диске" is deliberately not here. It was the one tile that did
	  show while the world was off - it is read from backups\, not from the
	  database - and it belonged to the screen "Копии". A figure that appears
	  on two screens and means one thing on each is a figure that reads as
	  noise on the wrong one.
	-->
	{#if hub.dataReadable}
		<section data-role="tiles" class="grid grid-cols-3 gap-3">
			{#each tiles as t, i (i)}
				<div class="gold-frame rounded-lg px-4 py-3">
					<div class="text-[10px] tracking-[0.14em] text-muted-foreground uppercase">{t.cap}</div>
					<div class="mt-1 text-xl {t.val ? 'text-foreground' : 'text-muted-foreground/50'}">
						{t.val ?? '—'}
					</div>
					{#if t.sub}<div class="mt-0.5 text-[11px] text-muted-foreground/70">{t.sub}</div>{/if}
				</div>
			{/each}
		</section>

		<!-- Read errors belong to the tiles above and to the technical block,
		     not to a free-floating line under them: the raw `dial tcp
		     127.0.0.1:3306: connectex: ...` is a line for a bug report, and on
		     this screen it repeated the problem box word for word. -->
		{#if hub.dataPending}
			<p class="text-xs text-muted-foreground">{t('tiles.reading')}</p>
		{:else if hub.loaded && (realm?.Err || hub.backupsErr)}
			<p data-role="read-error" class="text-xs text-warn">
				{realm?.Err ? t('readerr.db', { err: realm.Err }) : ''}
				{hub.backupsErr ? t('readerr.backups', { err: hub.backupsErr }) : ''}
			</p>
		{/if}
	{/if}

	<!-- ==================== технические детали ======================== -->
	<!--
	  Always open, and with no heading. A collapsible frame here was a
	  compromise that bought two things and cost one: it hid the buttons on
	  demand, and it let the checklist stay out of sight - but the checklist is
	  the only thing that answers "what is the panel actually doing", and a
	  heading that has to be clicked to find out is a heading people do not
	  click. What is left inside is the per-service control, which is worth
	  having whether or not a person is looking for it.
	-->
	<section data-role="tech" class="gold-frame overflow-hidden rounded-lg">
		<div class="flex flex-col gap-5 px-4 py-4">
			<div class="flex flex-col gap-2">
					{#each ids as id (id)}
						{@const st = hub.stateOf(id)}
						<div class="flex items-center gap-3 rounded border border-border/70 bg-card/40 px-3 py-2">
							<Dot state={st} />
							<div class="min-w-0 flex-1">
								<div class="text-sm">
									<span class="text-foreground">{svcPlain(id)}</span>
									<span class="text-[11px] text-muted-foreground/70">({SERVICE_NAME[id]})</span>
								</div>
								<div class="text-[11px] text-muted-foreground/70">{svcRole(id)}</div>
							</div>
							<div class="w-40 text-[11px] text-muted-foreground">{stateLabel(st) || st}</div>
							<div class="w-28 text-right text-[11px] text-muted-foreground/70">
								{hub.services.find((s) => s.ID === id)?.PID
									? t('svc.mem', {
											pid: hub.services.find((s) => s.ID === id)?.PID ?? 0,
											mem: hub.services.find((s) => s.ID === id)?.MemoryMB ?? 0
										})
									: '—'}
							</div>
							<div class="flex gap-1">
								<Button
									size="xs"
									variant="outline"
									data-act="start"
									data-svc={id}
									disabled={hub.busy > 0 || !hub.can('start', id)}
									onclick={() => hub.startService(id)}>{t('btn.start')}</Button
								>
								<Button
									size="xs"
									variant="outline"
									data-act="stop"
									data-svc={id}
									disabled={hub.busy > 0 || !hub.can('stop', id)}
									onclick={() => hub.stopService(id)}>{t('btn.stop')}</Button
								>
								<Button
									size="xs"
									variant="outline"
									data-act="restart"
									data-svc={id}
									disabled={hub.busy > 0 || !hub.can('restart', id)}
									onclick={() => hub.restartService(id)}>{t('btn.restart')}</Button
								>
							</div>
						</div>
					{/each}

				<!-- One button to start the world, one to stop it. The hero's big
				     button called exactly these two, so it is gone and these are
				     now the only ones on the screen - which means they carry the
				     weight alone and have to look like the main action. -->
					<div class="flex gap-2 pt-1">
						<Button
							size="lg"
							data-act="start-all"
							data-role="world-toggle"
							class={allUp ? 'hidden' : ''}
							disabled={hub.busy > 0 || hub.moving || !hub.canStartAll}
							onclick={() => hub.startAll()}>{t('btn.startServer')}</Button
						>
						<Button
							size="lg"
							variant="outline"
							data-act="stop-all"
							data-role="world-toggle"
							class={`border-bad/60 text-bad hover:bg-bad/10 hover:text-bad ${allUp ? '' : 'hidden'}`}
							disabled={hub.busy > 0 || hub.moving || !hub.canStopAll}
							onclick={() => hub.stopAll()}>{t('btn.stopServer')}</Button
						>
					</div>
				</div>

		</section>

		<!-- ======================= проверено и числа ===================== -->
		<!--
		  Two things that used to be one wall of text, and they were separated
		  by a line of small capitals and nothing else.

		  What FAILED is not here at all. It is already above, in words meant for
		  a person, and the same row repeated here with a raw Go error in it was
		  the third mention of one fact on one screen - the first in "Что нужно
		  сделать", the second in the checklist, the third in the error line under
		  the database list. A fact gets one place, not three.

		  What passed is worth keeping, but it is not news: it goes quiet, in two
		  columns, so the eye can scan a column instead of reading a stream.
		-->
		{#if passed.length}
			<section class="flex flex-col gap-2 px-1">
				<div class="flex items-baseline gap-2 text-[10px] tracking-[0.14em] text-muted-foreground uppercase">
					<span class="text-ok">✓</span>
					{t('checked.ok')}
				</div>
				{#each passed as s (s.Key)}
					<div class="grid grid-cols-[minmax(0,12rem)_minmax(0,1fr)] gap-4 text-xs">
						<span class="text-foreground">{s.Label}</span>
						<!-- Truncated because one of the details is a full path to the
						     repack, and a row that wraps ruins the column it sits in. -->
						<span class="truncate text-muted-foreground" title={s.Detail}>{s.Detail}</span>
					</div>
				{/each}
			</section>
		{/if}

		<!-- Разбивка по базам, в своей панели: это числа, а не настройка, и
		     смешивать их в одну стену значит заставлять глаз искать, где кончилось
		     одно и началось другое. Числа MySQL берёт из information_schema и
		     обновляет раз в сутки, поэтому это оценка занятого места, а не размер
		     файлов на диске. -->
			{#if realm?.Databases?.length || hub.dataPending}
				<section class="gold-frame flex flex-col gap-2 rounded-lg px-4 py-3">
					<div class="flex items-baseline gap-2 text-[10px] tracking-[0.14em] text-muted-foreground uppercase">
						{t('db.breakdown')}
						{#if realm?.ReadAt}<span class="normal-case tracking-normal">{t('db.readAt', { t: stamp(realm.ReadAt) })}</span>{/if}
					</div>
					{#if hub.dataPending}
						<div class="text-xs text-muted-foreground">{t('tiles.reading')}</div>
					{:else}
						<div class="grid grid-cols-[minmax(0,1fr)_6rem_7rem] gap-3 border-b border-border/60 pb-1 text-[10px] tracking-[0.12em] text-muted-foreground/60 uppercase">
							<span>{t('col.db')}</span>
							<span class="text-right">{t('col.tables')}</span>
							<span class="text-right">{t('col.size')}</span>
						</div>
					{#each realm?.Databases ?? [] as db (db.Name)}
						<div class="grid grid-cols-[minmax(0,1fr)_6rem_7rem] gap-3 text-xs">
							<span class="truncate font-mono text-muted-foreground">{db.Name}</span>
							<span class="text-right text-foreground">{num(db.Tables)}</span>
							<span class="text-right text-foreground">{bytes(db.SizeBytes)}</span>
						</div>
					{/each}
				{/if}
			</section>
		{/if}
</div>
