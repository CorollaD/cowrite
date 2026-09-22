// Connects to the local cowrite server and carries out publish jobs.
//
// Everything stays between this machine and the platforms the user is
// already signed in to: the extension talks only to 127.0.0.1 and to the
// platform tabs it opens.

import { injectUtils } from './platforms/utils.js';
import { zhihu } from './platforms/zhihu.js';
import { juejin } from './platforms/juejin.js';
import { wechat } from './platforms/wechat.js';
import { csdn } from './platforms/csdn.js';
import { segmentfault } from './platforms/segmentfault.js';

const PLATFORMS = { zhihu, juejin, wechat, csdn, segmentfault };
const DEFAULT_PORT = 8080;

let socket = null;
let reconnectDelay = 1000;

async function settings() {
  const { port, token } = await chrome.storage.local.get(['port', 'token']);
  return { port: port || DEFAULT_PORT, token: token || '' };
}

async function connect() {
  const { port, token } = await settings();
  if (!token) return; // not paired yet

  try {
    socket = new WebSocket(`ws://127.0.0.1:${port}/api/bridge`);
  } catch {
    return scheduleReconnect();
  }

  socket.onopen = () => {
    socket.send(JSON.stringify({ type: 'hello', token }));
  };

  socket.onmessage = async event => {
    let msg;
    try { msg = JSON.parse(event.data); } catch { return; }
    if (msg.type === 'hello') {
      reconnectDelay = 1000;
      chrome.action.setBadgeText({ text: '●' });
      chrome.action.setBadgeBackgroundColor({ color: '#16a34a' });
      return;
    }
    if (msg.type !== 'publish') return;

    const req = msg.payload;
    let result;
    try {
      result = await runJob(req);
    } catch (err) {
      result = { id: req.id, ok: false, error: String(err.message || err) };
    }
    socket.send(JSON.stringify({ type: 'result', payload: result }));
  };

  socket.onclose = () => {
    chrome.action.setBadgeText({ text: '' });
    scheduleReconnect();
  };
  socket.onerror = () => socket?.close();
}

function scheduleReconnect() {
  socket = null;
  setTimeout(connect, reconnectDelay);
  // Back off so a stopped server does not mean a reconnect every second.
  reconnectDelay = Math.min(reconnectDelay * 2, 30000);
}

async function isLoggedIn(platform) {
  if (!platform.loginCookie) return true;
  const cookie = await chrome.cookies.get(platform.loginCookie);
  return Boolean(cookie?.value);
}

async function runJob(req) {
  const platform = PLATFORMS[req.platform];
  if (!platform) throw new Error(`不支持的平台: ${req.platform}`);

  // Checking first means the user is told to sign in, rather than having
  // their article typed into a login page.
  if (!(await isLoggedIn(platform))) {
    throw new Error(`你还没有登录${platform.name}，请先在浏览器里登录`);
  }

  const tab = await chrome.tabs.create({ url: platform.editorURL, active: true });
  await waitForTabLoad(tab.id);

  // The editor mounts after load, and the injected helpers wait for it.
  await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: 'MAIN',
    func: injectUtils,
  });

  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: 'MAIN',
    func: platform.fill,
    args: [{ title: req.title, html: req.html, markdown: req.markdown }],
  });

  return { id: req.id, ok: true, url: result?.url };
}

function waitForTabLoad(tabId) {
  return new Promise(resolve => {
    const listener = (id, info) => {
      if (id === tabId && info.status === 'complete') {
        chrome.tabs.onUpdated.removeListener(listener);
        resolve();
      }
    };
    chrome.tabs.onUpdated.addListener(listener);
    setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve();
    }, 20000);
  });
}

chrome.runtime.onStartup.addListener(connect);
chrome.runtime.onInstalled.addListener(connect);
chrome.storage.onChanged.addListener(changes => {
  if (changes.token || changes.port) {
    socket?.close();
    reconnectDelay = 1000;
    connect();
  }
});

connect();
