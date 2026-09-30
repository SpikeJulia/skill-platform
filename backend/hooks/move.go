package hooks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// moveSkill POST /api/skills/move — 在中央源和各 agent 专属库之间搬动 skill
//
// 为什么需要：skill 装上之后经常要改归属——某个 skill 只对某个 agent 有意义时
// 该从中央源挪进它的专属库；反过来共用的该挪回中央源。平台原来只有装和删，
// 中间这一步只能手敲 mv。
//
// 目标用 target_agent 表达："" 或 "all" = 中央源（所有 agent 都能用），
// 其他值 = 该 agent 的专属库（只有它能用）。
func moveSkill(c *core.RequestEvent) error {
	var body struct {
		Name        string `json:"name"`
		TargetAgent string `json:"target_agent"`
	}
	if err := c.BindBody(&body); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid body: " + err.Error()})
	}

	name := strings.TrimSpace(body.Name)
	target := strings.TrimSpace(body.TargetAgent)

	// 源：中央源优先（与 ListSkills 的去重顺序一致，否则会出现
	// "移走的其实是另一个同名 skill"）
	srcPath, srcAgent, err := locateSkill(name)
	if err != nil {
		return c.JSON(404, map[string]string{"error": err.Error()})
	}

	// 目标路径：中央源走 SafePath，专属源走 SafePersonalPath（它会校验 agent 名
	// 合法、拒绝 "all"、并确认拼完仍在 personalDir 之下）
	var dstPath, dstAgent string
	if target == "" || target == "all" {
		if dstPath, err = SafePath(name); err != nil {
			return c.JSON(400, map[string]string{"error": err.Error()})
		}
		dstAgent = "all"
	} else {
		if dstPath, err = SafePersonalPath(target, name); err != nil {
			return c.JSON(400, map[string]string{"error": err.Error()})
		}
		dstAgent = target
	}

	// 已经在目标位置 → 不做任何事，也不报错（界面重复提交不该失败）
	if filepath.Clean(srcPath) == filepath.Clean(dstPath) {
		return c.JSON(200, map[string]any{
			"moved":        false,
			"unchanged":    true,
			"from":         srcPath,
			"to":           dstPath,
			"target_agent": dstAgent,
		})
	}

	// 目标已存在 → 拒绝。不做"合并"或"覆盖"，那是另一个破坏性操作，
	// 不该藏在一次"移动"后面。
	if _, err := os.Stat(dstPath); err == nil {
		return c.JSON(409, map[string]string{
			"error": fmt.Sprintf("目标位置已存在同名 skill：%s", dstPath),
		})
	}

	if err := moveDir(srcPath, dstPath); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"moved":        true,
		"from":         srcPath,
		"to":           dstPath,
		"from_agent":   srcAgent,
		"target_agent": dstAgent,
		"note":         "run 'bash ~/AI/agent-skills/sync.sh' on host to rebuild agent symlinks",
	})
}

// locateSkill 找出 skill 现在实际待在哪儿。
// 返回 (绝对路径, 所属 agent)；中央源返回 "all"。找不到返回错误。
func locateSkill(name string) (string, string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") {
		return "", "", fmt.Errorf("invalid skill name: %q", name)
	}

	// 先中央源——和 ListSkills 的去重优先级保持一致
	central, err := SafePath(name)
	if err != nil {
		return "", "", err
	}
	if st, err := os.Stat(central); err == nil && st.IsDir() {
		return central, "all", nil
	}

	// 再扫专属源。这里不走 SafePersonalPath（还不知道是哪个 agent），
	// 所以自己校验：agent 名必须合法、name 不含分隔符、拼完仍在 personalDir 之下。
	base := filepath.Clean(personalDir())
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", "", fmt.Errorf("skill %q 不在中央源，专属源也不可读: %w", name, err)
	}
	for _, ae := range entries {
		if !ae.IsDir() || strings.HasPrefix(ae.Name(), ".") {
			continue
		}
		full := filepath.Clean(filepath.Join(base, ae.Name(), name))
		if !strings.HasPrefix(full, base+string(os.PathSeparator)) {
			continue
		}
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			return full, ae.Name(), nil
		}
	}
	return "", "", fmt.Errorf("找不到 skill: %q", name)
}

// moveDir 移动整个目录。
//
// /skills 和 /personal 是两个独立的 bind mount，**可能不在同一个文件系统上**，
// 这时 os.Rename 会报 EXDEV（跨设备链接）。所以先试 rename，失败再退回
// 复制 + 删除。
func moveDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return fmt.Errorf("移动失败: %w", err)
	}

	// 跨文件系统：先复制到临时名，成功后再删源。
	// 用临时名是为了——复制中途失败时不会留下一个"看起来移成功了"的半个目录。
	tmp := dst + ".moving"
	_ = os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("跨文件系统移动失败（复制阶段）: %w", err)
	}
	if err := os.RemoveAll(src); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("跨文件系统移动失败（删除源目录）: %w", err)
	}
	return os.Rename(tmp, dst)
}

func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, os.ErrInvalid) || strings.Contains(le.Err.Error(), "cross-device") ||
			strings.Contains(le.Err.Error(), "invalid cross-device link")
	}
	return false
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		// 软链原样重建，不跟着指向的目标复制
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return nil // 跳过 socket/设备文件等
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
