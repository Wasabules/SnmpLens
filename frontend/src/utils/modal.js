/**
 * Dismiss a dialog when the click landed on the backdrop and not inside it.
 *
 * The obvious way to write this is a handler on the backdrop plus
 * `on:click|stopPropagation` on the panel, and that is how all eleven dialogs
 * here were written. It works, but the panel then carries a handler whose only
 * job is to do nothing — which the accessibility checks read as an interactive
 * element, correctly, since they cannot know the handler is inert. Answering
 * that with a role and a `tabindex` would be decorating a lie.
 *
 * Asking the backdrop whether the event was its own removes the handler
 * instead. `currentTarget` is the element the listener is on; `target` is what
 * was actually hit. They are equal only for the backdrop itself, so a click
 * anywhere in the panel is ignored without the panel having to say so.
 *
 * It also stops less. `stopPropagation` on the panel silences that event for
 * everything above the dialog, not just for the backdrop.
 *
 * @param {() => void} dismiss
 * @returns {(event: Event) => void}
 */
export function onBackdrop(dismiss) {
  return (event) => {
    if (event.target === event.currentTarget) dismiss();
  };
}
