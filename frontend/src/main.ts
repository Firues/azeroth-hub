import './app.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { hub } from './lib/state.svelte.js'

const target = document.getElementById('app')
if (!target) throw new Error('нет #app в index.html')

mount(App, { target })

// The one call that brings the interface to life: it subscribes to the backend
// events and paints once with what the cache already holds.
hub.start()