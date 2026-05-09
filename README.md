# BrainForever / 脑力永恒

> A loyal historian sits between you and the AI, quietly getting to know you — your personality, habits, interests, profession, thinking style, cultural taste, and even your food preferences — so the AI can serve you better over time.

What’s it like to have such a recorder? If you know Chinese history, you’ll know that only the emperor was entitled to have an "official diarist" — the Qijü Lang. So how to put it? It’s an honor, and also a tie. Either way, the information is recorded only on your computer. So we suggest you give it a try — and feel what it’s like to be the emperor.

> 一位忠诚的历史学家坐在你和AI之间，默默了解你——你的个性、习惯、兴趣、职业、思维风格、文化品味，甚至你的饮食偏好——以便AI随着时间推移更好地服务你。

拥有这样一个记录者的感觉是什么？如果你看中国的历史，你可能知道，只有皇帝才配得起有“起居郎”……所以，怎么说呢？是荣耀，也是牵绊。不管怎样，信息只记录在您的电脑上，所以我们建议你试试，这当皇帝的感觉！

## Why I Built This / 我为什么要建造这个

I started this project because I miss my father, who passed away over a decade ago. I can no longer talk to him. But I realized — one day, I too will be gone. And when that day comes, I want my children to be able to connect to a server, whenever they miss me, and have a conversation with an AI agent that carries the memories of half my lifetime.

This is BrainForever. The brain lives forever, and so love endures.

我开始这个项目是因为我想念十多年前去世的父亲。我再也无法和他说话了。但我意识到——总有一天，我也会离开。当那一天到来时，我希望我的孩子们能在想念我时连接服务器，与承载我半生记忆的AI代理对话。

这就是这个项目：脑力永恒 / BrainForever，脑力永恒，爱才会延续。

---

BrainForever is an AI chat companion that remembers who you are. Unlike ordinary chatbots that treat every conversation as a fresh start, BrainForever places a discreet "historian" between you and the LLM. As you chat naturally, this historian silently observes and builds a multi-dimensional profile of your character — your communication style, your values, your sense of humor, your expertise, your aesthetic preferences, and more. The more you talk, the better it understands you, and the more personalized and thoughtful the AI's responses become.

BrainForever 是一个记住你身份的 AI 聊天伙伴。与把每次对话当作新开始的普通聊天机器人不同，BrainForever在你和LLM之间安置了一个隐秘的“历史学家”。当你们自然交谈时，这位历史学家默默观察并构建了你性格的多维档案——你的沟通风格、价值观、幽默感、专业知识、审美偏好等等。你说得越多，它就越能理解你，AI的回应也越个性化、越用心。


## Why BrainForever? / 为什么选择 “脑力永恒”

Most AI chats are **memoryless** — each session starts from scratch, and the AI has no idea who you are or what you care about. BrainForever changes that.

大多数AI聊天都是无记忆的——每一次新的会话都从零开始，AI根本不知道你是谁，也不知道你关心什么。BrainForever 改变了这一点。

- **It learns you, not just your words** - It picks up on your personality traits, your decision-making patterns, your cultural references, and even your taste in food. Over time, it builds a **personal trait library** that captures who you truly are.
- **它学习的是你，而不仅仅是你的言语** - 它会捕捉你的性格特质、决策模式、文化参考，甚至你的饮食品味。随着时间推移，它会建立一个个人特质库，捕获你的真性情。

- **It gets better over time** - The more conversations you have, the richer your personal profile becomes, and the more the AI's responses feel like they're coming from someone who truly knows you.

- **你会越来越好，它也是** - 随着时光流转，与你对话越多，特质档案就越丰富，AI的回复也越让你觉得，在这个世界，你有个真正了解你的人（你会可惜它并不是真人），它你一起成长，你们一起变好。

- **It's subtle and natural** - You don't need to fill out forms or answer questionnaires. Just talk, and the historian does the rest.
- **这很微妙，但它是自然的** - 你不需要填写表格或回答问卷。只要说话，起居郞（也可以是起居娘，你愿意的话）会处理剩下的。

- **Your data stays yours** - Everything is stored locally — no cloud, no surveillance, no third-party profiling.Except for that day — when we must hand ourselves back to God, while wanting to leave a part of ourselves to the world. When that day comes, you may entrust your selected memories to www.[brain-online.com](www.brain-online.com),  giving your brain eternal life — keeping it online forever.
- **你的数据依然属于你自己** - 所有数据都存储在本地——没有云端，没有监控，没有第三方画像。除了那一天到来——我们要自己交还给上帝，同时想把自己留给这个世界——这时，你可以把经过你筛选的一些记忆交给 [brain-online.com](www.brain-online.com)，让你的大脑永生，让你的大脑永远在线。

## Platform-specific setup

**Windows:**
- Install [MinGW-w64](https://www.mingw-w64.org/) (e.g., via MSYS2) and ensure `gcc` is in your `PATH`.
- SQLite3 headers are bundled with `go-sqlite3`, no extra installation needed.

**Linux (Debian/Ubuntu):**
```bash
sudo apt update
sudo apt install -y gcc libsqlite3-dev
```

**macOS:**
```bash
# Xcode Command Line Tools includes gcc and SQLite3 headers
xcode-select --install
```

## Build and run

**Windows:**
```batch
b.bat
brain-forever.exe
```

**Linux/macOS:**
```bash
./b.sh
./brain-forever
```

## Open the frontend

Navigate to [http://localhost:8080](http://localhost:8080) in your browser.

## License

BrainForever is **dual-licensed** under the following terms:

- **Open Source**: Licensed under the [GNU Affero General Public License v3.0 (AGPL v3)](LICENSE) — for personal, non-commercial, and open-source use.
- **Commercial**: A commercial license is available for organizations that wish to use BrainForever in proprietary, closed-source environments without the obligations of the AGPL v3. See [COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md) for details.
