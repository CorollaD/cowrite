export const zhihu = {
  id: 'zhihu',
  name: '知乎',
  editorURL: 'https://zhuanlan.zhihu.com/write',
  // A cookie that is only set once signed in.
  loginCookie: { url: 'https://www.zhihu.com', name: 'z_c0' },

  // Runs in the page's main world.
  async fill({ title, html }) {
    const titleEl = await window.__cw_waitFor('textarea[placeholder*="标题"], .WriteIndex-titleInput textarea');
    if (!titleEl) throw new Error('找不到知乎的标题输入框，编辑器结构可能变了');
    window.__cw_setValue(titleEl, title);

    const body = await window.__cw_waitFor('.public-DraftEditor-content, [contenteditable="true"]');
    if (!body) throw new Error('找不到知乎的正文编辑器，编辑器结构可能变了');
    window.__cw_pasteHTML(body, html);
    return { ok: true, url: location.href };
  },
};
