package hooks

import (
	"strings"
	"testing"
)

// 这些测试盯的是一件事：**从模型回复里取出来的 SKILL.md 必须是完整的**。
//
// 背景：早期用「第一个 ``` 到下一个 ```」取围栏内容，把正文内部的围栏
// 当成了外层围栏的结束标记——进化 tdd 时写出来 32 行、结尾停在
// 「## The Iron Law」，因为原文第 33 行正好是一个 ``` 围栏。skill 直接残废。
//
// 下面每一例都按真实形状构造，尤其是 skill 正文里带代码围栏的情形。

const tddFrontmatter = "---\nname: tdd\ndescription: Test-driven development.\n---\n"

// tddLikeBody 是一个"正文里带代码围栏"的 skill 正文。
// 这正是当年被截断的那一类：Iron Law 那段本身就是一个围栏。
const tddLikeBody = "\n# Test-Driven Development (TDD)\n\n## The Iron Law\n\n```\nNO PRODUCTION CODE WITHOUT A FAILING TEST FIRST\n```\n\nWrite code before the test? Delete it. Start over.\n"

func TestExtractSkillMD_DelimiterProtocol(t *testing.T) {
	out := "我做了这些改动：\n- 补了一节\n\n" +
		skillBeginMark + "\n" + tddFrontmatter + tddLikeBody + "\n" + skillEndMark + "\n\n补充说明。"

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if ex.Truncated {
		t.Fatal("不应该判定为截断")
	}
	if !strings.Contains(ex.Content, "NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST") {
		t.Errorf("正文里的代码围栏内容丢了：\n%s", ex.Content)
	}
	if !strings.Contains(ex.Content, "Write code before the test? Delete it. Start over.") {
		t.Errorf("正文结尾丢了：\n%s", ex.Content)
	}
	if !looksLikeSkillMD(ex.Content) {
		t.Error("取出来的内容应该带 YAML frontmatter")
	}
}

// 回归：正文里有代码围栏时，内容不能被从中间切开。
func TestExtractSkillMD_DelimiterKeepsInnerFences(t *testing.T) {
	out := skillBeginMark + "\n" + tddFrontmatter + tddLikeBody + skillEndMark

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if ex.Truncated {
		t.Fatal("不应该判定为截断")
	}
	if !strings.Contains(ex.Content, "NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST") {
		t.Errorf("正文里的围栏内容被切掉了：\n%s", ex.Content)
	}
	if !strings.Contains(ex.Content, "Write code before the test? Delete it. Start over.") {
		t.Errorf("正文尾巴被切掉了：\n%s", ex.Content)
	}
	// 围栏本身必须原样保留——它是正文的一部分
	if strings.Count(ex.Content, "```") != 2 {
		t.Errorf("正文里的围栏应该原样保留 2 个，实际 %d 个：\n%s",
			strings.Count(ex.Content, "```"), ex.Content)
	}
}

// 回归：模型没听指令、仍然用 ```markdown 包着，正文里又有围栏。
// 早先的实现在这里会把内容截断到第一个内层围栏。
func TestExtractSkillMD_LegacyFenceKeepsInnerFences(t *testing.T) {
	out := "改动说明：\n- 补了一节\n\n```markdown\n" +
		tddFrontmatter + tddLikeBody + "```\n"

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if ex.Truncated {
		t.Fatal("不应该判定为截断")
	}
	if !strings.Contains(ex.Content, "NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST") {
		t.Errorf("正文里的围栏内容被切掉了：\n%s", ex.Content)
	}
	if !strings.Contains(ex.Content, "Write code before the test? Delete it. Start over.") {
		t.Errorf("正文尾巴被切掉了：\n%s", ex.Content)
	}
}

// 模型有时会多写一个闭合标记，末尾不能留下孤零零的围栏。
func TestExtractSkillMD_LegacyFenceDropsExtraClosingFence(t *testing.T) {
	out := "说明\n\n```markdown\n" + tddFrontmatter + tddLikeBody + "```\n```\n\n**说明**：只改了一个文件。\n"

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if !strings.HasSuffix(ex.Content, "Write code before the test? Delete it. Start over.") {
		t.Errorf("结尾应该是正文最后一句，实际：\n%s", ex.Content)
	}
	if strings.HasSuffix(strings.TrimSpace(ex.Content), "```") {
		t.Errorf("结尾不该留下多余的闭合围栏：\n%s", ex.Content)
	}
}

// 有开头没结尾 = 输出被截断，必须报错而不是写半截文件。
func TestExtractSkillMD_TruncatedDelimiter(t *testing.T) {
	out := "改动说明\n\n" + skillBeginMark + "\n" + tddFrontmatter + tddLikeBody

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能识别出（截断的）内容")
	}
	if !ex.Truncated {
		t.Fatal("应该判定为截断")
	}
	if _, err := prepareSkillMD(out, "tdd"); err == nil {
		t.Fatal("截断的输出必须拒绝写入")
	} else if !strings.Contains(err.Error(), "截断") {
		t.Errorf("错误信息要说明是截断：%v", err)
	}
}

// 只有"改动说明"、没有 SKILL.md —— 这种绝不能写盘，会直接毁掉 skill。
func TestPrepareSkillMD_RefusesExplanationOnly(t *testing.T) {
	out := "我做了这些改动：\n- 把描述改得更具体了\n- 加了一节反例\n"

	if _, err := prepareSkillMD(out, "tdd"); err == nil {
		t.Fatal("只有说明文字时必须拒绝写入")
	} else if !strings.Contains(err.Error(), "已跳过写入") {
		t.Errorf("错误信息要说明没有写入：%v", err)
	}
}

// 模型把 name 写错了 —— 目录名是用户定的，frontmatter 必须跟它对齐。
func TestPrepareSkillMD_NormalizesName(t *testing.T) {
	out := skillBeginMark + "\n" +
		"---\nname: 老名字\ndescription: d\n---\n\n# 正文\n" +
		skillEndMark

	got, err := prepareSkillMD(out, "新名字")
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if n := frontmatterField(got, "name"); n != "新名字" {
		t.Errorf("name 应该是 %q，实际 %q", "新名字", n)
	}
	if !strings.Contains(got, "# 正文") {
		t.Errorf("正文丢了：\n%s", got)
	}
}

// 分隔标记里又套了一层代码围栏 —— 也要能取出来。
func TestExtractSkillMD_DelimiterWithNestedFence(t *testing.T) {
	out := skillBeginMark + "\n```markdown\n" + tddFrontmatter + tddLikeBody + "```\n" + skillEndMark

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if !strings.Contains(ex.Content, "NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST") {
		t.Errorf("正文里的围栏内容被切掉了：\n%s", ex.Content)
	}
}

// 整段回复就是 SKILL.md（模型没加任何围栏）。
func TestExtractSkillMD_RawSkill(t *testing.T) {
	out := tddFrontmatter + tddLikeBody

	ex, ok := extractSkillMD(out)
	if !ok {
		t.Fatal("应该能取出 SKILL.md")
	}
	if ex.Truncated {
		t.Fatal("不应该判定为截断")
	}
	if !strings.Contains(ex.Content, "NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST") {
		t.Errorf("正文丢了：\n%s", ex.Content)
	}
}

func TestStripSkillMarks(t *testing.T) {
	in := "说明\n\n" + skillBeginMark + "\n正文\n" + skillEndMark + "\n"
	got := stripSkillMarks(in)
	if strings.Contains(got, skillBeginMark) || strings.Contains(got, skillEndMark) {
		t.Errorf("分隔标记应该被去掉：\n%s", got)
	}
	if !strings.Contains(got, "正文") {
		t.Errorf("正文不该被删：\n%s", got)
	}
}

func TestFenceOpenAndClose(t *testing.T) {
	if _, _, ok := fenceOpen("```"); !ok {
		t.Error("``` 应该是围栏行")
	}
	if _, _, ok := fenceOpen("``"); ok {
		t.Error("两个反引号不是围栏")
	}
	if _, _, ok := fenceOpen("普通文本"); ok {
		t.Error("普通文本不是围栏")
	}
	if _, info, _ := fenceOpen("```markdown"); info != "markdown" {
		t.Errorf("语言标记应该是 markdown，实际 %q", info)
	}
	if !isFenceClose("```", 3) {
		t.Error("裸 ``` 应该能闭合三个反引号的围栏")
	}
	if isFenceClose("```js", 3) {
		t.Error("带语言标记的行不能当闭合标记")
	}
	if isFenceClose("```", 4) {
		t.Error("三个反引号闭合不了四个反引号开的围栏")
	}
}
