package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v0.2 增量：agent_tabs collection
//   - name:        唯一 agent 标识（minimax/codex/...）
//   - label:       UI 显示名
//   - sort_order:  排序权重
//   - created_at:  标准时间戳
// 种子：5 个默认 agent，对应 sync.sh 默认 AGENT_DIRS
func init() {
	m.Register(func(app core.App) error {
		agentsCol := core.NewBaseCollection("agent_tabs")
		agentsCol.Fields.Add(
			&core.TextField{Name: "name", Required: true, Min: 1, Max: 64, Pattern: `^[a-zA-Z0-9_-]+$`},
			&core.TextField{Name: "label", Required: true, Max: 128},
			&core.NumberField{Name: "sort_order", Required: false},
			&core.AutodateField{Name: "created_at", OnCreate: true, OnUpdate: false},
		)
		// 单用户本地 → 所有 API rule 公开（无 auth）
		agentsCol.ListRule = nil
		agentsCol.ViewRule = nil
		agentsCol.CreateRule = nil
		agentsCol.UpdateRule = nil
		agentsCol.DeleteRule = nil

		if err := app.Save(agentsCol); err != nil {
			return err
		}

		// 种子 5 个默认 agent（按 sync.sh AGENT_DIRS 顺序）
		seeds := []struct {
			name  string
			label string
			order int
		}{
			{"minimax", "minimax", 10},
			{"codex", "codex", 20},
			{"claude", "claude", 30},
			{"cursor", "cursor", 40},
			{"factory", "factory", 50},
		}
		for _, s := range seeds {
			rec := core.NewRecord(agentsCol)
			rec.Set("name", s.name)
			rec.Set("label", s.label)
			rec.Set("sort_order", s.order)
			rec.Set("created_at", types.NowDateTime())
			if err := app.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		// rollback：删 collection
		col, err := app.FindCollectionByNameOrId("agent_tabs")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
