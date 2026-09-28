import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/700.css'
import './assets/main.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { trackAppHeight } from './lib/appHeight'
import { registerPWA } from './lib/pwa'
import { installNotificationSpy } from './lib/notificationSpy'

// E2E builds: record notifications for the scenarios (dropped in production).
if (import.meta.env.VITE_E2E === '1') installNotificationSpy(window)
trackAppHeight()
createApp(App).use(createPinia()).mount('#app')
void registerPWA({
  production: import.meta.env.PROD || import.meta.env.VITE_E2E === '1',
  secureContext: window.isSecureContext,
  serviceWorker: 'serviceWorker' in navigator ? navigator.serviceWorker : undefined,
  debug: (...args) => console.debug(...args),
})
