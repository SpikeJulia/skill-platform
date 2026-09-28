package hooks

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"
)

// listSkills GET /api/skills - 列出所有 skill
func listSkills(c *core.RequestEvent) error {
	skills, err := ListSkills()
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, map[string]any{
		"skills": skills,
		"count":  len(skills),
	})
}

// getSkill GET /api/skills/{name} - 单个 skill 详情（含 SKILL.md 全文）
func getSkill(c *core.RequestEvent) error {
	name := c.Request.PathValue("name")
	content, err := ReadSkillMD(name)
	if err != nil {
		return c.JSON(404, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, map[string]any{
		"name":      name,
		"content":   content,
		"path":      filepath.Join(skillsDir(), name),
		"hasSkillMD": true,
	})
}

// deleteSkill DELETE /api/skills/{name} - 卸载 skill
// 注意：只删除中央源的实际目录，5 个 agent 的软链由 sync.sh 自动清理
func deleteSkill(c *core.RequestEvent) error {
	name := c.Request.PathValue("name")
	path, err := SafePath(name)
	if err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if err := os.RemoveAll(path); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, map[string]any{
		"deleted": name,
		"path":    path,
		"note":    "run 'bash ~/AI/agent-skills/sync.sh' on host to clean agent symlinks",
	})
}

// 用于检查文件存在
var _ = fmt.Sprintf
