package hooks

import (
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// AgentInfo 描述一个 agent tab（从 agent_tabs collection 读出）
type AgentInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	SortOrder  int    `json:"sort_order"`
	CreatedAt  string `json:"created_at"`
}

// agentNameRe 严格 [a-zA-Z0-9_-]+，与 migration pattern 一致
var agentNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// reservedAgentNames 系统保留名，不能创建/删除
var reservedAgentNames = map[string]bool{
	"all": true, // UI "全部" tab，由前端固定渲染，不来自 DB
}

// agentTabsColName collection 名
const agentTabsColName = "agent_tabs"

// GET /api/agents
// 返回所有 agent（按 sort_order ASC, name ASC），自动 prepend "all" 给前端
func listAgents(c *core.RequestEvent) error {
	col, err := c.App.FindCollectionByNameOrId(agentTabsColName)
	if err != nil {
		return c.JSON(500, map[string]string{"error": "agent_tabs collection missing: " + err.Error()})
	}
	records, err := c.App.FindRecordsByFilter(col, "", "sort_order,id", 0, 0)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	agents := make([]AgentInfo, 0, len(records)+1)
	// 系统固定 tab "all"。
	// label 不叫「全部」：这个 tab 只列**已纳管**的 skill（中央源 + 各 agent 专属源），
	// agent 自带的那批未纳管 skill 平台看不见，列在主页底部的「未纳管」区块里。
	// 叫「全部」会让人以为它就是全部。
	agents = append(agents, AgentInfo{Name: "all", Label: "已纳管", SortOrder: 0})
	for _, r := range records {
		agents = append(agents, AgentInfo{
			ID:        r.Id,
			Name:      r.GetString("name"),
			Label:     r.GetString("label"),
			SortOrder: r.GetInt("sort_order"),
			CreatedAt: r.GetString("created_at"),
		})
	}
	return c.JSON(200, map[string]any{
		"agents": agents,
		"count":  len(agents),
	})
}

// POST /api/agents
// body: { "name": "...", "label": "...", "sort_order": 100 }
//   - name 必填，唯一，pattern [a-zA-Z0-9_-]+
//   - label 可省，默认同 name
//   - sort_order 可省，默认取 max + 10
func createAgent(c *core.RequestEvent) error {
	var body struct {
		Name      string `json:"name"`
		Label     string `json:"label"`
		SortOrder *int   `json:"sort_order"`
	}
	if err := c.BindBody(&body); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid body: " + err.Error()})
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		return c.JSON(400, map[string]string{"error": "name required"})
	}
	if reservedAgentNames[body.Name] {
		return c.JSON(400, map[string]string{"error": "name is reserved: " + body.Name})
	}
	if !agentNameRe.MatchString(body.Name) {
		return c.JSON(400, map[string]string{"error": "name must match [a-zA-Z0-9_-]+"})
	}
	if body.Label == "" {
		body.Label = body.Name
	}

	col, err := c.App.FindCollectionByNameOrId(agentTabsColName)
	if err != nil {
		return c.JSON(500, map[string]string{"error": "agent_tabs collection missing: " + err.Error()})
	}

	// 查重
	if existing, _ := c.App.FindFirstRecordByFilter(col, "name={:n}", map[string]any{"n": body.Name}); existing != nil {
		return c.JSON(409, map[string]string{"error": "agent already exists: " + body.Name})
	}

	// sort_order：未指定则 max + 10
	sortOrder := 0
	if body.SortOrder != nil {
		sortOrder = *body.SortOrder
	} else {
		// 用 records 数量粗算（也用 sql max 更准但简单点够用）
		existing, _ := c.App.FindRecordsByFilter(col, "", "-sort_order", 1, 0)
		if len(existing) > 0 {
			sortOrder = existing[0].GetInt("sort_order") + 10
		} else {
			sortOrder = 10
		}
	}

	rec := core.NewRecord(col)
	rec.Set("name", body.Name)
	rec.Set("label", body.Label)
	rec.Set("sort_order", sortOrder)
	rec.Set("created_at", types.NowDateTime())
	if err := c.App.Save(rec); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, AgentInfo{
		ID:        rec.Id,
		Name:      rec.GetString("name"),
		Label:     rec.GetString("label"),
		SortOrder: rec.GetInt("sort_order"),
		CreatedAt: rec.GetString("created_at"),
	})
}

// PATCH /api/agents/{name}
// body: { "label": "...", "sort_order": 100 } — 至少一项
// 注意：name 是路径参数，body 不能再改 name（避免混乱）
func updateAgent(c *core.RequestEvent) error {
	name := c.Request.PathValue("name")
	if name == "" {
		return c.JSON(400, map[string]string{"error": "name required"})
	}
	if reservedAgentNames[name] {
		return c.JSON(400, map[string]string{"error": "cannot modify reserved name: " + name})
	}
	var body struct {
		Label     *string `json:"label"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := c.BindBody(&body); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid body: " + err.Error()})
	}
	if body.Label == nil && body.SortOrder == nil {
		return c.JSON(400, map[string]string{"error": "label or sort_order required"})
	}

	col, err := c.App.FindCollectionByNameOrId(agentTabsColName)
	if err != nil {
		return c.JSON(500, map[string]string{"error": "agent_tabs collection missing: " + err.Error()})
	}
	rec, err := c.App.FindFirstRecordByFilter(col, "name={:n}", map[string]any{"n": name})
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found: " + name})
	}
	if body.Label != nil {
		rec.Set("label", *body.Label)
	}
	if body.SortOrder != nil {
		rec.Set("sort_order", *body.SortOrder)
	}
	if err := c.App.Save(rec); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, AgentInfo{
		ID:        rec.Id,
		Name:      rec.GetString("name"),
		Label:     rec.GetString("label"),
		SortOrder: rec.GetInt("sort_order"),
		CreatedAt: rec.GetString("created_at"),
	})
}

// DELETE /api/agents/{name}
// 只删 tab 标签，不动 /personal/<name>/ 目录（数据保留）
// "all" 不可删
func deleteAgent(c *core.RequestEvent) error {
	name := c.Request.PathValue("name")
	if name == "" {
		return c.JSON(400, map[string]string{"error": "name required"})
	}
	if reservedAgentNames[name] {
		return c.JSON(400, map[string]string{"error": "cannot delete reserved name: " + name})
	}
	col, err := c.App.FindCollectionByNameOrId(agentTabsColName)
	if err != nil {
		return c.JSON(500, map[string]string{"error": "agent_tabs collection missing: " + err.Error()})
	}
	rec, err := c.App.FindFirstRecordByFilter(col, "name={:n}", map[string]any{"n": name})
	if err != nil {
		return c.JSON(404, map[string]string{"error": "agent not found: " + name})
	}
	if err := c.App.Delete(rec); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, map[string]any{
		"deleted": name,
		"note":    "personal data under /personal/" + name + "/ preserved",
	})
}
