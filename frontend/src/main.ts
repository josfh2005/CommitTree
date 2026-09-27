import { mount } from 'svelte'
import './theme.css'
import App from './App.svelte'
import { highContrast, theme } from './lib/stores'
import { applyContrast, applyTheme } from './lib/theme'

// Before mounting, so the first paint already uses the chosen palette.
highContrast.subscribe((on) => applyContrast(document.documentElement, on))
theme.subscribe((t) => applyTheme(document.documentElement, t))

const app = mount(App, { target: document.getElementById('app')! })

export default app
