export const wechat = {
  id: 'wechat',
  name: '公众号',
  editorURL: 'https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_edit_v2&action=edit&isNew=1&type=77',
  loginCookie: { url: 'https://mp.weixin.qq.com', name: 'slave_sid' },

  // For accounts without API credentials, or without certification, which
  // is what the draft API requires.
  async fill({ title, html }) {
    const titleEl = await window.__cw_waitFor(
      '#title, textarea[placeholder*="标题"], .js_title input');
    if (!titleEl) throw new Error('找不到公众号的标题输入框，编辑器结构可能变了');
    window.__cw_setValue(titleEl, title);

    // The body is a ProseMirror instance inside the editing iframe, and
    // pasting lets WeChat run its own sanitizer and image upload.
    const frame = document.querySelector('#ueditor_0, iframe.js_editor_iframe');
    const doc = frame?.contentDocument;
    const body = doc?.body || await window.__cw_waitFor('.ProseMirror, [contenteditable="true"]');
    if (!body) throw new Error('找不到公众号的正文编辑器，编辑器结构可能变了');

    body.focus();
    const dt = new DataTransfer();
    dt.setData('text/html', html);
    body.dispatchEvent(new ClipboardEvent('paste', {
      bubbles: true, cancelable: true, clipboardData: dt,
    }));
    return { ok: true, url: location.href };
  },
};
