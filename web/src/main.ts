import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/700.css'
import './assets/main.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { trackAppHeight } from './lib/appHeight'
import { registerPWA } from './lib/pwa'

trackAppHeight()
createApp(App).use(createPinia()).mount('#app')
void registerPWA({
  production: import.meta.env.PROD || import.meta.env.VITE_E2E === '1',
  secureContext: window.isSecureContext,
  serviceWorker: 'serviceWorker' in navigator ? navigator.serviceWorker : undefined,
  debug: (...args) => console.debug(...args),
})
