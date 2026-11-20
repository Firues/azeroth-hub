<!--
  Everything that is not the world and not the copies, in closed sections. A
  person opens the one they came for; the others stay out of the way.

  The character and account lists load on first open, not at startup: the
  character list is an order of magnitude more data than everything else here,
  and in most sessions nobody opens it.

  The language picker sits above them: it is the one control that changes
  every word on screen, and burying it behind a click would make it
  undiscoverable exactly when a person needs it.
-->
<script lang="ts">
	import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '$lib/components/ui/accordion'
	import { Button } from '$lib/components/ui/button'
	import { Input } from '$lib/components/ui/input'
	import { hub } from '$lib/state.svelte.js'
	import * as api from '$lib/api.js'
	import { num, stamp } from '$lib/format.js'
	import { LANGS, i18n, t, P, stateLabel, svcPlain, checkAdvice, raceName, className, type Lang } from '$lib/i18n.svelte.js'
	import { CHECK_OWNER, HAS_ADVICE, RENDER_MAX, SETTLED } from '$lib/words.js'

	let opened = $state<string[]>([])

	// Loading on first open, once per screen: the old version did this with a
	// radio-button listener, and the data arrives from the same call either way.
	$effect(() => {
		if (opened.includes('chars') && hub.characters === null && !hub.loadingChars) {
			hub.loadCharacters()
		}
		if (opened.includes('accounts') && hub.accounts === null && !hub.loadingAccounts) {
			hub.loadAccounts()
		}
	})

	// Switching has two halves: the backend learns it (so the checklist, the
	// health details and every error from now on are built in the new
	// language, and the choice survives a restart), and the page learns it
	// (so everything it renders itself repaints at once). The checklist and
	// the data snapshot are re-pulled here because they are asked for, not
	// pushed: they would otherwise keep the old language until the next event.
	async function switchLang(id: Lang) {
		if (i18n.lang === id) return
		try {
			await api.SetLanguage(id)
			i18n.set(id)
			hub.loadSetup()
			hub.refreshData()
		} catch (err) {
			hub.say(msg(err), 'error')
		}
	}

	function msg(err: unknown): string {
		if (err && typeof err === 'object' && 'message' in err) return String((err as Error).message)
		return String(err)
	}

	const charsShown = $derived(hub.charList.slice(0, RENDER_MAX))
	const accountsShown = $derived(hub.accountList.slice(0, RENDER_MAX))

	/**
	 * A check that fails while its service is stopped or starting is not a
	 * problem - the port of a worldserver you deliberately stopped is supposed
	 * to be closed. Three marks, not two: ✓ passed, ! a real failure with an
	 * action, · neutral. A red exclamation that is sometimes harmless is a
	 * badge people learn to ignore.
	 */
	const level = (id: string, ok: boolean) => {
		if (ok) return 'ok'
		const owner = CHECK_OWNER[id]
		return !owner || SETTLED[hub.stateOf(owner)] ? 'bad' : 'idle'
	}
</script>

<div class="mx-auto flex max-w-4xl flex-col gap-4">
	<header>
		<h1 class="font-serif text-2xl text-[#f2eadb]">{t('more.title')}</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			{t('more.intro')}
		</p>
	</header>

	<!-- ============================ язык ============================ -->
	<section class="gold-frame rounded-xl px-5 py-4">
		<div class="text-sm font-medium">{t('lang.title')}</div>
		<p class="mt-1 text-xs text-muted-foreground">{t('lang.hint')}</p>
		<div class="mt-3 flex flex-wrap gap-2">
			{#each LANGS as l (l.id)}
				<Button
					size="sm"
					variant={i18n.lang === l.id ? 'default' : 'outline'}
					data-role="lang"
					data-lang={l.id}
					aria-pressed={i18n.lang === l.id}
					onclick={() => switchLang(l.id)}
				>
					{l.name}
				</Button>
			{/each}
		</div>
	</section>

	<Accordion type="multiple" bind:value={opened} class="flex flex-col gap-3">
		<!-- ========================= персонажи ========================== -->
		<AccordionItem value="chars" class="gold-frame overflow-hidden rounded-lg">
			<AccordionTrigger class="px-4 py-3 text-sm hover:no-underline">
				{t('chars.title')}
				<span class="ml-2 text-[11px] text-muted-foreground">
					{hub.characters ? num(hub.characters.Total ?? hub.charList.length) : ''}
				</span>
			</AccordionTrigger>
			<AccordionContent class="px-4 pb-4">
				<div class="mb-3 flex items-center gap-2">
					<Input
						data-role="chars-search"
						placeholder={t('search.chars')}
						bind:value={hub.charQuery}
						class="h-8 max-w-72 text-xs"
					/>
					<Button size="sm" variant="outline" onclick={() => hub.loadCharacters()}>{t('btn.refresh')}</Button>
					<span class="text-[11px] text-muted-foreground">
						{#if hub.loadingChars}
							{t('reading.more')}
						{:else if hub.charQuery.trim()}
							{t('list.found', { n: num(hub.charList.length), m: num(hub.characters?.List?.length ?? 0) })}
						{:else if hub.characters?.List?.length}
							{t('list.of', { n: num(hub.characters.List.length), m: num(hub.characters.Total) })}
						{/if}
					</span>
				</div>

				{#if hub.characters?.Err}
					<p class="text-xs text-bad">{t('err.list', { err: hub.characters.Err })}</p>
				{:else if !hub.characters}
					<p class="text-xs text-muted-foreground">{t('list.notLoaded')}</p>
				{:else if !charsShown.length}
					<p class="text-xs text-muted-foreground">
						{hub.charQuery.trim() ? t('none.found') : t('db.empty.chars')}
					</p>
				{:else}
					<ul class="flex flex-col gap-1">
						{#each charsShown as c (c.GUID)}
							<li class="flex items-center gap-3 rounded px-2 py-1 text-xs odd:bg-white/3">
								<span class="w-8 shrink-0 text-right font-semibold {c.Level >= 80 ? 'text-gold' : 'text-foreground'}">
									{c.Level}
								</span>
								<span class="min-w-0 flex-1">
									<span class="text-foreground">{c.Name}</span>
									<span class="ml-2 text-muted-foreground/70">
										{raceName(c.Race) || c.Race} · {className(c.Class) || c.Class}
									</span>
								</span>
								<span class="w-28 shrink-0 truncate text-muted-foreground/70">{c.Account || '—'}</span>
								{#if c.Online}<span class="text-ok">{t('online')}</span>{/if}
							</li>
						{/each}
					</ul>
					{#if hub.charList.length > charsShown.length}
						<p class="mt-2 text-[11px] text-muted-foreground/70">
							{t('list.shown', { n: num(charsShown.length), m: num(hub.charList.length) })}
						</p>
					{/if}
				{/if}
			</AccordionContent>
		</AccordionItem>

		<!-- ========================== аккаунты ========================== -->
		<!-- Read-only in the strongest sense: the query behind this does not
		     select the salt, the verifier, the session key or the TOTP secret.
		     A list that never asks for a credential cannot put one into a
		     screenshot attached to a bug report. -->
		<AccordionItem value="accounts" class="gold-frame overflow-hidden rounded-lg">
			<AccordionTrigger class="px-4 py-3 text-sm hover:no-underline">
				{t('accounts.title')}
				<span class="ml-2 text-[11px] text-muted-foreground">
					{hub.accounts ? num(hub.accounts.Total ?? 0) : ''}
				</span>
			</AccordionTrigger>
			<AccordionContent class="px-4 pb-4">
				<div class="mb-3 flex items-center gap-2">
					<Input
						data-role="accounts-search"
						placeholder={t('search.accts')}
						bind:value={hub.accountQuery}
						class="h-8 max-w-72 text-xs"
					/>
					<Button size="sm" variant="outline" onclick={() => hub.loadAccounts()}>{t('btn.refresh')}</Button>
					<span class="text-[11px] text-muted-foreground">
						{#if hub.loadingAccounts}
							{t('reading.more')}
						{:else if hub.accountQuery.trim()}
							{t('list.found', { n: num(hub.accountList.length), m: num(hub.accounts?.List?.length ?? 0) })}
						{:else if hub.accounts?.List?.length}
							{t('list.of', { n: num(hub.accounts.List.length), m: num(hub.accounts.Total) })}
						{/if}
					</span>
				</div>

				{#if hub.accounts?.Err}
					<p class="text-xs text-bad">{t('err.list', { err: hub.accounts.Err })}</p>
				{:else if !hub.accounts}
					<p class="text-xs text-muted-foreground">{t('list.notLoaded')}</p>
				{:else if !accountsShown.length}
					<p class="text-xs text-muted-foreground">
						{hub.accountQuery.trim() ? t('none.found') : t('db.empty.accts')}
					</p>
				{:else}
					<ul class="flex flex-col gap-1">
						{#each accountsShown as a (a.ID)}
							<li class="flex items-center gap-3 rounded px-2 py-1 text-xs odd:bg-white/3">
								<span
									class="w-14 shrink-0 {a.GMLevel > 0 ? 'text-gold' : 'text-muted-foreground/70'}"
								>
									{a.GMLevel > 0 ? t('gm', { lvl: a.GMLevel }) : t('player')}
								</span>
								<span class="min-w-0 flex-1">
									<span class="text-foreground {a.Locked ? 'line-through opacity-60' : ''}">
										{a.Username || '—'}
									</span>
									<span class="ml-2 text-muted-foreground/70">
										{#if a.LastIP}{a.LastIP}{/if}
										{#if a.TotalTimeSec > 3600}
											· {t('hours.played', { n: num(Math.floor(a.TotalTimeSec / 3600)) })}
										{/if}
									</span>
								</span>
								<!-- The zero time means the account has never
								     signed in, which is a fact and not a missing
								     value - so it is said in words. -->
								<span class="w-28 shrink-0 text-right text-muted-foreground/70">
									{stamp(a.LastLogin) || t('never')}
								</span>
								{#if a.Online}<span class="text-ok">{t('online')}</span>{/if}
							</li>
						{/each}
					</ul>
					{#if hub.accountList.length > accountsShown.length}
						<p class="mt-2 text-[11px] text-muted-foreground/70">
							{t('list.shown', { n: num(accountsShown.length), m: num(hub.accountList.length) })}
						</p>
					{/if}
				{/if}
			</AccordionContent>
		</AccordionItem>

		<!-- ========================== проверки ========================== -->
		<!-- Six technical facts about three processes, each with what to do
		     about a failure. The question "is the realm playable right now" is
		     answered on the server screen instead, in the user's terms. -->
		<AccordionItem value="checks" class="gold-frame overflow-hidden rounded-lg">
			<AccordionTrigger class="px-4 py-3 text-sm hover:no-underline">
				{t('checks.title')}
				<span class="ml-2 text-[11px] text-muted-foreground">
					{#if !hub.checks.length}{t('checks.updating')}{:else if hub.attention.length}{P(hub.attention.length, 'checks.notes')}{:else}{t('checks.allok')}{/if}
				</span>
			</AccordionTrigger>
			<AccordionContent class="px-4 pb-4">
				<ul class="flex flex-col gap-2">
					{#each hub.checks as c (c.ID)}
						{@const lv = level(c.ID, !!c.OK)}
						<li class="flex items-start gap-2 text-xs">
							<span class={lv === 'ok' ? 'text-ok' : lv === 'bad' ? 'text-bad' : 'text-muted-foreground/50'}>
								{lv === 'ok' ? '✓' : lv === 'bad' ? '!' : '·'}
							</span>
							<span class="min-w-0 flex-1">
								<span class="text-foreground">{c.Label}</span>
								<span class="ml-2 text-muted-foreground">{c.Detail}</span>
								{#if lv === 'bad' && HAS_ADVICE[c.ID]}
									<span class="mt-0.5 block text-muted-foreground/80">{checkAdvice(c.ID)}</span>
								{/if}
							</span>
						</li>
						{:else}
							<li class="text-xs text-muted-foreground">{t('checks.none')}</li>
						{/each}
				</ul>
				<p class="mt-3 text-[11px] text-muted-foreground/70">
					{t('checks.neutral')} {#each ['mysql', 'auth', 'world'] as id (id)}
						<span class="mx-1 inline-flex items-center gap-1.5">
							{svcPlain(id)} — {stateLabel(hub.stateOf(id)) || hub.stateOf(id)}
						</span>{/each}
				</p>
			</AccordionContent>
		</AccordionItem>

		<!-- =========================== папки =========================== -->
		<AccordionItem value="folders" class="gold-frame overflow-hidden rounded-lg">
			<AccordionTrigger class="px-4 py-3 text-sm hover:no-underline">{t('folders.title')}</AccordionTrigger>
			<AccordionContent class="flex flex-wrap gap-2 px-4 pb-4">
				<Button size="sm" variant="outline" onclick={() => hub.openFolder('backups', t('folder.label.backups'))}>
					{t('folders.openBackups')}
				</Button>
				<Button size="sm" variant="outline" onclick={() => hub.openFolder('logs', t('folder.label.logs'))}>
					{t('folders.openLogs')}
				</Button>
				<Button size="sm" variant="outline" onclick={() => hub.openFolder('root', t('folder.label.root'))}>
					{t('folders.openRoot')}
				</Button>
				<p class="w-full text-[11px] text-muted-foreground/70">
					{t('folders.hint')}
				</p>
			</AccordionContent>
		</AccordionItem>
	</Accordion>
</div>
