# 前端 API 错误响应 Body 读取改造计划（v11 终稿）

## 方案

**所有 API 调用**在 `!response.ok` 时：读取 body → `showToast('前缀：' + t, 'error')` → 返回原值（**去掉 `console.warn`**）

---

## 一、[`frontend/static/chat-api.js`](frontend/static/chat-api.js)

### 1.1 原 `console.warn` + return 类（9 处）

| # | 函数 | 当前 | 改为 |
|---|------|------|------|
| 1 | `fetchChatTitle()` | `if(!response.ok)return;` | `const t=await response.text();showToast('获取标题失败：'+t,'error');return;` |
| 2 | `putChatTitle()` | `console.warn('更新标题失败:',status);return false;` | `const t=await response.text();showToast('更新标题失败：'+t,'error');return false;` |
| 3 | `createBlankChat()` | `console.warn('初始化对话失败:',status);return null;` | `const t=await response.text();showToast('初始化对话失败：'+t,'error');return null;` |
| 4 | `onChatLogin()` | `console.warn('登录失败:',status);return false;` | `const t=await response.text();showToast('登录失败：'+t,'error');return false;` |
| 5 | `onChatLogout()` | `console.warn('退出登录失败:',status);return false;` | `const t=await response.text();showToast('退出登录失败：'+t,'error');return false;` |
| 6 | `switchChat()` | `console.warn('切换会话失败:',status);return null;` | `const t=await response.text();showToast('切换会话失败：'+t,'error');return null;` |
| 7 | `fetchChatTags()` | `console.warn('获取话题标签失败:',status);return null;` | `const t=await response.text();showToast('获取话题标签失败：'+t,'error');return null;` |
| 8 | `fetchChatGroups()` | `console.warn('获取聊天分组失败:',status);return null;` | `const t=await response.text();showToast('获取聊天分组失败：'+t,'error');return null;` |
| 9 | `fetchFavorites()` | `console.warn('获取收藏列表失败:',status);return null;` | `const t=await response.text();showToast('获取收藏列表失败：'+t,'error');return null;` |

### 1.2 原 `return response.ok` 类（8 处）

| # | 函数 | 当前 | 改为 |
|---|------|------|------|
| 10 | `addFavoriteChat()` | `return response.ok;` | `if(!response.ok){const t=await response.text();showToast('添加收藏失败：'+t,'error');return false;}return true;` |
| 11 | `removeFavoriteChat()` | `return response.ok;` | 同上 |
| 12 | `togglePinChat()` | `return response.ok;` | 同上（前缀：'操作失败：'） |
| 13 | `deleteChat()` | `return response.ok;` | 同上（前缀：'删除失败：'） |
| 14 | `restoreChat()` | `return response.ok;` | 同上（前缀：'恢复失败：'） |
| 15 | `permanentDeleteChat()` | `return response.ok;` | 同上（前缀：'永久删除失败：'） |
| 16 | `emptyTrash()` | `return response.ok;` | 同上（前缀：'清空回收站失败：'） |
| 17 | `deleteMessage()` | `return response.ok;` | 同上（前缀：'删除失败：'） |

### 1.3 `extractTraits()`（console.warn → showToast）

| # | 当前 | 改为 |
|---|------|------|
| 18 | `const errText=await response.text();console.warn(...);return null;` | `showToast('提取个人特征失败：'+errText,'error');return null;` |

### 1.4 原无声类（全部改为 showToast，5 处）

| # | 函数 | 当前 | 改为 |
|---|------|------|------|
| 19 | `fetchLlmInfo()` | 仅 `if(response.ok)` | `const t=await response.text();showToast('获取AI信息失败：'+t,'error');return null;` |
| 20 | `fetchSession()` | 仅 `if(response.ok)` | `const t=await response.text();showToast('获取session失败：'+t,'error');return null;` |
| 21 | `fetchChatList()` | 仅 `if(response.ok)` | `const t=await response.text();showToast('获取对话列表失败：'+t,'error');return null;` |
| 22 | `listDeletedChats()` | 仅 `if(response.ok)` | `const t=await response.text();showToast('获取回收站列表失败：'+t,'error');return null;` |
| 23 | GET apikey | 仅 `if(resp.ok)` | `const t=await resp.text();showToast('获取API-Key设置失败：'+t,'error');return null;` |

### 1.5 不变（1 处）

| # | 函数 | 说明 |
|---|------|------|
| 24 | `sendChatMessage()` | 已读 body + throw，`handleStreamError` 捕获后展示 |

---

## 二、调用方去掉重复 Toast（[`frontend/static/chat-list.js`](frontend/static/chat-list.js) — 10 处）

| # | 行 | 当前 | 改为 |
|---|----|------|------|
| 1 | L782-784 | `if(!ok){showToast('重命名失败','error');return false;}` | `if(!ok)return false;` |
| 2 | L801-804 | `if(!ok){showToast('取消收藏失败','error');return;}` | `if(!ok)return;` |
| 3 | L848-850 | `if(!ok){showToast('添加收藏失败','error');return false;}` | `if(!ok)return false;` |
| 4 | L997-999 | `if(!ok){showToast('操作失败','error');return;}` | `if(!ok)return;` |
| 5 | L1118-1120 | `if(!ok){showToast('删除失败','error');return;}` | `if(!ok)return;` |
| 6 | L1281-1283 | `if(!ok){showToast('恢复失败','error');return;}` | `if(!ok)return;` |
| 7 | L1324-1326 | `if(!ok){showToast('永久删除失败','error');return;}` | `if(!ok)return;` |
| 8 | L1402-1405 | `if(!ok){showToast('清空回收站失败','error');return;}` | `if(!ok)return;` |
| 9 | L220-222 | `if(!result){showToast('加载对话失败','error');return;}` | `if(!result)return;` |
| 10 | L705-707 | `if(!result){showToast('提取个人特征失败','error');return;}` | `if(!result)return;` |

---

## 三、其他 `.js` 文件（7 处）— 全部 showToast

| # | 文件 | 行 | 当前 | 改为 |
|---|------|----|------|------|
| 1 | `apikey-dialog.js` | L195 | `return response.ok` | `if(!response.ok){const t=await response.text();showToast('保存API-Key失败：'+t,'error');return false;}return true;` |
| 2 | `portrait-dialog.js` | L392-393 | `throw new Error('请求失败('+status+')')` | `const t=await response.text();throw new Error(t)` |
| 3 | `portrait-dialog.js` | L522 | `if(!response.ok)return null` | `const t=await response.text();showToast('获取画像标题失败：'+t,'error');return null;` |
| 4 | `theme-loader.js` | L87 | `throw new Error('HTTP '+resp.status)` | `const t=await resp.text();throw new Error(t)` |
| 5 | `alpine-store.js` | L597 | `throw new Error('HTTP '+resp.status)` | `const t=await resp.text();throw new Error(t)` |
| 6 | `alpine-store.js` | L170 | 仅 `.catch()` | 加 `.then(r=>{if(!r.ok)return r.text().then(t=>{showToast('同步亮暗模式失败：'+t,'error');});})` |
| 7 | `alpine-store.js` | L199 | 仅 `.catch()` | 加 `.then(r=>{if(!r.ok)return r.text().then(t=>{showToast('同步主题选择失败：'+t,'error');});})` |

---

## 四、HTML 内联 `<script>`（4 处）

### [`frontend/signin/index.html`](frontend/signin/index.html)

| # | 函数 | 行 | 当前 | 改为 |
|---|------|----|------|------|
| 1 | `fetchCaptchaImage()` | L234-239 | `throw new Error('获取验证码失败')` | `const t=await resp.text();throw new Error(t||'获取验证码失败')` |
| 2 | `doCaptchaVerify()` | L340-368 | **已正确** | 不变 |
| 3 | `onLogin()` | L446-502 | **已正确** | 不变 |

### [`frontend/index.html`](frontend/index.html)

| # | 场景 | 行 | 当前 | 改为 |
|---|------|----|------|------|
| 4 | pageshow 检查 session | L16-22 | 失败则跳登录页 | 不变 |

---

## 五、后端清理（1 处）

| # | 文件 | 行 | 当前 | 改为 |
|---|------|----|------|------|
| 1 | `internal/theme/handler.go` | L29 | `{"error":"cannot read theme manifest"}` | `"cannot read theme manifest"` |
