# UI Preferences

- Wants UI/design proposals shown as an HTML mockup, served via a local Python server and opened in the browser, BEFORE implementing in Flutter; explicitly rejects screenshots ("先用html给我看看","不要用截图/图片"). Confidence: 0.9
- Mockups must match the user's existing Flutter design language, not invent new styles ("按照我当前的flutter已有的设计啊，不要写新的"). Confidence: 0.85
- UI copy must be user-facing Chinese, and must NOT leak business/code logic, internal types, task IDs, status enums, or implementation details ("不要把业务逻辑和代码逻辑当文案"). Confidence: 0.9
- Prefers custom-styled dropdowns/components over native widgets for a consistent, good-looking look. Confidence: 0.75
- Prefers light themes (accepts light blue); explicitly dislikes dark themes and blue-purple color schemes. Confidence: 0.6
- Wants responsive layouts (adaptive to mobile and PC/web). Confidence: 0.75
- Dislikes UI overflow/occlusion; wants consistent component heights, alignment, and unified icon styling (icons redrawn in the primary color, same style). Confidence: 0.65
