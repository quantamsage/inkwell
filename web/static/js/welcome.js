// Content of the first document created on an empty database.

export const WELCOME_TITLE = 'Welcome to Inkwell';

export const WELCOME_CONTENT = `# Welcome to Inkwell

Inkwell is a calm place to write **Markdown**. Type on the left and the
preview on the right updates as you go. Everything is saved automatically.

## Formatting

You can write *italic*, **bold**, ~~strikethrough~~ and \`inline code\`.
Select some text and use the toolbar, or press **Ctrl+B**, **Ctrl+I** and
**Ctrl+K** (use **⌘** on macOS).

> Blockquotes are great for callouts and citations.

### Lists

- Bulleted lists
  - can be nested — press Tab to indent
- Press Enter to continue a list

1. Numbered lists
2. renumber themselves

- [x] Task lists
- [ ] with checkboxes

### Code

\`\`\`go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Inkwell!")
}
\`\`\`

### Tables

| Feature        | Shortcut              |
| :------------- | :-------------------- |
| Save           | Ctrl+S                |
| Toggle preview | Ctrl+Shift+P          |
| Focus mode     | Ctrl+Shift+F          |

---

Links like [the CommonMark spec](https://commonmark.org) and images work too.
Use **Export** to download Markdown, HTML or a PDF, and **Import** to open
\`.md\` files. Press **Ctrl+/** for all keyboard shortcuts.
`;
