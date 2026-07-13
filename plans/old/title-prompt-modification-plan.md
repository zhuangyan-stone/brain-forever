# 标题生成提示词修改计划

## 修改内容

### 1. [`lang/zh-CN/system_prompt.toml`](../lang/zh-CN/system_prompt.toml:13) — `[title]` 段落

**当前内容：**
```
other = "你是一个标题生成助手。下面我会提供一段对话内容，其中 A 代表用户（提问方），B 代表 AI 助手（回答方）。请根据对话内容，生成一个简洁、准确的标题，长度严格控制在20个汉字以内。直接输出标题即可，不要多余的解释。"
```

**修改后：**
```
other = "你是一个标题生成助手。下面我会提供一段对话内容，其中 A 和 B 代表对话双方。请根据对话内容，生成一个简洁、准确的标题，长度严格控制在20个汉字以内。直接输出标题即可，不要多余的解释。"
```

### 2. [`lang/en/system_prompt.toml`](../lang/en/system_prompt.toml:13) — `[title]` 段落（同步修改）

**当前内容：**
```
other = "You are a title generation assistant. Below is a conversation where A represents the user (the questioner) and B represents the AI assistant (the responder). Based on the conversation content, generate a concise and accurate title. The title should be within 20 words. Output the title directly without any extra explanation."
```

**修改后：**
```
other = "You are a title generation assistant. Below is a conversation where A and B represent the two parties in the conversation. Based on the conversation content, generate a concise and accurate title. The title should be within 20 words. Output the title directly without any extra explanation."
```

## 执行步骤

1. 修改 `lang/zh-CN/system_prompt.toml` 第 14 行，将 `其中 A 代表用户（提问方），B 代表 AI 助手（回答方）` 替换为 `其中 A 和 B 代表对话双方`
2. 修改 `lang/en/system_prompt.toml` 第 14 行，将 `where A represents the user (the questioner) and B represents the AI assistant (the responder)` 替换为 `where A and B represent the two parties in the conversation`
