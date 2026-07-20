# [excerpt] 系统提示词设计计划

## 1. 背景与目标

当前 [`lang/zh-CN/system_prompt.toml`](../lang/zh-CN/system_prompt.toml:565) 中的 `[excerpt]` 节定义了 AI 用于从对话中摘录用户"闪亮片段"的系统提示词。该提示词用于后台任务，定期扫描对话中的用户消息，提取值得收录的精彩语句并打上分类标签。

经分析，当前版本存在**两个设计缺陷**需要修复：

---

## 2. 缺陷一："仅从用户消息摘录"原则不明确

### 2.1 当前现状

当前提示词中虽然有一些提及"用户消息"的表述：

> "你是一位有品味的阅读者，并专注负责**从用户消息中捕捉**'闪亮片段'的记录者。你将审阅完整对话中的**每一条用户消息**……"

但存在以下模糊/矛盾之处：

1. **`index` 字段的描述存在歧义：**

   > "其中的 'index' ，表示该摘录来自 chat 中的第2条消息（记数从 0 开始，且**包含助手消息**）"

   `index` 计数包含助手消息，但摘录内容本身是否可能来自助手消息？当前描述没有明确禁止。

2. **缺少"助手消息仅用于辅助理解"的明确陈述：**

   当前 prompt 完全没有提及"助手消息的角色是什么"。AI 可能误以为助手消息中的精彩表达也可作为摘录来源。

3. **`context_summary` 的来源模糊：**

   `context_summary` 描述的是"上下文摘要"，但它应该**基于什么信息生成**？是仅基于用户消息，还是可结合助手消息来理解上下文？当前没有说明。

### 2.2 参考对照

[`[traits]`](../lang/zh-CN/system_prompt.toml:133) 节对此有明确的约束表述，可作为设计参考：

> "分析主要依据是用户的消息内容。AI 的回复**仅用于帮助理解**用户所说的内容的真实含义……**绝不能**将 AI 的回复误认为用户的能力。"

### 2.3 需要设计的规则

| 规则项 | 说明 |
|--------|------|
| **摘录源限定** | 摘录的原文（`excerpt_text`）必须且只能来自**用户消息**。助手消息中的任何内容不得作为摘录原文。 |
| **助手消息的角色** | 助手消息仅用于：①帮助理解用户消息的真实含义和上下文；②辅助生成 `context_summary`。 |
| **消息编号方式** | 详见下文第 4 节"消息显式编号方案"。 |
| **引用/转述的判定** | 如果助手消息纠正了用户的错误认知（如用户说"李白是非洲人"，助手纠正了），摘录不应包含用户的错误表述。 |

---

## 3. 缺陷二：`context_summary` 生成规则过于模糊

### 3.1 当前现状

当前对 `context_summary` 的唯一描述：

> "`context_summary` 控制在 20-40 字，用于未来引用时自然地引出该摘录。"

### 3.2 存在的问题

1. **没有定义"上下文"的范围**：是仅包含摘录语句所在的单条消息上下文？还是包含前后多条消息的对话语境？
2. **没有定义内容要素**：`context_summary` 应该包含哪些信息要素？（时间、话题、情绪、触发场景等）
3. **没有定义风格约束**：应该用陈述句还是描述句？第一人称还是第三人称？
4. **没有定义禁止项**：哪些内容不应该出现在 `context_summary` 中？
5. **`context_summary` 与 `reason` 的区别不清晰**：当前 `reason` 是"为什么这段话值得摘录"，`context_summary` 是"上下文摘要"，但两者边界模糊。示例中：

   ```json
   {
     "context_summary": "用户正在吐槽改PPT改到崩溃——都做到第18页了，领导说前面都没问题、就第一页改改",
     "reason": "独特的比喻，既有见解又有文采。"
   }
   ```

   `context_summary` 实际上描述了具体场景，而 `reason` 描述了收录理由，界限还算清晰。但需要固化这个设计。

### 3.3 需要设计的规则

| 规则项 | 说明 |
|--------|------|
| **内容要素** | `context_summary` 应包含：①话题/场景（用户在谈论什么）；②摘录语句的关键触发点（什么语境下说出了这句话）。 |
| **风格约束** | 应使用简洁的陈述句/描述句，第三人称（"用户……"），控制在 20-40 字。 |
| **与 `reason` 的分工** | `context_summary` = "在什么场景下说的"；`reason` = "为什么值得收录"。两者不应重叠。 |
| **禁止项** | ①不应直接评价用户（如"用户说了一句很有哲理的话"）；②不应包含模型自身的判断或分析；③不应透露 `reason` 中的内容。 |
| **上下文范围** | 应基于摘录语句所在的整条用户消息 + 前后各 1-2 条消息（含助手消息）来理解场景，但摘要本身只描述"用户当时的场景"。 |

---

## 4. 核心设计方案：消息显式编号

### 4.1 问题背景

LLM 本质上不擅长精确"数位置"。让 LLM 通过心算消息在列表中的序号来确定 `index`，无论用哪种计数方式（0-based、1-based、包含/排除 system prompt）都不可靠。

### 4.2 解决方案：使用数据库 ID 作为显式编号标签

在发送给 LLM 的消息内容前，加上 `[N]` 编号标签，N 直接使用数据库 [`chat_messages.id`](../internal/store/messages.go:8)（自增主键），让 LLM **直接读取**编号而非心算位置。

#### 消息格式

```
[99]  做PPT像是在拼乐高，我都快搭完第18层了，领导才说最底那块得换换……
[110] 哈哈这个比喻太形象了，不过领导的指示确实让人崩溃。
[114] 今天又被领导批评了……
```

关键决策：

| 决策项 | 最终方案 | 理由 |
|--------|---------|------|
| **编号来源** | 直接使用 `store.Message.ID`（数据库主键） | 唯一、无需额外生成 |
| **编号示例** | 非连续整数（99, 110, 114...） | 避免误导 LLM 认为编号总是连续递增（ID 全局自增，用户删除消息会产生间隔） |
| **角色前缀** | 不添加 `用户:` / `助手:` | 消息 API 的 `role` 字段已区分角色，内容中无需重复 |
| **system prompt** | 不编号 | system prompt 没有被摘录的可能 |

#### LLM 输出

LLM 在输出中通过 `msg_id` 字段引用消息编号：

```json
{
  "excerpt_text": "做PPT像是在拼乐高，我都快搭完第18层了，领导才说最底那块得换换……",
  "value_types": ["vent", "literary"],
  "context_summary": "用户吐槽改PPT时领导临时推翻基础框架",
  "reason": "用拼乐高比喻改PPT，生动形象且富有画面感",
  "msg_id": 99
}
```

#### 后端处理

`msg_id` 本身就是数据库消息 ID，可以直接用于存储或消息查询。**无需额外映射步骤**。

#### 优点

1. **LLM 无需计数**：编号直接显示在消息文本中，LLM 只需"看见并读取"
2. **天然防错**：即使 LLM 跳过某条消息不处理，编号仍然准确
3. **零映射成本**：`msg_id` = 数据库 ID，后端直接使用
4. **对消息截断友好**：即使助手消息被截断（如 [`buildTraitsLLMMessages`](../internal/agent/on_traits.go:315) 的做法），编号不受影响

---

## 5. 实施步骤

### 步骤 1：新增消息构建函数

**涉及文件**：新建或修改 `internal/agent/` 下的代码

创建一个类似 [`buildTraitsLLMMessages`](../internal/agent/on_traits.go:299) 但专用于 excerpt 的消息构建函数：

```go
func buildExcerptLLMMessages(title string, dbMessages []store.Message, lang string) []llm.Message {
    systemContent := getExcerptSystemPrompt(lang, title)
    
    llmMsgs := make([]llm.Message, 0, 1+len(dbMessages))
    llmMsgs = append(llmMsgs, llm.Message{
        Role:    llm.RoleSystem,
        Content: systemContent,
    })
    
    for _, m := range dbMessages {
        role := llm.RoleUser
        if m.Role == 1 {
            role = llm.RoleAssistant
        }
        
        content := m.Content
        // 助手消息可以截断（保留头尾各 500 字）
        if role == llm.RoleAssistant {
            runes := []rune(content)
            if len(runes) > 1024 {
                content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
            }
        }
        
        // 使用数据库 ID 作为编号
        numberedContent := fmt.Sprintf("[%d] %s", m.ID, content)
        
        llmMsgs = append(llmMsgs, llm.Message{
            Role:    role,
            Content: numberedContent,
        })
    }
    
    return llmMsgs
}
```

关键区别：使用 `m.ID` 而非 `i+1` 作为编号。

### 步骤 2：修改系统提示词 `[excerpt]` 节 ✅（已完成）

**涉及文件**：[`lang/zh-CN/system_prompt.toml`](../lang/zh-CN/system_prompt.toml:565) + [`lang/en/system_prompt.toml`](../lang/en/system_prompt.toml:529)

**已完成修改**：

1. **新增"消息格式"小节**，说明 `[N]` 编号标签的格式和使用方式
2. **`index` → `msg_id`**，新增 `excerpt_text` 字段
3. **全部 15 个示例**已更新为非连续编号
4. **注意事项**增加 `msg_id` 和 `excerpt_text` 说明
5. **编号描述**：`"编号从 1 开始，顺次递增"` → `"编号为 1 到消息总数之间的整数，系统已确保编号唯一"`

**待完成（如果需要）**：
- 新增"摘录来源限定"小节（明确助手消息仅用于辅助理解）
- 扩展 `context_summary` 的生成规则
- 优化"原文优先"原则

### 步骤 3：实现后台任务框架

**涉及文件**：[`internal/tasks/excerpt_job.go`](../internal/tasks/excerpt_job.go)（当前为空）

参照 [`internal/tasks/traits_job.go`](../internal/tasks/traits_job.go) 的模式实现：
- 注册周期性摘录任务（`RegisterPeriodicExcerptJob`）
- 从配置读取间隔、启用开关等参数
- 查询待处理的对话（可复用 traits 的 `queryPendingChats` 或新建）
- 调用消息构建函数 → 调用 LLM → 解析结果 → 持久化存储

### 步骤 4：数据库表设计（如有需要）

如果需要持久化存储摘录结果，设计新表：

```sql
CREATE TABLE chat_excerpts (
    id              BIGSERIAL PRIMARY KEY,
    chat_id         BIGINT NOT NULL REFERENCES chat_sessions(id),
    user_id         BIGINT NOT NULL REFERENCES users(id),
    msg_id          BIGINT NOT NULL REFERENCES chat_messages(id),
    excerpt_text    TEXT NOT NULL,
    value_types     TEXT[] NOT NULL,      -- 标签数组，如 {insight,literary}
    context_summary TEXT NOT NULL,
    reason          TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

> 注意：`msg_id` 直接使用 LLM 返回的 `msg_id`，无需映射。

---

## 6. 流程图

```mermaid
flowchart TD
    A[构建消息列表\n每条前加 [ID] 标签] --> B[发送给 LLM]
    B --> C{LLM 审阅每条用户消息}
    C -->|用户消息 [N]| D[评估是否符合收录标准]
    C -->|助手消息 [N]| E[仅用于理解上下文]
    E --> F[辅助生成 context_summary]
    D --> G{是否值得收录?}
    G -->|是| H[提取原文 excerpt_text]
    G -->|否| I[跳过]
    H --> J[读取 msg_id=[N]\nN 即为数据库消息 ID]
    H --> K[打分类标签 value_types]
    H --> L[生成 context_summary]
    H --> M[给出收录理由 reason]
    J --> N[输出 JSON]
    K --> N
    L --> N
    M --> N
    N --> O[后端直接使用 msg_id\n作为 chat_messages.id 持久化]
```

---

## 7. 验收标准

1. **原则明确**：提示词中清晰说明了"摘录原文只能来自用户消息"，没有歧义
2. **消息编号正确**：每条对话消息前有 `[N]` 格式的显式编号，N 为数据库 `chat_messages.id`
3. **`msg_id` 字段**：LLM 输出使用 `msg_id` 而非 `index`，直接填写消息前的编号
4. **`context_summary` 规则完整**：包含内容要素、风格约束、与 `reason` 的分工、禁止项
5. **示例更新**：输出格式示例体现新规则，编号使用非连续整数
6. **中英文同步**：`zh-CN` 和 `en` 版本内容一致
7. **不破坏现有功能**：`[excerpt]` 已有的收录标准、标签体系、注意事项等保持不变
8. **零映射**：`msg_id` 直接作为数据库 ID 使用，无需额外转换

---

## 8. 相关参考

- 当前 [`[excerpt]` 节完整内容](../lang/zh-CN/system_prompt.toml:565-709)
- [`[traits]` 节中关于"AI 消息仅用于辅助理解"的约束写法](../lang/zh-CN/system_prompt.toml:146-147)（作为设计参考）
- [`buildTraitsLLMMessages`](../internal/agent/on_traits.go:299)（消息构建函数的参考实现）
- [`internal/tasks/excerpt_job.go`](../internal/tasks/excerpt_job.go)（后期待实现的后台任务代码文件，当前为空）
- [`internal/tasks/traits_job.go`](../internal/tasks/traits_job.go)（特征提取后台任务的参考实现）
- [`store.Message`](../internal/store/messages.go:8)（数据库消息结构体，`ID` 字段即编号来源）
- [`llm.Message`](../infra/llm/client.go:124)（LLM 消息结构体）
