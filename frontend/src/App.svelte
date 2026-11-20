<!--
  The shell.

  Three rules this file exists to keep:

  - Nothing is repeated. There is no header: the brand and the current screen
    are in the sidebar, the service state is in the service rows, and the
    checks are in the checklist. What stays on screen at all times is the
    bottom strip - the result of the last action and refresh - because those
    have no other home.
  - The navigation is three buttons and nothing else. Every screen answers one
    question; "Ещё" holds what a person needs on the day they look for it.
  - Nothing here decides what a service may do. That is CAN in words.ts, and
    the screens ask the state object.
-->
<script lang="ts">
	import Server from './routes/Server.svelte'
	import Backups from './routes/Backups.svelte'
	import More from './routes/More.svelte'
	import ConfirmKill from '$lib/components/ConfirmKill.svelte'
	import DeleteAsk from '$lib/components/DeleteAsk.svelte'
	import QuitAsk from '$lib/components/QuitAsk.svelte'
	import RestoreAsk from '$lib/components/RestoreAsk.svelte'
	import { hub } from './lib/state.svelte.js'
	import * as api from './lib/api.js'
	import { t } from '$lib/i18n.svelte.js'
	import { type Route } from '$lib/words.js'

	const strip = $derived(
		hub.last.level === 'error'
			? 'text-bad'
			: hub.last.level === 'warn'
				? 'text-warn'
				: 'text-muted-foreground'
	)

	// The navigation, in the current language. Derived, so switching the
	// language repaints the sidebar without a reload.
	const routes = $derived<{ id: Route; title: string; hint: string }[]>([
		{ id: 'server', title: t('route.server.title'), hint: t('route.server.hint') },
		{ id: 'backups', title: t('route.backups.title'), hint: t('route.backups.hint') },
		{ id: 'more', title: t('route.more.title'), hint: t('route.more.hint') }
	])

	/**
	 * The window has no system frame, so the strip at the top is the frame:
	 * it drags the window, and it holds the only buttons that can close it.
	 *
	 * Empty on purpose. A title here would repeat the brand that the sidebar
	 * already carries four lines lower, and this is the exact mistake the last
	 * three changes were about. The strip's job is to be something to grab.
	 *
	 * `--wails-draggable: drag` is not CSS of ours: Wails reads that property
	 * on the element under the mouse and calls the Win32 drag. A button inside
	 * a drag region is still a button, because the runtime requires the press
	 * to start on the region itself.
	 */
	let maximised = $state(false)

	$effect(() => {
		api.WindowIsMaximised().then((v) => (maximised = !!v))
	})
</script>

<div class="flex h-full flex-col">
	<!-- ============================== полоса ============================ -->
	<div
		data-role="titlebar"
		class="titlebar flex h-9 shrink-0 items-stretch justify-end border-b border-gold-dim/30 bg-black/50"
	>
		<!-- The drag region is this empty child and nothing else. On the strip
		     itself it would inherit down onto the buttons, and the runtime would
		     defer the drag to the first mousemove - a hand that moved a pixel
		     would turn the click into a drag, and every button would need two
		     tries.

		     Double click here is the same command as the restore button, which
		     is what a system titlebar does and what a hand expects after a
		     thousand windows. It cannot fight the drag: a double click is two
		     presses in the same spot, so no mousemove happens and the runtime
		     never starts a drag. -->
		<div
			data-role="titlebar-drag"
			class="titlebar-drag flex-1"
			ondblclick={() => api.WindowToggleMaximise()}
		></div>
		<!-- Close, not Minimise. A hidden window keeps its webview alive, and
		     the webview is 285 MB in twelve WebView2 processes - the whole
		     reason the panel is worth closing when it is not being looked at.
		     The tray brings it back; see main.go's newWindow. -->
		<button
			type="button"
			data-role="win-minimise"
			title={t('win.minimise')}
			aria-label={t('win.minimise')}
			class="w-12 transition-colors hover:bg-white/8"
			onclick={() => api.WindowClose()}
		>
			<svg viewBox="0 0 12 12" class="mx-auto size-3" fill="none" stroke="currentColor" stroke-width="1">
				<path d="M2.5 6h7" />
			</svg>
		</button>
		<button
			type="button"
			data-role="win-maximise"
			title={maximised ? t('win.restore') : t('win.maximise')}
			aria-label={maximised ? t('win.restore') : t('win.maximise')}
			class="w-12 transition-colors hover:bg-white/8"
			onclick={() => api.WindowToggleMaximise()}
		>
			<svg viewBox="0 0 12 12" class="mx-auto size-3" fill="none" stroke="currentColor" stroke-width="1">
				{#if maximised}
					<rect x="2.5" y="3.5" width="7" height="6" rx="1" />
					<path d="M4 3.5V2.5h7v7H8.5" />
				{:else}
					<rect x="2.5" y="2.5" width="7" height="7" rx="1" />
				{/if}
			</svg>
		</button>
		<!-- closeWindow, not api.Quit(): running servers are the one thing this
		     button must ask about. The backend's OnShutdown leaves them running
		     on purpose, and that is exactly why the question belongs here - see
		     hub.closeWindow and components/QuitAsk.svelte. -->
		<button
			type="button"
			data-role="win-close"
			title={t('win.close')}
			aria-label={t('win.close')}
			class="w-12 transition-colors hover:bg-bad hover:text-white"
			onclick={() => hub.closeWindow()}
		>
			<svg viewBox="0 0 12 12" class="mx-auto size-3" fill="none" stroke="currentColor" stroke-width="1">
				<path d="M3 3l6 6M9 3l-6 6" />
			</svg>
		</button>
	</div>

	<div class="flex min-h-0 flex-1">
	<!-- ============================ сайдбар ============================ -->
	<aside class="flex w-56 shrink-0 flex-col border-r border-gold-dim/40 bg-black/40 px-4 py-5">
		<div class="flex flex-col items-center gap-2">
			<div class="crest"><span class="font-serif text-xl font-bold text-gold-bright">A</span></div>
			<div class="font-serif text-sm tracking-[0.14em] text-[#ece2cf] uppercase">Azeroth Hub</div>
			<div class="text-[10px] tracking-[0.16em] text-muted-foreground uppercase">{t('brand.sub')}</div>
		</div>

		<div class="mt-6 mb-2 px-3 text-[9px] tracking-[0.18em] text-muted-foreground/70 uppercase">
			{t('nav.world')}
		</div>
		<nav class="flex flex-col gap-1">
			{#each routes as r (r.id)}
				<button
					type="button"
					data-route={r.id}
					onclick={() => (hub.route = r.id)}
					aria-current={hub.route === r.id ? 'page' : undefined}
					class="flex min-h-12 items-center gap-3 rounded-r border border-transparent px-3 py-2 text-left transition-colors
						{(hub.route === r.id)
						? 'border-gold-dim/40 bg-gradient-to-r from-gold/15 to-ice/10 text-[#f2eadb] shadow-[inset_3px_0_0_var(--gold)]'
						: 'text-[#9ba6b3] hover:bg-white/5 hover:text-[#d7dfe7]'}"
				>
					<span class="grid size-6 shrink-0 place-items-center text-gold">
						{#if r.id === 'server'}
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" class="size-[19px]">
								<path d="M5 19V8l7-4 7 4v11M8 19v-5h8v5M4 20h16" /><path d="M12 4v4" />
							</svg>
						{:else if r.id === 'backups'}
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" class="size-[19px]">
								<path d="M4 7h16M6 4h12v16H6z" /><path d="M9 11h6M9 15h4" />
							</svg>
						{:else}
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" class="size-[19px]">
								<circle cx="12" cy="5" r="2" /><circle cx="12" cy="12" r="2" /><circle cx="12" cy="19" r="2" />
							</svg>
						{/if}
					</span>
					<span class="min-w-0">
						<span class="block text-xs font-semibold">{r.title}</span>
						<span class="mt-0.5 block truncate text-[10px] text-muted-foreground">{r.hint}</span>
					</span>
				</button>
			{/each}
		</nav>

		</aside>

	<!-- ============================== право ============================ -->
	<div class="flex min-w-0 flex-1 flex-col">
		<!-- No header. Everything it held is already on screen and was a second
		     copy of it: the brand and the current screen are in the sidebar, the
		     three dots are the service rows, and the checks summary is the
		     checklist. A copy of a fact is a place to be wrong, and the person
		     pays for it by reading the same thing twice. Refresh went to the
		     bottom strip, which is on screen at all times anyway. -->

		<main class="min-h-0 flex-1 overflow-auto px-6 py-5">
			{#if hub.route === 'server'}
				<Server />
			{:else if hub.route === 'backups'}
				<Backups />
			{:else}
				<More />
			{/if}
		</main>

		<!-- The outcome of an operation, and any interface error the page
		     catches, readable from whichever screen is open. -->
		<footer class="flex h-9 shrink-0 items-center gap-3 border-t border-border px-5 text-[11px]">
			<span class="shrink-0 text-muted-foreground/70">{t('footer.last')}</span>
			<span data-role="last-action" class="min-w-0 flex-1 truncate whitespace-pre-line {strip}">{hub.last.text}</span>
			<button
				type="button"
				data-role="refresh"
				onclick={() => {
					hub.refreshData()
					hub.loadSetup()
					hub.say(t('footer.refreshing'))
				}}
				title={t('footer.refresh')}
				aria-label={t('footer.refresh')}
				class="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-card hover:text-foreground"
			>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" class="size-4">
					<polyline points="23 4 23 10 17 10" /><polyline points="1 20 1 14 7 14" />
					<path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
				</svg>
			</button>
		</footer>
	</div>
	</div>
</div>

<ConfirmKill />
<RestoreAsk />
<DeleteAsk />
<QuitAsk />