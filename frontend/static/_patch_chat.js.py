import re
with open('frontend/static/chat.js', 'r', encoding='utf-8') as f:
    content = f.read()
content = content.replace(
    "'use strict';",
    "'use strict';\n\n// 暴露图标常量给 Alpine 模板（x-html 引用）\nwindow.ICON_DELETE = ICON_DELETE;\nwindow.ICON_EDIT = ICON_EDIT;"
)
with open('frontend/static/chat.js', 'w', encoding='utf-8') as f:
    f.write(content)
print('Done')
