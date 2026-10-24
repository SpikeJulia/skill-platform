package hooks

import (
	"fmt"
	"log"
	"strings"
)

// —— 从模型回复里取出 SKILL.md 正文 ——
//
// 这块为什么要单独一个文件、还写得这么细：
//
// SKILL.md 正文里几乎必然含有代码围栏（哪怕只是一句 ASCII 标语）。
// 早先的取法是「找到第一个 ```，再找下一个 ```」——把正文内部的围栏
// 当成了外层围栏的结束标记，内容在半路被截断，写盘后 skill 直接残废。
// 实测：进化 tdd 时写出来 32 行、结尾停在「## The Iron Law」，
// 因为原文第 33 行正好是一个 ``` 围栏。
//
// CommonMark 也救不了：围栏不能嵌套，开启围栏用三个反引号时，正文里的
// 三个反引号就是合法的闭合标记。所以这里不指望"解析得更聪明"，而是换协议：
// 让模型用专用分隔标记输出，再用代码围栏兜底（兜底走"不配对"的贪心取法）。

const (
	// skillBeginMark / skillEndMark 是模型输出 SKILL.md 的专用分隔标记。
	// 正文里不会出现这串字符，所以不会跟内容打架。
	skillBeginMark = "<<<SKILL_MD>>>"
	skillEndMark   = "<<<END_SKILL_MD>>>"
)

// skillOutputFormat 是"输出 SKILL.md"的格式约定，合并和进化共用。
//
// 用专用分隔标记而不是通用代码围栏，是因为 SKILL.md 正文里几乎必然有围栏，
// 通用围栏包一层会在半路被截断（见本文件顶部注释）。
const skillOutputFormat = `输出格式（严格遵守）：

1. 先用不超过 5 行列出你做了哪些改动（普通 Markdown 文本，不要放进分隔标记之间）
2. 然后原样输出下面这两个分隔标记，两者之间是**完整的 SKILL.md 全文**：

<<<SKILL_MD>>>
---
name: <skill 名>
description: <一句话描述>
---

<SKILL.md 正文>
<<<END_SKILL_MD>>>

硬性要求：
- SKILL.md 正文**不要**再套代码围栏。分隔标记已经划定了边界；正文里如果需要代码块，照常写就行。
- 必须是完整的一份：frontmatter + 全文。任何一段都不能省略，不能用「（略）」「其余同上」代替。
- <<<END_SKILL_MD>>> 必须出现。没有它就等于没写完。`

// skillExtract 是一次"从模型回复里取 SKILL.md"的结果。
type skillExtract struct {
	Content   string // SKILL.md 正文（不含分隔标记）
	Source    string // delimiter / fence / raw —— 取自哪儿，出错时要说清楚
	Truncated bool   // 输出被截断（标记或围栏没闭合）
}

// codeBlock 是从模型回复里扫出来的一个代码块。
type codeBlock struct {
	lang    string // 围栏的语言标记（```markdown 的 markdown）
	content string // 围栏内部的内容
	closed  bool   // 围栏有没有正常闭合
}

// extractSkillMD 从模型回复里取出 SKILL.md 正文。
//
// 优先级：
//  1. 专用分隔标记 <<<SKILL_MD>>> … <<<END_SKILL_MD>>>（首选，不会被正文内容干扰）
//  2. 整段回复本身就是 SKILL.md（开头就是 YAML frontmatter）
//  3. 代码围栏（模型没听分隔标记的指令时的兜底）
//
// 都取不到时返回 ok=false——宁可报错，也不能把模型的"改动说明"当成 skill
// 内容写进去，那会直接毁掉一个 skill。
func extractSkillMD(s string) (skillExtract, bool) {
	// 1. 专用分隔标记
	if i := strings.Index(s, skillBeginMark); i >= 0 {
		body := s[i+len(skillBeginMark):]
		ex := skillExtract{Source: "分隔标记"}
		var content string
		if j := strings.Index(body, skillEndMark); j >= 0 {
			content = strings.TrimSpace(body[:j])
		} else {
			// 有开头没结尾 = 输出被截断
			content = strings.TrimSpace(body)
			ex.Truncated = true
		}
		if looksLikeSkillMD(content) {
			ex.Content = content
			return ex, true
		}
		// 模型可能又在分隔标记里面套了一层代码围栏
		if inner, ok := legacyFenceExtract(content); ok {
			inner.Source = "分隔标记内的代码围栏"
			inner.Truncated = inner.Truncated || ex.Truncated
			return inner, true
		}
		ex.Content = content
		return ex, true
	}

	// 2. 整段回复就是 SKILL.md
	if looksLikeSkillMD(s) {
		return skillExtract{Content: strings.TrimSpace(s), Source: "整段回复"}, true
	}

	// 3. 代码围栏兜底
	return legacyFenceExtract(s)
}

// legacyFenceExtract 在模型只用了代码围栏（没听分隔标记的指令）时取出 SKILL.md。
//
// 关键：**不做围栏配对**。围栏不能嵌套，外层三个反引号时，正文里的三个
// 反引号就是合法的闭合标记，配对一定会把正文从中间切开。办法是：
//
//	起点 = "内容以 YAML frontmatter 开头"的那个围栏
//	终点 = 回复里**最后一个**裸围栏行（模型的外层闭合围栏本来就在最后）
//
// 中间整段都是 SKILL.md 正文，正文里的围栏原样保留。
func legacyFenceExtract(s string) (skillExtract, bool) {
	lines := strings.Split(s, "\n")

	// 找起点：围栏后面（允许空行）紧跟 frontmatter 起始 ---
	start := -1
	for i := 0; i < len(lines); i++ {
		if _, _, ok := fenceOpen(lines[i]); !ok {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j < len(lines) && strings.TrimSpace(lines[j]) == "---" {
			start = i
			break
		}
	}
	if start < 0 {
		return skillExtract{}, false
	}

	// 找终点：最后一个裸围栏行
	end := -1
	for i := len(lines) - 1; i > start; i-- {
		if _, info, ok := fenceOpen(lines[i]); ok && info == "" {
			end = i
			break
		}
	}
	if end < 0 {
		// 没有闭合围栏 = 输出被截断
		return skillExtract{
			Source:    "代码围栏",
			Truncated: true,
			Content:   strings.TrimSpace(strings.Join(lines[start+1:], "\n")),
		}, true
	}

	content := strings.TrimSpace(trimTrailingBareFences(strings.Join(lines[start+1:end], "\n")))
	return skillExtract{Content: content, Source: "代码围栏"}, true
}

// prepareSkillMD 从模型回复里取出**可以写盘**的 SKILL.md。
//
// 返回错误时磁盘**没有**被碰过——调用方只要把错误原样回给用户即可。
// expectedName 是这次操作的目标 skill 名（进化=原名，合并=新名），
// frontmatter 里的 name 会按它对齐：目录名是用户定的，name 字段必须跟它一致，
// 否则注册的时候会出现"目录叫 A、frontmatter 叫 B"的错位。
func prepareSkillMD(result, expectedName string) (string, error) {
	ex, ok := extractSkillMD(result)
	if !ok {
		return "", fmt.Errorf("模型没有返回完整的 SKILL.md：找不到 %s 分隔标记，内容也不是带 YAML frontmatter 的 SKILL.md。已跳过写入，文件未改动。模型输出：%s",
			skillBeginMark, truncate(result, 400))
	}
	if ex.Truncated {
		return "", fmt.Errorf("模型的输出被截断了（%s 没有正常闭合），写进去会得到一份残缺的 SKILL.md。已跳过写入，文件未改动。请重试，或调大「最大输出 Token」后重试。",
			ex.Source)
	}
	if !looksLikeSkillMD(ex.Content) {
		return "", fmt.Errorf("模型返回的内容不像 SKILL.md（缺少 YAML frontmatter 里的 name / description）。已跳过写入，文件未改动。模型输出：%s",
			truncate(result, 400))
	}
	if n := frontmatterField(ex.Content, "name"); n != "" && n != expectedName {
		log.Printf("[agent] frontmatter name=%q 与目标 %q 不一致，已按目标名对齐", n, expectedName)
	}
	return setFrontmatterName(ex.Content, expectedName), nil
}

// extractCodeBlocks 扫出所有**顶层**代码块（按 CommonMark 配对）。
//
// 只用于"看看回复里有哪些代码块"这类辅助判断；真正取 SKILL.md 用的是
// legacyFenceExtract——它不配对，所以能保住正文里的围栏。
func extractCodeBlocks(s string) []codeBlock {
	lines := strings.Split(s, "\n")
	var blocks []codeBlock
	for i := 0; i < len(lines); i++ {
		ticks, lang, ok := fenceOpen(lines[i])
		if !ok {
			continue
		}
		var body []string
		j := i + 1
		closed := false
		for ; j < len(lines); j++ {
			if isFenceClose(lines[j], ticks) {
				closed = true
				break
			}
			body = append(body, lines[j])
		}
		blocks = append(blocks, codeBlock{
			lang:    lang,
			content: strings.Join(body, "\n"),
			closed:  closed,
		})
		if closed {
			i = j // 从闭合行之后继续扫
		} else {
			break // 没闭合，后面不可能再有完整块
		}
	}
	return blocks
}

// fenceOpen 判断一行是不是围栏行，返回反引号数量和后面的信息串
// （```markdown 的 markdown；裸围栏返回空串）。
func fenceOpen(line string) (int, string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "`") {
		return 0, "", false
	}
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	if n < 3 {
		return 0, "", false
	}
	return n, strings.TrimSpace(t[n:]), true
}

// isFenceClose 判断一行能不能作为围栏的闭合标记：
// 反引号数不少于开启时的数量，且后面不能有别的字符。
func isFenceClose(line string, minTicks int) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "`") {
		return false
	}
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	return n >= minTicks && strings.TrimSpace(t[n:]) == ""
}

// trimTrailingBareFences 去掉内容末尾多余的裸围栏行。
// 模型有时会多写一个闭合标记（```markdown … ``` ```），留着会在文件末尾
// 留一个孤零零的围栏，把后面的 Markdown 吞成代码块。
func trimTrailingBareFences(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" {
			lines = lines[:len(lines)-1]
			continue
		}
		if _, info, ok := fenceOpen(last); ok && info == "" {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return strings.Join(lines, "\n")
}

// frontmatterField 取 SKILL.md 的 YAML frontmatter 里某个字段的值。
func frontmatterField(s, field string) string {
	front, _, ok := splitFrontmatter(s)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, field+":") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		return strings.Trim(v, `"'`)
	}
	return ""
}

// setFrontmatterName 把 frontmatter 里的 name 改成指定值。
func setFrontmatterName(s, name string) string {
	front, body, ok := splitFrontmatter(s)
	if !ok {
		return s
	}
	lines := strings.Split(front, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "name:") {
			lines[i] = "name: " + name
			break
		}
	}
	return "---" + strings.Join(lines, "\n") + "---" + body
}

// splitFrontmatter 拆出 YAML frontmatter 和正文。
// 形如 "---\nname: x\ndescription: y\n---\n\n# 正文"。
func splitFrontmatter(s string) (front, body string, ok bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "---") {
		return "", "", false
	}
	rest := t[3:]
	idx := strings.Index(rest, "---")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+3:], true
}

// stripSkillMarks 去掉回复里的专用分隔标记，让界面显示出来的内容干净。
func stripSkillMarks(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == skillBeginMark || t == skillEndMark {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
