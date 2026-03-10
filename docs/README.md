# 文档说明

`docs/` 下的设计文档按以下层级组织：

- `draft.md`：最初的人类想法，用来表达问题、目标和总体方向。
- `cdr/`：Conceptual Design Report，根据 `draft.md` 写出的概念设计，用来收敛关键工作流、术语和边界。
- `tdr/`：Technical Design Report，根据 `cdr/` 写出的技术设计，用来把概念设计继续下沉为可实现、可评审的实现方案。
- `adr/`：Architecture Decision Record，用来记录已经确认的关键架构决策。

推荐阅读顺序：

1. 先读 `draft.md`
2. 再读 `cdr/`
3. 最后读 `tdr/` 和相关 `adr/`
