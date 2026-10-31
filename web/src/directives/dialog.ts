import type { ObjectDirective } from 'vue'

// All create/edit dialogs retain their contents until explicitly closed.
// Nested dependency dialogs share the scroll lock and keep keyboard focus on top.
const stack: HTMLElement[] = []
const cleanups = new WeakMap<HTMLElement, () => void>()
let originalOverflow = ''
let nextId = 0
export const dialog: ObjectDirective<HTMLElement> = {
  mounted(overlay) {
    const modal = overlay.querySelector<HTMLElement>('.modal')
    if (!modal) return
    const previousFocus = document.activeElement as HTMLElement | null
    if (!stack.length) { originalOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden' }
    stack.push(modal)
    if (!modal.hasAttribute('role')) modal.setAttribute('role', 'dialog')
    modal.setAttribute('aria-modal', 'true')
    modal.tabIndex = -1
    const heading = modal.querySelector<HTMLElement>('h2')
    if (heading && !modal.hasAttribute('aria-labelledby')) {
      if (!heading.id) heading.id = `viya-dialog-title-${++nextId}`
      modal.setAttribute('aria-labelledby', heading.id)
    }
    function focusable() {
      return Array.from(modal!.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]')).filter(el => el.getClientRects().length > 0)
    }
    function keydown(event: KeyboardEvent) {
      if (stack.at(-1) !== modal || event.key !== 'Tab') return
      const controls = focusable(), first = controls[0], last = controls.at(-1)
      if (!first || !last) { event.preventDefault(); modal!.focus(); return }
      if (event.shiftKey && (document.activeElement === first || document.activeElement === modal)) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && (document.activeElement === last || document.activeElement === modal)) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', keydown)
    queueMicrotask(() => { if (modal.isConnected && stack.at(-1) === modal) modal.focus() })
    cleanups.set(overlay, () => {
      document.removeEventListener('keydown', keydown)
      const index = stack.indexOf(modal)
      if (index >= 0) stack.splice(index, 1)
      if (!stack.length) document.body.style.overflow = originalOverflow
      if (previousFocus?.isConnected && (!stack.length || stack.at(-1)?.contains(previousFocus))) previousFocus.focus()
      else stack.at(-1)?.focus()
    })
  },
  beforeUnmount(overlay) { cleanups.get(overlay)?.(); cleanups.delete(overlay) },
}
