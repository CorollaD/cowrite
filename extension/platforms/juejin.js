export const juejin = {
  id: 'juejin',
  name: '掘金',
  editorURL: 'https://juejin.cn/editor/drafts/new',
  loginCookie: { url: 'https://juejin.cn', name: 'sessionid' },

  // Juejin's editor is markdown-native, so the markdown goes in directly
  // and renders with its own pipeline rather than pasted HTML.
  async fill({ title, markdown }) {
    const titleEl = await window.__cw_waitFor('input[placeholder*="标题"]');
    if (!titleEl) throw new Error('找不到掘金的标题输入框，编辑器结构可能变了');
    window.__cw_setValue(titleEl, title);

    const cm = await window.__cw_waitFor('.CodeMirror, .bytemd-editor .CodeMirror, textarea');
    if (!cm) throw new Error('找不到掘金的正文编辑器，编辑器结构可能变了');

    if (cm.CodeMirror) {
      cm.CodeMirror.setValue(markdown);
    } else if (!window.__cw_setValue(cm, markdown)) {
      throw new Error('无法填入掘金正文');
    }
    return { ok: true, url: location.href };
  },
};
