import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/700.css'
import './assets/main.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { trackAppHeight } from './lib/appHeight'

trackAppHeight()
createApp(App).use(createPinia()).mount('#app')
