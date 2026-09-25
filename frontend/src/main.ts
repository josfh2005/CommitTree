import { mount } from 'svelte'
import './theme.css'
import App from './App.svelte'
import { highContrast } from './lib/stores'
import { applyContrast } from './lib/theme'

// Before mounting, so the first paint already uses the chosen palette.
highContrast.subscribe((on) => applyContrast(document.documentElement, on))

const app = mount(App, { target: document.getElementById('app')! })

export default app
