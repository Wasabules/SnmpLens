import './style.css'
import './styles/shared.css'
import { setupI18n } from './i18n/index.js'
import App from './App.svelte'

setupI18n().then(() => {
  new App({
    target: document.getElementById('app')
  })
})
