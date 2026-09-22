export const csdn = {
  id: 'csdn',
  name: 'CSDN',
  editorURL: 'https://editor.csdn.net/md/',
  loginCookie: { url: 'https://www.csdn.net', name: 'UserName' },

  // CSDN's markdown editor is CodeMirror-based.
  async fill({ title, markdown }) {
    const titleEl = await window.__cw_waitFor(
      'input.article-bar__title, input[placeholder*="标题"]');
    if (!titleEl) throw new Error('找不到 CSDN 的标题输入框，编辑器结构可能变了');
    window.__cw_setValue(titleEl, title);

    const cm = await window.__cw_waitFor('.CodeMirror, textarea#editor');
    if (!cm) throw new Error('找不到 CSDN 的正文编辑器，编辑器结构可能变了');
    if (cm.CodeMirror) {
      cm.CodeMirror.setValue(markdown);
    } else if (!window.__cw_setValue(cm, markdown)) {
      throw new Error('无法填入 CSDN 正文');
    }
    return { ok: true, url: location.href };
  },
};
