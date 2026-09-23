# 安装与部署

cowrite 是一个本地运行的写作程序。它只有一个可执行文件，不需要装数据库、
不需要装运行时、也不用配置环境。下载、双击、打开浏览器，就能开始写。

文章以普通 `.md` 文件存在你自己的目录里，随时可以用别的编辑器打开，
也可以用 git 管理。卸载 cowrite 不会带走你的文章。

---

## 目录

- [选择你的版本](#选择你的版本)
- [macOS](#macos)
- [Windows](#windows)
- [Linux](#linux)
- [首次启动](#首次启动)
- [配置 AI](#配置-ai)
- [常用参数](#常用参数)
- [开机自启](#开机自启)
- [从源码构建](#从源码构建)
- [升级与卸载](#升级与卸载)
- [常见问题](#常见问题)

---

## 选择你的版本

| 你的电脑 | 下载这个 |
|---|---|
| Mac（M1/M2/M3/M4 芯片） | `cowrite-darwin-arm64` |
| Mac（Intel 芯片） | `cowrite-darwin-amd64` |
| Windows | `cowrite-windows-amd64.exe` |
| Linux（普通 PC / 服务器） | `cowrite-linux-amd64` |
| Linux（树莓派 / ARM 服务器） | `cowrite-linux-arm64` |

**不确定 Mac 是哪种芯片？** 点左上角苹果图标 → 「关于本机」，
写着「Apple M 系列」就选 arm64，写着「Intel」就选 amd64。

---

## macOS

### 1. 下载并放好

把下载的文件改名成 `cowrite`，放到一个固定位置，比如：

```bash
mkdir -p ~/Applications
mv ~/Downloads/cowrite-darwin-arm64 ~/Applications/cowrite
chmod +x ~/Applications/cowrite
```

`chmod +x` 是给文件加上「可执行」权限，下载来的文件默认没有。

### 2. 绕过安全提示

macOS 会拦截未签名的程序。第一次运行前执行：

```bash
xattr -d com.apple.quarantine ~/Applications/cowrite
```

> 如果不做这一步，双击会弹「无法打开，因为无法验证开发者」。
> 也可以在弹窗后去「系统设置 → 隐私与安全性」点「仍要打开」。

### 3. 启动

```bash
~/Applications/cowrite
```

看到这行就说明起来了：

```
level=INFO msg="cowrite listening" url=http://127.0.0.1:8080
```

浏览器打开 <http://127.0.0.1:8080>。

### 想放进 PATH，随处输 `cowrite` 启动

```bash
sudo ln -s ~/Applications/cowrite /usr/local/bin/cowrite
```

之后任何终端里输 `cowrite` 就能启动。

---

## Windows

### 1. 下载并放好

把 `cowrite-windows-amd64.exe` 改名成 `cowrite.exe`，
放到一个固定文件夹，比如 `C:\Users\你的用户名\cowrite\`。

### 2. 绕过 SmartScreen

双击时 Windows 可能弹「已保护你的电脑」蓝色窗口。
点 **「更多信息」→「仍要运行」**。

这是因为程序没有购买代码签名证书，不是有问题。

### 3. 启动

双击 `cowrite.exe`，会弹出一个黑色命令行窗口并保持打开——
**这个窗口不能关**，关了服务就停了。可以最小化。

浏览器打开 <http://127.0.0.1:8080>。

### 不想看到黑窗口

新建一个 `启动cowrite.vbs`，内容：

```vbs
CreateObject("WScript.Shell").Run """C:\Users\你的用户名\cowrite\cowrite.exe""", 0, False
```

双击这个 vbs 就会在后台启动，没有窗口。
停止的话去任务管理器结束 `cowrite.exe`。

---

## Linux

### 1. 下载并放好

```bash
sudo mv ~/Downloads/cowrite-linux-amd64 /usr/local/bin/cowrite
sudo chmod +x /usr/local/bin/cowrite
```

### 2. 启动

```bash
cowrite
```

浏览器打开 <http://127.0.0.1:8080>。

### 桌面版需要装一个包（可选但建议）

cowrite 把 AI 密钥存进系统密钥环。桌面 Linux 需要 Secret Service：

```bash
# Debian / Ubuntu
sudo apt install gnome-keyring

# Fedora
sudo dnf install gnome-keyring

# Arch
sudo pacman -S gnome-keyring
```

**没装也能用**——cowrite 会自动降级成一个权限 `0600` 的文件
（`~/cowrite/.cowrite/secrets.json`），只有你自己能读。
服务器上没有图形界面，走的就是这条路，不用管。

---

## 首次启动

第一次运行会在你的家目录建一个工作区：

```
~/cowrite/
├── posts/              ← 你的文章都在这里，普通 .md 文件
│   └── 2026/09/
└── .cowrite/           ← 程序自己用的，可以不管
    ├── index.db        ← 搜索索引，删了会自动重建
    ├── versions/       ← 版本快照，第一次存快照后出现
    ├── sounds/         ← 环境音，导入音频后出现
    └── secrets.json    ← 仅在没有系统密钥环时出现
```

刚装好时只有 `posts/` 和 `index.db`，其余目录用到了才建。

**`posts/` 是你的东西，`.cowrite/` 是程序的东西。**
备份只需要备份 `posts/`，其余都能重新生成。

想换个地方放文章：

```bash
cowrite --workspace ~/Documents/我的文章
```

---

## 配置 AI

AI 功能（润色、续写、起标题、摘要、语音整理）需要一个模型。
打开界面点工具栏的 **⚙**。

### 三种选择

**一、本地模型，完全免费，稿件不出本机**

先装 [Ollama](https://ollama.com)，然后：

```bash
ollama pull qwen3
```

cowrite 会自动探测到本机的 Ollama，设置里选它即可，不用填 key。

**二、在线模型，中文写作推荐 DeepSeek**

去 [platform.deepseek.com](https://platform.deepseek.com) 注册拿 key，
在设置里选 DeepSeek，粘贴 key，模型填 `deepseek-chat`。

**三、免费额度**

设置里带「免费」标签的服务商都有免费额度，按提示填 key 即可。

### 密钥存在哪

按这个顺序找：

1. 环境变量，比如 `DEEPSEEK_API_KEY`、`OPENAI_API_KEY`
2. 系统密钥环（macOS 钥匙串 / Windows 凭据管理器 / Linux gnome-keyring）
3. 上述都不可用时，降级到 `0600` 权限的文件

**密钥不会写进你的文章目录，也不会传回浏览器。**
用环境变量的话，key 根本不落盘：

```bash
export DEEPSEEK_API_KEY=sk-xxxxx
cowrite
```

---

## 常用参数

```bash
cowrite --port 9000                      # 换端口（默认 8080 被占用时）
cowrite --workspace ~/Documents/写作      # 换文章目录
cowrite --version                        # 看版本
cowrite --help                           # 看全部参数
```

### ⚠️ 关于 `--host`

默认只监听 `127.0.0.1`，即只有本机能访问。

**这个程序没有任何登录和密码。** 如果你改成 `--host 0.0.0.0`，
同一个网络里的任何人都能打开你的文章、用你的 AI 额度、
以你的身份往各平台发内容。

真要在服务器上跑给自己远程用，正确做法是**保持默认绑定，用 SSH 隧道**：

```bash
# 在你自己的电脑上执行
ssh -L 8080:127.0.0.1:8080 你的用户名@服务器地址
```

然后在本机浏览器打开 <http://127.0.0.1:8080>，流量走加密隧道，
服务器上的端口不对外暴露。

---

## 开机自启

### macOS

新建 `~/Library/LaunchAgents/com.cowrite.plist`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.cowrite</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/你的用户名/Applications/cowrite</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
```

加载：

```bash
launchctl load ~/Library/LaunchAgents/com.cowrite.plist
```

取消开机自启：

```bash
launchctl unload ~/Library/LaunchAgents/com.cowrite.plist
```

### Windows

按 `Win + R`，输入 `shell:startup` 回车，
把上面那个 `启动cowrite.vbs` 的快捷方式拖进打开的文件夹。

### Linux（systemd 用户服务）

新建 `~/.config/systemd/user/cowrite.service`：

```ini
[Unit]
Description=cowrite

[Service]
ExecStart=/usr/local/bin/cowrite
Restart=on-failure

[Install]
WantedBy=default.target
```

启用：

```bash
systemctl --user daemon-reload
systemctl --user enable --now cowrite
systemctl --user status cowrite      # 看运行状态
journalctl --user -u cowrite -f      # 看日志
```

---

## 从源码构建

需要 **Go 1.26.4 或更高**（公式渲染库的要求）。

```bash
git clone <仓库地址>
cd cowrite
make build          # 产物在 bin/cowrite
make test           # 跑测试
make release        # 交叉编译全部五个平台，产物在 dist/
```

不用装 C 编译器——SQLite 用的是纯 Go 实现，`CGO_ENABLED=0`。
前端是原生 TypeScript，没有构建步骤，也不需要 Node。

---

## 升级与卸载

### 升级

下载新版本，覆盖掉旧的那个文件，重启即可。
文章、设置、版本历史都不受影响。

```bash
# macOS / Linux 示例
mv ~/Downloads/cowrite-darwin-arm64 ~/Applications/cowrite
chmod +x ~/Applications/cowrite
```

### 卸载

删掉可执行文件就行。想连数据一起删：

```bash
rm -rf ~/cowrite          # ⚠️ 这会删掉你所有的文章
```

密钥存在系统密钥环里的话，去钥匙串 / 凭据管理器搜 `cowrite` 手动删。

---

## 常见问题

**打不开 <http://127.0.0.1:8080>**

先看终端有没有 `cowrite listening` 那行。有的话再确认端口对不对——
如果 8080 被别的程序占了，换一个：`cowrite --port 9000`。

**提示端口被占用**

```bash
# macOS / Linux 查谁占了
lsof -i :8080

# Windows
netstat -ano | findstr :8080
```

**macOS 说「无法打开，因为无法验证开发者」**

见 [macOS 第 2 步](#2-绕过安全提示)。

**Windows 弹蓝色「已保护你的电脑」**

点「更多信息」→「仍要运行」。

**文章不见了**

文章是普通文件，去 `~/cowrite/posts/` 看一眼还在不在。
在的话是索引出了问题，删掉 `~/cowrite/.cowrite/index.db` 重启，会自动重建。
界面里删除文章只是从列表移除，文件本身不会被删。

**搜不到刚写的内容**

索引是保存时更新的。如果是在 cowrite 外面用别的编辑器改的文件，
它会自动检测到；实在不行重启一次会全量重扫。

**AI 按钮是灰的**

还没配模型。点 ⚙ 选一个服务商，填好 key 和模型名，保存。

**AI 报错「你的 IP 不在白名单」**

这是微信公众号的接口限制，不是 AI 的问题。
去公众号后台「设置与开发 → 基本配置 → IP 白名单」加上你的公网 IP。
家用宽带 IP 会变，变了要重新加。

**语音录音没反应**

浏览器需要麦克风权限，第一次用会弹窗询问，要点允许。
另外 Chrome 只在 `localhost` 或 https 下允许录音——
用默认的 `127.0.0.1` 访问就没问题。

**想换个地方放文章**

```bash
cowrite --workspace /你想要的/路径
```

已有的文章可以直接把 `posts/` 整个目录拷过去。
