# Coding Style Preferences

- Strongly dislikes fallback / 兜底 / 降级 code. Wants the correct business logic only, with no fuzzy compatibility shims ("不要兜底","不要降级，旧的错误的不在使用的只做清理"). Confidence: 0.9
- Prefers removing obsolete or incorrect code and keeping a single clean implementation instead of adding compatibility layers ("旧数据不做兼容了，没用的代码也可以清理掉"). Confidence: 0.9
- Wants root-cause investigation and a plan/design reviewed BEFORE writing code; frequently says "先排查/先了解/先不急着改代码/先查看不写". Confidence: 0.9
- When removing/replacing a feature, wants the old code actually deleted, and confirms the cleanup reached the source rather than just being filtered/hidden. Confidence: 0.8
- Wants side effects and regressions reasoned about before applying a fix ("先推演一遍这样修会不会出现新的问题"). Confidence: 0.75
- Avoids adding redundant code when fixing; dislikes over-engineering and wants minimal, clean changes. Confidence: 0.75
- Prefers splitting code across files/modules rather than cramming too much into one file. Confidence: 0.6
- Wants explicit permission before making changes they did not authorise ("没有我的允许，你不要改"). Confidence: 0.7
- Expects the agent to fully understand the real domain/spec at field level before implementing — reading the upstream official API docs and, when those are unreachable, an actual working reference implementation — and to fill the largest hole properly ("你需要完全了解这两类生图模型，做的好一点") instead of shipping a minimal shim. Confidence: 0.7
- When new capability support is added (e.g. video generation), expects the full upstream parameter surface to be wired through client and server, not a bare-bones request body — asks "生成视频没有什么参数吗？" and wants the missing upstream options exposed in the UI and sent in the request. Confidence: 0.7
- Treats smuggling internal parameters through text as a defect: flags generation options being concatenated into the prompt string ("为什么参数会混淆到提示词里面了") and prefers such values carried as explicit structured metadata fields rather than embedded in user-visible message/prompt content (where they leak into stored history and copies). When offered the proper field-based fix vs keeping the text-param workaround, unambiguously picks the field-based one ("改成第一条吧") even though it touches the full client→relay→CLI chain. Confidence: 0.7
