import { onBeforeUnmount, watch, ref, readonly } from 'vue'
const media = window.matchMedia('(prefers-reduced-motion: reduce)')
const systemReduced = ref(media.matches)
export const reducedMotion = readonly(systemReduced)

/** Initialized once by App; components consume the shared preference. */
export function useMotionPreferences() {
  localStorage.removeItem('viya-motion')
  const changed = () => { systemReduced.value = media.matches }
  media.addEventListener('change', changed)
  watch(reducedMotion, () => {
    document.documentElement.dataset.motion = reducedMotion.value ? 'reduced' : 'full'
  }, { immediate: true })
  onBeforeUnmount(() => media.removeEventListener('change', changed))
}
