import './style.css'
import './styles/shared.css'
import { setupI18n } from './i18n/index.js'
import { mount } from 'svelte'
import App from './App.svelte'

// `new App({ target })` was the Svelte 3 and 4 way and Svelte 5 refuses it
// outright (`component_api_invalid_new`). A component is no longer a class, so
// there is nothing to construct; `mount` is the replacement and this is the only
// call site in the app.
//
// setupI18n() must still resolve BEFORE this runs. svelte-i18n throws on a
// `$_(...)` evaluated before a locale is loaded, and App.svelte reads one in its
// first render — so mounting early does not show untranslated text, it shows
// nothing at all.
setupI18n().then(() => {
  mount(App, {
    target: document.getElementById('app')
  })
})
