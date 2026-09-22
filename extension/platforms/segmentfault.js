export const segmentfault = {
  id: 'segmentfault',
  name: '思否',
  editorURL: 'https://segmentfault.com/write',
  loginCookie: { url: 'https://segmentfault.com', name: 'PHPSESSID' },

  async fill({ title, markdown }) {
    const titleEl = await window.__cw_waitFor('input[placeholder*="标题"], #title');
    if (!titleEl) throw new Error('找不到思否的标题输入框，编辑器结构可能变了');
    window.__cw_setValue(titleEl, title);

    const body = await window.__cw_waitFor('textarea.sf-editor-input, .CodeMirror, textarea');
    if (!body) throw new Error('找不到思否的正文编辑器，编辑器结构可能变了');
    if (body.CodeMirror) {
      body.CodeMirror.setValue(markdown);
    } else if (!window.__cw_setValue(body, markdown)) {
      throw new Error('无法填入思否正文');
    }
    return { ok: true, url: location.href };
  },
};
