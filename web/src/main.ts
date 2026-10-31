import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import './usability.css'
import './feature-layout.css'
import './refinement.css'
import './theme.css'
import { dialog } from './directives/dialog'
createApp(App).directive('dialog', dialog).mount('#app')
