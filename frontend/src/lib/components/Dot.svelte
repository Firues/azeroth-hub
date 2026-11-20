<!--
  The state dot. Three marks that mean three different things, because a red
  exclamation that is sometimes harmless is a dot people learn to ignore:

    running / external - the service is up
    starting / stopping - it is on its way
    stopped / failed - it is not, and for failed the dot is red
-->
<script lang="ts">
	import { stateLabel } from '$lib/i18n.svelte.js'

	let { state = 'stopped' }: { state?: string } = $props()

	const tone = $derived(
		state === 'running' || state === 'external'
			? 'bg-ok'
			: state === 'starting' || state === 'stopping'
				? 'bg-warn'
				: state === 'failed'
					? 'bg-bad'
					: 'bg-muted-foreground/50'
	)
</script>

<span class="inline-block size-2 shrink-0 rounded-full {tone}" title={stateLabel(state) || state}></span>