package hooks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// SkillInfo 描述一个 skill
type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	HasSkillMD  bool     `json:"hasSkillMD"`
	// Agents 标识这个 skill 属于哪些 agent
	//   - ["all"] 表示中央源（所有 agent 都有）
	//   - ["minimax", "codex"] 等表示专属
	Agents      []string `json:"agents"`
}

// UnmanagedInfo 描述一个「存在但平台管不到」的 skill。
// 由宿主 sync.sh 扫描 ~/.<agent>/skills/ 与 ~/.<agent>/.builtin-skills/ 生成。
type UnmanagedInfo struct {
	Agent       string `json:"agent"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // "local" 可纳管 / "builtin" agent 自带
	Path        string `json:"path"`
	Description string `json:"description"`
}

// unmanagedFile 清单文件路径：放在专属源里（该目录已挂载进容器），
// 文件名以 . 开头，所以 ListSkills 扫专属源时不会把它当成一个 skill 目录。
func unmanagedFile() string {
	return filepath.Join(personalDir(), ".unmanaged.json")
}

// ListUnmanaged 读取宿主 sync.sh 生成的未纳管清单。
//
// 清单不存在是正常状态（sync.sh 还没跑过），返回空列表而不是报错——
// 这只是个可见性补充，不该成为 /api/skills 之外的故障源。
func ListUnmanaged() ([]UnmanagedInfo, error) {
	data, err := os.ReadFile(unmanagedFile())
	if err != nil {
		if os.IsNotExist(err) {
			return []UnmanagedInfo{}, nil
		}
		return nil, err
	}
	var items []UnmanagedInfo
	if err := json.Unmarshal(data, &items); err != nil {
		// 清单是 sync.sh 的输出，写坏了不该让整个接口挂掉
		fmt.Fprintf(os.Stderr, "warn: unmanaged manifest malformed: %v\n", err)
		return []UnmanagedInfo{}, nil
	}
	if items == nil {
		return []UnmanagedInfo{}, nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Agent != items[j].Agent {
			return items[i].Agent < items[j].Agent
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// skillsDir 优先用 SKILLS_DIR 环境变量（宿主机直接跑时覆盖）
// 容器里默认 /skills，宿主机 dev 用 ~/AI/agent-skills
func skillsDir() string {
	if v := os.Getenv("SKILLS_DIR"); v != "" {
		return v
	}
	return SkillsDir
}

// personalDir 优先用 SKILLS_PERSONAL_DIR 环境变量（宿主机直接跑时覆盖）
// 容器里默认 /personal，宿主机 dev 用 ~/AI/agent-skills-personal
// 路径下结构：<agent>/<name>/SKILL.md
func personalDir() string {
	if v := os.Getenv("SKILLS_PERSONAL_DIR"); v != "" {
		return v
	}
	return "/personal"
}

// SafePath 把用户提供的 name 安全地拼到 skillsDir 下，并验证不越界
// 拒绝包含 "..", "/", 绝对路径等危险字符
func SafePath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid skill name: %q", name)
	}
	full := filepath.Join(skillsDir(), name)
	// 双重检查：clean 后必须以 skillsDir 开头
	cleaned := filepath.Clean(full)
	if !strings.HasPrefix(cleaned, filepath.Clean(skillsDir())) {
		return "", fmt.Errorf("path traversal attempt: %q", name)
	}
	return cleaned, nil
}

// ListSkills 扫描 SkillsDir（中央源）+ personalDir（专属源）下所有子目录
// 返回每个 skill 标记 agents 字段：
//   - 中央源 skill: agents=["all"]
//   - 专属源 skill: agents=["<agent_name>"]
// 相同 name 出现时，中央源优先（避免重复）
func ListSkills() ([]SkillInfo, error) {
	byName := make(map[string]SkillInfo)

	// 1) 中央源
	if entries, err := os.ReadDir(skillsDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info := scanSkillDir(filepath.Join(skillsDir(), e.Name()))
			info.Agents = []string{"all"}
			byName[info.Name] = info
		}
	} else {
		// 中央源不存在不算错（个人专属模式也可以）
		fmt.Fprintf(os.Stderr, "warn: skills dir not accessible: %v\n", err)
	}

	// 2) 专属源：/personal/<agent>/<name>/
	if entries, err := os.ReadDir(personalDir()); err == nil {
		for _, agentEntry := range entries {
			if !agentEntry.IsDir() || strings.HasPrefix(agentEntry.Name(), ".") {
				continue
			}
			agentName := agentEntry.Name()
			agentPath := filepath.Join(personalDir(), agentName)
			skillEntries, err := os.ReadDir(agentPath)
			if err != nil {
				continue
			}
			for _, se := range skillEntries {
				if !se.IsDir() || strings.HasPrefix(se.Name(), ".") {
					continue
				}
				name := se.Name()
				// 中央源已有同名 skill → 跳过（中央源优先）
				if _, exists := byName[name]; exists {
					continue
				}
				info := scanSkillDir(filepath.Join(agentPath, name))
				info.Agents = []string{agentName}
				byName[name] = info
			}
		}
	} else {
		// 专属源不存在不算错（首次跑或仅中央源模式）
		fmt.Fprintf(os.Stderr, "warn: personal dir not accessible: %v\n", err)
	}

	// 转换 map → slice
	// 必须排序：Go 的 map 迭代顺序是随机的，直接转出来每次调用顺序都不同，
	// 会让「用户没在 localStorage 里固定过顺序」的 skill（例如刚装的新 skill）
	// 每次刷新都跳到随机位置。
	skills := make([]SkillInfo, 0, len(byName))
	for _, s := range byName {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// scanSkillDir 读一个 skill 目录的 SKILL.md frontmatter
func scanSkillDir(path string) SkillInfo {
	info := SkillInfo{
		Name: filepath.Base(path),
		Path: path,
	}
	skillMDPath := filepath.Join(path, "SKILL.md")
	if data, err := os.ReadFile(skillMDPath); err == nil {
		info.HasSkillMD = true
		info.Description = extractDescription(string(data))
	}
	return info
}

// extractDescription 从 SKILL.md 提取 YAML frontmatter 的 description 字段
// 支持两种格式：
//   description: simple one line
//   description: |
//     multi-line block scalar (| 或 >)
func extractDescription(content string) string {
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontmatter := false
	var inBlockScalar bool
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}
		lower := strings.ToLower(trimmed)
		if inBlockScalar {
			// block scalar 内的所有行（缩进 ≥ 2 空格的）都算 description
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				continue
			}
			// 缩进减少或换段 → block scalar 结束
			inBlockScalar = false
		}
		if strings.HasPrefix(lower, "description:") {
			rest := strings.TrimSpace(trimmed[len("description:"):])
			if rest == "|" || rest == ">" || rest == "|-" || rest == ">-" {
				// block scalar：返回接下来的内容（缩进行拼成一行）
				inBlockScalar = true
				// 继续读下一轮，把 block scalar 内容拼出来
				var block strings.Builder
				for scanner.Scan() {
					bline := scanner.Text()
					btrim := strings.TrimSpace(bline)
					if btrim == "---" {
						break
					}
					if len(bline) == 0 {
						continue
					}
					if line2 := bline; len(line2) > 0 && line2[0] != ' ' && line2[0] != '\t' {
						// 缩进减少，说明 description 块结束
						break
					}
					if block.Len() > 0 {
						block.WriteString(" ")
					}
					block.WriteString(btrim)
				}
				return block.String()
			}
			// 普通单行
			return rest
		}
	}
	return ""
}

// ReadSkillMD 读指定 skill 的 SKILL.md
func ReadSkillMD(name string) (string, error) {
	path, err := SafePath(name)
	if err != nil {
		return "", err
	}
	skillMD := filepath.Join(path, "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}
	return string(data), nil
}

// safeBashCmds 是允许 bash 工具执行的命令白名单（前缀匹配）
// 防止 agent 误删 / 误改系统
var safeBashCmds = []string{
	"git clone", "git pull", "git fetch",
	"ls", "find", "grep", "cat", "head", "tail", "wc",
	"cp", "mv", "mkdir",
	"bash ", "sh ", // 允许跑 sync.sh 等脚本
	"echo", "test", "[", // 条件测试
}

// SafeBash 在白名单内的命令才能执行，限制工作目录到 SkillsDir
func SafeBash(cmd string) (string, error) {
	trimmed := strings.TrimSpace(cmd)
	if !hasSafePrefix(trimmed) {
		return "", fmt.Errorf("bash command not in whitelist: %q", cmd)
	}
	// 危险字符检查
	if strings.Contains(cmd, "rm -rf /") || strings.Contains(cmd, "rm -rf ~") ||
		strings.Contains(cmd, "sudo") || strings.Contains(cmd, "> /") {
		return "", fmt.Errorf("dangerous pattern in bash: %q", cmd)
	}
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

func hasSafePrefix(cmd string) bool {
	for _, p := range safeBashCmds {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

// SafePersonalPath 把 agent + skill 名安全地拼到 personalDir 下，并验证不越界。
// 装到某个 agent 的专属库时用这个，不能复用 SafePath（它只认中央源）。
func SafePersonalPath(agent, name string) (string, error) {
	// "all" 是 UI 的保留 tab 名，不是一个真实 agent
	if agent == "" || !agentNameRe.MatchString(agent) || agent == "all" {
		return "", fmt.Errorf("invalid agent name: %q", agent)
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid skill name: %q", name)
	}
	base := filepath.Clean(personalDir())
	full := filepath.Join(base, agent, name)
	if !strings.HasPrefix(filepath.Clean(full), base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal attempt: agent=%q name=%q", agent, name)
	}
	return full, nil
}

// WriteFile 在中央源下写文件（受 SafePath 保护）
func WriteFile(name string, filename string, content string) error {
	dir, err := SafePath(name)
	if err != nil {
		return err
	}
	return writeInto(dir, filename, content)
}

// WriteFileToPersonal 写到某个 agent 的专属库（受 SafePersonalPath 保护）
func WriteFileToPersonal(agent, name, filename, content string) error {
	dir, err := SafePersonalPath(agent, name)
	if err != nil {
		return err
	}
	return writeInto(dir, filename, content)
}

func writeInto(dir, filename, content string) error {
	// filename 不能包含路径分隔符（只能写文件到 skill 目录顶层）
	if filename == "" || strings.ContainsAny(filename, "/\\") {
		return fmt.Errorf("filename must not contain path separator: %q", filename)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
}
