package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- 移动 skill 的测试 ----
//
// 重点不是"能不能搬动一个目录"（那是 os.Rename 的事），而是两件容易出事的地方：
// 1) locateSkill 找错源（中央源优先的顺序必须和 ListSkills 一致）
// 2) copyTree / 路径拼接有没有越界

// setupRoots 把中央源和专属源指到临时目录，并造几个 skill
func setupRoots(t *testing.T) (central, personal string) {
	t.Helper()
	root := t.TempDir()
	central = filepath.Join(root, "skills")
	personal = filepath.Join(root, "personal")
	t.Setenv("SKILLS_DIR", central)
	t.Setenv("SKILLS_PERSONAL_DIR", personal)

	mk := func(dir string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, body string) {
		mk(dir)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(central, "central-only"), "x")
	write(filepath.Join(central, "both-places"), "来自中央源")
	write(filepath.Join(personal, "codex", "personal-only"), "x")
	write(filepath.Join(personal, "codex", "both-places"), "来自专属库")
	return central, personal
}

func TestLocateSkill(t *testing.T) {
	setupRoots(t)

	cases := []struct {
		name      string
		wantPath  string // 相对判定，避免平台差异
		wantAgent string
	}{
		{"central-only", "central-only", "all"},
		{"personal-only", "personal-only", "codex"},
		// 同名时中央源优先——必须和 ListSkills 的去重顺序一致，
		// 否则"移动"会搬走一个界面上根本没显示的那个
		{"both-places", "both-places", "all"},
	}
	for _, c := range cases {
		path, agent, err := locateSkill(c.name)
		if err != nil {
			t.Errorf("locateSkill(%q) 报错: %v", c.name, err)
			continue
		}
		if agent != c.wantAgent {
			t.Errorf("locateSkill(%q) agent = %q, 期望 %q", c.name, agent, c.wantAgent)
		}
		if filepath.Base(path) != c.wantPath {
			t.Errorf("locateSkill(%q) path = %q", c.name, path)
		}
		// 中央源的应落在 skillsDir 下，专属源的应落在 personal/<agent> 下
		if c.wantAgent == "all" && filepath.Dir(path) != skillsDir() {
			t.Errorf("中央源 skill 路径应直接在 skillsDir 下，实际 %q", path)
		}
		if c.wantAgent != "all" && filepath.Base(filepath.Dir(path)) != c.wantAgent {
			t.Errorf("专属源 skill 应在 personal/%s/ 下，实际 %q", c.wantAgent, path)
		}
	}
}

func TestLocateSkillRejectsTraversal(t *testing.T) {
	setupRoots(t)

	for _, bad := range []string{
		"../escape",
		"..",
		"a/b",
		`a\b`,
		".hidden",
		"",
	} {
		if _, _, err := locateSkill(bad); err == nil {
			t.Errorf("locateSkill(%q) 应该报错，实际通过了", bad)
		}
	}
}

func TestLocateSkillNotFound(t *testing.T) {
	setupRoots(t)
	if _, _, err := locateSkill("nope"); err == nil {
		t.Error("不存在的 skill 应报错")
	}
}

func TestMoveDirRefusesToOverwriteNonEmptyTarget(t *testing.T) {
	setupRoots(t)

	// 先在目标位置造一个非空目录。os.Rename 到已存在的**非空**目录会 ENOTEMPTY 失败，
	// 不会静默覆盖 —— 这是底层就有的保护，不依赖 handler 里的 409 检查。
	dst := filepath.Join(personalDir(), "codex", "central-only")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("原有的"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(skillsDir(), "central-only")
	if err := moveDir(src, dst); err == nil {
		t.Error("目标已存在且非空时，moveDir 应报错而不是覆盖")
	}

	// 确认原有内容没被动过
	b, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "原有的" {
		t.Errorf("目标内容被改写了: %q", b)
	}
	// 源也该还在
	if _, err := os.Stat(src); err != nil {
		t.Errorf("源目录不该被删: %v", err)
	}
}

func TestCopyTreeCopiesNestedAndSymlinks(t *testing.T) {
	setupRoots(t)

	src := filepath.Join(skillsDir(), "central-only")
	// 加嵌套文件和软链，验证复制保结构
	if err := os.MkdirAll(filepath.Join(src, "refs", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "refs", "deep", "a.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../SKILL.md", filepath.Join(src, "refs", "link.md")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "copied")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md 没复制过来: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "refs", "deep", "a.md")); err != nil {
		t.Errorf("嵌套文件没复制过来: %v", err)
	}
	li, err := os.Lstat(filepath.Join(dst, "refs", "link.md"))
	if err != nil {
		t.Fatalf("软链没复制过来: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Error("软链被复制成了普通文件（应该保持软链）")
	}
	target, err := os.Readlink(filepath.Join(dst, "refs", "link.md"))
	if err != nil || target != "../SKILL.md" {
		t.Errorf("软链指向变了: %q (%v)", target, err)
	}
}

// 个人专属库那个"all"是界面 tab 名，不是真实 agent——不能拿它当移动目标
func TestMoveTargetAllFallsBackToCentral(t *testing.T) {
	setupRoots(t)
	for _, target := range []string{"all", ""} {
		p, err := SafePath("central-only")
		if err != nil {
			t.Fatal(err)
		}
		// 语义等价："" 和 "all" 都解析到中央源
		if !strings.HasPrefix(p, filepath.Clean(skillsDir())) {
			t.Errorf("target=%q 应解析到中央源，实际 %q", target, p)
		}
		if _, err := SafePersonalPath(target, "x"); err == nil {
			t.Errorf("SafePersonalPath(%q) 应报错（all 不是合法 agent）", target)
		}
	}
}

func TestSafePersonalPathRejectsBadAgent(t *testing.T) {
	setupRoots(t)
	for _, bad := range []string{"../etc", "a/b", "a\\b", ".hidden", "", "all"} {
		if _, err := SafePersonalPath(bad, "x"); err == nil {
			t.Errorf("SafePersonalPath(%q) 应该报错", bad)
		}
	}
	got, err := SafePersonalPath("codex", "my-skill")
	if err != nil {
		t.Fatalf("合法参数应通过: %v", err)
	}
	base := filepath.Clean(personalDir())
	if !strings.HasPrefix(filepath.Clean(got), base+string(os.PathSeparator)) {
		t.Errorf("结果 %q 不在专属源 %q 之下", got, base)
	}
}
