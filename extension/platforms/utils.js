// Helpers injected into the page's main world.
//
// Platform editors are React or ProseMirror based, so assigning .value
// directly does not register: the framework tracks its own state and
// overwrites the DOM on the next render. Setting through the native
// setter and dispatching the events the framework listens for is what
// makes the change stick.
export function injectUtils() {
  window.__cw_waitFor = (selector, timeout = 15000) =>
    new Promise(resolve => {
      const found = document.querySelector(selector);
      if (found) return resolve(found);

      const observer = new MutationObserver(() => {
        const el = document.querySelector(selector);
        if (el) {
          observer.disconnect();
          resolve(el);
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
      setTimeout(() => {
        observer.disconnect();
        resolve(document.querySelector(selector));
      }, timeout);
    });

  window.__cw_setValue = (el, value) => {
    if (!el) return false;
    el.focus();
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
      const proto = el.tagName === 'INPUT'
        ? window.HTMLInputElement.prototype
        : window.HTMLTextAreaElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
      if (setter) setter.call(el, value); else el.value = value;
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    }
    if (el.isContentEditable) {
      el.innerHTML = value;
      el.dispatchEvent(new InputEvent('input', { bubbles: true }));
      return true;
    }
    return false;
  };

  // Pasting is how rich content reaches a contenteditable editor with the
  // platform's own sanitizer and image handling applied.
  window.__cw_pasteHTML = (el, html) => {
    if (!el) return false;
    el.focus();
    const dt = new DataTransfer();
    dt.setData('text/html', html);
    dt.setData('text/plain', html.replace(/<[^>]+>/g, ''));
    return el.dispatchEvent(new ClipboardEvent('paste', {
      bubbles: true, cancelable: true, clipboardData: dt,
    }));
  };
}
