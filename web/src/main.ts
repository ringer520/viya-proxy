import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import './usability.css'
import './feature-layout.css'
import './refinement.css'
import './theme.css'
import './site-list.css'
import './forward-list.css'
import './security-policy.css'
import './settings-panel.css'
import { dialog } from './directives/dialog'

createApp(App).directive('dialog', dialog).mount('#app')

import './operations.css'
import './anime-themes.css'

import './traffic-display.css'
