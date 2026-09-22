const portEl = document.getElementById('port');
const tokenEl = document.getElementById('token');
const stateEl = document.getElementById('state');

chrome.storage.local.get(['port', 'token']).then(({ port, token }) => {
  portEl.value = port || 8080;
  if (token) tokenEl.placeholder = '已保存，留空则不修改';
});

chrome.action.getBadgeText({}).then(text => {
  stateEl.textContent = text ? '已连接' : '未连接';
  stateEl.style.color = text ? '#16a34a' : '#888';
});

document.getElementById('save').onclick = async () => {
  const update = { port: Number(portEl.value) || 8080 };
  if (tokenEl.value) update.token = tokenEl.value;
  await chrome.storage.local.set(update);
  tokenEl.value = '';
  stateEl.textContent = '正在连接…';
};
